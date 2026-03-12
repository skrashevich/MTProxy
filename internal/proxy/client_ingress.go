package proxy

import (
	"context"
	"fmt"
	"net"
)

// IncomingPacket is a decrypted MTProto packet received from a Telegram client.
type IncomingPacket struct {
	Data     []byte
	ClientIP net.IP
	ClientPort int
	TargetDC int16
}

// DataplaneHandler receives decrypted MTProto packets from the ingress layer and
// forwards them to the outbound (Telegram-side) connection.
type DataplaneHandler interface {
	HandlePacket(pkt IncomingPacket) error
}

// ClientIngressServer wraps IngressServer and implements the obfuscated2 handshake
// for every incoming Telegram-client TCP connection.
type ClientIngressServer struct {
	secrets  [][]byte // list of 16-byte proxy secrets
	dataplane DataplaneHandler
	inner    *IngressServer
}

// NewClientIngressServer creates a ClientIngressServer that listens on addr.
// secrets is the list of valid 16-byte proxy secrets (at least one required).
// dp is the dataplane handler that receives decrypted packets.
func NewClientIngressServer(addr string, secrets [][]byte, dp DataplaneHandler) *ClientIngressServer {
	s := &ClientIngressServer{
		secrets:  secrets,
		dataplane: dp,
	}
	s.inner = NewIngressServer(addr, s.handleConn)
	return s
}

// ListenAndServe starts listening and blocks until ctx is cancelled.
func (s *ClientIngressServer) ListenAndServe(ctx context.Context) error {
	return s.inner.ListenAndServe(ctx)
}

// handleConn is called in its own goroutine for every accepted connection.
// It performs the obfuscated2 handshake and then pumps decrypted packets to
// the dataplane handler.
func (s *ClientIngressServer) handleConn(conn net.Conn) {
	defer conn.Close()

	// Extract client IP / port from the TCP remote address.
	clientIP, clientPort, err := parseRemoteAddr(conn.RemoteAddr())
	if err != nil {
		return
	}

	// Step 1: read the 64-byte obfuscated2 header.
	var raw [64]byte
	if _, err := readExact(conn, raw[:]); err != nil {
		return
	}

	// Step 2: try each secret until one yields a valid magic.
	var (
		hdr      Obfuscated2Header
		decState *AESStreamState
		encState *AESStreamState
	)

	found := false
	for _, secret := range s.secrets {
		h, dec, enc, err2 := ParseObfuscated2Header(raw, secret)
		if err2 != nil {
			continue // wrong secret or bad magic
		}
		hdr = h
		decState = dec
		encState = enc
		found = true
		break
	}

	// If secrets list is empty, try without secret (legacy / no-secret mode).
	if !found && len(s.secrets) == 0 {
		hdr, decState, encState, err = ParseObfuscated2Header(raw, nil)
		if err != nil {
			return
		}
		found = true
	}

	if !found {
		return
	}

	// Step 3: read MTProto packets in a loop and forward to dataplane.
	_ = encState // encState is passed to dataplane for writing responses

	for {
		payload, err := ReadPacket(conn, decState, hdr.Transport)
		if err != nil {
			return
		}
		pkt := IncomingPacket{
			Data:       payload,
			ClientIP:   clientIP,
			ClientPort: clientPort,
			TargetDC:   hdr.TargetDC,
		}
		if err := s.dataplane.HandlePacket(pkt); err != nil {
			return
		}
	}
}

// parseRemoteAddr extracts IP and port from a net.Addr (typically *net.TCPAddr).
func parseRemoteAddr(addr net.Addr) (net.IP, int, error) {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected remote addr type %T", addr)
	}
	return tcp.IP, tcp.Port, nil
}

// readExact reads exactly len(buf) bytes from conn.
func readExact(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
