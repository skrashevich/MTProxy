package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
	"github.com/TelegramMessenger/MTProxy/internal/proxy"
)

func TestProxyRunnerRunAndForward(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("default 2; proxy_for 2 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lifecycle := proxy.NewLifecycle(config.NewManager(path), cli.Options{})
	var logs bytes.Buffer
	runner := NewProxyRunner(lifecycle, &logs)

	sigCh := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(context.Background(), sigCh)
	}()
	sigCh <- syscall.SIGTERM

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runner timeout")
	}

	d, err := runner.Forward(proxy.ForwardRequest{TargetDC: 99, PayloadSize: 32})
	if err != nil {
		t.Fatalf("forward failed: %v", err)
	}
	if !d.UsedDefault {
		t.Fatalf("expected default fallback")
	}

	stats := runner.StatsSnapshot()
	if !stats.HasCurrentConfig {
		t.Fatalf("expected current config in stats snapshot")
	}
}
