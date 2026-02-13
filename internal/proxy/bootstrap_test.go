package proxy

import (
	"testing"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

func TestValidateBootstrapRequiresSecretForTLSMode(t *testing.T) {
	opts := cli.Options{
		Domains: []string{"example.com"},
	}
	cfg := config.Config{
		HaveProxy: true,
	}

	_, err := ValidateBootstrap(opts, cfg)
	if err == nil {
		t.Fatalf("expected error when TLS mode is enabled without secret")
	}
}

func TestValidateBootstrapWarnsAboutWorkersInTLSMode(t *testing.T) {
	opts := cli.Options{
		Domains: []string{"example.com"},
		Workers: 2,
		Secrets: [][16]byte{{1}},
	}
	cfg := config.Config{
		HaveProxy: true,
	}

	res, err := ValidateBootstrap(opts, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("expected warning for workers in TLS mode")
	}
}

func TestValidateBootstrapNoTLSMode(t *testing.T) {
	opts := cli.Options{
		Workers: 2,
	}
	cfg := config.Config{
		HaveProxy: true,
	}

	res, err := ValidateBootstrap(opts, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
}
