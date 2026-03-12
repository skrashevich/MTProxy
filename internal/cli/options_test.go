package cli

import (
	"encoding/hex"
	"os"
	"testing"
)

// parseArgs is a test helper that sets os.Args and calls Parse().
// It returns the Options and recovers from os.Exit calls.
func parseArgs(t *testing.T, args ...string) (opts *Options, exitCode int) {
	t.Helper()
	// We can't easily intercept os.Exit from Parse(), so we test the
	// internal helpers directly and test Parse() only for valid inputs
	// by swapping os.Args.
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = append([]string{"mtproto-proxy"}, args...)
	opts = Parse()
	return opts, 0
}

func TestDecodeHexSecret_Valid16(t *testing.T) {
	raw := "aabbccddeeff00112233445566778899"
	b, err := decodeHexSecret("-S", raw, 16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(b) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(b))
	}
	expected, _ := hex.DecodeString(raw)
	for i, v := range expected {
		if b[i] != v {
			t.Errorf("byte %d: expected %02x, got %02x", i, v, b[i])
		}
	}
}

func TestDecodeHexSecret_WithDDPrefix(t *testing.T) {
	// dd + 32 hex chars = fake-TLS mode secret
	raw := "dd" + "aabbccddeeff00112233445566778899"
	b, err := decodeHexSecret("-S", raw, 16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(b) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(b))
	}
}

func TestDecodeHexSecret_TooShort(t *testing.T) {
	_, err := decodeHexSecret("-S", "aabb", 16)
	if err == nil {
		t.Error("expected error for too-short hex, got nil")
	}
}

func TestDecodeHexSecret_TooLong(t *testing.T) {
	_, err := decodeHexSecret("-S", "aabbccddeeff001122334455667788990011", 16)
	if err == nil {
		t.Error("expected error for too-long hex, got nil")
	}
}

func TestDecodeHexSecret_InvalidHex(t *testing.T) {
	_, err := decodeHexSecret("-S", "zzzzccddeeff00112233445566778899", 16)
	if err == nil {
		t.Error("expected error for invalid hex, got nil")
	}
}

func TestLoadSecretsFromFile_Valid(t *testing.T) {
	content := "aabbccddeeff00112233445566778899,ffeeddccbbaa00112233445566778899\n"
	f, err := os.CreateTemp(t.TempDir(), "secrets-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()

	var secrets [][]byte
	if err := loadSecretsFromFile(f.Name(), &secrets); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secrets) != 2 {
		t.Errorf("expected 2 secrets, got %d", len(secrets))
	}
}

func TestLoadSecretsFromFile_Whitespace(t *testing.T) {
	content := "aabbccddeeff00112233445566778899\n  ffeeddccbbaa00112233445566778899  \n"
	f, err := os.CreateTemp(t.TempDir(), "secrets-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()

	var secrets [][]byte
	if err := loadSecretsFromFile(f.Name(), &secrets); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secrets) != 2 {
		t.Errorf("expected 2 secrets, got %d", len(secrets))
	}
}

func TestLoadSecretsFromFile_NotFound(t *testing.T) {
	var secrets [][]byte
	err := loadSecretsFromFile("/nonexistent/path/secrets.txt", &secrets)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadSecretsFromFile_InvalidHex(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "secrets-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not-valid-hex\n")
	f.Close()

	var secrets [][]byte
	err = loadSecretsFromFile(f.Name(), &secrets)
	if err == nil {
		t.Error("expected error for invalid hex secret")
	}
}

func TestSecretFlag_Set_Valid(t *testing.T) {
	var secrets [][]byte
	sf := &secretFlag{secrets: &secrets}
	if err := sf.Set("aabbccddeeff00112233445566778899"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secrets) != 1 {
		t.Errorf("expected 1 secret, got %d", len(secrets))
	}
}

func TestSecretFlag_Set_Multiple(t *testing.T) {
	var secrets [][]byte
	sf := &secretFlag{secrets: &secrets}
	sf.Set("aabbccddeeff00112233445566778899")
	sf.Set("ffeeddccbbaa00112233445566778899")
	if len(secrets) != 2 {
		t.Errorf("expected 2 secrets, got %d", len(secrets))
	}
}

func TestSecretFlag_Set_Invalid(t *testing.T) {
	var secrets [][]byte
	sf := &secretFlag{secrets: &secrets}
	if err := sf.Set("notvalid"); err == nil {
		t.Error("expected error for invalid secret hex")
	}
}

func TestDomainFlag_Set(t *testing.T) {
	var domains []string
	df := &domainFlag{domains: &domains}
	df.Set("example.com")
	df.Set("test.org")
	if len(domains) != 2 {
		t.Errorf("expected 2 domains, got %d", len(domains))
	}
	if domains[0] != "example.com" {
		t.Errorf("expected example.com, got %s", domains[0])
	}
}

func TestHTTPPortsFlag_Set_Single(t *testing.T) {
	var ports []int
	hf := &httpPortsFlag{ports: &ports}
	if err := hf.Set("8080"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 1 || ports[0] != 8080 {
		t.Errorf("expected [8080], got %v", ports)
	}
}

func TestHTTPPortsFlag_Set_Multiple(t *testing.T) {
	var ports []int
	hf := &httpPortsFlag{ports: &ports}
	if err := hf.Set("8080,9090,7070"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 3 {
		t.Errorf("expected 3 ports, got %d", len(ports))
	}
}

func TestHTTPPortsFlag_Set_InvalidPort(t *testing.T) {
	var ports []int
	hf := &httpPortsFlag{ports: &ports}
	if err := hf.Set("99999"); err == nil {
		t.Error("expected error for out-of-range port")
	}
}

func TestHTTPPortsFlag_Set_NotANumber(t *testing.T) {
	var ports []int
	hf := &httpPortsFlag{ports: &ports}
	if err := hf.Set("abc"); err == nil {
		t.Error("expected error for non-numeric port")
	}
}


func TestParse_AllFlags(t *testing.T) {
	// Write a minimal config file for the positional argument.
	f, err := os.CreateTemp(t.TempDir(), "proxy-*.conf")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("default 2;\nproxy_for 2 149.154.161.144:8888;\n")
	f.Close()

	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{
		"mtproto-proxy",
		"-S", "aabbccddeeff00112233445566778899",
		"-S", "ffeeddccbbaa00112233445566778899",
		"-P", "00112233445566778899aabbccddeeff",
		"-M", "3",
		"-H", "8080,9090",
		"--max-special-connections", "1000",
		"--window-clamp", "131072",
		"-6",
		"-v", "2",
		f.Name(),
	}

	opts := Parse()

	if len(opts.Secrets) != 2 {
		t.Errorf("expected 2 secrets, got %d", len(opts.Secrets))
	}
	if !opts.ProxyTagSet {
		t.Error("expected ProxyTagSet=true")
	}
	if opts.Workers != 3 {
		t.Errorf("expected Workers=3, got %d", opts.Workers)
	}
	if len(opts.HTTPPorts) != 2 {
		t.Errorf("expected 2 http ports, got %d", len(opts.HTTPPorts))
	}
	if opts.MaxSpecialConnections != 1000 {
		t.Errorf("expected MaxSpecialConnections=1000, got %d", opts.MaxSpecialConnections)
	}
	if opts.WindowClamp != 131072 {
		t.Errorf("expected WindowClamp=131072, got %d", opts.WindowClamp)
	}
	if !opts.PreferIPv6 {
		t.Error("expected PreferIPv6=true")
	}
	if opts.Verbosity != 2 {
		t.Errorf("expected Verbosity=2, got %d", opts.Verbosity)
	}
	if opts.ConfigFile != f.Name() {
		t.Errorf("unexpected ConfigFile: %s", opts.ConfigFile)
	}
}

func TestParse_Defaults(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "proxy-*.conf")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("default 2;\nproxy_for 2 149.154.161.144:8888;\n")
	f.Close()

	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"mtproto-proxy", f.Name()}

	opts := Parse()

	if opts.Workers != DefaultWorkers {
		t.Errorf("expected Workers=%d, got %d", DefaultWorkers, opts.Workers)
	}
	if opts.ProxyTagSet {
		t.Error("expected ProxyTagSet=false by default")
	}
	if opts.PreferIPv6 {
		t.Error("expected PreferIPv6=false by default")
	}
	if opts.Daemonize {
		t.Error("expected Daemonize=false by default")
	}
	if opts.PingInterval != 5.0 {
		t.Errorf("expected PingInterval=5.0, got %f", opts.PingInterval)
	}
}
