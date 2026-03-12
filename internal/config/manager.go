package config

import (
	"fmt"
	"log"
	"sync"
)

// Manager provides thread-safe config loading and reload.
type Manager struct {
	mu       sync.RWMutex
	filename string
	current  *Config
}

// NewManager creates a new ConfigManager for the given config file.
// It does not load the config immediately; call Load() first.
func NewManager(filename string) *Manager {
	return &Manager{filename: filename}
}

// Load reads and parses the configuration file, replacing the current config.
func (m *Manager) Load() error {
	cfg, err := ParseConfig(m.filename)
	if err != nil {
		return fmt.Errorf("config load: %w", err)
	}
	m.mu.Lock()
	m.current = cfg
	m.mu.Unlock()
	log.Printf("config loaded from %s (%d bytes, %d clusters)", m.filename, cfg.Bytes, len(cfg.Clusters))
	return nil
}

// Reload reloads the configuration file. If parsing fails, the current config
// remains unchanged.
func (m *Manager) Reload() error {
	cfg, err := ParseConfig(m.filename)
	if err != nil {
		log.Printf("config reload failed, keeping old config: %v", err)
		return err
	}
	m.mu.Lock()
	m.current = cfg
	m.mu.Unlock()
	log.Printf("config reloaded from %s (%d bytes, %d clusters)", m.filename, cfg.Bytes, len(cfg.Clusters))
	return nil
}

// Get returns the current config. Safe for concurrent use.
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}
