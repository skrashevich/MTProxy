package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
)

type StatsServer struct {
	server   *http.Server
	listener net.Listener
}

func NewStatsHandler(rt *Runtime) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, rt.StatsSnapshot().RenderText())
	})
	return mux
}

func StartStatsServer(rt *Runtime, addr string, logw io.Writer) (*StatsServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: NewStatsHandler(rt),
	}
	out := &StatsServer{
		server:   srv,
		listener: ln,
	}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(logw, "stats server error: %v\n", err)
		}
	}()
	fmt.Fprintf(logw, "stats server listening on %s\n", ln.Addr().String())
	return out, nil
}

func (s *StatsServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *StatsServer) Addr() string {
	return s.listener.Addr().String()
}
