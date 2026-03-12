package cli

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultPort    = 8888
	DefaultWorkers = 1
)

// Options holds all parsed CLI flags, matching the C mtproto-proxy flags exactly.
type Options struct {
	// -S / --mtproto-secret — 16-byte secrets as hex strings (32 hex chars each).
	// May be specified multiple times. Also loaded from --mtproto-secret-file.
	Secrets [][]byte

	// -P / --proxy-tag — 16-byte proxy tag as hex string (32 hex chars).
	ProxyTag    []byte
	ProxyTagSet bool

	// -M / --slaves — number of worker processes (default 1).
	Workers int

	// -H / --http-ports — comma-separated list of HTTP listen ports.
	HTTPPorts []int

	// --aes-pwd — path to file with AES RPC secret.
	AESPwdFile string

	// --http-stats — enable HTTP stats endpoint on the main port.
	HTTPStats bool

	// --max-special-connections / -C — max accepted client connections per worker.
	MaxSpecialConnections int

	// --window-clamp / -W — TCP window clamp for client connections.
	WindowClamp int

	// -u / --user — username for setuid.
	Username string

	// -6 — prefer IPv6.
	PreferIPv6 bool

	// -v / --verbosity — verbosity level.
	Verbosity int

	// -d / --daemonize — daemonize.
	Daemonize bool

	// --domain / -D — TLS domain(s), disables other transports when set.
	Domains []string

	// --ping-interval / -T — ping interval in seconds.
	PingInterval float64

	// --mtproto-secret-file — path to file with secrets.
	SecretFile string

	// Positional argument: path to proxy-multi.conf.
	ConfigFile string
}

// secretFlag is a flag.Value that accumulates multiple -S values.
type secretFlag struct {
	secrets *[][]byte
}

func (s *secretFlag) String() string { return "" }
func (s *secretFlag) Set(v string) error {
	b, err := decodeHexSecret("--mtproto-secret", v, 16)
	if err != nil {
		return err
	}
	*s.secrets = append(*s.secrets, b)
	return nil
}

// domainFlag accumulates multiple -D values.
type domainFlag struct {
	domains *[]string
}

func (d *domainFlag) String() string { return "" }
func (d *domainFlag) Set(v string) error {
	*d.domains = append(*d.domains, v)
	return nil
}

// httpPortsFlag parses comma-separated port list.
type httpPortsFlag struct {
	ports *[]int
}

func (h *httpPortsFlag) String() string { return "" }
func (h *httpPortsFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := strconv.Atoi(part)
		if err != nil || p <= 0 || p >= 65536 {
			return fmt.Errorf("invalid port %q", part)
		}
		*h.ports = append(*h.ports, p)
	}
	return nil
}

// Parse parses os.Args[1:] and returns the filled Options.
// On error it prints usage and calls os.Exit(2).
func Parse() *Options {
	opts := &Options{
		Workers:      DefaultWorkers,
		PingInterval: 5.0,
	}

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.Usage = func() { PrintUsage(fs) }

	// -S / --mtproto-secret (repeatable)
	sf := &secretFlag{secrets: &opts.Secrets}
	fs.Var(sf, "S", "16-byte secret in hex (32 hex chars); may be repeated")
	fs.Var(sf, "mtproto-secret", "16-byte secret in hex (32 hex chars); may be repeated")

	// --mtproto-secret-file
	fs.StringVar(&opts.SecretFile, "mtproto-secret-file", "", "path to file with mtproto secrets (comma or whitespace-separated)")

	// -P / --proxy-tag
	proxyTagStr := ""
	fs.StringVar(&proxyTagStr, "P", "", "16-byte proxy tag in hex (32 hex chars)")
	fs.StringVar(&proxyTagStr, "proxy-tag", "", "16-byte proxy tag in hex (32 hex chars)")

	// -M / --slaves
	fs.IntVar(&opts.Workers, "M", DefaultWorkers, "number of worker processes")
	fs.IntVar(&opts.Workers, "slaves", DefaultWorkers, "number of worker processes")

	// -H / --http-ports
	hpf := &httpPortsFlag{ports: &opts.HTTPPorts}
	fs.Var(hpf, "H", "comma-separated list of HTTP listen ports")
	fs.Var(hpf, "http-ports", "comma-separated list of HTTP listen ports")

	// --aes-pwd
	fs.StringVar(&opts.AESPwdFile, "aes-pwd", "", "path to AES secret file for RPC")

	// --http-stats
	fs.BoolVar(&opts.HTTPStats, "http-stats", false, "enable HTTP stats endpoint")

	// -C / --max-special-connections
	fs.IntVar(&opts.MaxSpecialConnections, "C", 0, "max client connections per worker (0 = unlimited)")
	fs.IntVar(&opts.MaxSpecialConnections, "max-special-connections", 0, "max client connections per worker (0 = unlimited)")

	// -W / --window-clamp
	fs.IntVar(&opts.WindowClamp, "W", 0, "TCP window clamp for client connections (0 = default 131072)")
	fs.IntVar(&opts.WindowClamp, "window-clamp", 0, "TCP window clamp for client connections")

	// -u / --user
	fs.StringVar(&opts.Username, "u", "", "username for setuid")
	fs.StringVar(&opts.Username, "user", "", "username for setuid")

	// -6
	fs.BoolVar(&opts.PreferIPv6, "6", false, "prefer IPv6 for outbound connections")

	// -v / --verbosity
	fs.IntVar(&opts.Verbosity, "v", 0, "verbosity level (0=silent, higher=more)")
	fs.IntVar(&opts.Verbosity, "verbosity", 0, "verbosity level")

	// -d / --daemonize
	fs.BoolVar(&opts.Daemonize, "d", false, "daemonize")
	fs.BoolVar(&opts.Daemonize, "daemonize", false, "daemonize")

	// -D / --domain (repeatable)
	df := &domainFlag{domains: &opts.Domains}
	fs.Var(df, "D", "TLS domain; disables non-TLS transport when set; may be repeated")
	fs.Var(df, "domain", "TLS domain; disables non-TLS transport when set; may be repeated")

	// -T / --ping-interval
	fs.Float64Var(&opts.PingInterval, "T", 5.0, "ping interval in seconds")
	fs.Float64Var(&opts.PingInterval, "ping-interval", 5.0, "ping interval in seconds")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		PrintUsage(fs)
		os.Exit(2)
	}

	// Positional: config file
	args := fs.Args()
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "error: exactly one positional argument required: path to proxy-multi.conf\n")
		PrintUsage(fs)
		os.Exit(2)
	}
	opts.ConfigFile = args[0]

	// Parse proxy-tag
	if proxyTagStr != "" {
		b, err := decodeHexSecret("--proxy-tag", proxyTagStr, 16)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(2)
		}
		opts.ProxyTag = b
		opts.ProxyTagSet = true
	}

	// Load secrets from file if specified
	if opts.SecretFile != "" {
		if err := loadSecretsFromFile(opts.SecretFile, &opts.Secrets); err != nil {
			fmt.Fprintf(os.Stderr, "error loading secret file: %v\n", err)
			os.Exit(2)
		}
	}

	return opts
}

// decodeHexSecret decodes a hex string into exactly wantBytes bytes.
func decodeHexSecret(flag, value string, wantBytes int) ([]byte, error) {
	// Support "dd" prefix for fake-TLS mode (skip first 2 chars)
	v := value
	if len(v) == wantBytes*2+2 && strings.HasPrefix(strings.ToLower(v), "dd") {
		v = v[2:]
	}
	if len(v) != wantBytes*2 {
		return nil, fmt.Errorf("%s: expected %d hex chars, got %d in %q", flag, wantBytes*2, len(v), value)
	}
	b, err := hex.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid hex %q: %w", flag, value, err)
	}
	return b, nil
}

// loadSecretsFromFile reads secrets from a file (comma or whitespace separated).
func loadSecretsFromFile(filename string, secrets *[][]byte) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("open %s: %w", filename, err)
	}
	content := string(data)
	// Replace commas with spaces
	content = strings.ReplaceAll(content, ",", " ")
	for _, tok := range strings.Fields(content) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		b, err := decodeHexSecret("--mtproto-secret-file", tok, 16)
		if err != nil {
			return err
		}
		*secrets = append(*secrets, b)
	}
	return nil
}
