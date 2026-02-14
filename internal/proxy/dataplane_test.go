package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
	"github.com/TelegramMessenger/MTProxy/internal/protocol"
)

func TestRuntimeHandleMTProtoPacketHandshake(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 10})

	frame := makeHandshakeFrame(protocol.CodeReqPQ)
	decision, info, err := rt.HandleMTProtoPacket(1001, 2, frame)
	if err != nil {
		t.Fatalf("handle handshake packet: %v", err)
	}
	if info.Kind != protocol.PacketKindDHHandshake {
		t.Fatalf("unexpected packet kind: %v", info.Kind)
	}
	if info.Function != protocol.CodeReqPQ {
		t.Fatalf("unexpected handshake function: 0x%08x", info.Function)
	}
	if decision.ResolvedClusterID != 2 {
		t.Fatalf("unexpected target cluster: %d", decision.ResolvedClusterID)
	}

	state, ok := rt.DataPlaneSessionState(1001)
	if !ok {
		t.Fatalf("missing dataplane session")
	}
	if state != protocol.SessionStateHandshake {
		t.Fatalf("unexpected state: %v", state)
	}

	s := rt.DataPlaneStats()
	if s.ActiveSessions != 1 || s.PacketsTotal != 1 || s.PacketsHandshake != 1 || s.BytesTotal != uint64(len(frame)) {
		t.Fatalf("unexpected dataplane stats: %+v", s)
	}
}

func TestRuntimeHandleMTProtoPacketEncryptedTransition(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 10})

	if _, _, err := rt.HandleMTProtoPacket(77, 2, makeHandshakeFrame(protocol.CodeReqDHParams)); err != nil {
		t.Fatalf("handle handshake: %v", err)
	}
	decision, info, err := rt.HandleMTProtoPacket(77, 2, makeEncryptedFrame(0x1122334455667788))
	if err != nil {
		t.Fatalf("handle encrypted: %v", err)
	}
	if info.Kind != protocol.PacketKindEncrypted {
		t.Fatalf("unexpected packet kind: %v", info.Kind)
	}
	if info.AuthKeyID != 0x1122334455667788 {
		t.Fatalf("unexpected auth key id: %016x", info.AuthKeyID)
	}
	if decision.Target.ClusterID != 2 {
		t.Fatalf("unexpected target: %+v", decision.Target)
	}

	state, ok := rt.DataPlaneSessionState(77)
	if !ok {
		t.Fatalf("missing dataplane session")
	}
	if state != protocol.SessionStateEncrypted {
		t.Fatalf("unexpected state: %v", state)
	}

	s := rt.DataPlaneStats()
	if s.PacketsTotal != 2 || s.PacketsEncrypted != 1 || s.PacketsHandshake != 1 {
		t.Fatalf("unexpected dataplane stats: %+v", s)
	}
}

func TestRuntimeHandleMTProtoPacketConnectionLimit(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 1})

	if _, _, err := rt.HandleMTProtoPacket(1, 2, makeHandshakeFrame(protocol.CodeReqPQ)); err != nil {
		t.Fatalf("first connection should pass: %v", err)
	}
	_, _, err := rt.HandleMTProtoPacket(2, 2, makeHandshakeFrame(protocol.CodeReqPQMulti))
	if !errors.Is(err, ErrConnectionLimitReached) {
		t.Fatalf("expected connection limit error, got: %v", err)
	}

	s := rt.DataPlaneStats()
	if s.ActiveSessions != 1 || s.PacketsRejectedByLimit != 1 || s.PacketsDropped != 1 {
		t.Fatalf("unexpected dataplane stats: %+v", s)
	}
}

func TestRuntimeHandleMTProtoPacketParseError(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 2})

	_, _, err := rt.HandleMTProtoPacket(1, 2, []byte{1, 2, 3})
	if err == nil {
		t.Fatalf("expected parse error")
	}

	s := rt.DataPlaneStats()
	if s.PacketsParseErrors != 1 || s.PacketsDropped != 1 || s.PacketsTotal != 1 {
		t.Fatalf("unexpected dataplane stats: %+v", s)
	}
}

func TestRuntimeHandleMTProtoPacketDHRateLimit(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 10, MaxDHAcceptRate: 1})
	fixedNow := time.Unix(1700000000, 0).UTC()
	rt.dataplane.now = func() time.Time { return fixedNow }

	if _, _, err := rt.HandleMTProtoPacket(1, 2, makeHandshakeFrame(protocol.CodeReqPQ)); err != nil {
		t.Fatalf("first dh handshake should pass: %v", err)
	}
	_, _, err := rt.HandleMTProtoPacket(2, 2, makeHandshakeFrame(protocol.CodeReqPQMulti))
	if !errors.Is(err, ErrDHAcceptRateExceeded) {
		t.Fatalf("expected dh rate limit error, got: %v", err)
	}

	s := rt.DataPlaneStats()
	if s.PacketsRejectedByDH != 1 || s.PacketsDropped != 1 {
		t.Fatalf("unexpected dataplane stats: %+v", s)
	}
}

func TestRuntimeCloseAndPruneConnections(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 10})

	if _, _, err := rt.HandleMTProtoPacket(1, 2, makeHandshakeFrame(protocol.CodeReqPQ)); err != nil {
		t.Fatalf("handle packet #1: %v", err)
	}
	if _, _, err := rt.HandleMTProtoPacket(2, 2, makeHandshakeFrame(protocol.CodeReqPQ)); err != nil {
		t.Fatalf("handle packet #2: %v", err)
	}

	if !rt.CloseConnection(1) {
		t.Fatalf("expected connection 1 close to succeed")
	}
	if rt.CloseConnection(1) {
		t.Fatalf("expected repeated close to fail")
	}

	pruned := rt.PruneIdleConnections(100*time.Millisecond, time.Now().Add(5*time.Second))
	if pruned != 1 {
		t.Fatalf("unexpected pruned count: %d", pruned)
	}

	s := rt.DataPlaneStats()
	if s.ActiveSessions != 0 || s.SessionsClosed < 2 {
		t.Fatalf("unexpected dataplane stats: %+v", s)
	}
}

func TestRuntimeHandleMTProtoPacketOutboundError(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 10})
	rt.SetOutboundSender(&failingOutboundSender{err: fmt.Errorf("outbound test error")})

	_, _, err := rt.HandleMTProtoPacket(1, 2, makeHandshakeFrame(protocol.CodeReqPQ))
	if err == nil {
		t.Fatalf("expected outbound error")
	}

	s := rt.DataPlaneStats()
	if s.PacketsOutboundErrors != 1 || s.PacketsDropped != 1 {
		t.Fatalf("unexpected dataplane stats: %+v", s)
	}
}

func TestRuntimeHandleMTProtoPacketMarksTargetHealthOnOutboundResult(t *testing.T) {
	lc := NewLifecycle(config.NewManager("/tmp/non-existent"), cli.Options{MaxConn: 10})
	rt := NewRuntime(lc, &bytes.Buffer{})
	cfg, err := config.Parse(`
default 2;
proxy_for 2 149.154.175.50:443;
proxy_for 2 149.154.175.51:443;
`)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	rt.applyConfig(cfg)
	rt.randSource = &seqRand{seq: []int{0, 1, 1, 1}}
	rt.SetOutboundSender(&selectiveOutboundSender{failHost: "149.154.175.50"})

	if _, _, err := rt.HandleMTProtoPacket(1, 2, makeHandshakeFrame(protocol.CodeReqPQ)); err == nil {
		t.Fatalf("expected first outbound error")
	}
	decision, _, err := rt.HandleMTProtoPacket(2, 2, makeHandshakeFrame(protocol.CodeReqPQ))
	if err != nil {
		t.Fatalf("expected second outbound success: %v", err)
	}
	if decision.Target.Host != "149.154.175.51" {
		t.Fatalf("expected failover to healthy host, got %s", decision.Target.Host)
	}

	h0, ok0 := rt.TargetHealth(cfg.Targets[0])
	if !ok0 || h0 {
		t.Fatalf("expected first target unhealthy after outbound error: ok=%v healthy=%v", ok0, h0)
	}
	h1, ok1 := rt.TargetHealth(cfg.Targets[1])
	if !ok1 || !h1 {
		t.Fatalf("expected second target healthy after outbound success: ok=%v healthy=%v", ok1, h1)
	}
}

func newDataPlaneRuntimeForTest(t *testing.T, opts cli.Options) *Runtime {
	t.Helper()
	lc := NewLifecycle(config.NewManager("/tmp/non-existent"), opts)
	rt := NewRuntime(lc, &bytes.Buffer{})
	cfg, err := config.Parse(`
default 2;
proxy_for 2 149.154.175.50:443;
`)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	rt.applyConfig(cfg)
	return rt
}

func makeHandshakeFrame(function uint32) []byte {
	frame := make([]byte, 40)
	putU32LE(frame[16:20], 20)
	putU32LE(frame[20:24], function)
	return frame
}

func makeEncryptedFrame(authKeyID uint64) []byte {
	frame := make([]byte, 56)
	putU64LE(frame[:8], authKeyID)
	return frame
}

func putU32LE(dst []byte, v uint32) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
}

func putU64LE(dst []byte, v uint64) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
	dst[4] = byte(v >> 32)
	dst[5] = byte(v >> 40)
	dst[6] = byte(v >> 48)
	dst[7] = byte(v >> 56)
}

type failingOutboundSender struct {
	err error
}

func (f *failingOutboundSender) Exchange(context.Context, config.Target, []byte) ([]byte, error) {
	return nil, f.err
}

func (f *failingOutboundSender) Stats() OutboundStats {
	return OutboundStats{}
}

func (f *failingOutboundSender) Close() error {
	return nil
}

type selectiveOutboundSender struct {
	failHost string
}

func (s *selectiveOutboundSender) Exchange(_ context.Context, target config.Target, _ []byte) ([]byte, error) {
	if s.failHost != "" && target.Host == s.failHost {
		return nil, fmt.Errorf("forced outbound failure for host %s", target.Host)
	}
	return nil, nil
}

func (s *selectiveOutboundSender) Stats() OutboundStats {
	return OutboundStats{}
}

func (s *selectiveOutboundSender) Close() error {
	return nil
}
