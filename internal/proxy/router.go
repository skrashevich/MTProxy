package proxy

import (
	"fmt"
	"math/rand"
	"sync"

	"github.com/TelegramMessenger/MTProxy/internal/config"
)

type Router struct {
	mu sync.RWMutex

	defaultClusterID int
	clusters         map[int][]config.Target
	nextIndex        map[int]int
}

type RouterStats struct {
	DefaultClusterID int
	Clusters         int
	Targets          int
}

type ChooseResult struct {
	Target            config.Target
	RequestedCluster  int
	ResolvedClusterID int
	UsedDefault       bool
}

type targetRandSource interface {
	Intn(n int) int
}

type defaultRandSource struct{}

func (defaultRandSource) Intn(n int) int {
	return rand.Intn(n)
}

func NewRouter() *Router {
	return &Router{
		clusters:  make(map[int][]config.Target),
		nextIndex: make(map[int]int),
	}
}

func (r *Router) Update(cfg config.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.defaultClusterID = cfg.DefaultClusterID
	r.clusters = make(map[int][]config.Target, len(cfg.Clusters))
	r.nextIndex = make(map[int]int, len(cfg.Clusters))

	for _, cl := range cfg.Clusters {
		targets := make([]config.Target, len(cl.Targets))
		copy(targets, cl.Targets)
		r.clusters[cl.ID] = targets
		r.nextIndex[cl.ID] = 0
	}
}

func (r *Router) Stats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	targets := 0
	for _, list := range r.clusters {
		targets += len(list)
	}
	return RouterStats{
		DefaultClusterID: r.defaultClusterID,
		Clusters:         len(r.clusters),
		Targets:          targets,
	}
}

func (r *Router) Select(clusterID int) (config.Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	targets, resolvedClusterID, _, err := r.getTargetsLocked(clusterID, false)
	if err != nil {
		return config.Target{}, err
	}
	return r.selectRoundRobinLocked(resolvedClusterID, targets), nil
}

func (r *Router) SelectWithDefault(clusterID int) (config.Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	targets, resolvedClusterID, _, err := r.getTargetsLocked(clusterID, true)
	if err != nil {
		return config.Target{}, err
	}
	return r.selectRoundRobinLocked(resolvedClusterID, targets), nil
}

func (r *Router) SelectDefault() (config.Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	targets, resolvedClusterID, _, err := r.getTargetsLocked(r.defaultClusterID, false)
	if err != nil {
		return config.Target{}, err
	}
	return r.selectRoundRobinLocked(resolvedClusterID, targets), nil
}

func (r *Router) ChooseProxyTarget(clusterID int, attempts int, isHealthy func(config.Target) bool, rnd targetRandSource) (config.Target, error) {
	res, err := r.ChooseProxyTargetDetailed(clusterID, attempts, isHealthy, rnd)
	if err != nil {
		return config.Target{}, err
	}
	return res.Target, nil
}

func (r *Router) ChooseProxyTargetDetailed(clusterID int, attempts int, isHealthy func(config.Target) bool, rnd targetRandSource) (ChooseResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	targets, resolvedClusterID, usedDefault, err := r.getTargetsLocked(clusterID, true)
	if err != nil {
		return ChooseResult{}, err
	}
	if attempts <= 0 {
		attempts = 1
	}
	if isHealthy == nil {
		isHealthy = func(config.Target) bool { return true }
	}
	if rnd == nil {
		rnd = defaultRandSource{}
	}

	for i := 0; i < attempts; i++ {
		t := targets[rnd.Intn(len(targets))]
		if isHealthy(t) {
			return ChooseResult{
				Target:            t,
				RequestedCluster:  clusterID,
				ResolvedClusterID: resolvedClusterID,
				UsedDefault:       usedDefault,
			}, nil
		}
	}
	return ChooseResult{}, fmt.Errorf("no healthy targets available for cluster %d", clusterID)
}

func (r *Router) getTargetsLocked(clusterID int, fallbackDefault bool) ([]config.Target, int, bool, error) {
	originalClusterID := clusterID
	targets, ok := r.clusters[clusterID]
	usedDefault := false
	if !ok && fallbackDefault {
		clusterID = r.defaultClusterID
		targets, ok = r.clusters[clusterID]
		usedDefault = true
	}
	if !ok || len(targets) == 0 {
		return nil, 0, false, fmt.Errorf("cluster %d has no targets", clusterID)
	}
	if clusterID == originalClusterID {
		usedDefault = false
	}
	return targets, clusterID, usedDefault, nil
}

func (r *Router) selectRoundRobinLocked(clusterID int, targets []config.Target) config.Target {
	i := r.nextIndex[clusterID]
	if i < 0 || i >= len(targets) {
		i = 0
	}
	t := targets[i]
	r.nextIndex[clusterID] = (i + 1) % len(targets)
	return t
}
