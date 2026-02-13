package proxy

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/TelegramMessenger/MTProxy/internal/config"
)

type Runtime struct {
	lifecycle *Lifecycle
	router    *Router
	forwarder *Forwarder
	logw      io.Writer

	healthMu      sync.RWMutex
	targetHealth  map[targetIdentity]bool
	healthChecker func(config.Target) bool
	randSource    targetRandSource

	logMu       sync.RWMutex
	logReopener func() error
}

func NewRuntime(lifecycle *Lifecycle, logw io.Writer) *Runtime {
	rt := &Runtime{
		lifecycle:    lifecycle,
		router:       NewRouter(),
		logw:         logw,
		targetHealth: make(map[targetIdentity]bool),
		healthChecker: func(config.Target) bool {
			return true
		},
		randSource: defaultRandSource{},
	}
	rt.forwarder = NewForwarder(rt)
	return rt
}

func (r *Runtime) Run(ctx context.Context, signals <-chan os.Signal) error {
	snapshot, warnings, err := r.lifecycle.LoadInitial()
	if err != nil {
		return err
	}
	r.applyConfig(snapshot.Config)

	for _, w := range warnings {
		fmt.Fprintln(r.logw, w)
	}
	fmt.Fprintf(
		r.logw,
		"runtime initialized: targets=%d clusters=%d bytes=%d md5=%s\n",
		len(snapshot.Config.Targets),
		len(snapshot.Config.Clusters),
		snapshot.Bytes,
		snapshot.MD5Hex,
	)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig, ok := <-signals:
			if !ok {
				return nil
			}
			action, nextSnapshot, nextWarnings, err := r.lifecycle.HandleSignal(sig)
			switch action {
			case SignalActionReload:
				if err != nil {
					fmt.Fprintf(r.logw, "configuration reload failed: %v\n", err)
					continue
				}
				r.applyConfig(nextSnapshot.Config)
				for _, w := range nextWarnings {
					fmt.Fprintln(r.logw, w)
				}
				fmt.Fprintf(
					r.logw,
					"configuration file %s re-read successfully (%d bytes parsed), new configuration active\n",
					nextSnapshot.SourcePath,
					nextSnapshot.Bytes,
				)
			case SignalActionShutdown:
				fmt.Fprintf(r.logw, "Terminated by %s.\n", signalName(sig))
				return nil
			case SignalActionLogRotate:
				reopened, err := r.reopenLog()
				if err != nil {
					fmt.Fprintf(r.logw, "SIGUSR1 log reopen failed: %v\n", err)
				} else if reopened {
					fmt.Fprintln(r.logw, "SIGUSR1 received: log file reopened.")
				} else {
					fmt.Fprintln(r.logw, "SIGUSR1 received: no log file configured, skipping reopen.")
				}
			}
		}
	}
}

func (r *Runtime) ChooseProxyTarget(targetDC int) (config.Target, error) {
	return r.router.ChooseProxyTarget(targetDC, 5, r.getHealthChecker(), r.randSource)
}

func (r *Runtime) ChooseProxyTargetDetailed(targetDC int) (ChooseResult, error) {
	return r.router.ChooseProxyTargetDetailed(targetDC, 5, r.getHealthChecker(), r.randSource)
}

func (r *Runtime) Forward(req ForwardRequest) (ForwardDecision, error) {
	return r.forwarder.Decide(req)
}

func (r *Runtime) ForwardStats() ForwardStats {
	return r.forwarder.Stats()
}

func (r *Runtime) SetHealthChecker(fn func(config.Target) bool) {
	if fn == nil {
		fn = func(config.Target) bool { return true }
	}
	r.healthMu.Lock()
	r.healthChecker = fn
	r.healthMu.Unlock()
}

func (r *Runtime) SetLogReopener(fn func() error) {
	r.logMu.Lock()
	r.logReopener = fn
	r.logMu.Unlock()
}

func (r *Runtime) getHealthChecker() func(config.Target) bool {
	return func(t config.Target) bool {
		return r.isTargetHealthy(t)
	}
}

func (r *Runtime) MarkTargetHealthy(t config.Target) {
	r.setTargetHealth(t, true)
}

func (r *Runtime) MarkTargetUnhealthy(t config.Target) {
	r.setTargetHealth(t, false)
}

func (r *Runtime) TargetHealth(t config.Target) (healthy bool, ok bool) {
	r.healthMu.RLock()
	defer r.healthMu.RUnlock()
	healthy, ok = r.targetHealth[makeTargetIdentity(t)]
	return healthy, ok
}

func (r *Runtime) TargetHealthStats() (healthy int, unhealthy int) {
	r.healthMu.RLock()
	defer r.healthMu.RUnlock()
	for _, h := range r.targetHealth {
		if h {
			healthy++
		} else {
			unhealthy++
		}
	}
	return healthy, unhealthy
}

func (r *Runtime) applyConfig(cfg config.Config) {
	r.router.Update(cfg)

	r.healthMu.Lock()
	defer r.healthMu.Unlock()
	next := make(map[targetIdentity]bool, len(cfg.Targets))
	for _, t := range cfg.Targets {
		id := makeTargetIdentity(t)
		if prev, ok := r.targetHealth[id]; ok {
			next[id] = prev
		} else {
			next[id] = true
		}
	}
	r.targetHealth = next
}

func (r *Runtime) isTargetHealthy(t config.Target) bool {
	r.healthMu.RLock()
	defer r.healthMu.RUnlock()
	id := makeTargetIdentity(t)
	healthy, ok := r.targetHealth[id]
	if !ok {
		healthy = true
	}
	return healthy && r.healthChecker(t)
}

func (r *Runtime) setTargetHealth(t config.Target, healthy bool) {
	r.healthMu.Lock()
	defer r.healthMu.Unlock()
	r.targetHealth[makeTargetIdentity(t)] = healthy
}

func (r *Runtime) reopenLog() (bool, error) {
	r.logMu.RLock()
	fn := r.logReopener
	r.logMu.RUnlock()
	if fn == nil {
		return false, nil
	}
	if err := fn(); err != nil {
		return false, err
	}
	return true, nil
}

type targetIdentity struct {
	ClusterID int
	Host      string
	Port      int
}

func makeTargetIdentity(t config.Target) targetIdentity {
	return targetIdentity{
		ClusterID: t.ClusterID,
		Host:      t.Host,
		Port:      t.Port,
	}
}

func signalName(sig os.Signal) string {
	switch sig {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGUSR1:
		return "SIGUSR1"
	default:
		return sig.String()
	}
}
