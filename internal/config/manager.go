package config

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Snapshot struct {
	Config     Config
	LoadedAt   time.Time
	Bytes      int
	MD5Hex     string
	SourcePath string
}

type Manager struct {
	mu      sync.RWMutex
	path    string
	current *Snapshot

	checkCalls    atomic.Uint64
	reloadCalls   atomic.Uint64
	reloadSuccess atomic.Uint64

	lastErrMu sync.RWMutex
	lastErr   string
}

type ManagerStats struct {
	CheckCalls    uint64
	ReloadCalls   uint64
	ReloadSuccess uint64
	LastError     string
}

func NewManager(path string) *Manager {
	return &Manager{path: path}
}

func (m *Manager) Check() (Snapshot, error) {
	m.checkCalls.Add(1)
	data, err := os.ReadFile(m.path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("cannot re-read config file %s: %w", m.path, err)
	}

	cfg, err := Parse(string(data))
	if err != nil {
		return Snapshot{}, err
	}

	sum := md5.Sum(data)
	return Snapshot{
		Config:     cfg,
		LoadedAt:   time.Now().UTC(),
		Bytes:      len(data),
		MD5Hex:     hex.EncodeToString(sum[:]),
		SourcePath: m.path,
	}, nil
}

func (m *Manager) Reload() (Snapshot, error) {
	m.reloadCalls.Add(1)
	s, err := m.Check()
	if err != nil {
		m.setLastError(err.Error())
		return Snapshot{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = &s
	m.setLastError("")
	m.reloadSuccess.Add(1)
	return s, nil
}

func (m *Manager) Current() (Snapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return Snapshot{}, false
	}
	return *m.current, true
}

func (m *Manager) Stats() ManagerStats {
	return ManagerStats{
		CheckCalls:    m.checkCalls.Load(),
		ReloadCalls:   m.reloadCalls.Load(),
		ReloadSuccess: m.reloadSuccess.Load(),
		LastError:     m.getLastError(),
	}
}

func (m *Manager) setLastError(s string) {
	m.lastErrMu.Lock()
	defer m.lastErrMu.Unlock()
	m.lastErr = s
}

func (m *Manager) getLastError() string {
	m.lastErrMu.RLock()
	defer m.lastErrMu.RUnlock()
	return m.lastErr
}
