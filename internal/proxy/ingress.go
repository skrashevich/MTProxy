package proxy

import (
	"context"
	"fmt"
	"net"
)

// IngressServer is a generic TCP listener that accepts connections and
// dispatches each to a handler goroutine. It supports graceful shutdown via context.
type IngressServer struct {
	addr    string
	handler func(conn net.Conn)
}

// NewIngressServer creates an IngressServer listening on addr.
// handler is called in a new goroutine for every accepted connection.
func NewIngressServer(addr string, handler func(conn net.Conn)) *IngressServer {
	return &IngressServer{
		addr:    addr,
		handler: handler,
	}
}

// ListenAndServe starts the TCP listener and blocks until ctx is cancelled or a
// fatal listen error occurs. It closes the listener when ctx is done.
func (s *IngressServer) ListenAndServe(ctx context.Context) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("ingress listen %s: %w", s.addr, err)
	}

	// Close listener when context is cancelled so Accept() unblocks.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// After context cancellation the listener is closed; treat as clean exit.
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("ingress accept: %w", err)
			}
		}
		go s.handler(conn)
	}
}
