package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/config"
)

var ErrOutboundPayloadTooLarge = errors.New("outbound payload too large")

type OutboundDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type OutboundConfig struct {
	ConnectTimeout  time.Duration
	WriteTimeout    time.Duration
	ReadTimeout     time.Duration
	IdleConnTimeout time.Duration
	MaxFrameSize    int
	Dialer          OutboundDialer
}

type OutboundStats struct {
	Dials           uint64
	DialErrors      uint64
	Sends           uint64
	SendErrors      uint64
	BytesSent       uint64
	Responses       uint64
	ResponseErrors  uint64
	ResponseBytes   uint64
	ActiveSends     uint64
	ClosedAfterSend uint64
	ActiveConns     uint64
	PoolHits        uint64
	PoolMisses      uint64
	Reconnects      uint64
	IdleEvictions   uint64
}

type OutboundSender interface {
	Exchange(ctx context.Context, target config.Target, payload []byte) ([]byte, error)
	Stats() OutboundStats
	Close() error
}

type pooledConn struct {
	target     config.Target
	conn       net.Conn
	hadConn    bool
	lastUsedAt time.Time
	mu         sync.Mutex
}

type OutboundProxy struct {
	dialer          OutboundDialer
	connectTimeout  time.Duration
	writeTimeout    time.Duration
	readTimeout     time.Duration
	idleConnTimeout time.Duration
	maxFrameSize    int
	now             func() time.Time

	poolMu sync.Mutex
	pool   map[string]*pooledConn
	closed bool

	dials           atomic.Uint64
	dialErrors      atomic.Uint64
	sends           atomic.Uint64
	sendErrors      atomic.Uint64
	bytesSent       atomic.Uint64
	responses       atomic.Uint64
	responseErrors  atomic.Uint64
	responseBytes   atomic.Uint64
	activeSends     atomic.Uint64
	closedAfterSend atomic.Uint64
	poolHits        atomic.Uint64
	poolMisses      atomic.Uint64
	reconnects      atomic.Uint64
	idleEvictions   atomic.Uint64
}

func NewOutboundProxy(cfg OutboundConfig) *OutboundProxy {
	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 3 * time.Second
	}
	writeTimeout := cfg.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = 5 * time.Second
	}
	readTimeout := cfg.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 250 * time.Millisecond
	}
	idleConnTimeout := cfg.IdleConnTimeout
	if idleConnTimeout <= 0 {
		idleConnTimeout = 90 * time.Second
	}
	maxFrameSize := cfg.MaxFrameSize
	if maxFrameSize <= 0 {
		maxFrameSize = 8 << 20
	}
	dialer := cfg.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: connectTimeout}
	}
	return &OutboundProxy{
		dialer:          dialer,
		connectTimeout:  connectTimeout,
		writeTimeout:    writeTimeout,
		readTimeout:     readTimeout,
		idleConnTimeout: idleConnTimeout,
		maxFrameSize:    maxFrameSize,
		now:             time.Now,
		pool:            make(map[string]*pooledConn),
	}
}

func (o *OutboundProxy) Exchange(ctx context.Context, target config.Target, payload []byte) ([]byte, error) {
	if len(payload) > o.maxFrameSize {
		o.sendErrors.Add(1)
		return nil, fmt.Errorf("%w: %d > %d", ErrOutboundPayloadTooLarge, len(payload), o.maxFrameSize)
	}

	o.evictIdleConnections()

	pc, err := o.getOrCreatePooledConn(target)
	if err != nil {
		return nil, err
	}

	o.activeSends.Add(1)
	defer o.activeSends.Add(^uint64(0))

	pc.mu.Lock()
	defer pc.mu.Unlock()
	defer func() {
		pc.lastUsedAt = o.now()
	}()

	conn, err := o.ensureConn(ctx, pc)
	if err != nil {
		return nil, err
	}

	frame := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)

	if o.writeTimeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(o.writeTimeout))
	}
	if err := writeAll(conn, frame); err != nil {
		o.sendErrors.Add(1)
		o.closeConnLocked(pc)

		conn, err = o.ensureConn(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("retry connect after write failure: %w", err)
		}
		if o.writeTimeout > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(o.writeTimeout))
		}
		if err := writeAll(conn, frame); err != nil {
			o.sendErrors.Add(1)
			o.closeConnLocked(pc)
			return nil, fmt.Errorf("send frame after reconnect: %w", err)
		}
	}

	o.sends.Add(1)
	o.bytesSent.Add(uint64(len(frame)))

	if o.readTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(o.readTimeout))
	}
	resp, err := readLenFrame(conn, o.maxFrameSize)
	if err != nil {
		if isNoResponseReadErr(err) {
			if isConnClosedNoResponseErr(err) {
				o.closeConnLocked(pc)
			}
			return nil, nil
		}
		o.responseErrors.Add(1)
		o.closeConnLocked(pc)
		return nil, fmt.Errorf("read response: %w", err)
	}
	o.responses.Add(1)
	o.responseBytes.Add(uint64(len(resp)))
	return resp, nil
}

func (o *OutboundProxy) Stats() OutboundStats {
	return OutboundStats{
		Dials:           o.dials.Load(),
		DialErrors:      o.dialErrors.Load(),
		Sends:           o.sends.Load(),
		SendErrors:      o.sendErrors.Load(),
		BytesSent:       o.bytesSent.Load(),
		Responses:       o.responses.Load(),
		ResponseErrors:  o.responseErrors.Load(),
		ResponseBytes:   o.responseBytes.Load(),
		ActiveSends:     o.activeSends.Load(),
		ClosedAfterSend: o.closedAfterSend.Load(),
		ActiveConns:     o.countActiveConns(),
		PoolHits:        o.poolHits.Load(),
		PoolMisses:      o.poolMisses.Load(),
		Reconnects:      o.reconnects.Load(),
		IdleEvictions:   o.idleEvictions.Load(),
	}
}

func (o *OutboundProxy) Close() error {
	o.poolMu.Lock()
	if o.closed {
		o.poolMu.Unlock()
		return nil
	}
	o.closed = true
	pcs := make([]*pooledConn, 0, len(o.pool))
	for _, pc := range o.pool {
		pcs = append(pcs, pc)
	}
	o.pool = map[string]*pooledConn{}
	o.poolMu.Unlock()

	for _, pc := range pcs {
		pc.mu.Lock()
		o.closeConnLocked(pc)
		pc.mu.Unlock()
	}
	return nil
}

func (o *OutboundProxy) getOrCreatePooledConn(target config.Target) (*pooledConn, error) {
	key := targetKey(target)
	o.poolMu.Lock()
	defer o.poolMu.Unlock()
	if o.closed {
		return nil, fmt.Errorf("outbound is closed")
	}
	if pc, ok := o.pool[key]; ok {
		o.poolHits.Add(1)
		return pc, nil
	}
	o.poolMisses.Add(1)
	pc := &pooledConn{target: target}
	o.pool[key] = pc
	return pc, nil
}

func (o *OutboundProxy) ensureConn(ctx context.Context, pc *pooledConn) (net.Conn, error) {
	if pc.conn != nil {
		return pc.conn, nil
	}
	if pc.hadConn {
		o.reconnects.Add(1)
	}
	conn, err := o.dialTarget(ctx, pc.target)
	if err != nil {
		return nil, err
	}
	pc.conn = conn
	pc.hadConn = true
	pc.lastUsedAt = o.now()
	return conn, nil
}

func (o *OutboundProxy) dialTarget(ctx context.Context, target config.Target) (net.Conn, error) {
	addr := net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port))
	o.dials.Add(1)

	dialCtx := ctx
	var cancel context.CancelFunc
	if o.connectTimeout > 0 {
		dialCtx, cancel = context.WithTimeout(ctx, o.connectTimeout)
	}
	if cancel != nil {
		defer cancel()
	}

	conn, err := o.dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		o.dialErrors.Add(1)
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

func (o *OutboundProxy) closeConnLocked(pc *pooledConn) {
	if pc.conn == nil {
		return
	}
	_ = pc.conn.Close()
	pc.conn = nil
	o.closedAfterSend.Add(1)
}

func (o *OutboundProxy) evictIdleConnections() {
	if o.idleConnTimeout <= 0 {
		return
	}

	type poolEntry struct {
		key string
		pc  *pooledConn
	}
	o.poolMu.Lock()
	if o.closed {
		o.poolMu.Unlock()
		return
	}
	entries := make([]poolEntry, 0, len(o.pool))
	for key, pc := range o.pool {
		entries = append(entries, poolEntry{key: key, pc: pc})
	}
	o.poolMu.Unlock()

	now := o.now()
	cutoff := now.Add(-o.idleConnTimeout)
	for _, entry := range entries {
		pc := entry.pc
		remove := false

		pc.mu.Lock()
		if !pc.lastUsedAt.IsZero() && pc.lastUsedAt.Before(cutoff) {
			if pc.conn != nil {
				o.closeConnLocked(pc)
				o.idleEvictions.Add(1)
			}
			remove = pc.conn == nil
		}
		pc.mu.Unlock()

		if remove {
			o.poolMu.Lock()
			if existing, ok := o.pool[entry.key]; ok && existing == pc {
				delete(o.pool, entry.key)
			}
			o.poolMu.Unlock()
		}
	}
}

func (o *OutboundProxy) countActiveConns() uint64 {
	o.poolMu.Lock()
	pcs := make([]*pooledConn, 0, len(o.pool))
	for _, pc := range o.pool {
		pcs = append(pcs, pc)
	}
	o.poolMu.Unlock()

	var active uint64
	for _, pc := range pcs {
		pc.mu.Lock()
		if pc.conn != nil {
			active++
		}
		pc.mu.Unlock()
	}
	return active
}

func targetKey(t config.Target) string {
	return net.JoinHostPort(t.Host, fmt.Sprintf("%d", t.Port))
}

func writeAll(conn net.Conn, buf []byte) error {
	for len(buf) > 0 {
		n, err := conn.Write(buf)
		if err != nil {
			return err
		}
		buf = buf[n:]
	}
	return nil
}

func readLenFrame(conn net.Conn, maxFrameSize int) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.LittleEndian.Uint32(hdr[:]))
	if n < 0 || n > maxFrameSize {
		return nil, fmt.Errorf("bad response frame length: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func isNoResponseReadErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}

func isConnClosedNoResponseErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
}
