package engine

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// ReloadFunc is called when SIGHUP is received to reload configuration.
type ReloadFunc func() error

// Runner manages the main event loop and POSIX signal handling.
type Runner struct {
	reload ReloadFunc
}

// NewRunner creates a Runner with the given config reload callback.
func NewRunner(reload ReloadFunc) *Runner {
	return &Runner{reload: reload}
}

// Run blocks until SIGINT or SIGTERM is received, handling SIGHUP for reloads.
// It returns when the process should exit.
func (r *Runner) Run(ctx context.Context) {
	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGUSR1,
	)
	defer signal.Stop(sigCh)

	log.Println("engine running, waiting for signals")

	for {
		select {
		case <-ctx.Done():
			log.Println("context cancelled, shutting down")
			return
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGINT:
				log.Println("received SIGINT, shutting down")
				return
			case syscall.SIGTERM:
				log.Println("received SIGTERM, shutting down")
				return
			case syscall.SIGHUP:
				log.Println("received SIGHUP, reloading config")
				if r.reload != nil {
					if err := r.reload(); err != nil {
						log.Printf("config reload failed: %v", err)
					}
				}
			case syscall.SIGUSR1:
				log.Println("received SIGUSR1 (log reopen not implemented)")
			}
		}
	}
}
