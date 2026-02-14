package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type ClientIngressConfig struct {
	Addr          string
	TargetDC      int
	MaxFrameSize  int
	IdleTimeout   time.Duration
	MaxAcceptRate int
	ReadBufBytes  int
	WriteBufBytes int
	Secrets       [][16]byte
}

type ClientIngressServer struct {
	runtime *Runtime
	cfg     ClientIngressConfig
	logw    io.Writer
	now     func() time.Time

	listener net.Listener
	closed   chan struct{}
	once     sync.Once
	wg       sync.WaitGroup

	nextConnID int64

	acceptLimiter *fixedWindowRateLimiter

	acceptedConnections atomic.Uint64
	acceptRateLimited   atomic.Uint64
	closedConnections   atomic.Uint64
	activeConnections   atomic.Uint64
	framesReceived      atomic.Uint64
	framesHandled       atomic.Uint64
	framesReturned      atomic.Uint64
	framesFailed        atomic.Uint64
	bytesReceived       atomic.Uint64
	bytesReturned       atomic.Uint64
	readErrors          atomic.Uint64
	writeErrors         atomic.Uint64
	invalidFrames       atomic.Uint64
}

func StartClientIngressServer(rt *Runtime, cfg ClientIngressConfig, logw io.Writer) (*ClientIngressServer, error) {
	if cfg.MaxFrameSize <= 0 {
		cfg.MaxFrameSize = 4 << 20
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 45 * time.Second
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("ingress addr is required")
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, err
	}

	s := &ClientIngressServer{
		runtime:       rt,
		cfg:           cfg,
		logw:          logw,
		now:           time.Now,
		listener:      ln,
		closed:        make(chan struct{}),
		nextConnID:    1,
		acceptLimiter: newFixedWindowRateLimiter(cfg.MaxAcceptRate),
	}

	s.wg.Add(1)
	go s.acceptLoop()
	fmt.Fprintf(logw, "ingress server listening on %s\n", ln.Addr().String())
	return s, nil
}

func (s *ClientIngressServer) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *ClientIngressServer) Shutdown(ctx context.Context) error {
	s.once.Do(func() {
		close(s.closed)
		if s.listener != nil {
			_ = s.listener.Close()
		}
	})

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *ClientIngressServer) Stats() IngressStats {
	return IngressStats{
		AcceptedConnections: s.acceptedConnections.Load(),
		AcceptRateLimited:   s.acceptRateLimited.Load(),
		ClosedConnections:   s.closedConnections.Load(),
		ActiveConnections:   s.activeConnections.Load(),
		FramesReceived:      s.framesReceived.Load(),
		FramesHandled:       s.framesHandled.Load(),
		FramesReturned:      s.framesReturned.Load(),
		FramesFailed:        s.framesFailed.Load(),
		BytesReceived:       s.bytesReceived.Load(),
		BytesReturned:       s.bytesReturned.Load(),
		ReadErrors:          s.readErrors.Load(),
		WriteErrors:         s.writeErrors.Load(),
		InvalidFrames:       s.invalidFrames.Load(),
	}
}

func (s *ClientIngressServer) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			fmt.Fprintf(s.logw, "ingress accept error: %v\n", err)
			return
		}

		if !s.acceptLimiter.Allow(s.now()) {
			s.acceptRateLimited.Add(1)
			_ = conn.Close()
			continue
		}

		connID := atomic.AddInt64(&s.nextConnID, 1)
		s.acceptedConnections.Add(1)
		s.activeConnections.Add(1)
		s.wg.Add(1)
		go s.handleConn(connID, conn)
	}
}

func (s *ClientIngressServer) handleConn(connID int64, conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	defer s.runtime.CloseConnection(connID)
	defer s.closedConnections.Add(1)
	defer s.activeConnections.Add(^uint64(0))

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if s.cfg.ReadBufBytes > 0 {
			_ = tcpConn.SetReadBuffer(s.cfg.ReadBufBytes)
		}
		if s.cfg.WriteBufBytes > 0 {
			_ = tcpConn.SetWriteBuffer(s.cfg.WriteBufBytes)
		}
	}

	reader := bufio.NewReader(conn)
	transport := newMTProtoClientTransport(s.cfg.MaxFrameSize)
	if s.cfg.IdleTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
	}
	if err := transport.init(reader, s.cfg.Secrets); err != nil {
		s.invalidFrames.Add(1)
		s.framesFailed.Add(1)
		return
	}

	targetDC := s.cfg.TargetDC
	if transport.targetDC != 0 {
		targetDC = transport.targetDC
	}

	for {
		if s.cfg.IdleTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
		}

		frame, err := transport.readPacket(reader)
		if err != nil {
			if isConnClosedRead(err) {
				return
			}
			s.readErrors.Add(1)
			s.framesFailed.Add(1)
			return
		}
		if len(frame) == 0 {
			s.invalidFrames.Add(1)
			s.framesFailed.Add(1)
			return
		}

		s.framesReceived.Add(1)
		s.bytesReceived.Add(uint64(len(frame)))

		_, _, response, err := s.runtime.HandleMTProtoPacketWithResponse(connID, targetDC, frame)
		if err != nil {
			s.framesFailed.Add(1)
			continue
		}
		if len(response) > 0 {
			if s.cfg.IdleTimeout > 0 {
				_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.IdleTimeout))
			}
			if err := transport.writePacket(conn, response); err != nil {
				s.writeErrors.Add(1)
				s.framesFailed.Add(1)
				return
			}
			s.framesReturned.Add(1)
			s.bytesReturned.Add(uint64(len(response)))
		}
		s.framesHandled.Add(1)
	}
}
