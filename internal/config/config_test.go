package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDockerBackendConf(t *testing.T) {
	path := filepath.Join("..", "..", "docker", "telegram", "backend.conf")
	cfg, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse backend.conf: %v", err)
	}
	if !cfg.HaveProxy {
		t.Fatalf("expected HaveProxy=true")
	}
	if cfg.DefaultClusterID != 2 {
		t.Fatalf("unexpected default cluster id: %d", cfg.DefaultClusterID)
	}
	if len(cfg.Targets) == 0 || len(cfg.Clusters) == 0 {
		t.Fatalf("expected non-empty targets and clusters")
	}
}

func TestParseIntermixedProxyForRejected(t *testing.T) {
	input := `
proxy_for 1 149.154.175.50:8888;
proxy_for 2 149.154.161.144:8888;
proxy_for 1 149.154.175.51:8888;
`
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected error for intermixed proxy_for directives")
	}
}

func TestParseMissingProxyRejected(t *testing.T) {
	input := `default 2; timeout 100;`
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected error when no proxy directives exist")
	}
}

func TestParseTimeoutValidation(t *testing.T) {
	input := `proxy 149.154.175.50:8888; timeout 5;`
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected timeout validation error")
	}
}

func TestParseMinMaxValidation(t *testing.T) {
	input := `
proxy 149.154.175.50:8888;
min_connections 10;
max_connections 5;
`
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected min/max validation error")
	}
}

func TestParseRequiresTrailingSemicolon(t *testing.T) {
	input := `proxy 149.154.175.50:8888`
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected parse error when trailing semicolon is missing")
	}
}

func TestParseProxyClusterZeroGrouping(t *testing.T) {
	input := `
proxy 149.154.175.50:8888;
proxy 149.154.175.51:8888;
`
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got := len(cfg.Clusters); got != 1 {
		t.Fatalf("unexpected cluster count: %d", got)
	}
	if cfg.Clusters[0].ID != 0 {
		t.Fatalf("unexpected cluster id: %d", cfg.Clusters[0].ID)
	}
	if got := len(cfg.Clusters[0].Targets); got != 2 {
		t.Fatalf("unexpected target count in cluster 0: %d", got)
	}
}

func TestParseTooManyTargetsRejected(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxCfgTargets+1; i++ {
		b.WriteString("proxy 149.154.175.50:8888;\n")
	}
	_, err := Parse(b.String())
	if err == nil {
		t.Fatalf("expected too many targets error")
	}
}

func TestParseTooManyClustersRejected(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxCfgClusters+1; i++ {
		b.WriteString(fmt.Sprintf("proxy_for %d 149.154.175.50:8888;\n", i))
	}
	_, err := Parse(b.String())
	if err == nil {
		t.Fatalf("expected too many clusters error")
	}
}

func TestParseTargetConnectionLimitsFollowDirectiveOrder(t *testing.T) {
	input := `
min_connections 2;
max_connections 5;
proxy 149.154.175.50:8888;
min_connections 3;
max_connections 7;
proxy 149.154.175.51:8888;
`
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got := len(cfg.Targets); got != 2 {
		t.Fatalf("unexpected targets count: %d", got)
	}
	if cfg.Targets[0].MinConnections != 2 || cfg.Targets[0].MaxConnections != 5 {
		t.Fatalf("unexpected first target limits: min=%d max=%d", cfg.Targets[0].MinConnections, cfg.Targets[0].MaxConnections)
	}
	if cfg.Targets[1].MinConnections != 3 || cfg.Targets[1].MaxConnections != 7 {
		t.Fatalf("unexpected second target limits: min=%d max=%d", cfg.Targets[1].MinConnections, cfg.Targets[1].MaxConnections)
	}
}

func TestDefaultClusterLookup(t *testing.T) {
	cfg, err := Parse(`
default 2;
proxy_for 2 149.154.161.144:8888;
proxy_for 2 149.154.161.145:8888;
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	cl, ok := cfg.DefaultCluster()
	if !ok {
		t.Fatalf("expected default cluster to be found")
	}
	if cl.ID != 2 {
		t.Fatalf("unexpected cluster id: %d", cl.ID)
	}
	if len(cl.Targets) != 2 {
		t.Fatalf("unexpected default cluster targets: %d", len(cl.Targets))
	}
}

func TestDefaultClusterLookupMissing(t *testing.T) {
	cfg, err := Parse(`
default 3;
proxy_for 2 149.154.161.144:8888;
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, ok := cfg.DefaultCluster()
	if ok {
		t.Fatalf("expected default cluster lookup to fail for missing cluster id")
	}
}

func TestParseIPv6Targets(t *testing.T) {
	cfg, err := Parse(`
proxy [2001:db8::1]:443;
proxy_for 2 2001:db8::2:8443;
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("unexpected targets count: %d", len(cfg.Targets))
	}
	if cfg.Targets[0].Host != "2001:db8::1" || cfg.Targets[0].Port != 443 {
		t.Fatalf("unexpected bracketed IPv6 target: %+v", cfg.Targets[0])
	}
	if cfg.Targets[1].Host != "2001:db8::2" || cfg.Targets[1].Port != 8443 {
		t.Fatalf("unexpected loose IPv6 target: %+v", cfg.Targets[1])
	}
}

func TestParseTargetIDBoundsAccepted(t *testing.T) {
	cfg, err := Parse(`
default -32768;
proxy_for -32768 149.154.175.50:8888;
proxy_for 32767 149.154.175.51:8888;
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if cfg.DefaultClusterID != -32768 {
		t.Fatalf("unexpected default cluster id: %d", cfg.DefaultClusterID)
	}
	if _, ok := cfg.ClusterByID(-32768); !ok {
		t.Fatalf("expected cluster -32768")
	}
	if _, ok := cfg.ClusterByID(32767); !ok {
		t.Fatalf("expected cluster 32767")
	}
}

func TestParseTargetIDOutOfRangeRejected(t *testing.T) {
	for _, input := range []string{
		"default -32769; proxy 149.154.175.50:8888;",
		"default 32768; proxy 149.154.175.50:8888;",
		"proxy_for -32769 149.154.175.50:8888;",
		"proxy_for 32768 149.154.175.50:8888;",
	} {
		_, err := Parse(input)
		if err == nil {
			t.Fatalf("expected parse error for input: %q", input)
		}
	}
}
