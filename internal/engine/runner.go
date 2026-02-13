package engine

import (
	"context"
	"io"
	"os"

	"github.com/TelegramMessenger/MTProxy/internal/config"
	"github.com/TelegramMessenger/MTProxy/internal/proxy"
)

type Runner interface {
	Run(ctx context.Context, signals <-chan os.Signal) error
}

type ProxyRunner struct {
	runtime *proxy.Runtime
}

func NewProxyRunner(lifecycle *proxy.Lifecycle, logw io.Writer) *ProxyRunner {
	return &ProxyRunner{
		runtime: proxy.NewRuntime(lifecycle, logw),
	}
}

func (r *ProxyRunner) Run(ctx context.Context, signals <-chan os.Signal) error {
	return r.runtime.Run(ctx, signals)
}

func (r *ProxyRunner) Forward(req proxy.ForwardRequest) (proxy.ForwardDecision, error) {
	return r.runtime.Forward(req)
}

func (r *ProxyRunner) StatsSnapshot() proxy.RuntimeStats {
	return r.runtime.StatsSnapshot()
}

func (r *ProxyRunner) SetHealthChecker(fn func(config.Target) bool) {
	r.runtime.SetHealthChecker(fn)
}

func (r *ProxyRunner) Runtime() *proxy.Runtime {
	return r.runtime
}
