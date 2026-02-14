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
		"Go runtime enabled: send SIGHUP to reload config, SIGTERM/SIGINT to stop.\n",
	)

	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)
	defer signal.Stop(sigCh)

	runner := engine.NewProxyRunner(lifecycle, logw)
	if reopener, ok := logw.(interface{ Reopen() error }); ok {
		runner.Runtime().SetLogReopener(reopener.Reopen)
	}
	var statsServer *proxy.StatsServer
	var ingressServers []ingressStatsServer
	var outbound proxy.OutboundSender
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
	if serveIngress, reason := shouldStartDataPlaneIngress(opts, supervisedWorker); !serveIngress {
		if reason != "" {
			fmt.Fprintln(logw, reason)
		}
	} else {
		ingressAddrs, err := resolveIngressAddrs(opts)
		if err != nil {
			fmt.Fprintf(logw, "failed to resolve ingress addresses: %v\n", err)
			os.Exit(2)
		}
		for _, ingressAddr := range ingressAddrs {
			ingressServer, startErr := startIngressServer(runner.Runtime(), opts, ingressAddr, logw)
			if startErr != nil {
				fmt.Fprintf(logw, "failed to start ingress server on %s: %v\n", ingressAddr, startErr)
				for _, started := range ingressServers {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					_ = started.Shutdown(ctx)
					cancel()
				}
				os.Exit(2)
			}
			ingressServers = append(ingressServers, ingressServer)
		}
		runner.Runtime().SetIngressStatsProvider(func() proxy.IngressStats {
			total := proxy.IngressStats{}
			for _, srv := range ingressServers {
				total = mergeIngressStats(total, srv.Stats())
			}
			return total
		})
	}
	if serveOutbound, reason := shouldStartOutboundTransport(opts, len(ingressServers) > 0, supervisedWorker); !serveOutbound {
		if reason != "" {
			fmt.Fprintln(logw, reason)
		}
	} else {
		outCfg, err := outboundConfigFromEnv()
		if err != nil {
			fmt.Fprintf(logw, "invalid outbound env config: %v\n", err)
			os.Exit(2)
		}
		outbound = proxy.NewOutboundProxy(outCfg)
		runner.Runtime().SetOutboundSender(outbound)
		fmt.Fprintln(logw, "outbound transport enabled.")
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
	for _, ingressServer := range ingressServers {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := ingressServer.Shutdown(ctx); err != nil {
			fmt.Fprintf(logw, "ingress server shutdown error: %v\n", err)
		}
		cancel()
	}
	if outbound != nil {
		if err := outbound.Close(); err != nil {
			fmt.Fprintf(logw, "outbound transport shutdown error: %v\n", err)
		}
	}
	os.Exit(0)
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

type ingressStatsServer interface {
	Shutdown(context.Context) error
	Stats() proxy.IngressStats
}

func startIngressServer(
	rt *proxy.Runtime,
	opts cli.Options,
	addr string,
	logw io.Writer,
) (ingressStatsServer, error) {
	if shouldUseClientFacingIngress(opts) {
		return proxy.StartClientIngressServer(
			rt,
			proxy.ClientIngressConfig{
				Addr:          addr,
				TargetDC:      0,
				MaxFrameSize:  4 << 20,
				IdleTimeout:   45 * time.Second,
				MaxAcceptRate: opts.MaxAcceptRate,
				ReadBufBytes:  int(opts.MsgBuffersSizeBytes),
				Secrets:       opts.Secrets,
			},
			logw,
		)
	}
	return proxy.StartIngressServer(
		rt,
		proxy.IngressConfig{
			Addr:          addr,
			TargetDC:      0,
			MaxFrameSize:  4 << 20,
			IdleTimeout:   45 * time.Second,
			MaxAcceptRate: opts.MaxAcceptRate,
			ReadBufBytes:  int(opts.MsgBuffersSizeBytes),
		},
		logw,
	)
}

func shouldUseClientFacingIngress(opts cli.Options) bool {
	if len(opts.HTTPPorts) == 0 {
		return false
	}
	if os.Getenv("MTPROXY_GO_INGRESS_LEGACY") == "1" {
		return false
	}
	return true
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

func shouldStartDataPlaneIngress(opts cli.Options, supervisedWorker bool) (bool, string) {
	if opts.DisableTCP {
		return false, "tcp is disabled by --disable-tcp, skipping ingress startup"
	}
	if raw := os.Getenv("MTPROXY_GO_ENABLE_INGRESS"); raw != "" {
		if raw != "1" {
			return false, ""
		}
	} else if len(opts.HTTPPorts) == 0 {
		return false, ""
	}
	if !supervisedWorker {
		return true, ""
	}
	workerID, ok := currentWorkerID()
	if !ok {
		return false, "ingress requested in supervisor mode but worker id is missing, skipping ingress startup"
	}
	if workerID != 0 {
		return false, fmt.Sprintf(
			"ingress requested in supervisor mode, only worker 0 serves ingress (current worker=%d), skipping ingress startup",
			workerID,
		)
	}
	return true, ""
}

func shouldStartOutboundTransport(opts cli.Options, ingressEnabled bool, supervisedWorker bool) (bool, string) {
	if opts.DisableTCP {
		return false, "tcp is disabled by --disable-tcp, skipping outbound startup"
	}
	if raw := os.Getenv("MTPROXY_GO_ENABLE_OUTBOUND"); raw != "" {
		if raw != "1" {
			return false, ""
		}
	} else if !ingressEnabled {
		return false, ""
	}
	if !supervisedWorker {
		return true, ""
	}
	workerID, ok := currentWorkerID()
	if !ok {
		return false, "outbound requested in supervisor mode but worker id is missing, skipping outbound startup"
	}
	if workerID != 0 {
		return false, fmt.Sprintf(
			"outbound requested in supervisor mode, only worker 0 enables outbound transport (current worker=%d), skipping outbound startup",
			workerID,
		)
	}
	return true, ""
}

func outboundConfigFromEnv() (proxy.OutboundConfig, error) {
	connectTimeout, err := durationFromEnvMS("MTPROXY_GO_OUTBOUND_CONNECT_TIMEOUT_MS", 3*time.Second)
	if err != nil {
		return proxy.OutboundConfig{}, err
	}
	writeTimeout, err := durationFromEnvMS("MTPROXY_GO_OUTBOUND_WRITE_TIMEOUT_MS", 5*time.Second)
	if err != nil {
		return proxy.OutboundConfig{}, err
	}
	readTimeout, err := durationFromEnvMS("MTPROXY_GO_OUTBOUND_READ_TIMEOUT_MS", 250*time.Millisecond)
	if err != nil {
		return proxy.OutboundConfig{}, err
	}
	idleTimeout, err := durationFromEnvMS("MTPROXY_GO_OUTBOUND_IDLE_TIMEOUT_MS", 90*time.Second)
	if err != nil {
		return proxy.OutboundConfig{}, err
	}
	maxFrameSize, err := intFromEnv("MTPROXY_GO_OUTBOUND_MAX_FRAME_SIZE", 8<<20, 1)
	if err != nil {
		return proxy.OutboundConfig{}, err
	}
	return proxy.OutboundConfig{
		ConnectTimeout:  connectTimeout,
		WriteTimeout:    writeTimeout,
		ReadTimeout:     readTimeout,
		IdleConnTimeout: idleTimeout,
		MaxFrameSize:    maxFrameSize,
	}, nil
}

func durationFromEnvMS(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	ms, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be integer milliseconds: %w", name, err)
	}
	if ms < 0 {
		return 0, fmt.Errorf("%s must be >= 0", name)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func intFromEnv(name string, fallback int, min int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be integer: %w", name, err)
	}
	if v < min {
		return 0, fmt.Errorf("%s must be >= %d", name, min)
	}
	return v, nil
}

func resolveIngressAddrs(opts cli.Options) ([]string, error) {
	if addr := os.Getenv("MTPROXY_GO_INGRESS_ADDR"); addr != "" {
		return []string{addr}, nil
	}
	host := opts.BindAddress
	if host == "" {
		host = "0.0.0.0"
	}
	if len(opts.HTTPPorts) > 0 {
		addrs := make([]string, 0, len(opts.HTTPPorts))
		seen := make(map[int]struct{}, len(opts.HTTPPorts))
		for _, port := range opts.HTTPPorts {
			if _, ok := seen[port]; ok {
				continue
			}
			seen[port] = struct{}{}
			addrs = append(addrs, fmt.Sprintf("%s:%d", host, port))
		}
		return addrs, nil
	}
	if opts.LocalPort > 0 {
		return []string{fmt.Sprintf("%s:%d", host, opts.LocalPort)}, nil
	}
	return nil, fmt.Errorf("ingress requires client port (-H/--http-ports) or single local port (-p/--port), got -H=%v -p=%q", opts.HTTPPorts, opts.LocalPortRaw)
}

func mergeIngressStats(a, b proxy.IngressStats) proxy.IngressStats {
	return proxy.IngressStats{
		AcceptedConnections: a.AcceptedConnections + b.AcceptedConnections,
		AcceptRateLimited:   a.AcceptRateLimited + b.AcceptRateLimited,
		ClosedConnections:   a.ClosedConnections + b.ClosedConnections,
		ActiveConnections:   a.ActiveConnections + b.ActiveConnections,
		FramesReceived:      a.FramesReceived + b.FramesReceived,
		FramesHandled:       a.FramesHandled + b.FramesHandled,
		FramesReturned:      a.FramesReturned + b.FramesReturned,
		FramesFailed:        a.FramesFailed + b.FramesFailed,
		BytesReceived:       a.BytesReceived + b.BytesReceived,
		BytesReturned:       a.BytesReturned + b.BytesReturned,
		ReadErrors:          a.ReadErrors + b.ReadErrors,
		WriteErrors:         a.WriteErrors + b.WriteErrors,
		InvalidFrames:       a.InvalidFrames + b.InvalidFrames,
	}
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
