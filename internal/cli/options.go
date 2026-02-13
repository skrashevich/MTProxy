package cli

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

var (
	ErrConfigFileRequired = errors.New("exactly one <config-file> argument is required")
	ErrTooManySecrets     = errors.New("too many mtproto secrets")
)

const (
	maxSecrets          = 128
	maxWorkers          = 256
	maxHTTPListenPorts  = 128
	maxNATInfoRules     = 16
	defaultPingInterval = 5.0
)

type Options struct {
	ShowHelp bool

	Verbosity    int
	EnableIPv6   bool
	LocalPortRaw string
	LocalPort    int
	StartPort    int
	EndPort      int
	Backlog      int
	MaxConn      int
	LogFile      string
	Username     string
	Daemonize    bool
	NiceSet      bool
	NiceValue    int

	MsgBuffersSizeRaw   string
	MsgBuffersSizeBytes int64

	AESPwdFile      string
	AllowSkipDH     bool
	DisableTCP      bool
	UseCRC32C       bool
	ForceDH         bool
	MaxAcceptRate   int
	MaxDHAcceptRate int
	BindAddress     string
	NATInfoRules    []NATInfoRule

	HTTPStats             bool
	MaxSpecialConnections int
	WindowClamp           int
	HTTPPorts             []int
	Workers               int
	PingInterval          float64
	Domains               []string

	ConfigFile string
	Secrets    [][16]byte
	ProxyTag   *[16]byte
}

type NATInfoRule struct {
	Local  netip.Addr
	Global netip.Addr
}

func Parse(args []string) (Options, error) {
	opts := Options{
		PingInterval: defaultPingInterval,
	}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}

		if strings.HasPrefix(arg, "--") && len(arg) > 2 {
			name, value, hasValue := splitLongOption(arg[2:])
			if err := parseLongOption(&opts, name, value, hasValue, args, &i); err != nil {
				return Options{}, err
			}
			continue
		}

		if strings.HasPrefix(arg, "-") && arg != "-" {
			if err := parseShortOptions(&opts, arg[1:], args, &i); err != nil {
				return Options{}, err
			}
			continue
		}

		positional = append(positional, arg)
	}

	if len(opts.Secrets) > maxSecrets {
		return Options{}, ErrTooManySecrets
	}
	if opts.Workers < 0 || opts.Workers > maxWorkers {
		return Options{}, fmt.Errorf("workers out of range: %d (expected 0..%d)", opts.Workers, maxWorkers)
	}
	if len(opts.HTTPPorts) > maxHTTPListenPorts {
		return Options{}, fmt.Errorf("too many http ports: %d (max %d)", len(opts.HTTPPorts), maxHTTPListenPorts)
	}
	if len(opts.NATInfoRules) > maxNATInfoRules {
		return Options{}, fmt.Errorf("too many rules in --nat-info")
	}
	if err := parseLocalPortRange(&opts); err != nil {
		return Options{}, err
	}
	if opts.ShowHelp {
		return opts, nil
	}
	if len(positional) != 1 {
		return Options{}, ErrConfigFileRequired
	}

	opts.ConfigFile = positional[0]
	return opts, nil
}

func splitLongOption(raw string) (name, value string, hasValue bool) {
	if p := strings.IndexByte(raw, '='); p >= 0 {
		return raw[:p], raw[p+1:], true
	}
	return raw, "", false
}

func parseLongOption(opts *Options, name, value string, hasValue bool, args []string, i *int) error {
	switch name {
	case "help":
		if err := noValueExpected(name, hasValue); err != nil {
			return err
		}
		opts.ShowHelp = true
		return nil
	case "verbosity":
		if !hasValue {
			opts.Verbosity++
			return nil
		}
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid --verbosity value: %w", err)
		}
		opts.Verbosity = v
		return nil
	case "user":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		opts.Username = v
		return nil
	case "log":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		opts.LogFile = v
		return nil
	case "daemonize":
		if !hasValue {
			opts.Daemonize = !opts.Daemonize
			return nil
		}
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid --daemonize value: %w", err)
		}
		opts.Daemonize = v != 0
		return nil
	case "backlog":
		return parseLongInt(name, value, hasValue, &opts.Backlog, args, i)
	case "connections":
		return parseLongInt(name, value, hasValue, &opts.MaxConn, args, i)
	case "nice":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid --nice value: %w", err)
		}
		opts.NiceSet = true
		opts.NiceValue = n
		return nil
	case "msg-buffers-size":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		bytes, err := parseMemoryLimit(v)
		if err != nil {
			return fmt.Errorf("invalid --msg-buffers-size value: %w", err)
		}
		opts.MsgBuffersSizeRaw = v
		opts.MsgBuffersSizeBytes = bytes
		return nil
	case "port":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		opts.LocalPortRaw = v
		return nil
	case "aes-pwd":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		opts.AESPwdFile = v
		return nil
	case "ipv6":
		if err := noValueExpected(name, hasValue); err != nil {
			return err
		}
		opts.EnableIPv6 = true
		return nil
	case "allow-skip-dh":
		if err := noValueExpected(name, hasValue); err != nil {
			return err
		}
		opts.AllowSkipDH = true
		return nil
	case "disable-tcp":
		if err := noValueExpected(name, hasValue); err != nil {
			return err
		}
		opts.DisableTCP = true
		return nil
	case "crc32c":
		if err := noValueExpected(name, hasValue); err != nil {
			return err
		}
		opts.UseCRC32C = true
		return nil
	case "force-dh":
		if err := noValueExpected(name, hasValue); err != nil {
			return err
		}
		opts.ForceDH = true
		return nil
	case "max-accept-rate":
		return parseLongInt(name, value, hasValue, &opts.MaxAcceptRate, args, i)
	case "max-dh-accept-rate":
		return parseLongInt(name, value, hasValue, &opts.MaxDHAcceptRate, args, i)
	case "address":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		opts.BindAddress = v
		return nil
	case "nat-info":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		rule, err := parseNATInfo(v)
		if err != nil {
			return err
		}
		opts.NATInfoRules = append(opts.NATInfoRules, rule)
		return nil
	case "http-stats":
		if err := noValueExpected(name, hasValue); err != nil {
			return err
		}
		opts.HTTPStats = true
		return nil
	case "mtproto-secret":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		return addSecret(opts, v)
	case "mtproto-secret-file":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		return addSecretsFromFile(opts, v)
	case "proxy-tag":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		return setProxyTag(opts, v)
	case "domain":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		opts.Domains = append(opts.Domains, v)
		return nil
	case "max-special-connections":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid --max-special-connections value: %w", err)
		}
		if n < 0 {
			n = 0
		}
		opts.MaxSpecialConnections = n
		return nil
	case "window-clamp":
		return parseLongInt(name, value, hasValue, &opts.WindowClamp, args, i)
	case "http-ports":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		ports, err := parseHTTPPorts(v)
		if err != nil {
			return err
		}
		opts.HTTPPorts = append(opts.HTTPPorts, ports...)
		return nil
	case "slaves":
		return parseLongInt(name, value, hasValue, &opts.Workers, args, i)
	case "ping-interval":
		v, err := longValue(name, value, hasValue, args, i)
		if err != nil {
			return err
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("invalid --ping-interval value: %w", err)
		}
		if f <= 0 {
			f = defaultPingInterval
		}
		opts.PingInterval = f
		return nil
	default:
		return fmt.Errorf("unrecognized option --%s", name)
	}
}

func noValueExpected(name string, hasValue bool) error {
	if hasValue {
		return fmt.Errorf("option --%s does not take a value", name)
	}
	return nil
}

func parseShortOptions(opts *Options, body string, args []string, i *int) error {
	for p := 0; p < len(body); p++ {
		switch body[p] {
		case 'h':
			opts.ShowHelp = true
		case '6':
			opts.EnableIPv6 = true
		case 'v':
			if p+1 < len(body) && isDigits(body[p+1:]) {
				n, err := strconv.Atoi(body[p+1:])
				if err != nil {
					return fmt.Errorf("invalid -v value: %w", err)
				}
				opts.Verbosity = n
				p = len(body)
			} else {
				opts.Verbosity++
			}
		case 'u':
			v, consumed, err := shortValue("u", body, p, args, i)
			if err != nil {
				return err
			}
			opts.Username = v
			if consumed {
				p = len(body)
			}
			return nil
		case 'l':
			v, consumed, err := shortValue("l", body, p, args, i)
			if err != nil {
				return err
			}
			opts.LogFile = v
			if consumed {
				p = len(body)
			}
			return nil
		case 'd':
			if p+1 < len(body) && isDigits(body[p+1:]) {
				n, err := strconv.Atoi(body[p+1:])
				if err != nil {
					return fmt.Errorf("invalid -d value: %w", err)
				}
				opts.Daemonize = n != 0
				p = len(body)
			} else {
				opts.Daemonize = !opts.Daemonize
			}
		case 'b':
			v, consumed, err := shortValue("b", body, p, args, i)
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid -b value: %w", err)
			}
			opts.Backlog = n
			if consumed {
				p = len(body)
			}
			return nil
		case 'c':
			v, consumed, err := shortValue("c", body, p, args, i)
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid -c value: %w", err)
			}
			opts.MaxConn = n
			if consumed {
				p = len(body)
			}
			return nil
		case 'p':
			v, consumed, err := shortValue("p", body, p, args, i)
			if err != nil {
				return err
			}
			opts.LocalPortRaw = v
			if consumed {
				p = len(body)
			}
			return nil
		case 'C':
			v, consumed, err := shortValue("C", body, p, args, i)
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid -C value: %w", err)
			}
			if n < 0 {
				n = 0
			}
			opts.MaxSpecialConnections = n
			if consumed {
				p = len(body)
			}
			return nil
		case 'W':
			v, consumed, err := shortValue("W", body, p, args, i)
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid -W value: %w", err)
			}
			opts.WindowClamp = n
			if consumed {
				p = len(body)
			}
			return nil
		case 'H':
			v, consumed, err := shortValue("H", body, p, args, i)
			if err != nil {
				return err
			}
			ports, err := parseHTTPPorts(v)
			if err != nil {
				return err
			}
			opts.HTTPPorts = append(opts.HTTPPorts, ports...)
			if consumed {
				p = len(body)
			}
			return nil
		case 'M':
			v, consumed, err := shortValue("M", body, p, args, i)
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid -M value: %w", err)
			}
			opts.Workers = n
			if consumed {
				p = len(body)
			}
			return nil
		case 'T':
			v, consumed, err := shortValue("T", body, p, args, i)
			if err != nil {
				return err
			}
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("invalid -T value: %w", err)
			}
			if f <= 0 {
				f = defaultPingInterval
			}
			opts.PingInterval = f
			if consumed {
				p = len(body)
			}
			return nil
		case 'S':
			v, consumed, err := shortValue("S", body, p, args, i)
			if err != nil {
				return err
			}
			if err := addSecret(opts, v); err != nil {
				return err
			}
			if consumed {
				p = len(body)
			}
			return nil
		case 'D':
			v, consumed, err := shortValue("D", body, p, args, i)
			if err != nil {
				return err
			}
			opts.Domains = append(opts.Domains, v)
			if consumed {
				p = len(body)
			}
			return nil
		case 'P':
			v, consumed, err := shortValue("P", body, p, args, i)
			if err != nil {
				return err
			}
			if err := setProxyTag(opts, v); err != nil {
				return err
			}
			if consumed {
				p = len(body)
			}
			return nil
		default:
			return fmt.Errorf("unrecognized option -%c", body[p])
		}
	}
	return nil
}

func shortValue(name, body string, p int, args []string, i *int) (string, bool, error) {
	if p+1 < len(body) {
		return body[p+1:], true, nil
	}
	if *i+1 >= len(args) {
		return "", false, fmt.Errorf("option -%s requires a value", name)
	}
	*i += 1
	return args[*i], false, nil
}

func longValue(name, value string, hasValue bool, args []string, i *int) (string, error) {
	if hasValue {
		return value, nil
	}
	if *i+1 >= len(args) {
		return "", fmt.Errorf("option --%s requires a value", name)
	}
	*i += 1
	return args[*i], nil
}

func parseLongInt(name, value string, hasValue bool, target *int, args []string, i *int) error {
	raw, err := longValue(name, value, hasValue, args, i)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("invalid --%s value: %w", name, err)
	}
	*target = n
	return nil
}

func parseHTTPPorts(raw string) ([]int, error) {
	if raw == "" {
		return nil, errors.New("http ports list is empty")
	}
	parts := strings.Split(raw, ",")
	ports := make([]int, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 || p[0] < '1' || p[0] > '9' {
			return nil, fmt.Errorf("invalid http port: %q", p)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n >= 65536 {
			return nil, fmt.Errorf("invalid http port: %q", p)
		}
		ports = append(ports, n)
	}
	return ports, nil
}

func parseNATInfo(raw string) (NATInfoRule, error) {
	var out NATInfoRule
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return out, fmt.Errorf("invalid --nat-info format, expected local:global")
	}
	local, err := netip.ParseAddr(parts[0])
	if err != nil || !local.Is4() {
		return out, fmt.Errorf("invalid local addr in --nat-info: %q", parts[0])
	}
	global, err := netip.ParseAddr(parts[1])
	if err != nil || !global.Is4() {
		return out, fmt.Errorf("invalid global addr in --nat-info: %q", parts[1])
	}
	out.Local = local
	out.Global = global
	return out, nil
}

func parseLocalPortRange(opts *Options) error {
	if opts.LocalPortRaw == "" {
		return nil
	}
	raw := opts.LocalPortRaw
	if !strings.Contains(raw, ":") {
		p, err := strconv.Atoi(raw)
		if err != nil || p <= 0 || p >= 65536 {
			return fmt.Errorf("invalid -p/--port value: %q", raw)
		}
		opts.LocalPort = p
		return nil
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid -p/--port range: %q", raw)
	}
	startPort, err1 := strconv.Atoi(parts[0])
	endPort, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return fmt.Errorf("invalid -p/--port range: %q", raw)
	}
	if startPort <= 0 || endPort <= 0 || startPort > endPort || endPort >= 65536 {
		return fmt.Errorf("invalid -p/--port range: %q", raw)
	}
	opts.StartPort = startPort
	opts.EndPort = endPort
	return nil
}

func addSecret(opts *Options, raw string) error {
	if len(opts.Secrets) >= maxSecrets {
		return ErrTooManySecrets
	}
	parsed, err := parseHex16(raw)
	if err != nil {
		return fmt.Errorf("invalid -S/--mtproto-secret value: %w", err)
	}
	opts.Secrets = append(opts.Secrets, parsed)
	return nil
}

func addSecretsFromFile(opts *Options, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("cannot open mtproto secret file %q: %w", filename, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	loaded := 0
	for sc.Scan() {
		line := sc.Text()
		if p := strings.IndexByte(line, '#'); p >= 0 {
			line = line[:p]
		}
		for _, token := range splitSecretTokens(line) {
			if err := addSecret(opts, token); err != nil {
				return err
			}
			loaded++
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("failed reading mtproto secret file %q: %w", filename, err)
	}
	if loaded == 0 {
		return fmt.Errorf("mtproto secret file %q does not contain secrets", filename)
	}
	return nil
}

func splitSecretTokens(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
}

func setProxyTag(opts *Options, raw string) error {
	parsed, err := parseHex16(raw)
	if err != nil {
		return fmt.Errorf("invalid -P/--proxy-tag value: %w", err)
	}
	opts.ProxyTag = &parsed
	return nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseHex16(raw string) ([16]byte, error) {
	var out [16]byte
	if len(raw) != 32 {
		return out, fmt.Errorf("expected exactly 32 hex chars, got %d", len(raw))
	}
	_, err := hex.Decode(out[:], []byte(raw))
	if err != nil {
		return out, fmt.Errorf("not a valid hex string")
	}
	return out, nil
}

func parseMemoryLimit(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}

	suffix := byte(0)
	numPart := s
	last := s[len(s)-1]
	if (last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') {
		suffix = last
		numPart = s[:len(s)-1]
		if numPart == "" {
			return 0, fmt.Errorf("missing numeric part")
		}
	}

	n, err := strconv.ParseInt(numPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric part")
	}
	if suffix == 0 {
		return n, nil
	}

	shift := 0
	switch suffix | 0x20 {
	case 'k':
		shift = 10
	case 'm':
		shift = 20
	case 'g':
		shift = 30
	case 't':
		shift = 40
	default:
		return 0, fmt.Errorf("unknown suffix %q", string(suffix))
	}

	if shift == 0 {
		return n, nil
	}
	if n > math.MaxInt64>>shift || n < math.MinInt64>>shift {
		return 0, fmt.Errorf("value overflow")
	}
	return n << shift, nil
}
