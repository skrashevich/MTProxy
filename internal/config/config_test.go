package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "proxy-*.conf")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestParseConfig_Normal(t *testing.T) {
	content := `# force_probability 10 10
default 2;
proxy_for 1 149.154.175.50:8888;
proxy_for -1 149.154.175.50:8888;
proxy_for 2 149.154.161.144:8888;
proxy_for -2 149.154.161.144:8888;
proxy_for 4 91.108.4.225:8888;
proxy_for 4 91.108.4.133:8888;
`
	path := writeTemp(t, content)
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultClusterID != 2 {
		t.Errorf("expected DefaultClusterID=2, got %d", cfg.DefaultClusterID)
	}
	// DC 4 should have 2 targets
	cl4, ok := cfg.Clusters[4]
	if !ok {
		t.Fatal("cluster DC=4 not found")
	}
	if len(cl4.Targets) != 2 {
		t.Errorf("expected 2 targets for DC=4, got %d", len(cl4.Targets))
	}
	// DC -1 should exist
	if _, ok := cfg.Clusters[-1]; !ok {
		t.Error("cluster DC=-1 not found")
	}
}

func TestParseConfig_WithComments(t *testing.T) {
	content := `# this is a comment
# another comment
default 3; # inline comment not supported but stripped
proxy_for 1 10.0.0.1:443;
`
	path := writeTemp(t, content)
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultClusterID != 3 {
		t.Errorf("expected DefaultClusterID=3, got %d", cfg.DefaultClusterID)
	}
	cl1, ok := cfg.Clusters[1]
	if !ok {
		t.Fatal("cluster DC=1 not found")
	}
	if cl1.Targets[0].Addr != "10.0.0.1" || cl1.Targets[0].Port != 443 {
		t.Errorf("unexpected target: %v", cl1.Targets[0])
	}
}

func TestParseConfig_Empty(t *testing.T) {
	path := writeTemp(t, "# only comments\n")
	_, err := ParseConfig(path)
	if err == nil {
		t.Error("expected error for empty config, got nil")
	}
}

func TestParseConfig_NoProxyFor(t *testing.T) {
	path := writeTemp(t, "default 2;\n")
	_, err := ParseConfig(path)
	if err == nil {
		t.Error("expected error when no proxy_for entries")
	}
}

func TestParseConfig_InvalidPort(t *testing.T) {
	path := writeTemp(t, "proxy_for 1 149.154.175.50:99999;\n")
	_, err := ParseConfig(path)
	if err == nil {
		t.Error("expected error for invalid port 99999")
	}
}

func TestParseConfig_MissingPort(t *testing.T) {
	path := writeTemp(t, "proxy_for 1 149.154.175.50;\n")
	_, err := ParseConfig(path)
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestParseConfig_FileNotFound(t *testing.T) {
	_, err := ParseConfig(filepath.Join(t.TempDir(), "nonexistent.conf"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParseConfig_MultipleTargetsSameDC(t *testing.T) {
	content := `
proxy_for 4 91.108.4.225:8888;
proxy_for 4 91.108.4.133:8888;
proxy_for 4 91.108.4.202:8888;
`
	path := writeTemp(t, content)
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Clusters[4].Targets) != 3 {
		t.Errorf("expected 3 targets for DC=4, got %d", len(cfg.Clusters[4].Targets))
	}
}

func TestParseConfig_DefaultCluster(t *testing.T) {
	content := `
default 5;
proxy_for 5 91.108.56.100:8888;
`
	path := writeTemp(t, content)
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultClusterID != 5 {
		t.Errorf("expected DefaultClusterID=5, got %d", cfg.DefaultClusterID)
	}
}

func TestParseConfig_RealProxyMultiConf(t *testing.T) {
	// Use the actual proxy-multi.conf from the repo if it exists.
	path := "../../proxy-multi.conf"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("proxy-multi.conf not found, skipping")
	}
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("unexpected error parsing proxy-multi.conf: %v", err)
	}
	if cfg.DefaultClusterID != 2 {
		t.Errorf("expected DefaultClusterID=2, got %d", cfg.DefaultClusterID)
	}
	if len(cfg.Clusters) == 0 {
		t.Error("expected clusters, got none")
	}
}

func TestManager_LoadAndReload(t *testing.T) {
	content := "default 1;\nproxy_for 1 10.0.0.1:8888;\n"
	path := writeTemp(t, content)

	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg := m.Get()
	if cfg == nil {
		t.Fatal("Get returned nil after Load")
	}
	if cfg.DefaultClusterID != 1 {
		t.Errorf("expected DefaultClusterID=1, got %d", cfg.DefaultClusterID)
	}

	// Update file and reload
	if err := os.WriteFile(path, []byte("default 3;\nproxy_for 3 10.0.0.3:8888;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	cfg2 := m.Get()
	if cfg2.DefaultClusterID != 3 {
		t.Errorf("after reload expected DefaultClusterID=3, got %d", cfg2.DefaultClusterID)
	}
}

func TestManager_ReloadKeepsOldOnError(t *testing.T) {
	content := "default 1;\nproxy_for 1 10.0.0.1:8888;\n"
	path := writeTemp(t, content)

	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}

	// Write invalid config
	if err := os.WriteFile(path, []byte("# empty\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = m.Reload() // expect error, ignore

	// Old config should still be active
	cfg := m.Get()
	if cfg.DefaultClusterID != 1 {
		t.Errorf("expected old DefaultClusterID=1 after failed reload, got %d", cfg.DefaultClusterID)
	}
}
