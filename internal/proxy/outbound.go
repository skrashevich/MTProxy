package proxy

import (
	"fmt"
	"sync"
	"time"

	"github.com/skrashevich/MTProxy/internal/protocol"
)

// OutboundConfig holds configuration for the outbound proxy pool.
type OutboundConfig struct {
	Secret   []byte            // AES/DH shared secret (proxy password)
	ProxyTag []byte            // 16-byte proxy tag, or nil
	ForceDH  bool              // require DH key exchange
	NatInfo  map[uint32]uint32 // local IPv4 → public IPv4 (for key derivation behind NAT)
}

// OutboundProxy manages a pool of RPC connections to Telegram DC servers.
// There is at most one active rpcOutboundConn per target address.
//
// Implements the Outbounder interface expected by DataPlane.
// Corresponds to the outbound connection management in net/net-connections.c.
type OutboundProxy struct {
	cfg OutboundConfig

	mu    sync.Mutex
	conns map[string]*rpcOutboundConn // keyed by "host:port"
}

// NewOutboundProxy creates a new outbound proxy connection pool.
func NewOutboundProxy(cfg OutboundConfig) *OutboundProxy {
	return &OutboundProxy{
		cfg:   cfg,
		conns: make(map[string]*rpcOutboundConn),
	}
}

// ForwardPacket implements the Outbounder interface used by DataPlane.
// It sends an already-serialised RPC_PROXY_REQ frame (req) to the target DC
// and returns the raw RPC_PROXY_ANS payload bytes.
func (p *OutboundProxy) ForwardPacket(target string, req []byte) ([]byte, error) {
	conn, err := p.getConnection(target)
	if err != nil {
		return nil, err
	}

	// The caller (DataPlane / protocol.BuildProxyReq) has already serialised
	// the full RPC_PROXY_REQ frame including the ext_conn_id.
	// We need to extract the ext_conn_id to register a pending channel.
	if len(req) < 16 {
		return nil, fmt.Errorf("outbound: req too short: %d bytes", len(req))
	}
	// RPC_PROXY_REQ layout: [type(4)][flags(4)][ext_conn_id(8)]...
	extConnID := int64(uint64(req[8]) | uint64(req[9])<<8 | uint64(req[10])<<16 | uint64(req[11])<<24 |
		uint64(req[12])<<32 | uint64(req[13])<<40 | uint64(req[14])<<48 | uint64(req[15])<<56)

	respCh := make(chan ProxyResponse, 1)
	conn.RegisterPending(extConnID, respCh)

	// Send the frame as-is (already fully serialised by BuildProxyReq)
	if err := conn.writeEncryptedFrame(req); err != nil {
		conn.UnregisterPending(extConnID)
		return nil, fmt.Errorf("outbound: send to %s: %w", target, err)
	}

	select {
	case resp := <-respCh:
		// RPC_CLOSE_EXT from DC means "close this client connection"
		if resp.Flags == int32(protocol.RPCCloseExt) {
			return nil, fmt.Errorf("outbound: DC requested close for conn %d", extConnID)
		}
		return resp.Data, nil
	case <-conn.closed:
		return nil, fmt.Errorf("outbound: connection to %s closed", target)
	case <-time.After(30 * time.Second):
		conn.UnregisterPending(extConnID)
		return nil, fmt.Errorf("outbound: timeout waiting for response from %s", target)
	}
}

// GetConnection returns an active connection to the given Target, establishing
// a new one if necessary. Thread-safe. Used by DataPlane.
func (p *OutboundProxy) GetConnection(target Target) (*rpcOutboundConn, error) {
	return p.getConnection(target.Addr)
}

// getConnection returns an active connection to the given addr, establishing
// a new one if necessary. Thread-safe.
func (p *OutboundProxy) getConnection(addr string) (*rpcOutboundConn, error) {
	p.mu.Lock()
	conn, ok := p.conns[addr]
	p.mu.Unlock()

	if ok && !conn.isClosed() {
		return conn, nil
	}

	return p.reconnect(addr)
}

// reconnect creates and connects a new rpcOutboundConn for the given addr,
// replacing any previous (closed) connection.
func (p *OutboundProxy) reconnect(addr string) (*rpcOutboundConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring lock
	if conn, ok := p.conns[addr]; ok && !conn.isClosed() {
		return conn, nil
	}

	conn := newRPCOutboundConn(addr, p.cfg.Secret, p.cfg.ForceDH, p.cfg.NatInfo)
	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}

	p.conns[addr] = conn

	// Remove from pool when connection closes
	go p.watchConn(addr, conn)

	return conn, nil
}

// watchConn blocks until the connection closes, then removes it from the pool.
func (p *OutboundProxy) watchConn(addr string, conn *rpcOutboundConn) {
	<-conn.closed

	p.mu.Lock()
	if p.conns[addr] == conn {
		delete(p.conns, addr)
	}
	p.mu.Unlock()
}

// Close shuts down all connections in the pool.
func (p *OutboundProxy) Close() {
	p.mu.Lock()
	conns := make([]*rpcOutboundConn, 0, len(p.conns))
	for _, c := range p.conns {
		conns = append(conns, c)
	}
	p.conns = make(map[string]*rpcOutboundConn)
	p.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}
}

// isClosed reports whether the connection's closed channel has been closed.
func (c *rpcOutboundConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}
