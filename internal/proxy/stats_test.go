package proxy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

func TestRuntimeStatsSnapshotAndRender(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("default 2; proxy_for 2 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(config.NewManager(path), cli.Options{})
	var logs bytes.Buffer
	rt := NewRuntime(lc, &logs)

	sigCh := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() {
		done <- rt.Run(context.Background(), sigCh)
	}()
	sigCh <- syscall.SIGTERM

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runtime returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime timeout")
	}

	if _, err := rt.Forward(ForwardRequest{TargetDC: 99, PayloadSize: 8}); err != nil {
		t.Fatalf("forward decision failed: %v", err)
	}
	_, _ = rt.Forward(ForwardRequest{TargetDC: -999, PayloadSize: 8})
	tgt, err := rt.ChooseProxyTarget(2)
	if err != nil {
		t.Fatalf("choose proxy target: %v", err)
	}
	rt.MarkTargetUnhealthy(tgt)

	stats := rt.StatsSnapshot()
	if !stats.HasCurrentConfig {
		t.Fatalf("expected current config in stats")
	}
	if stats.ConfigSize == 0 || stats.ConfigMD5 == "" {
		t.Fatalf("unexpected config metadata: %+v", stats)
	}
	if stats.RouterStats.Targets == 0 {
		t.Fatalf("expected router stats to have targets")
	}
	if stats.ManagerStats.ReloadCalls == 0 {
		t.Fatalf("expected manager reload stats to be populated")
	}
	if stats.ForwardStats.TotalRequests == 0 {
		t.Fatalf("expected forward stats to be populated")
	}
	if stats.HealthyTargets != 0 || stats.UnhealthyTargets != 1 {
		t.Fatalf("unexpected health stats: healthy=%d unhealthy=%d", stats.HealthyTargets, stats.UnhealthyTargets)
	}

	rendered := stats.RenderText()
	for _, marker := range []string{
		"config_filename\t",
		"config_md5\t",
		"router_clusters\t",
		"targets_healthy\t",
		"targets_unhealthy\t",
		"config_reload_calls\t",
		"forward_total\t",
		"forward_bytes\t",
		"forward_last_error\t",
		"dataplane_packets_total\t",
		"dataplane_packets_rejected_dh_rate\t",
		"dataplane_active_sessions\t",
		"outbound_sends\t",
		"outbound_responses\t",
		"outbound_idle_evictions\t",
		"ingress_frames_received\t",
		"ingress_accept_rate_limited\t",
		"ingress_frames_returned\t",
	} {
		if !strings.Contains(rendered, marker) {
			t.Fatalf("stats output missing marker %q:\n%s", marker, rendered)
		}
	}
}
