package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
	"github.com/TelegramMessenger/MTProxy/internal/engine"
	"github.com/TelegramMessenger/MTProxy/internal/proxy"
)

const fullVersion = "mtproxy-go-dev"

func main() {
	opts, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can not parse options: %v\n", err)
		fmt.Fprint(os.Stderr, cli.Usage(os.Args[0], fullVersion))
		os.Exit(2)
	}

	if opts.ShowHelp {
		fmt.Fprint(os.Stdout, cli.Usage(os.Args[0], fullVersion))
		os.Exit(2)
	}

	supervisedWorker := isSupervisedWorkerProcess()
	if supervisedWorker && opts.Workers > 0 {
		opts.Workers = 0
	}

	logw, closeLog, err := setupLogWriter(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "can not initialize log writer: %v\n", err)
		os.Exit(2)
	}
	defer closeLog()
	if supervisedWorker {
		logw = maybeWrapWorkerLogWriter(logw)
	}

	if os.Getenv("MTPROXY_GO_SIGNAL_LOOP") == "1" {
		if opts.Workers > 0 && !supervisedWorker {
			fmt.Fprintf(logw, "Go bootstrap supervisor enabled: workers=%d\n", opts.Workers)
			var reopenLogFn func() error
			if reopener, ok := logw.(interface{ Reopen() error }); ok {
				reopenLogFn = reopener.Reopen
			}
			if err := runSupervisedWorkers(logw, opts.Workers, reopenLogFn); err != nil {
				fmt.Fprintf(logw, "supervisor error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		}

		manager := config.NewManager(opts.ConfigFile)
		lifecycle := proxy.NewLifecycle(manager, opts)
		fmt.Fprintf(
			logw,
			"Go bootstrap signal loop enabled: send SIGHUP to reload config, SIGTERM/SIGINT to stop.\n",
		)

		sigCh := make(chan os.Signal, 4)
		signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)
		defer signal.Stop(sigCh)

		runner := engine.NewProxyRunner(lifecycle, logw)
		if reopener, ok := logw.(interface{ Reopen() error }); ok {
			runner.Runtime().SetLogReopener(reopener.Reopen)
		}
		var statsServer *proxy.StatsServer
		if opts.HTTPStats {
			if serveStats, reason := shouldStartStatsServer(supervisedWorker); !serveStats {
				fmt.Fprintln(logw, reason)
			} else {
				if opts.LocalPort > 0 {
					addr := fmt.Sprintf("127.0.0.1:%d", opts.LocalPort)
					var err error
					statsServer, err = proxy.StartStatsServer(runner.Runtime(), addr, logw)
					if err != nil {
						fmt.Fprintf(logw, "failed to start stats server on %s: %v (continuing without stats server)\n", addr, err)
					}
				} else {
					fmt.Fprintln(logw, "http-stats requested but local port is not a single value, skipping stats server startup")
				}
			}
		}

		runCtx, cancel := supervisedWorkerParentContext(supervisedWorker, logw)
		defer cancel()

		if err := runner.Run(runCtx, sigCh); err != nil {
			fmt.Fprintf(logw, "signal loop error: %v\n", err)
			os.Exit(1)
		}
		if statsServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := statsServer.Shutdown(ctx); err != nil {
				fmt.Fprintf(logw, "stats server shutdown error: %v\n", err)
			}
		}
		os.Exit(0)
	}

	manager := config.NewManager(opts.ConfigFile)
	lifecycle := proxy.NewLifecycle(manager, opts)
	snapshot, warnings, err := lifecycle.LoadInitial()
	if err != nil {
		fmt.Fprintf(logw, "config check failed! (%v)\n", err)
		os.Exit(2)
	}
	for _, w := range warnings {
		fmt.Fprintln(logw, w)
	}

	fmt.Fprintf(
		logw,
		"Go implementation bootstrap: config loaded (targets=%d clusters=%d bytes=%d md5=%s), runtime is not implemented yet.\n",
		len(snapshot.Config.Targets),
		len(snapshot.Config.Clusters),
		snapshot.Bytes,
		snapshot.MD5Hex,
	)
	fmt.Fprint(logw, cli.Usage(os.Args[0], fullVersion))
	os.Exit(2)
}

func setupLogWriter(opts cli.Options) (io.Writer, func(), error) {
	if opts.LogFile == "" {
		return os.Stderr, func() {}, nil
	}

	lw, err := newReopenableLogWriter(opts.LogFile)
	if err != nil {
		return nil, nil, err
	}
	return lw, func() {
		_ = lw.Close()
	}, nil
}

func isSupervisedWorkerProcess() bool {
	return os.Getenv("MTPROXY_GO_SUPERVISED_WORKER") == "1"
}

func shouldStartStatsServer(supervisedWorker bool) (bool, string) {
	if !supervisedWorker {
		return true, ""
	}
	workerID, ok := currentWorkerID()
	if !ok {
		return false, "http-stats requested in supervisor mode but worker id is missing, skipping stats server startup"
	}
	if workerID != 0 {
		return false, fmt.Sprintf(
			"http-stats requested in supervisor mode, only worker 0 serves stats (current worker=%d), skipping stats server startup",
			workerID,
		)
	}
	return true, ""
}

func supervisedWorkerParentContext(supervisedWorker bool, logw io.Writer) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if !supervisedWorker {
		return ctx, cancel
	}

	rawPID := os.Getenv("MTPROXY_GO_SUPERVISOR_PID")
	supervisorPID, err := strconv.Atoi(rawPID)
	if err != nil || supervisorPID <= 0 {
		fmt.Fprintf(logw, "supervised worker startup error: invalid MTPROXY_GO_SUPERVISOR_PID=%q\n", rawPID)
		cancel()
		return ctx, cancel
	}

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				currentParent := os.Getppid()
				if currentParent != supervisorPID {
					fmt.Fprintf(
						logw,
						"supervised worker parent changed: expected=%d got=%d, shutting down\n",
						supervisorPID,
						currentParent,
					)
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}
