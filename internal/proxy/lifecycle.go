package proxy

import (
	"os"
	"sync"
	"syscall"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

type SignalAction string

const (
	SignalActionNone      SignalAction = "none"
	SignalActionReload    SignalAction = "reload"
	SignalActionShutdown  SignalAction = "shutdown"
	SignalActionLogRotate SignalAction = "log_rotate"
)

type Lifecycle struct {
	manager *config.Manager
	opts    cli.Options

	mu         sync.RWMutex
	snapshot   config.Snapshot
	hasCurrent bool
	warnings   []string
}

func NewLifecycle(manager *config.Manager, opts cli.Options) *Lifecycle {
	return &Lifecycle{
		manager: manager,
		opts:    opts,
	}
}

func (l *Lifecycle) LoadInitial() (config.Snapshot, []string, error) {
	return l.reload()
}

func (l *Lifecycle) Reload() (config.Snapshot, []string, error) {
	return l.reload()
}

func (l *Lifecycle) Current() (config.Snapshot, []string, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if !l.hasCurrent {
		return config.Snapshot{}, nil, false
	}
	outWarnings := append([]string(nil), l.warnings...)
	return l.snapshot, outWarnings, true
}

func (l *Lifecycle) ManagerStats() config.ManagerStats {
	return l.manager.Stats()
}

func (l *Lifecycle) HandleSignal(sig os.Signal) (SignalAction, config.Snapshot, []string, error) {
	switch sig {
	case syscall.SIGHUP:
		s, warnings, err := l.Reload()
		if err != nil {
			return SignalActionReload, config.Snapshot{}, nil, err
		}
		return SignalActionReload, s, warnings, nil
	case syscall.SIGTERM, syscall.SIGINT:
		return SignalActionShutdown, config.Snapshot{}, nil, nil
	case syscall.SIGUSR1:
		return SignalActionLogRotate, config.Snapshot{}, nil, nil
	default:
		return SignalActionNone, config.Snapshot{}, nil, nil
	}
}

func (l *Lifecycle) reload() (config.Snapshot, []string, error) {
	snapshot, bootstrap, err := LoadAndValidate(l.manager, l.opts)
	if err != nil {
		return config.Snapshot{}, nil, err
	}

	l.mu.Lock()
	l.snapshot = snapshot
	l.warnings = append([]string(nil), bootstrap.Warnings...)
	l.hasCurrent = true
	l.mu.Unlock()

	return snapshot, append([]string(nil), bootstrap.Warnings...), nil
}
