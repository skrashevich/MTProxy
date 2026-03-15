package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/skrashevicj/mtproxy/internal/cli"
	"github.com/skrashevicj/mtproxy/internal/proxy"
)

func main() {
	opts := cli.Parse()

	// Set up logging.
	lw := NewLogWriter("[mtproxy] ", os.Stderr)
	log.SetOutput(lw)
	log.SetFlags(log.LstdFlags)

	if opts.Verbosity > 0 {
		log.Printf("verbosity=%d", opts.Verbosity)
	}

	// If -M > 1: run supervisor mode.
	if opts.Workers > 1 {
		if os.Getenv("MTPROXY_WORKER_SLAVE") != "1" {
			workerArgs := buildWorkerArgs(opts)
			runSupervisor(opts.Workers, workerArgs)
			return
		}
	}

	if len(opts.Secrets) == 0 {
		log.Println("warning: no mtproto secrets configured (-S)")
	}

	// Determine listen address from -H ports.
	listenAddr := fmt.Sprintf(":%d", cli.DefaultPort)
	if len(opts.HTTPPorts) > 0 {
		listenAddr = fmt.Sprintf(":%d", opts.HTTPPorts[0])
	}

	// Read AES secret for outbound RPC connections.
	var aesSecret []byte
	if opts.AESPwdFile != "" {
		data, err := os.ReadFile(opts.AESPwdFile)
		if err != nil {
			log.Fatalf("fatal: cannot read --aes-pwd %s: %v", opts.AESPwdFile, err)
		}
		aesSecret = data
	}

	// HTTP stats address — use a separate port to avoid conflict with the MTProto listener.
	// Derives stats port as listen_port + 8000 (e.g., :4431 → :12431).
	httpStatsAddr := ""
	if opts.HTTPStats {
		statsPort := 8888 + 8000 // default
		if len(opts.HTTPPorts) > 0 {
			statsPort = opts.HTTPPorts[0] + 8000
		}
		httpStatsAddr = fmt.Sprintf(":%d", statsPort)
	}

	// Build runtime options.
	rtOpts := proxy.RuntimeOptions{
		ListenAddr:              listenAddr,
		HTTPStatsAddr:           httpStatsAddr,
		ConfigFile:              opts.ConfigFile,
		MaxConnectionsPerSecret: opts.MaxSpecialConnections,
	}

	// Build NAT translation table: string IPs → uint32 LE
	var natMap map[uint32]uint32
	if len(opts.NatInfo) > 0 {
		natMap = make(map[uint32]uint32)
		for localStr, pubStr := range opts.NatInfo {
			localIP := net.ParseIP(localStr).To4()
			pubIP := net.ParseIP(pubStr).To4()
			if localIP == nil || pubIP == nil {
				log.Fatalf("fatal: --nat-info: invalid IP pair %s:%s", localStr, pubStr)
			}
			localU := uint32(localIP[0]) | uint32(localIP[1])<<8 | uint32(localIP[2])<<16 | uint32(localIP[3])<<24
			pubU := uint32(pubIP[0]) | uint32(pubIP[1])<<8 | uint32(pubIP[2])<<16 | uint32(pubIP[3])<<24
			natMap[localU] = pubU
			log.Printf("nat-info: %s (0x%08x) → %s (0x%08x)", localStr, localU, pubStr, pubU)
		}
	}

	outCfg := proxy.OutboundConfig{
		Secret:   aesSecret,
		ProxyTag: opts.ProxyTag,
		ForceDH:  false, // TODO: add --force-dh flag
		NatInfo:  natMap,
	}

	rt, err := proxy.New(rtOpts, opts.Secrets, opts.ProxyTag, outCfg)
	if err != nil {
		log.Fatalf("fatal: %v", err)
	}

	ctx := context.Background()
	if err := rt.Start(ctx); err != nil {
		log.Fatalf("fatal: %v", err)
	}

	log.Println("exiting")
}

// buildWorkerArgs constructs the argv for a worker process.
func buildWorkerArgs(opts *cli.Options) []string {
	args := make([]string, len(os.Args))
	copy(args, os.Args)
	return args
}
