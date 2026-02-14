package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

type workerExit struct {
	id  int
	err error
}

type workerHandle struct {
	id  int
	cmd *exec.Cmd
}

func runSupervisedWorkers(logw io.Writer, workers int, reopenLogFn func() error) error {
	if workers <= 0 {
		return fmt.Errorf("invalid workers count: %d", workers)
	}

	exitCh := make(chan workerExit, workers*2)
	workerSet := make(map[int]*workerHandle, workers)
	for i := 0; i < workers; i++ {
		wh, err := startWorker(i, exitCh)
		if err != nil {
			_ = shutdownWorkers(workerSet, exitCh, syscall.SIGTERM, 5*time.Second, logw)
			return fmt.Errorf("start worker %d: %w", i, err)
		}
		workerSet[i] = wh
		fmt.Fprintf(logw, "supervisor started worker id=%d pid=%d\n", i, wh.cmd.Process.Pid)
	}

	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)
	defer signal.Stop(sigCh)

	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				forwardSignal(workerSet, sig, logw)
			case syscall.SIGUSR1:
				if reopenLogFn == nil {
					fmt.Fprintln(logw, "supervisor SIGUSR1: no log file configured, skipping reopen.")
				} else {
					if err := reopenLogFn(); err != nil {
						fmt.Fprintf(logw, "supervisor SIGUSR1: log reopen failed: %v\n", err)
					} else {
						fmt.Fprintln(logw, "supervisor SIGUSR1: log file reopened.")
					}
				}
				forwardSignal(workerSet, sig, logw)
			case syscall.SIGTERM, syscall.SIGINT:
				fmt.Fprintf(logw, "supervisor received %s, shutting down workers\n", signalDisplayName(sig))
				return shutdownWorkers(workerSet, exitCh, sig, 5*time.Second, logw)
			}
		case ex := <-exitCh:
			wh, ok := workerSet[ex.id]
			if !ok {
				continue
			}
			delete(workerSet, ex.id)
			if ex.err != nil {
				fmt.Fprintf(logw, "worker id=%d pid=%d exited unexpectedly: %v\n", ex.id, wh.cmd.Process.Pid, ex.err)
				_ = shutdownWorkers(workerSet, exitCh, syscall.SIGTERM, 5*time.Second, logw)
				return fmt.Errorf("worker %d exited unexpectedly: %w", ex.id, ex.err)
			}
			fmt.Fprintf(logw, "worker id=%d pid=%d exited unexpectedly with code 0\n", ex.id, wh.cmd.Process.Pid)
			_ = shutdownWorkers(workerSet, exitCh, syscall.SIGTERM, 5*time.Second, logw)
			return fmt.Errorf("worker %d exited unexpectedly", ex.id)
		}
	}
}

func startWorker(id int, exitCh chan<- workerExit) (*workerHandle, error) {
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_SUPERVISED_WORKER=1",
		fmt.Sprintf("MTPROXY_GO_WORKER_ID=%d", id),
		fmt.Sprintf("MTPROXY_GO_SUPERVISOR_PID=%d", os.Getpid()),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		exitCh <- workerExit{id: id, err: cmd.Wait()}
	}()
	return &workerHandle{id: id, cmd: cmd}, nil
}

func forwardSignal(workerSet map[int]*workerHandle, sig os.Signal, logw io.Writer) {
	for _, wh := range workerSet {
		if wh.cmd == nil || wh.cmd.Process == nil {
			continue
		}
		if err := wh.cmd.Process.Signal(sig); err != nil {
			fmt.Fprintf(logw, "failed to forward %s to worker id=%d pid=%d: %v\n", signalDisplayName(sig), wh.id, wh.cmd.Process.Pid, err)
		}
	}
}

func shutdownWorkers(
	workerSet map[int]*workerHandle,
	exitCh <-chan workerExit,
	shutdownSignal os.Signal,
	timeout time.Duration,
	logw io.Writer,
) error {
	if len(workerSet) == 0 {
		return nil
	}

	forwardSignal(workerSet, shutdownSignal, logw)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	var firstErr error
	for len(workerSet) > 0 {
		select {
		case ex := <-exitCh:
			wh, ok := workerSet[ex.id]
			if !ok {
				continue
			}
			delete(workerSet, ex.id)
			if ex.err != nil {
				if isExpectedShutdownExit(ex.err, shutdownSignal) {
					fmt.Fprintf(logw, "worker id=%d pid=%d stopped\n", ex.id, wh.cmd.Process.Pid)
					continue
				}
				if firstErr == nil {
					firstErr = fmt.Errorf("worker %d exit error: %w", ex.id, ex.err)
				}
				fmt.Fprintf(logw, "worker id=%d pid=%d exited with error: %v\n", ex.id, wh.cmd.Process.Pid, ex.err)
			} else {
				fmt.Fprintf(logw, "worker id=%d pid=%d stopped\n", ex.id, wh.cmd.Process.Pid)
			}
		case <-deadline.C:
			for _, wh := range workerSet {
				if wh.cmd != nil && wh.cmd.Process != nil {
					_ = wh.cmd.Process.Kill()
				}
			}
			return fmt.Errorf("timeout waiting for workers to stop")
		}
	}
	return firstErr
}

func isExpectedShutdownExit(err error, shutdownSignal os.Signal) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return false
	}
	return syscall.Signal(status.Signal()) == shutdownSignal
}

func signalDisplayName(sig os.Signal) string {
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
