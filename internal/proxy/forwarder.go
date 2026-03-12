package proxy

// Forwarder routes MTProto packets from the ingress (client) side to an
// outbound Telegram DC connection using RPC_PROXY_REQ framing.
//
// This is a thin convenience wrapper around OutboundProxy that accepts
// the existing ClientMeta type (from proxy_meta.go) and the Target type
// (from router.go) used throughout the package.
//
// The primary forwarding path is DataPlane.HandlePacket → Outbounder.ForwardPacket.
// Forwarder is an alternative entry point for direct use when the caller
// already holds a resolved Target.
type Forwarder struct {
	pool *OutboundProxy
}

// NewForwarder creates a Forwarder backed by the given outbound pool.
func NewForwarder(pool *OutboundProxy) *Forwarder {
	return &Forwarder{pool: pool}
}

// ForwardRaw sends a pre-serialised RPC_PROXY_REQ payload (req) to the
// resolved target address and returns the RPC_PROXY_ANS payload bytes.
//
// req must already contain the correct ext_conn_id at bytes [8:16] so
// that the response can be correlated by the async read loop.
func (f *Forwarder) ForwardRaw(targetAddr string, req []byte) ([]byte, error) {
	return f.pool.ForwardPacket(targetAddr, req)
}
