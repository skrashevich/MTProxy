package proxy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/protocol"
)

var ErrConnectionLimitReached = errors.New("connection limit reached")
var ErrDHAcceptRateExceeded = errors.New("dh accept rate exceeded")

type DataPlaneStats struct {
	ActiveSessions         uint64
	SessionLimit           int
	SessionsCreated        uint64
	SessionsClosed         uint64
	PacketsTotal           uint64
	PacketsEncrypted       uint64
	PacketsHandshake       uint64
	PacketsDropped         uint64
	PacketsParseErrors     uint64
	PacketsRouteErrors     uint64
	PacketsRejectedByLimit uint64
	PacketsRejectedByDH    uint64
	PacketsOutboundErrors  uint64
	BytesTotal             uint64
}

type dataPlaneSession struct {
	session    *protocol.Session
	lastSeenAt time.Time
	packets    uint64
}

type DataPlane struct {
	runtime *Runtime

	maxConnections int
	now            func() time.Time
	dhRateLimiter  *fixedWindowRateLimiter

	sessionMu sync.RWMutex
	sessions  map[int64]*dataPlaneSession

	sessionsCreated        atomic.Uint64
	sessionsClosed         atomic.Uint64
	packetsTotal           atomic.Uint64
	packetsEncrypted       atomic.Uint64
	packetsHandshake       atomic.Uint64
	packetsDropped         atomic.Uint64
	packetsParseErrors     atomic.Uint64
	packetsRouteErrors     atomic.Uint64
	packetsRejectedByLimit atomic.Uint64
	packetsRejectedByDH    atomic.Uint64
	packetsOutboundErrors  atomic.Uint64
	bytesTotal             atomic.Uint64
}

func NewDataPlane(rt *Runtime, maxConnections int, maxDHAcceptRate int) *DataPlane {
	if maxConnections < 0 {
		maxConnections = 0
	}
	return &DataPlane{
		runtime:        rt,
		maxConnections: maxConnections,
		now:            func() time.Time { return time.Now().UTC() },
		dhRateLimiter:  newFixedWindowRateLimiter(maxDHAcceptRate),
		sessions:       make(map[int64]*dataPlaneSession),
	}
}

func (d *DataPlane) HandlePacket(connID int64, targetDC int, frame []byte) (protocol.PacketInfo, ForwardDecision, error) {
	info, decision, _, err := d.HandlePacketWithResponse(connID, targetDC, frame)
	return info, decision, err
}

func (d *DataPlane) HandlePacketWithResponse(connID int64, targetDC int, frame []byte) (protocol.PacketInfo, ForwardDecision, []byte, error) {
	d.packetsTotal.Add(1)
	d.bytesTotal.Add(uint64(len(frame)))

	info, err := protocol.ParseMTProtoPacket(frame)
	if err != nil {
		d.packetsParseErrors.Add(1)
		d.packetsDropped.Add(1)
		return protocol.PacketInfo{}, ForwardDecision{}, nil, err
	}

	now := d.now()
	if info.Kind == protocol.PacketKindDHHandshake && !d.dhRateLimiter.Allow(now) {
		d.packetsRejectedByDH.Add(1)
		d.packetsDropped.Add(1)
		return info, ForwardDecision{}, nil, ErrDHAcceptRateExceeded
	}
	sess, err := d.getOrCreateSession(connID, now)
	if err != nil {
		d.packetsRejectedByLimit.Add(1)
		d.packetsDropped.Add(1)
		return protocol.PacketInfo{}, ForwardDecision{}, nil, err
	}

	d.sessionMu.Lock()
	sess.lastSeenAt = now
	sess.packets++
	sess.session.AcceptInfo(info)
	d.sessionMu.Unlock()

	switch info.Kind {
	case protocol.PacketKindEncrypted:
		d.packetsEncrypted.Add(1)
	case protocol.PacketKindDHHandshake:
		d.packetsHandshake.Add(1)
	}

	decision, err := d.runtime.Forward(ForwardRequest{
		TargetDC:    targetDC,
		AuthKeyID:   int64(info.AuthKeyID),
		PayloadSize: len(frame),
	})
	if err != nil {
		d.packetsRouteErrors.Add(1)
		d.packetsDropped.Add(1)
		return info, ForwardDecision{}, nil, err
	}
	outCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := d.runtime.exchangeOutbound(outCtx, decision.Target, frame)
	if err != nil {
		d.runtime.MarkTargetUnhealthy(decision.Target)
		d.packetsOutboundErrors.Add(1)
		d.packetsDropped.Add(1)
		return info, ForwardDecision{}, nil, err
	}
	d.runtime.MarkTargetHealthy(decision.Target)
	return info, decision, resp, nil
}

func (d *DataPlane) CloseConnection(connID int64) bool {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if _, ok := d.sessions[connID]; !ok {
		return false
	}
	delete(d.sessions, connID)
	d.sessionsClosed.Add(1)
	return true
}

func (d *DataPlane) PruneIdle(idle time.Duration, now time.Time) int {
	if idle < 0 {
		idle = 0
	}
	cutoff := now.Add(-idle)
	pruned := 0

	d.sessionMu.Lock()
	for connID, sess := range d.sessions {
		if sess.lastSeenAt.Before(cutoff) {
			delete(d.sessions, connID)
			pruned++
		}
	}
	d.sessionMu.Unlock()

	if pruned > 0 {
		d.sessionsClosed.Add(uint64(pruned))
	}
	return pruned
}

func (d *DataPlane) SessionState(connID int64) (protocol.SessionState, bool) {
	d.sessionMu.RLock()
	defer d.sessionMu.RUnlock()
	s, ok := d.sessions[connID]
	if !ok {
		return protocol.SessionStateInit, false
	}
	return s.session.State(), true
}

func (d *DataPlane) SessionLimit() int {
	return d.maxConnections
}

func (d *DataPlane) SetSessionLimit(maxConnections int) {
	if maxConnections < 0 {
		maxConnections = 0
	}
	d.sessionMu.Lock()
	d.maxConnections = maxConnections
	d.sessionMu.Unlock()
}

func (d *DataPlane) Stats() DataPlaneStats {
	d.sessionMu.RLock()
	active := len(d.sessions)
	limit := d.maxConnections
	d.sessionMu.RUnlock()

	return DataPlaneStats{
		ActiveSessions:         uint64(active),
		SessionLimit:           limit,
		SessionsCreated:        d.sessionsCreated.Load(),
		SessionsClosed:         d.sessionsClosed.Load(),
		PacketsTotal:           d.packetsTotal.Load(),
		PacketsEncrypted:       d.packetsEncrypted.Load(),
		PacketsHandshake:       d.packetsHandshake.Load(),
		PacketsDropped:         d.packetsDropped.Load(),
		PacketsParseErrors:     d.packetsParseErrors.Load(),
		PacketsRouteErrors:     d.packetsRouteErrors.Load(),
		PacketsRejectedByLimit: d.packetsRejectedByLimit.Load(),
		PacketsRejectedByDH:    d.packetsRejectedByDH.Load(),
		PacketsOutboundErrors:  d.packetsOutboundErrors.Load(),
		BytesTotal:             d.bytesTotal.Load(),
	}
}

func (d *DataPlane) getOrCreateSession(connID int64, now time.Time) (*dataPlaneSession, error) {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()

	if sess, ok := d.sessions[connID]; ok {
		sess.lastSeenAt = now
		return sess, nil
	}

	if d.maxConnections > 0 && len(d.sessions) >= d.maxConnections {
		return nil, ErrConnectionLimitReached
	}

	sess := &dataPlaneSession{
		session:    protocol.NewSession(),
		lastSeenAt: now,
	}
	d.sessions[connID] = sess
	d.sessionsCreated.Add(1)
	return sess, nil
}
