package main

import (
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// supervisor forks N worker processes, restarts them if they die, and
// forwards SIGINT/SIGTERM to all children.
func runSupervisor(n int, args []string) {
	log.Printf("supervisor: starting %d workers", n)

	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	type workerState struct {
		id  int
		cmd *exec.Cmd
		mu  sync.Mutex
	}

	workers := make([]*workerState, n)
	for i := range workers {
		workers[i] = &workerState{id: i}
	}

	startWorker := func(ws *workerState) {
		ws.mu.Lock()
		defer ws.mu.Unlock()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(), "MTPROXY_WORKER_SLAVE=1", "MTPROXY_WORKER_ID="+itoa(ws.id))
		if err := cmd.Start(); err != nil {
			log.Printf("supervisor: failed to start worker %d: %v", ws.id, err)
			return
		}
		ws.cmd = cmd
		log.Printf("supervisor: started worker %d (pid %d)", ws.id, cmd.Process.Pid)
	}

	killAll := func(sig os.Signal) {
		for _, ws := range workers {
			ws.mu.Lock()
			if ws.cmd != nil && ws.cmd.Process != nil {
				_ = ws.cmd.Process.Signal(sig)
			}
			ws.mu.Unlock()
		}
	}

	// Start all workers initially.
	for _, ws := range workers {
		startWorker(ws)
	}

	// Monitor workers in background goroutines; restart on unexpected exit.
	stopping := make(chan struct{})
	var wg sync.WaitGroup
	for _, ws := range workers {
		wg.Add(1)
		go func(ws *workerState) {
			defer wg.Done()
			for {
				ws.mu.Lock()
				cmd := ws.cmd
				ws.mu.Unlock()
				if cmd == nil {
					return
				}
				err := cmd.Wait()
				select {
				case <-stopping:
					return
				default:
				}
				if err != nil {
					log.Printf("supervisor: worker %d exited: %v — restarting in 1s", ws.id, err)
				} else {
					log.Printf("supervisor: worker %d exited cleanly — restarting in 1s", ws.id)
				}
				time.Sleep(time.Second)
				select {
				case <-stopping:
					return
				default:
				}
				startWorker(ws)
			}
		}(ws)
	}

	// Handle signals from the OS.
	for sig := range sigCh {
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			log.Printf("supervisor: received %v, shutting down workers", sig)
			close(stopping)
			killAll(sig)
			wg.Wait()
			return
		case syscall.SIGHUP:
			log.Println("supervisor: received SIGHUP, forwarding to workers")
			killAll(syscall.SIGHUP)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
