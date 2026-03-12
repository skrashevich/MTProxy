package proxy

import (
	"testing"

	"github.com/nicholasgasior/mtproxy/internal/config"
)

func makeTestConfig() *config.Config {
	return &config.Config{
		DefaultClusterID: 2,
		Clusters: map[int]*config.Cluster{
			1: {ID: 1, Targets: []config.Target{{Addr: "dc1.example.com", Port: 443}}},
			2: {ID: 2, Targets: []config.Target{
				{Addr: "dc2a.example.com", Port: 443},
				{Addr: "dc2b.example.com", Port: 443},
			}},
			5: {ID: 5, Targets: []config.Target{{Addr: "dc5.example.com", Port: 443}}},
		},
		Bytes: 100,
	}
}

func TestRouter_RouteKnownDC(t *testing.T) {
	r := NewRouter(makeTestConfig())
	target, err := r.Route(1)
	if err != nil {
		t.Fatalf("Route(1) error: %v", err)
	}
	if target.Addr != "dc1.example.com:443" {
		t.Errorf("target.Addr = %q, want dc1.example.com:443", target.Addr)
	}
}

func TestRouter_RouteUnknownDCFallback(t *testing.T) {
	r := NewRouter(makeTestConfig())
	// DC 99 не существует — должен вернуть default (DC 2)
	target, err := r.Route(99)
	if err != nil {
		t.Fatalf("Route(99) error: %v", err)
	}
	// default cluster = 2, адрес должен быть одним из dc2a или dc2b
	if target.Addr != "dc2a.example.com:443" && target.Addr != "dc2b.example.com:443" {
		t.Errorf("fallback target.Addr = %q, want dc2a or dc2b", target.Addr)
	}
}

func TestRouter_RouteRandomMultiTarget(t *testing.T) {
	r := NewRouter(makeTestConfig())
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		target, err := r.Route(2)
		if err != nil {
			t.Fatalf("Route(2) error: %v", err)
		}
		seen[target.Addr] = true
	}
	if !seen["dc2a.example.com:443"] {
		t.Error("dc2a never selected")
	}
	if !seen["dc2b.example.com:443"] {
		t.Error("dc2b never selected")
	}
}

func TestRouter_RouteRoundRobin(t *testing.T) {
	r := NewRouter(makeTestConfig())
	t1, _ := r.RouteRoundRobin(2)
	t2, _ := r.RouteRoundRobin(2)
	if t1.Addr == t2.Addr {
		t.Errorf("round-robin returned same addr twice: %s", t1.Addr)
	}
	t3, _ := r.RouteRoundRobin(2)
	if t3.Addr != t1.Addr {
		t.Errorf("round-robin wrap: got %s, want %s", t3.Addr, t1.Addr)
	}
}

func TestRouter_Reload(t *testing.T) {
	r := NewRouter(makeTestConfig())

	newCfg := &config.Config{
		DefaultClusterID: 10,
		Clusters: map[int]*config.Cluster{
			10: {ID: 10, Targets: []config.Target{{Addr: "new.example.com", Port: 8080}}},
		},
	}
	r.Reload(newCfg)

	target, err := r.Route(99)
	if err != nil {
		t.Fatalf("Route after reload error: %v", err)
	}
	if target.Addr != "new.example.com:8080" {
		t.Errorf("after reload, target.Addr = %q, want new.example.com:8080", target.Addr)
	}
}

func TestRouter_NilConfig(t *testing.T) {
	r := &Router{rrIdx: make(map[int]int)}
	_, err := r.Route(1)
	if err == nil {
		t.Error("Route with nil config should return error")
	}
}
