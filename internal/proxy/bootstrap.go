package proxy

import (
	"context"
	"fmt"
	"log"
)

// bootstrapSequence запускает компоненты в порядке зависимостей.
//
// Порядок:
//  1. Router (зависит от Config)
//  2. RateLimiter
//  3. DataPlane (зависит от Router, Outbound, Stats)
//  4. HTTPStatsServer (зависит от Stats)
//  5. HotReloader (зависит от Config, Router)
func (rt *Runtime) bootstrapSequence(ctx context.Context) error {
	cfg := rt.configMgr.Get()
	if cfg == nil {
		return fmt.Errorf("bootstrap: config not loaded")
	}

	// 1. Router
	rt.Router = NewRouter(cfg)
	log.Printf("bootstrap: router initialized with %d clusters", len(cfg.Clusters))

	// 2. RateLimiter
	rt.rateLimiter = NewRateLimiter(rt.opts.MaxConnectionsPerSecret)
	log.Printf("bootstrap: rate limiter initialized (max=%d per secret)", rt.opts.MaxConnectionsPerSecret)

	// 3. DataPlane
	rt.DataPlane = NewDataPlane(rt.Router, rt.Outbound, rt.Stats, rt.ProxyTag)
	log.Println("bootstrap: data plane initialized")

	// 4. HTTPStatsServer
	if rt.opts.HTTPStatsAddr != "" {
		rt.httpStats = NewHTTPStatsServer(
			rt.opts.HTTPStatsAddr,
			rt.Stats,
			len(rt.Secrets),
			rt.ProxyTag,
			"mtproxy-go-0.1",
		)
		if err := rt.httpStats.Start(); err != nil {
			return fmt.Errorf("bootstrap: http stats: %w", err)
		}
		log.Printf("bootstrap: http stats listening on %s", rt.opts.HTTPStatsAddr)
	}

	// 5. HotReloader
	rt.hotReloader = NewHotReloader(rt.configMgr, rt.Router)
	rt.hotReloader.Start()
	log.Println("bootstrap: hot reloader started")

	return nil
}
