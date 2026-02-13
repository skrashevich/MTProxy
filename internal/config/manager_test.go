package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerReloadAndCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := NewManager(path)
	s, err := m.Reload()
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if s.Bytes == 0 {
		t.Fatalf("expected non-zero bytes")
	}
	if len(s.MD5Hex) != 32 {
		t.Fatalf("unexpected md5 length: %d", len(s.MD5Hex))
	}

	cur, ok := m.Current()
	if !ok {
		t.Fatalf("expected current snapshot")
	}
	if cur.Bytes != s.Bytes || cur.MD5Hex != s.MD5Hex {
		t.Fatalf("current snapshot mismatch")
	}

	stats := m.Stats()
	if stats.ReloadCalls != 1 || stats.ReloadSuccess != 1 || stats.CheckCalls != 1 {
		t.Fatalf("unexpected manager stats: %+v", stats)
	}
}

func TestManagerFailedReloadDoesNotReplaceCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := NewManager(path)
	s1, err := m.Reload()
	if err != nil {
		t.Fatalf("first reload failed: %v", err)
	}

	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	if _, err := m.Reload(); err == nil {
		t.Fatalf("expected reload error for invalid config")
	}

	cur, ok := m.Current()
	if !ok {
		t.Fatalf("expected current snapshot")
	}
	if cur.MD5Hex != s1.MD5Hex {
		t.Fatalf("current snapshot should remain previous valid snapshot")
	}

	stats := m.Stats()
	if stats.ReloadCalls != 2 || stats.ReloadSuccess != 1 || stats.LastError == "" {
		t.Fatalf("unexpected manager stats after failed reload: %+v", stats)
	}

	if err := os.WriteFile(path, []byte("proxy 149.154.175.51:8888;"), 0o600); err != nil {
		t.Fatalf("rewrite valid config: %v", err)
	}
	if _, err := m.Reload(); err != nil {
		t.Fatalf("expected successful reload after recovery, got: %v", err)
	}
	stats = m.Stats()
	if stats.ReloadCalls != 3 || stats.ReloadSuccess != 2 || stats.LastError != "" {
		t.Fatalf("unexpected manager stats after recovery reload: %+v", stats)
	}
}
