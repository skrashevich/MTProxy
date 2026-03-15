package proxy

import (
	"fmt"
	"math/rand"
	"sync"

	"github.com/skrashevicj/mtproxy/internal/config"
)

// Router выбирает целевой backend для клиентского соединения.
// Соответствует логике choose_proxy_target() из mtproto-proxy.c.
type Router struct {
	mu  sync.RWMutex
	cfg *config.Config

	// Индекс round-robin на DC (dcID -> следующий индекс)
	rrIdx map[int]int
}

// NewRouter создаёт Router с начальной конфигурацией.
func NewRouter(cfg *config.Config) *Router {
	return &Router{
		cfg:   cfg,
		rrIdx: make(map[int]int),
	}
}

// Reload атомарно заменяет конфигурацию маршрутизатора.
func (r *Router) Reload(cfg *config.Config) {
	r.mu.Lock()
	r.cfg = cfg
	r.rrIdx = make(map[int]int)
	r.mu.Unlock()
}

// Route возвращает Target для заданного targetDC.
//
// Логика (из choose_proxy_target в C):
//   - Ищем кластер с id == targetDC.
//   - Если не найден — используем DefaultClusterID.
//   - Из кластера выбираем target случайным образом.
func (r *Router) Route(targetDC int) (Target, error) {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()

	if cfg == nil {
		return Target{}, fmt.Errorf("router: config not loaded")
	}

	cl, ok := cfg.Clusters[targetDC]
	if !ok || len(cl.Targets) == 0 {
		cl, ok = cfg.Clusters[cfg.DefaultClusterID]
		if !ok || len(cl.Targets) == 0 {
			return Target{}, fmt.Errorf("router: no targets for dc=%d and no default cluster", targetDC)
		}
	}

	idx := rand.Intn(len(cl.Targets))
	ct := cl.Targets[idx]
	return Target{Addr: ct.String()}, nil
}

// RouteRoundRobin выбирает target по round-robin.
func (r *Router) RouteRoundRobin(targetDC int) (Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg := r.cfg
	if cfg == nil {
		return Target{}, fmt.Errorf("router: config not loaded")
	}

	cl, ok := cfg.Clusters[targetDC]
	if !ok || len(cl.Targets) == 0 {
		cl, ok = cfg.Clusters[cfg.DefaultClusterID]
		if !ok || len(cl.Targets) == 0 {
			return Target{}, fmt.Errorf("router: no targets for dc=%d and no default cluster", targetDC)
		}
	}

	idx := r.rrIdx[cl.ID] % len(cl.Targets)
	r.rrIdx[cl.ID] = idx + 1

	ct := cl.Targets[idx]
	return Target{Addr: ct.String()}, nil
}
