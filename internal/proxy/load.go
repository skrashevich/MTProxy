package proxy

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nicholasgasior/mtproxy/internal/config"
)

// HotReloader слушает SIGHUP и перезагружает конфигурацию.
// Соответствует обработке SIGHUP + reload_config() из engine-signals.c.
type HotReloader struct {
	manager *config.Manager
	router  *Router
	stopCh  chan struct{}
}

// NewHotReloader создаёт HotReloader, связывающий ConfigManager с Router.
func NewHotReloader(manager *config.Manager, router *Router) *HotReloader {
	return &HotReloader{
		manager: manager,
		router:  router,
		stopCh:  make(chan struct{}),
	}
}

// Start запускает горутину, ожидающую SIGHUP.
func (h *HotReloader) Start() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)

	go func() {
		defer signal.Stop(sigCh)
		for {
			select {
			case <-h.stopCh:
				return
			case sig := <-sigCh:
				log.Printf("received signal %s, reloading config", sig)
				h.reload()
			}
		}
	}()
}

// Stop останавливает HotReloader.
func (h *HotReloader) Stop() {
	close(h.stopCh)
}

// reload выполняет перезагрузку конфигурации и обновляет Router.
func (h *HotReloader) reload() {
	if err := h.manager.Reload(); err != nil {
		log.Printf("hot reload failed: %v", err)
		return
	}
	cfg := h.manager.Get()
	h.router.Reload(cfg)
	log.Printf("hot reload complete: %d clusters", len(cfg.Clusters))
}
