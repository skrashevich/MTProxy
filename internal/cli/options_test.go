package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHelp(t *testing.T) {
	opts, err := Parse([]string{"--help"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if !opts.ShowHelp {
		t.Fatalf("expected ShowHelp=true")
	}
}

func TestParseWithSecretsAndProxyTag(t *testing.T) {
	opts, err := Parse([]string{
		"-S", "0123456789abcdef0123456789abcdef",
		"--mtproto-secret", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"-P", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"proxy-multi.conf",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.ConfigFile != "proxy-multi.conf" {
		t.Fatalf("unexpected config file: %q", opts.ConfigFile)
	}
	if got := len(opts.Secrets); got != 2 {
		t.Fatalf("unexpected secrets count: %d", got)
	}
	if opts.ProxyTag == nil {
		t.Fatalf("expected proxy tag to be parsed")
	}
}

func TestParseInvalidSecret(t *testing.T) {
	_, err := Parse([]string{"-S", "zz", "proxy-multi.conf"})
	if err == nil {
		t.Fatalf("expected error for invalid secret")
	}
}

func TestParseRequiresConfigFile(t *testing.T) {
	_, err := Parse([]string{"-S", "0123456789abcdef0123456789abcdef"})
	if !errors.Is(err, ErrConfigFileRequired) {
		t.Fatalf("expected ErrConfigFileRequired, got: %v", err)
	}
}

func TestParseInterspersedDockerStyleArgs(t *testing.T) {
	opts, err := Parse([]string{
		"-p", "2398",
		"--http-stats",
		"-H", "443",
		"-M", "2",
		"-C", "60000",
		"--aes-pwd", "/etc/telegram/hello",
		"/etc/telegram/backend.conf",
		"--allow-skip-dh",
		"--nat-info", "10.0.0.1:1.2.3.4",
		"-S", "0123456789abcdef0123456789abcdef",
		"-P", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.ConfigFile != "/etc/telegram/backend.conf" {
		t.Fatalf("unexpected config file: %q", opts.ConfigFile)
	}
	if !opts.HTTPStats {
		t.Fatalf("expected http stats enabled")
	}
	if opts.Workers != 2 {
		t.Fatalf("unexpected workers: %d", opts.Workers)
	}
	if opts.MaxSpecialConnections != 60000 {
		t.Fatalf("unexpected max special connections: %d", opts.MaxSpecialConnections)
	}
	if !opts.AllowSkipDH {
		t.Fatalf("expected allow-skip-dh enabled")
	}
	if got := len(opts.NATInfoRules); got != 1 {
		t.Fatalf("unexpected nat-info rules count: %d", got)
	}
	if got := len(opts.HTTPPorts); got != 1 || opts.HTTPPorts[0] != 443 {
		t.Fatalf("unexpected http ports: %v", opts.HTTPPorts)
	}
}

func TestParseSecretFile(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secret.list")
	content := "# comment\n0123456789abcdef0123456789abcdef, aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"
	if err := os.WriteFile(secretFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	opts, err := Parse([]string{
		"--mtproto-secret-file", secretFile,
		"proxy-multi.conf",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got := len(opts.Secrets); got != 2 {
		t.Fatalf("unexpected secrets count: %d", got)
	}
}

func TestParseSecretFileComplexFormatting(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secret.list")
	content := "  # comment-only line\n0123456789abcdef0123456789abcdef,\tAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA # inline\n\nbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"
	if err := os.WriteFile(secretFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	opts, err := Parse([]string{
		"--mtproto-secret-file", secretFile,
		"proxy-multi.conf",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got := len(opts.Secrets); got != 3 {
		t.Fatalf("unexpected secrets count: %d", got)
	}
}

func TestParseSecretFileEmptyRejected(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secret.list")
	content := "# only comments\n\n  # another\n"
	if err := os.WriteFile(secretFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	_, err := Parse([]string{
		"--mtproto-secret-file", secretFile,
		"proxy-multi.conf",
	})
	if err == nil {
		t.Fatalf("expected parse error for empty secret file")
	}
}

func TestParseTooManySecrets(t *testing.T) {
	args := make([]string, 0, maxSecrets*2+1)
	for i := 0; i < maxSecrets+1; i++ {
		args = append(args, "-S", "0123456789abcdef0123456789abcdef")
	}
	args = append(args, "proxy-multi.conf")

	_, err := Parse(args)
	if !errors.Is(err, ErrTooManySecrets) {
		t.Fatalf("expected ErrTooManySecrets, got: %v", err)
	}
}

func TestParseInvalidHTTPPorts(t *testing.T) {
	_, err := Parse([]string{"-H", "0443", "proxy-multi.conf"})
	if err == nil {
		t.Fatalf("expected error for invalid -H value")
	}
}

func TestParseInvalidNATInfo(t *testing.T) {
	_, err := Parse([]string{"--nat-info", "10.0.0.1", "proxy-multi.conf"})
	if err == nil {
		t.Fatalf("expected error for invalid --nat-info")
	}
}

func TestParseEngineBaseOptions(t *testing.T) {
	opts, err := Parse([]string{
		"-b", "1024",
		"-c", "40000",
		"-d1",
		"-u", "nobody",
		"-l", "/tmp/mtproxy.log",
		"proxy-multi.conf",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.Backlog != 1024 {
		t.Fatalf("unexpected backlog: %d", opts.Backlog)
	}
	if opts.MaxConn != 40000 {
		t.Fatalf("unexpected max conn: %d", opts.MaxConn)
	}
	if !opts.Daemonize {
		t.Fatalf("expected daemonize enabled")
	}
	if opts.Username != "nobody" {
		t.Fatalf("unexpected username: %q", opts.Username)
	}
	if opts.LogFile != "/tmp/mtproxy.log" {
		t.Fatalf("unexpected log file: %q", opts.LogFile)
	}
}

func TestParseEngineBaseOptionsLongForm(t *testing.T) {
	opts, err := Parse([]string{
		"--backlog", "2048",
		"--connections", "20000",
		"--daemonize=0",
		"--user", "root",
		"--log", "/var/log/mtproxy.log",
		"proxy-multi.conf",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.Backlog != 2048 || opts.MaxConn != 20000 {
		t.Fatalf("unexpected backlog/conn options: %+v", opts)
	}
	if opts.Daemonize {
		t.Fatalf("expected daemonize disabled")
	}
	if opts.Username != "root" {
		t.Fatalf("unexpected username: %q", opts.Username)
	}
	if opts.LogFile != "/var/log/mtproxy.log" {
		t.Fatalf("unexpected log file: %q", opts.LogFile)
	}
}

func TestParseAdditionalEngineOptions(t *testing.T) {
	opts, err := Parse([]string{
		"--disable-tcp",
		"--crc32c",
		"--force-dh",
		"-D", "example.org",
		"--max-accept-rate", "100",
		"--max-dh-accept-rate", "10",
		"--address", "127.0.0.1",
		"--nice", "5",
		"--msg-buffers-size", "256m",
		"proxy-multi.conf",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if !opts.DisableTCP || !opts.UseCRC32C || !opts.ForceDH {
		t.Fatalf("expected bool engine options to be enabled: %+v", opts)
	}
	if got := len(opts.Domains); got != 1 || opts.Domains[0] != "example.org" {
		t.Fatalf("unexpected domains value: %v", opts.Domains)
	}
	if opts.MaxAcceptRate != 100 || opts.MaxDHAcceptRate != 10 {
		t.Fatalf("unexpected accept rate options: %+v", opts)
	}
	if opts.BindAddress != "127.0.0.1" {
		t.Fatalf("unexpected bind address: %q", opts.BindAddress)
	}
	if !opts.NiceSet || opts.NiceValue != 5 {
		t.Fatalf("unexpected nice settings: %+v", opts)
	}
	if opts.MsgBuffersSizeRaw != "256m" {
		t.Fatalf("unexpected msg-buffers-size value: %q", opts.MsgBuffersSizeRaw)
	}
	if opts.MsgBuffersSizeBytes != int64(256)<<20 {
		t.Fatalf("unexpected msg-buffers-size parsed bytes: %d", opts.MsgBuffersSizeBytes)
	}
}

func TestParseShortDomainWithInlineValue(t *testing.T) {
	opts, err := Parse([]string{
		"-Dexample.com",
		"proxy-multi.conf",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got := len(opts.Domains); got != 1 || opts.Domains[0] != "example.com" {
		t.Fatalf("unexpected domains value: %v", opts.Domains)
	}
}

func TestParsePortRange(t *testing.T) {
	opts, err := Parse([]string{"-p", "2000:2010", "proxy-multi.conf"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.StartPort != 2000 || opts.EndPort != 2010 {
		t.Fatalf("unexpected port range: %d:%d", opts.StartPort, opts.EndPort)
	}
}

func TestParseInvalidPortRange(t *testing.T) {
	_, err := Parse([]string{"-p", "2010:2000", "proxy-multi.conf"})
	if err == nil {
		t.Fatalf("expected error for invalid -p range")
	}
}

func TestParseTooManyNATRules(t *testing.T) {
	args := []string{"proxy-multi.conf"}
	for i := 0; i < maxNATInfoRules+1; i++ {
		args = append(args, "--nat-info", "10.0.0.1:1.2.3.4")
	}
	_, err := Parse(args)
	if err == nil {
		t.Fatalf("expected error for too many nat-info rules")
	}
}

func TestParseRejectsUnexpectedValueForNoArgLongFlags(t *testing.T) {
	for _, arg := range []string{
		"--help=1",
		"--ipv6=1",
		"--http-stats=1",
		"--allow-skip-dh=1",
		"--disable-tcp=1",
		"--crc32c=1",
		"--force-dh=1",
	} {
		_, err := Parse([]string{arg, "proxy-multi.conf"})
		if err == nil {
			t.Fatalf("expected parse error for %q", arg)
		}
	}
}

func TestParseDockerRunStyleWithSecretFileAndTag(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretFile, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	opts, err := Parse([]string{
		"-p", "2398",
		"--http-stats",
		"-H", "443",
		"-M", "2",
		"-C", "60000",
		"--aes-pwd", "/etc/telegram/hello-explorers-how-are-you-doing",
		"-u", "root",
		"/etc/telegram/backend.conf",
		"--allow-skip-dh",
		"--nat-info", "10.0.0.2:203.0.113.10",
		"--mtproto-secret-file", secretFile,
		"-P", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.ConfigFile != "/etc/telegram/backend.conf" {
		t.Fatalf("unexpected config file: %q", opts.ConfigFile)
	}
	if !opts.HTTPStats || opts.Workers != 2 || opts.MaxSpecialConnections != 60000 {
		t.Fatalf("unexpected docker style options: %+v", opts)
	}
	if got := len(opts.Secrets); got != 1 {
		t.Fatalf("unexpected secrets count: %d", got)
	}
	if opts.ProxyTag == nil {
		t.Fatalf("expected proxy tag to be parsed")
	}
}

func TestParseDockerRunStyleWithoutTag(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretFile, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	opts, err := Parse([]string{
		"-p", "2398",
		"--http-stats",
		"-H", "443",
		"-M", "4",
		"--aes-pwd", "/etc/telegram/hello-explorers-how-are-you-doing",
		"/etc/telegram/backend.conf",
		"--allow-skip-dh",
		"--nat-info", "10.0.0.2:203.0.113.10",
		"--mtproto-secret-file", secretFile,
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.ProxyTag != nil {
		t.Fatalf("expected nil proxy tag when -P is omitted")
	}
	if got := len(opts.Secrets); got != 1 {
		t.Fatalf("unexpected secrets count: %d", got)
	}
}

func TestParseMsgBuffersSizeSuffixes(t *testing.T) {
	tests := []struct {
		raw  string
		want int64
	}{
		{raw: "1024", want: 1024},
		{raw: "64k", want: 64 << 10},
		{raw: "64K", want: 64 << 10},
		{raw: "8m", want: 8 << 20},
		{raw: "2g", want: 2 << 30},
		{raw: "1t", want: 1 << 40},
	}
	for _, tc := range tests {
		opts, err := Parse([]string{"--msg-buffers-size", tc.raw, "proxy-multi.conf"})
		if err != nil {
			t.Fatalf("parse %q failed: %v", tc.raw, err)
		}
		if opts.MsgBuffersSizeBytes != tc.want {
			t.Fatalf("parse %q unexpected bytes: got=%d want=%d", tc.raw, opts.MsgBuffersSizeBytes, tc.want)
		}
	}
}

func TestParseMsgBuffersSizeInvalid(t *testing.T) {
	for _, raw := range []string{
		"",
		"abc",
		"12x",
		"k",
	} {
		_, err := Parse([]string{"--msg-buffers-size", raw, "proxy-multi.conf"})
		if err == nil {
			t.Fatalf("expected parse error for --msg-buffers-size %q", raw)
		}
	}
}
