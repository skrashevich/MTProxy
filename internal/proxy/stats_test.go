package proxy

import (
	"sync"
	"testing"
)

func TestStats_ActiveConnections(t *testing.T) {
	s := NewStats()

	s.IncActiveConnections()
	s.IncActiveConnections()
	if got := s.ActiveConnections; got != 2 {
		t.Errorf("ActiveConnections = %d, want 2", got)
	}
	if got := s.TotalConnections; got != 2 {
		t.Errorf("TotalConnections = %d, want 2", got)
	}

	s.DecActiveConnections()
	if got := s.ActiveConnections; got != 1 {
		t.Errorf("ActiveConnections after dec = %d, want 1", got)
	}
	// TotalConnections не уменьшается
	if got := s.TotalConnections; got != 2 {
		t.Errorf("TotalConnections after dec = %d, want 2", got)
	}
}

func TestStats_ByteCounters(t *testing.T) {
	s := NewStats()
	s.AddBytesIn(100)
	s.AddBytesOut(200)
	snap := s.Snapshot(0)
	if snap["bytes_in"] != 100 {
		t.Errorf("bytes_in = %d, want 100", snap["bytes_in"])
	}
	if snap["bytes_out"] != 200 {
		t.Errorf("bytes_out = %d, want 200", snap["bytes_out"])
	}
}

func TestStats_PerSecret(t *testing.T) {
	s := NewStats()
	s.IncSecretConnections(0)
	s.IncSecretConnections(0)
	s.IncSecretConnections(1)

	if got := s.GetSecretConnections(0); got != 2 {
		t.Errorf("secret 0 connections = %d, want 2", got)
	}
	if got := s.GetSecretConnections(1); got != 1 {
		t.Errorf("secret 1 connections = %d, want 1", got)
	}

	s.DecSecretConnections(0)
	if got := s.GetSecretConnections(0); got != 1 {
		t.Errorf("secret 0 connections after dec = %d, want 1", got)
	}
}

func TestStats_Concurrent(t *testing.T) {
	s := NewStats()
	var wg sync.WaitGroup
	n := 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			s.IncActiveConnections()
			s.AddBytesIn(1)
			s.AddBytesOut(1)
			s.IncForwardedQuery()
		}()
	}
	wg.Wait()

	if s.TotalConnections != int64(n) {
		t.Errorf("TotalConnections = %d, want %d", s.TotalConnections, n)
	}
}

func TestStats_Snapshot(t *testing.T) {
	s := NewStats()
	s.IncActiveConnections()
	s.IncSecretConnections(0)
	s.IncSecretAuthKeys(0)

	snap := s.Snapshot(2)

	if snap["active_connections"] != 1 {
		t.Errorf("snapshot active_connections = %d, want 1", snap["active_connections"])
	}
	if snap["secret_1_active_connections"] != 1 {
		t.Errorf("snapshot secret_1_active_connections = %d, want 1", snap["secret_1_active_connections"])
	}
	if snap["secret_1_active_auth_keys"] != 1 {
		t.Errorf("snapshot secret_1_active_auth_keys = %d, want 1", snap["secret_1_active_auth_keys"])
	}
	// secret_2 не трогали
	if snap["secret_2_active_connections"] != 0 {
		t.Errorf("snapshot secret_2_active_connections = %d, want 0", snap["secret_2_active_connections"])
	}
}
