package proxy

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

const (
	// drainTimeout — максимальное время ожидания завершения соединений при shutdown.
	drainTimeout = 5 * time.Second
)

// GracefulShutdown координирует остановку всех компонентов прокси.
// Соответствует mtfront_on_exit() + SIGTERM handling из engine.c.
type GracefulShutdown struct {
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	done     chan struct{}
	once     sync.Once
}

// NewGracefulShutdown создаёт новый экземпляр GracefulShutdown.
func NewGracefulShutdown() *GracefulShutdown {
	return &GracefulShutdown{
		conns: make(map[net.Conn]struct{}),
		done:  make(chan struct{}),
	}
}

// Track регистрирует соединение для отслеживания при shutdown.
func (g *GracefulShutdown) Track(c net.Conn) {
	g.mu.Lock()
	g.conns[c] = struct{}{}
	g.mu.Unlock()
}

// Untrack снимает соединение с отслеживания (вызывается при закрытии).
func (g *GracefulShutdown) Untrack(c net.Conn) {
	g.mu.Lock()
	delete(g.conns, c)
	g.mu.Unlock()
}

// Shutdown выполняет graceful shutdown:
//  1. Отменяет контекст (останавливает listeners через ctx cancel).
//  2. Ждёт drainTimeout для завершения активных соединений.
//  3. Принудительно закрывает оставшиеся соединения.
func (g *GracefulShutdown) Shutdown(cancel context.CancelFunc) {
	g.once.Do(func() {
		log.Println("shutdown: cancelling context")
		cancel()

		// Ждём завершения соединений
		deadline := time.NewTimer(drainTimeout)
		defer deadline.Stop()

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-deadline.C:
				log.Println("shutdown: drain timeout, forcing close")
				g.forceClose()
				close(g.done)
				return
			case <-ticker.C:
				g.mu.Lock()
				n := len(g.conns)
				g.mu.Unlock()
				if n == 0 {
					log.Println("shutdown: all connections closed")
					close(g.done)
					return
				}
				log.Printf("shutdown: waiting for %d connections", n)
			}
		}
	})
}

// Wait блокируется до завершения shutdown.
func (g *GracefulShutdown) Wait() {
	<-g.done
}

// forceClose принудительно закрывает все зарегистрированные соединения.
func (g *GracefulShutdown) forceClose() {
	g.mu.Lock()
	conns := make([]net.Conn, 0, len(g.conns))
	for c := range g.conns {
		conns = append(conns, c)
	}
	g.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}
}
