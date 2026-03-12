package proxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync/atomic"
	"time"
)

// ext_conn_id counter — unique per process, starting from a random-ish base.
var extConnIDCounter int64

func init() {
	// Start from pid*1000 to avoid collisions across restarts.
	extConnIDCounter = int64(uint32(time.Now().UnixNano())) << 16
}

// nextExtConnID returns a unique ext_conn_id for correlating RPC responses.
func nextExtConnID() int64 {
	return atomic.AddInt64(&extConnIDCounter, 1)
}

// IncomingPacket is a decrypted MTProto packet received from a Telegram client.
type IncomingPacket struct {
	Data       []byte
	ClientIP   net.IP
	ClientPort int
	TargetDC   int16
	ExtConnID  int64 // unique per client connection, used in RPC_PROXY_REQ
}

// DataplaneHandler receives decrypted MTProto packets from the ingress layer,
// forwards them to a Telegram DC, and returns the response data.
type DataplaneHandler interface {
	HandlePacket(pkt IncomingPacket) ([]byte, error)
}

// ClientIngressServer wraps IngressServer and implements the obfuscated2 handshake
// for every incoming Telegram-client TCP connection.
type ClientIngressServer struct {
	secrets   [][]byte // list of 16-byte proxy secrets
	dataplane DataplaneHandler
	inner     *IngressServer
	shutdown  *GracefulShutdown
}

// NewClientIngressServer creates a ClientIngressServer that listens on addr.
// secrets is the list of valid 16-byte proxy secrets (at least one required).
// dp is the dataplane handler that receives decrypted packets.
func NewClientIngressServer(addr string, secrets [][]byte, dp DataplaneHandler, shutdown *GracefulShutdown) *ClientIngressServer {
	s := &ClientIngressServer{
		secrets:   secrets,
		dataplane: dp,
		shutdown:  shutdown,
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
// the dataplane handler, writing responses back to the client.
func (s *ClientIngressServer) handleConn(conn net.Conn) {
	defer conn.Close()

	// Track connection for graceful shutdown.
	if s.shutdown != nil {
		s.shutdown.Track(conn)
		defer s.shutdown.Untrack(conn)
	}

	// Extract client IP / port from the TCP remote address.
	clientIP, clientPort, err := parseRemoteAddr(conn.RemoteAddr())
	if err != nil {
		log.Printf("ingress: bad remote addr: %v", err)
		return
	}

	log.Printf("ingress: new connection from %s:%d", clientIP, clientPort)

	// Step 1: read the 64-byte obfuscated2 header (with timeout).
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	var raw [64]byte
	if _, err := readExact(conn, raw[:]); err != nil {
		log.Printf("ingress: read header from %s:%d: %v", clientIP, clientPort, err)
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
		log.Printf("ingress: no valid secret for %s:%d", clientIP, clientPort)
		return
	}

	log.Printf("ingress: handshake OK from %s:%d, transport=%d, targetDC=%d", clientIP, clientPort, hdr.Transport, hdr.TargetDC)

	// Generate unique ext_conn_id for this client session.
	extConnID := nextExtConnID()

	// Step 3: read MTProto packets in a loop and forward to dataplane.
	for {
		// Set read deadline for each packet (idle timeout).
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		payload, err := ReadPacket(conn, decState, hdr.Transport)
		if err != nil {
			log.Printf("ingress: read packet from %s:%d: %v", clientIP, clientPort, err)
			return
		}

		pkt := IncomingPacket{
			Data:       payload,
			ClientIP:   clientIP,
			ClientPort: clientPort,
			TargetDC:   hdr.TargetDC,
			ExtConnID:  extConnID,
		}

		resp, err := s.dataplane.HandlePacket(pkt)
		if err != nil {
			log.Printf("ingress: dataplane error for %s:%d: %v", clientIP, clientPort, err)
			return
		}

		// Write response back to client (encrypted with obfuscated2 encState).
		if len(resp) > 0 {
			conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if err := WritePacket(conn, resp, encState, hdr.Transport); err != nil {
				log.Printf("ingress: write response to %s:%d: %v", clientIP, clientPort, err)
				return
			}
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
