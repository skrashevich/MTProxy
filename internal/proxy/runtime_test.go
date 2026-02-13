package proxy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

func TestRuntimeRunReloadAndShutdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(config.NewManager(path), cli.Options{})
	var logs bytes.Buffer
	rt := NewRuntime(lc, &logs)

	sigCh := make(chan os.Signal, 3)
	done := make(chan error, 1)
	go func() {
		done <- rt.Run(context.Background(), sigCh)
	}()

	if err := os.WriteFile(path, []byte("proxy 149.154.175.51:8888;"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	sigCh <- syscall.SIGUSR1
	sigCh <- syscall.SIGHUP
	sigCh <- syscall.SIGTERM

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runtime returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime timeout")
	}

	out := logs.String()
	if !strings.Contains(out, "runtime initialized:") {
		t.Fatalf("missing runtime init log: %s", out)
	}
	if !strings.Contains(out, "re-read successfully") {
		t.Fatalf("missing reload success log: %s", out)
	}
	if !strings.Contains(out, "SIGUSR1 received: no log file configured, skipping reopen.") {
		t.Fatalf("missing SIGUSR1 no-op reopen log: %s", out)
	}
	if !strings.Contains(out, "Terminated by SIGTERM.") {
		t.Fatalf("missing shutdown log: %s", out)
	}
}

func TestRuntimeSIGUSR1ReopensLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(config.NewManager(path), cli.Options{})
	var logs bytes.Buffer
	rt := NewRuntime(lc, &logs)

	reopenCalls := 0
	rt.SetLogReopener(func() error {
		reopenCalls++
		return nil
	})

	sigCh := make(chan os.Signal, 2)
	done := make(chan error, 1)
	go func() {
		done <- rt.Run(context.Background(), sigCh)
	}()
	sigCh <- syscall.SIGUSR1
	sigCh <- syscall.SIGTERM

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runtime returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime timeout")
	}

	if reopenCalls != 1 {
		t.Fatalf("unexpected reopen calls: %d", reopenCalls)
	}
	if !strings.Contains(logs.String(), "SIGUSR1 received: log file reopened.") {
		t.Fatalf("missing SIGUSR1 reopen success log: %s", logs.String())
	}
}

func TestRuntimeSIGUSR1ReopenError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(config.NewManager(path), cli.Options{})
	var logs bytes.Buffer
	rt := NewRuntime(lc, &logs)

	rt.SetLogReopener(func() error {
		return fmt.Errorf("test reopen failure")
	})

	sigCh := make(chan os.Signal, 2)
	done := make(chan error, 1)
	go func() {
		done <- rt.Run(context.Background(), sigCh)
	}()
	sigCh <- syscall.SIGUSR1
	sigCh <- syscall.SIGTERM

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runtime returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runtime timeout")
	}

	if !strings.Contains(logs.String(), "SIGUSR1 log reopen failed: test reopen failure") {
		t.Fatalf("missing SIGUSR1 reopen error log: %s", logs.String())
	}
}

func TestRuntimeChooseProxyTargetAfterRun(t *testing.T) {
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

	tgt, err := rt.ChooseProxyTarget(99)
	if err != nil {
		t.Fatalf("choose proxy target: %v", err)
	}
	if tgt.ClusterID != 2 {
		t.Fatalf("unexpected chosen cluster id: %d", tgt.ClusterID)
	}

	decision, err := rt.Forward(ForwardRequest{TargetDC: 99, PayloadSize: 64})
	if err != nil {
		t.Fatalf("forward decision failed: %v", err)
	}
	if !decision.UsedDefault {
		t.Fatalf("expected forward decision to use default cluster fallback")
	}

	stats := rt.ForwardStats()
	if stats.TotalRequests != 1 || stats.Successful != 1 || stats.UsedDefault != 1 {
		t.Fatalf("unexpected forward stats: %+v", stats)
	}
	if stats.ForwardedBytes != 64 {
		t.Fatalf("unexpected forwarded bytes: %d", stats.ForwardedBytes)
	}
}

func TestRuntimeHealthAwareTargetSelection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte(`
default 2;
proxy_for 2 149.154.175.50:8888;
proxy_for 2 149.154.175.51:8888;
`), 0o600); err != nil {
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

	rt.SetHealthChecker(func(tgt config.Target) bool {
		return tgt.Host == "149.154.175.51"
	})
	tgt, err := rt.ChooseProxyTarget(2)
	if err != nil {
		t.Fatalf("choose healthy target: %v", err)
	}
	if tgt.Host != "149.154.175.51" {
		t.Fatalf("unexpected chosen host: %s", tgt.Host)
	}

	rt.SetHealthChecker(func(config.Target) bool { return false })
	if _, err := rt.ChooseProxyTarget(2); err == nil {
		t.Fatalf("expected no healthy target error")
	}
}

func TestRuntimeHealthStateReconcileOnConfigApply(t *testing.T) {
	lc := NewLifecycle(config.NewManager("/tmp/non-existent"), cli.Options{})
	rt := NewRuntime(lc, &bytes.Buffer{})

	cfg1, err := config.Parse(`
proxy_for 2 149.154.175.50:8888;
`)
	if err != nil {
		t.Fatalf("parse cfg1: %v", err)
	}
	rt.applyConfig(cfg1)
	t1 := cfg1.Targets[0]
	rt.MarkTargetUnhealthy(t1)

	cfg2, err := config.Parse(`
proxy_for 2 149.154.175.50:8888;
proxy_for 2 149.154.175.51:8888;
`)
	if err != nil {
		t.Fatalf("parse cfg2: %v", err)
	}
	rt.applyConfig(cfg2)

	h1, ok1 := rt.TargetHealth(cfg2.Targets[0])
	if !ok1 || h1 {
		t.Fatalf("expected persisted unhealthy state for old target: healthy=%v ok=%v", h1, ok1)
	}
	h2, ok2 := rt.TargetHealth(cfg2.Targets[1])
	if !ok2 || !h2 {
		t.Fatalf("expected default healthy state for new target: healthy=%v ok=%v", h2, ok2)
	}

	cfg3, err := config.Parse(`
proxy_for 2 149.154.175.51:8888;
`)
	if err != nil {
		t.Fatalf("parse cfg3: %v", err)
	}
	rt.applyConfig(cfg3)

	if _, ok := rt.TargetHealth(t1); ok {
		t.Fatalf("expected removed target health entry to be dropped")
	}
}
