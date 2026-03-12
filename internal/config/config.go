package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Target represents a single backend server address.
type Target struct {
	Addr string
	Port int
}

func (t Target) String() string {
	return fmt.Sprintf("%s:%d", t.Addr, t.Port)
}

// Cluster represents a group of backend targets for a single DC ID.
type Cluster struct {
	ID      int
	Targets []Target
}

// Config holds the parsed proxy-multi.conf configuration.
type Config struct {
	// Clusters maps DC ID to cluster. Negative DC IDs are IPv6 clusters.
	Clusters         map[int]*Cluster
	DefaultClusterID int
	// Raw bytes read, for md5
	Bytes int
}

// ParseConfig reads and parses a proxy-multi.conf style configuration file.
//
// Format:
//
//	default <dc_id>;
//	proxy_for <dc_id> <host>:<port>;
//
// Lines starting with '#' are comments.
func ParseConfig(filename string) (*Config, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", filename, err)
	}
	defer f.Close()

	cfg := &Config{
		Clusters:         make(map[int]*Cluster),
		DefaultClusterID: 2, // telegram default
	}

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		cfg.Bytes += len(scanner.Bytes()) + 1

		// strip comment
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}
		// strip trailing semicolon
		line = strings.TrimSuffix(line, ";")
		line = strings.TrimSpace(line)

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "default":
			if len(fields) < 2 {
				return nil, fmt.Errorf("%s:%d: 'default' requires a DC id", filename, lineNo)
			}
			id, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid DC id %q: %w", filename, lineNo, fields[1], err)
			}
			cfg.DefaultClusterID = id

		case "proxy_for", "proxy":
			if len(fields) < 3 {
				return nil, fmt.Errorf("%s:%d: 'proxy_for' requires dc_id and addr:port", filename, lineNo)
			}
			dcID, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid DC id %q: %w", filename, lineNo, fields[1], err)
			}
			addrPort := fields[2]
			host, portStr, err := splitHostPort(addrPort)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid addr:port %q: %w", filename, lineNo, addrPort, err)
			}
			port, err := strconv.Atoi(portStr)
			if err != nil || port <= 0 || port >= 65536 {
				return nil, fmt.Errorf("%s:%d: invalid port %q", filename, lineNo, portStr)
			}

			cl, ok := cfg.Clusters[dcID]
			if !ok {
				cl = &Cluster{ID: dcID}
				cfg.Clusters[dcID] = cl
			}
			cl.Targets = append(cl.Targets, Target{Addr: host, Port: port})

		default:
			// skip unknown directives (timeout, min_connections, etc.)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading config %s: %w", filename, err)
	}
	if len(cfg.Clusters) == 0 {
		return nil, fmt.Errorf("config %s: no proxy_for entries found", filename)
	}
	return cfg, nil
}

// splitHostPort handles both IPv6 [::1]:port and IPv4 host:port.
func splitHostPort(s string) (host, port string, err error) {
	if len(s) == 0 {
		return "", "", fmt.Errorf("empty address")
	}
	if s[0] == '[' {
		end := strings.LastIndex(s, "]")
		if end < 0 {
			return "", "", fmt.Errorf("missing ']' in %q", s)
		}
		host = s[1:end]
		rest := s[end+1:]
		if len(rest) == 0 || rest[0] != ':' {
			return "", "", fmt.Errorf("missing port in %q", s)
		}
		port = rest[1:]
		return host, port, nil
	}
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("missing port in %q", s)
	}
	return s[:idx], s[idx+1:], nil
}
