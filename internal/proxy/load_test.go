package proxy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

func TestLoadAndValidateSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := config.NewManager(path)
	opts := cli.Options{}

	s, b, err := LoadAndValidate(manager, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Bytes == 0 {
		t.Fatalf("expected non-zero config bytes")
	}
	if len(b.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", b.Warnings)
	}
}

func TestLoadAndValidateTLSRequiresSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(path, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := config.NewManager(path)
	opts := cli.Options{
		Domains: []string{"example.com"},
	}

	_, _, err := LoadAndValidate(manager, opts)
	if err == nil {
		t.Fatalf("expected error for TLS mode without secret")
	}
}
