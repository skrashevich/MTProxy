package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/protocol"
)

func TestIngressServerHandlesFrames(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 8})
	var logs bytes.Buffer

	srv, err := StartIngressServer(rt, IngressConfig{
		Addr:         "127.0.0.1:0",
		TargetDC:     2,
		MaxFrameSize: 1 << 20,
		IdleTimeout:  3 * time.Second,
	}, &logs)
	if err != nil {
		t.Fatalf("start ingress: %v", err)
	}
	rt.SetIngressStatsProvider(srv.Stats)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer conn.Close()

	if err := writeFrame(conn, makeHandshakeFrame(protocol.CodeReqPQ)); err != nil {
		t.Fatalf("write handshake frame: %v", err)
	}
	if err := writeFrame(conn, makeEncryptedFrame(0x0102030405060708)); err != nil {
		t.Fatalf("write encrypted frame: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		dp := rt.DataPlaneStats()
		ig := srv.Stats()
		if dp.PacketsTotal >= 2 && ig.FramesHandled >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting ingress handling; dataplane=%+v ingress=%+v logs=%s", dp, ig, logs.String())
		}
		time.Sleep(15 * time.Millisecond)
	}

	state, ok := rt.DataPlaneSessionState(2)
	if !ok {
		t.Fatalf("missing session state for ingress connection")
	}
	if state != protocol.SessionStateEncrypted {
		t.Fatalf("unexpected session state: %v", state)
	}

	snapshot := rt.StatsSnapshot()
	if snapshot.IngressStats.FramesHandled < 2 {
		t.Fatalf("unexpected ingress stats in snapshot: %+v", snapshot.IngressStats)
	}
	if snapshot.DataPlaneStats.PacketsEncrypted < 1 || snapshot.DataPlaneStats.PacketsHandshake < 1 {
		t.Fatalf("unexpected dataplane stats in snapshot: %+v", snapshot.DataPlaneStats)
	}
}

func TestIngressServerRejectsBadFrameLength(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 2})

	srv, err := StartIngressServer(rt, IngressConfig{
		Addr:         "127.0.0.1:0",
		TargetDC:     2,
		MaxFrameSize: 32,
		IdleTimeout:  time.Second,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("start ingress: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}

	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], 64)
	if _, err := conn.Write(hdr[:]); err != nil {
		t.Fatalf("write oversize hdr: %v", err)
	}
	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		ig := srv.Stats()
		if ig.InvalidFrames >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting invalid frame count; ingress=%+v", ig)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func TestIngressServerAppliesAcceptRateLimit(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 2})

	srv, err := StartIngressServer(rt, IngressConfig{
		Addr:          "127.0.0.1:0",
		TargetDC:      2,
		MaxFrameSize:  128,
		IdleTimeout:   time.Second,
		MaxAcceptRate: 1,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("start ingress: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	fixedNow := time.Unix(1700000000, 0)
	srv.now = func() time.Time { return fixedNow }

	conn1, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial ingress #1: %v", err)
	}
	_ = conn1.Close()

	conn2, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial ingress #2: %v", err)
	}
	_ = conn2.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		ig := srv.Stats()
		if ig.AcceptRateLimited >= 1 && ig.AcceptedConnections >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting accept rate limit stats; ingress=%+v", ig)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func TestIngressServerShutdownContextTimeout(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{})

	srv, err := StartIngressServer(rt, IngressConfig{
		Addr:         "127.0.0.1:0",
		TargetDC:     2,
		MaxFrameSize: 64,
		IdleTimeout:  5 * time.Second,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("start ingress: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown ingress: %v", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel2()
	if err := srv.Shutdown(ctx2); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("repeated shutdown: %v", err)
	}
}

func writeFrame(conn net.Conn, frame []byte) error {
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(frame)))
	if _, err := conn.Write(hdr[:]); err != nil {
		return err
	}
	_, err := conn.Write(frame)
	return err
}
