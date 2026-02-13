package proxy

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

func TestLifecycleLoadInitialAndCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(config.NewManager(path), cli.Options{})
	s, warnings, err := lc.LoadInitial()
	if err != nil {
		t.Fatalf("load initial failed: %v", err)
	}
	if s.Bytes == 0 {
		t.Fatalf("expected non-zero config bytes")
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	cur, curWarnings, ok := lc.Current()
	if !ok {
		t.Fatalf("expected current snapshot")
	}
	if cur.MD5Hex != s.MD5Hex {
		t.Fatalf("unexpected current snapshot")
	}
	if len(curWarnings) != 0 {
		t.Fatalf("unexpected current warnings: %v", curWarnings)
	}
}

func TestLifecycleHandleReloadSignal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(config.NewManager(path), cli.Options{})
	s1, _, err := lc.LoadInitial()
	if err != nil {
		t.Fatalf("load initial failed: %v", err)
	}

	if err := os.WriteFile(path, []byte("proxy 149.154.175.51:8888;"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	action, s2, warnings, err := lc.HandleSignal(syscall.SIGHUP)
	if err != nil {
		t.Fatalf("reload signal failed: %v", err)
	}
	if action != SignalActionReload {
		t.Fatalf("unexpected action: %s", action)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if s1.MD5Hex == s2.MD5Hex {
		t.Fatalf("expected snapshot md5 to change after reload")
	}
}

func TestLifecycleHandleReloadSignalKeepsCurrentOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(config.NewManager(path), cli.Options{})
	s1, _, err := lc.LoadInitial()
	if err != nil {
		t.Fatalf("load initial failed: %v", err)
	}

	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	action, _, _, err := lc.HandleSignal(syscall.SIGHUP)
	if action != SignalActionReload {
		t.Fatalf("unexpected action: %s", action)
	}
	if err == nil {
		t.Fatalf("expected reload error")
	}

	cur, _, ok := lc.Current()
	if !ok {
		t.Fatalf("expected current snapshot")
	}
	if cur.MD5Hex != s1.MD5Hex {
		t.Fatalf("current snapshot changed after failed reload")
	}
}

func TestLifecycleHandleShutdownSignal(t *testing.T) {
	lc := NewLifecycle(config.NewManager("/tmp/does-not-matter"), cli.Options{})
	action, _, _, err := lc.HandleSignal(syscall.SIGTERM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != SignalActionShutdown {
		t.Fatalf("unexpected action: %s", action)
	}
}

func TestLifecycleHandleLogRotateSignal(t *testing.T) {
	lc := NewLifecycle(config.NewManager("/tmp/does-not-matter"), cli.Options{})
	action, _, _, err := lc.HandleSignal(syscall.SIGUSR1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != SignalActionLogRotate {
		t.Fatalf("unexpected action: %s", action)
	}
}
