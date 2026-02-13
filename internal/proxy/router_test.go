package proxy

import (
	"testing"

	"github.com/TelegramMessenger/MTProxy/internal/config"
)

type seqRand struct {
	seq []int
	i   int
}

func (r *seqRand) Intn(n int) int {
	if len(r.seq) == 0 {
		return 0
	}
	v := r.seq[r.i%len(r.seq)] % n
	r.i++
	return v
}

func TestRouterRoundRobinPerCluster(t *testing.T) {
	r := NewRouter()
	r.Update(config.Config{
		DefaultClusterID: 1,
		Clusters: []config.Cluster{
			{
				ID: 1,
				Targets: []config.Target{
					{ClusterID: 1, Host: "a", Port: 1},
					{ClusterID: 1, Host: "b", Port: 2},
				},
			},
		},
	})

	t1, err := r.Select(1)
	if err != nil {
		t.Fatalf("select #1: %v", err)
	}
	t2, err := r.Select(1)
	if err != nil {
		t.Fatalf("select #2: %v", err)
	}
	t3, err := r.Select(1)
	if err != nil {
		t.Fatalf("select #3: %v", err)
	}

	if t1.Host != "a" || t2.Host != "b" || t3.Host != "a" {
		t.Fatalf("unexpected round robin order: %s, %s, %s", t1.Host, t2.Host, t3.Host)
	}
}

func TestRouterSelectWithDefaultFallback(t *testing.T) {
	r := NewRouter()
	r.Update(config.Config{
		DefaultClusterID: 2,
		Clusters: []config.Cluster{
			{
				ID: 2,
				Targets: []config.Target{
					{ClusterID: 2, Host: "default", Port: 443},
				},
			},
		},
	})

	tgt, err := r.SelectWithDefault(99)
	if err != nil {
		t.Fatalf("select with default: %v", err)
	}
	if tgt.Host != "default" {
		t.Fatalf("unexpected target host: %s", tgt.Host)
	}
}

func TestRouterUpdateResetsIndex(t *testing.T) {
	r := NewRouter()
	r.Update(config.Config{
		DefaultClusterID: 1,
		Clusters: []config.Cluster{
			{
				ID: 1,
				Targets: []config.Target{
					{ClusterID: 1, Host: "a", Port: 1},
					{ClusterID: 1, Host: "b", Port: 2},
				},
			},
		},
	})

	if _, err := r.Select(1); err != nil {
		t.Fatalf("select before update: %v", err)
	}
	r.Update(config.Config{
		DefaultClusterID: 1,
		Clusters: []config.Cluster{
			{
				ID: 1,
				Targets: []config.Target{
					{ClusterID: 1, Host: "x", Port: 10},
					{ClusterID: 1, Host: "y", Port: 20},
				},
			},
		},
	})

	tgt, err := r.Select(1)
	if err != nil {
		t.Fatalf("select after update: %v", err)
	}
	if tgt.Host != "x" {
		t.Fatalf("expected router index reset to first target, got %s", tgt.Host)
	}
}

func TestRouterChooseProxyTargetByDCCWithFallback(t *testing.T) {
	r := NewRouter()
	r.Update(config.Config{
		DefaultClusterID: 2,
		Clusters: []config.Cluster{
			{
				ID: 2,
				Targets: []config.Target{
					{ClusterID: 2, Host: "default", Port: 443},
				},
			},
			{
				ID: 4,
				Targets: []config.Target{
					{ClusterID: 4, Host: "dc4", Port: 443},
				},
			},
		},
	})

	t1, err := r.ChooseProxyTarget(4, 5, nil, nil)
	if err != nil {
		t.Fatalf("choose dc4 target: %v", err)
	}
	if t1.Host != "dc4" {
		t.Fatalf("unexpected dc4 host: %s", t1.Host)
	}

	t2, err := r.ChooseProxyTarget(99, 5, nil, nil)
	if err != nil {
		t.Fatalf("choose fallback target: %v", err)
	}
	if t2.Host != "default" {
		t.Fatalf("unexpected fallback host: %s", t2.Host)
	}

	detail, err := r.ChooseProxyTargetDetailed(99, 5, nil, nil)
	if err != nil {
		t.Fatalf("choose fallback detail target: %v", err)
	}
	if !detail.UsedDefault {
		t.Fatalf("expected UsedDefault=true for missing cluster fallback")
	}
	if detail.ResolvedClusterID != 2 {
		t.Fatalf("unexpected resolved cluster id: %d", detail.ResolvedClusterID)
	}
}

func TestRouterChooseProxyTargetHealthyAttempts(t *testing.T) {
	r := NewRouter()
	r.Update(config.Config{
		DefaultClusterID: 1,
		Clusters: []config.Cluster{
			{
				ID: 1,
				Targets: []config.Target{
					{ClusterID: 1, Host: "a", Port: 1},
					{ClusterID: 1, Host: "b", Port: 2},
				},
			},
		},
	})

	checker := func(t config.Target) bool {
		return t.Host == "b"
	}

	tgt, err := r.ChooseProxyTarget(1, 3, checker, &seqRand{seq: []int{0, 0, 1}})
	if err != nil {
		t.Fatalf("choose healthy target: %v", err)
	}
	if tgt.Host != "b" {
		t.Fatalf("unexpected chosen host: %s", tgt.Host)
	}
}

func TestRouterChooseProxyTargetUnhealthy(t *testing.T) {
	r := NewRouter()
	r.Update(config.Config{
		DefaultClusterID: 1,
		Clusters: []config.Cluster{
			{
				ID: 1,
				Targets: []config.Target{
					{ClusterID: 1, Host: "a", Port: 1},
				},
			},
		},
	})

	_, err := r.ChooseProxyTarget(1, 2, func(config.Target) bool { return false }, nil)
	if err == nil {
		t.Fatalf("expected no healthy targets error")
	}
}
