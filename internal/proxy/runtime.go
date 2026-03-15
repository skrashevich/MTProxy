package proxy

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/skrashevicj/mtproxy/internal/config"
)

// RuntimeOptions содержит параметры запуска из CLI/конфига.
type RuntimeOptions struct {
	// Адрес для прослушивания клиентских соединений
	ListenAddr string

	// Адрес HTTP /stats эндпоинта (пустой = отключён)
	HTTPStatsAddr string

	// Путь к файлу конфигурации DC
	ConfigFile string

	// Максимум соединений на один секрет (0 = без ограничений)
	MaxConnectionsPerSecret int
}

// Runtime — центральный координатор прокси.
// Связывает все модули: Config → Ingress → Outbound → Router → Stats.
type Runtime struct {
	opts RuntimeOptions

	// Публичные компоненты
	Stats     *Stats
	Router    *Router
	DataPlane *DataPlane
	Outbound  *OutboundProxy

	// Секреты и proxy-тег
	Secrets  [][]byte
	ProxyTag []byte

	// Внутренние компоненты
	configMgr      *config.Manager
	clientIngress  *ClientIngressServer
	httpStats      *HTTPStatsServer
	hotReloader *HotReloader
	rateLimiter *RateLimiter
	shutdown    *GracefulShutdown

	cancelFn context.CancelFunc
}

// New создаёт Runtime из опций.
func New(opts RuntimeOptions, secrets [][]byte, proxyTag []byte, outboundCfg OutboundConfig) (*Runtime, error) {
	mgr := config.NewManager(opts.ConfigFile)
	if err := mgr.Load(); err != nil {
		return nil, fmt.Errorf("runtime: load config: %w", err)
	}

	rt := &Runtime{
		opts:      opts,
		Stats:     NewStats(),
		Secrets:   secrets,
		ProxyTag:  proxyTag,
		configMgr: mgr,
		shutdown:  NewGracefulShutdown(),
		Outbound:  NewOutboundProxy(outboundCfg),
	}
	return rt, nil
}

// Start запускает все компоненты и блокируется до сигнала завершения или отмены ctx.
func (rt *Runtime) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	rt.cancelFn = cancel
	defer cancel()

	if err := rt.bootstrapSequence(ctx); err != nil {
		return fmt.Errorf("runtime start: %w", err)
	}

	rt.clientIngress = NewClientIngressServer(rt.opts.ListenAddr, rt.Secrets, rt.DataPlane, rt.shutdown)
	log.Printf("runtime: listening on %s", rt.opts.ListenAddr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case sig := <-sigCh:
			log.Printf("runtime: received signal %s", sig)
			rt.Shutdown()
		case <-ctx.Done():
		}
	}()

	if err := rt.clientIngress.ListenAndServe(ctx); err != nil {
		return fmt.Errorf("runtime: ingress: %w", err)
	}
	return nil
}

// Shutdown выполняет graceful остановку всех компонентов.
func (rt *Runtime) Shutdown() {
	log.Println("runtime: shutting down")

	if rt.hotReloader != nil {
		rt.hotReloader.Stop()
	}
	if rt.httpStats != nil {
		rt.httpStats.Stop()
	}
	if rt.Outbound != nil {
		rt.Outbound.Close()
	}

	rt.shutdown.Shutdown(rt.cancelFn)
	rt.shutdown.Wait()

	log.Println("runtime: shutdown complete")
}

