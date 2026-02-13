package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultMinConnections = 4
	DefaultMaxConnections = 8

	MaxCfgClusters = 1024
	MaxCfgTargets  = 4096

	minTimeoutMS = 10
	maxTimeoutMS = 30000
)

type Target struct {
	ClusterID int
	Host      string
	Port      int

	MinConnections int
	MaxConnections int
}

type Cluster struct {
	ID      int
	Targets []Target
}

type Config struct {
	MinConnections int
	MaxConnections int
	TimeoutMS      int

	DefaultClusterID int
	HaveProxy        bool

	Targets  []Target
	Clusters []Cluster
}

func ParseFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("cannot read config file %q: %w", path, err)
	}
	return Parse(string(data))
}

func Parse(input string) (Config, error) {
	cfg := Config{
		MinConnections:   DefaultMinConnections,
		MaxConnections:   DefaultMaxConnections,
		TimeoutMS:        300,
		DefaultClusterID: 0,
	}

	cleaned := stripComments(input)
	if err := validateSemicolonTermination(cleaned); err != nil {
		return Config{}, err
	}
	chunks := strings.Split(cleaned, ";")

	clusterIndexByID := make(map[int]int)
	lastClusterID := 0
	haveAnyCluster := false

	for _, chunk := range chunks {
		stmt := strings.TrimSpace(chunk)
		if stmt == "" {
			continue
		}
		fields := strings.Fields(stmt)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "timeout":
			if len(fields) != 2 {
				return Config{}, fmt.Errorf("invalid timeout directive: %q", stmt)
			}
			ms, err := strconv.Atoi(fields[1])
			if err != nil {
				return Config{}, fmt.Errorf("invalid timeout value: %q", fields[1])
			}
			if ms < minTimeoutMS || ms > maxTimeoutMS {
				return Config{}, fmt.Errorf("invalid timeout: %d", ms)
			}
			cfg.TimeoutMS = ms
		case "min_connections":
			if len(fields) != 2 {
				return Config{}, fmt.Errorf("invalid min_connections directive: %q", stmt)
			}
			v, err := strconv.Atoi(fields[1])
			if err != nil {
				return Config{}, fmt.Errorf("invalid min_connections value: %q", fields[1])
			}
			if v < 1 || v > cfg.MaxConnections {
				return Config{}, fmt.Errorf("invalid min connections")
			}
			cfg.MinConnections = v
		case "max_connections":
			if len(fields) != 2 {
				return Config{}, fmt.Errorf("invalid max_connections directive: %q", stmt)
			}
			v, err := strconv.Atoi(fields[1])
			if err != nil {
				return Config{}, fmt.Errorf("invalid max_connections value: %q", fields[1])
			}
			if v < cfg.MinConnections || v > 1000 {
				return Config{}, fmt.Errorf("invalid max connections")
			}
			cfg.MaxConnections = v
		case "default":
			if len(fields) != 2 {
				return Config{}, fmt.Errorf("invalid default directive: %q", stmt)
			}
			id, err := parseTargetID(fields[1])
			if err != nil {
				return Config{}, err
			}
			cfg.DefaultClusterID = id
		case "proxy":
			if len(fields) != 2 {
				return Config{}, fmt.Errorf("invalid proxy directive: %q", stmt)
			}
			if len(cfg.Targets) >= MaxCfgTargets {
				return Config{}, fmt.Errorf("too many targets (%d)", len(cfg.Targets))
			}
			t, err := parseTarget(0, fields[1], cfg.MinConnections, cfg.MaxConnections)
			if err != nil {
				return Config{}, err
			}
			cfg.Targets = append(cfg.Targets, t)
			cfg.HaveProxy = true

			var errAdd error
			lastClusterID, haveAnyCluster, errAdd = addTargetToCluster(&cfg, clusterIndexByID, 0, t, lastClusterID, haveAnyCluster)
			if errAdd != nil {
				return Config{}, errAdd
			}
		case "proxy_for":
			if len(fields) != 3 {
				return Config{}, fmt.Errorf("invalid proxy_for directive: %q", stmt)
			}
			if len(cfg.Targets) >= MaxCfgTargets {
				return Config{}, fmt.Errorf("too many targets (%d)", len(cfg.Targets))
			}
			clusterID, err := parseTargetID(fields[1])
			if err != nil {
				return Config{}, err
			}
			t, err := parseTarget(clusterID, fields[2], cfg.MinConnections, cfg.MaxConnections)
			if err != nil {
				return Config{}, err
			}
			cfg.Targets = append(cfg.Targets, t)
			cfg.HaveProxy = true

			var errAdd error
			lastClusterID, haveAnyCluster, errAdd = addTargetToCluster(&cfg, clusterIndexByID, clusterID, t, lastClusterID, haveAnyCluster)
			if errAdd != nil {
				return Config{}, errAdd
			}
		default:
			return Config{}, fmt.Errorf("'proxy <ip>:<port>;' expected")
		}
	}

	if !cfg.HaveProxy || len(cfg.Clusters) == 0 {
		return Config{}, fmt.Errorf("expected to find a mtproto-proxy configuration with `proxy' directives")
	}

	return cfg, nil
}

func addTargetToCluster(
	cfg *Config,
	clusterIndexByID map[int]int,
	clusterID int,
	t Target,
	lastClusterID int,
	haveAnyCluster bool,
) (newLastClusterID int, newHaveAnyCluster bool, err error) {
	idx, ok := clusterIndexByID[clusterID]
	if !ok {
		if len(cfg.Clusters) >= MaxCfgClusters {
			return 0, false, fmt.Errorf("too many auth clusters")
		}
		cfg.Clusters = append(cfg.Clusters, Cluster{ID: clusterID, Targets: []Target{t}})
		clusterIndexByID[clusterID] = len(cfg.Clusters) - 1
		return clusterID, true, nil
	}

	if haveAnyCluster && lastClusterID != clusterID {
		return 0, false, fmt.Errorf("proxies for dc %d intermixed", clusterID)
	}
	cfg.Clusters[idx].Targets = append(cfg.Clusters[idx].Targets, t)
	return clusterID, true, nil
}

func stripComments(s string) string {
	var b strings.Builder
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		if p := strings.IndexByte(line, '#'); p >= 0 {
			line = line[:p]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func validateSemicolonTermination(cleaned string) error {
	if strings.TrimSpace(cleaned) == "" {
		return nil
	}
	if !strings.HasSuffix(strings.TrimSpace(cleaned), ";") {
		return fmt.Errorf("';' expected")
	}
	return nil
}

func parseTargetID(raw string) (int, error) {
	id64, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid target id")
	}
	if id64 < -0x8000 || id64 >= 0x8000 {
		return 0, fmt.Errorf("invalid target id (integer -32768..32767 expected)")
	}
	return int(id64), nil
}

func parseTarget(clusterID int, raw string, minConnections, maxConnections int) (Target, error) {
	host, port, err := splitHostPortLoose(raw)
	if err != nil {
		return Target{}, fmt.Errorf("invalid target format: %q", raw)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return Target{}, fmt.Errorf("invalid target port: %q", port)
	}
	if p <= 0 || p >= 65536 {
		return Target{}, fmt.Errorf("port number %d out of range", p)
	}
	return Target{
		ClusterID:      clusterID,
		Host:           host,
		Port:           p,
		MinConnections: minConnections,
		MaxConnections: maxConnections,
	}, nil
}

func splitHostPortLoose(raw string) (host, port string, err error) {
	if raw == "" {
		return "", "", fmt.Errorf("empty target")
	}
	if strings.HasPrefix(raw, "[") {
		end := strings.LastIndex(raw, "]:")
		if end <= 1 {
			return "", "", fmt.Errorf("invalid bracketed host:port")
		}
		host = raw[1:end]
		port = raw[end+2:]
		if host == "" || port == "" {
			return "", "", fmt.Errorf("invalid bracketed host:port")
		}
		return host, port, nil
	}

	sep := strings.LastIndexByte(raw, ':')
	if sep <= 0 || sep == len(raw)-1 {
		return "", "", fmt.Errorf("missing host or port")
	}
	host = raw[:sep]
	port = raw[sep+1:]
	return host, port, nil
}

func (c Config) ClusterByID(id int) (Cluster, bool) {
	for i := range c.Clusters {
		if c.Clusters[i].ID == id {
			return c.Clusters[i], true
		}
	}
	return Cluster{}, false
}

func (c Config) DefaultCluster() (Cluster, bool) {
	return c.ClusterByID(c.DefaultClusterID)
}
