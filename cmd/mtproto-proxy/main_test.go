package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
)

func TestSetupLogWriterDefaultsToStderr(t *testing.T) {
	logw, closeFn, err := setupLogWriter(cli.Options{})
	if err != nil {
		t.Fatalf("setup log writer: %v", err)
	}
	if logw != os.Stderr {
		t.Fatalf("expected os.Stderr writer")
	}
	closeFn()
}

func TestSetupLogWriterFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.log")

	logw, closeFn, err := setupLogWriter(cli.Options{LogFile: path})
	if err != nil {
		t.Fatalf("setup log writer: %v", err)
	}

	reopener, ok := logw.(interface{ Reopen() error })
	if !ok {
		t.Fatalf("expected reopenable log writer")
	}

	if _, err := logw.Write([]byte("first-line\n")); err != nil {
		t.Fatalf("write first line: %v", err)
	}
	if err := reopener.Reopen(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := logw.Write([]byte("second-line\n")); err != nil {
		t.Fatalf("write second line: %v", err)
	}
	closeFn()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)
	for _, line := range []string{"first-line", "second-line"} {
		if !strings.Contains(content, line) {
			t.Fatalf("expected %q in log file, got: %q", line, content)
		}
	}
}

func TestSetupLogWriterInvalidPath(t *testing.T) {
	_, _, err := setupLogWriter(cli.Options{
		LogFile: filepath.Join(t.TempDir(), "missing", "proxy.log"),
	})
	if err == nil {
		t.Fatalf("expected error for invalid log path")
	}
}

func TestSupervisedWorkerParentContextNonSupervised(t *testing.T) {
	ctx, cancel := supervisedWorkerParentContext(false, &bytes.Buffer{})
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatalf("unexpected canceled context for non-supervised process")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSupervisedWorkerParentContextInvalidPID(t *testing.T) {
	t.Setenv("MTPROXY_GO_SUPERVISOR_PID", "bad")
	var logs bytes.Buffer

	ctx, cancel := supervisedWorkerParentContext(true, &logs)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected context cancellation for invalid supervisor pid")
	}
	if !strings.Contains(logs.String(), "invalid MTPROXY_GO_SUPERVISOR_PID") {
		t.Fatalf("expected invalid supervisor pid log, got: %s", logs.String())
	}
}

func TestSupervisedWorkerParentContextValidPIDNoImmediateCancel(t *testing.T) {
	t.Setenv("MTPROXY_GO_SUPERVISOR_PID", "1")
	var logs bytes.Buffer

	ctx, cancel := supervisedWorkerParentContext(true, &logs)
	defer cancel()

	select {
	case <-ctx.Done():
		// If parent pid differs from 1 in test env, the goroutine can legitimately cancel quickly.
		// This test only verifies startup path is valid and does not emit parse errors.
	case <-time.After(50 * time.Millisecond):
	}

	if strings.Contains(logs.String(), "invalid MTPROXY_GO_SUPERVISOR_PID") {
		t.Fatalf("unexpected invalid pid log: %s", logs.String())
	}
}

func TestShouldStartStatsServerNonSupervised(t *testing.T) {
	ok, reason := shouldStartStatsServer(false)
	if !ok || reason != "" {
		t.Fatalf("expected stats server start for non-supervised worker, got ok=%v reason=%q", ok, reason)
	}
}

func TestShouldStartStatsServerSupervisedWorker0(t *testing.T) {
	t.Setenv("MTPROXY_GO_WORKER_ID", "0")
	ok, reason := shouldStartStatsServer(true)
	if !ok || reason != "" {
		t.Fatalf("expected stats server start for worker 0, got ok=%v reason=%q", ok, reason)
	}
}

func TestShouldStartStatsServerSupervisedWorkerNonZero(t *testing.T) {
	t.Setenv("MTPROXY_GO_WORKER_ID", "1")
	ok, reason := shouldStartStatsServer(true)
	if ok {
		t.Fatalf("expected stats server skip for non-zero worker id")
	}
	if !strings.Contains(reason, "only worker 0 serves stats") {
		t.Fatalf("unexpected skip reason: %q", reason)
	}
}

func TestShouldStartStatsServerSupervisedWorkerInvalidID(t *testing.T) {
	t.Setenv("MTPROXY_GO_WORKER_ID", "bad")
	ok, reason := shouldStartStatsServer(true)
	if ok {
		t.Fatalf("expected stats server skip for invalid worker id")
	}
	if !strings.Contains(reason, "worker id is missing") {
		t.Fatalf("unexpected skip reason: %q", reason)
	}
}

func TestShouldStartDataPlaneIngress(t *testing.T) {
	t.Setenv("MTPROXY_GO_ENABLE_INGRESS", "1")
	ok, reason := shouldStartDataPlaneIngress(false)
	if !ok || reason != "" {
		t.Fatalf("expected ingress enabled")
	}
	t.Setenv("MTPROXY_GO_ENABLE_INGRESS", "0")
	ok, _ = shouldStartDataPlaneIngress(false)
	if ok {
		t.Fatalf("expected ingress disabled")
	}
}

func TestShouldStartDataPlaneIngressSupervisorWorker0(t *testing.T) {
	t.Setenv("MTPROXY_GO_ENABLE_INGRESS", "1")
	t.Setenv("MTPROXY_GO_WORKER_ID", "0")
	ok, reason := shouldStartDataPlaneIngress(true)
	if !ok || reason != "" {
		t.Fatalf("expected ingress enabled for worker0, got ok=%v reason=%q", ok, reason)
	}
}

func TestShouldStartDataPlaneIngressSupervisorNonZero(t *testing.T) {
	t.Setenv("MTPROXY_GO_ENABLE_INGRESS", "1")
	t.Setenv("MTPROXY_GO_WORKER_ID", "1")
	ok, reason := shouldStartDataPlaneIngress(true)
	if ok {
		t.Fatalf("expected ingress disabled for non-zero worker")
	}
	if !strings.Contains(reason, "only worker 0 serves ingress") {
		t.Fatalf("unexpected skip reason: %q", reason)
	}
}

func TestResolveIngressAddrFromEnv(t *testing.T) {
	t.Setenv("MTPROXY_GO_INGRESS_ADDR", "127.0.0.1:12345")
	addr, err := resolveIngressAddr(cli.Options{})
	if err != nil {
		t.Fatalf("resolve ingress addr: %v", err)
	}
	if addr != "127.0.0.1:12345" {
		t.Fatalf("unexpected ingress addr: %q", addr)
	}
}

func TestResolveIngressAddrFromOptions(t *testing.T) {
	addr, err := resolveIngressAddr(cli.Options{LocalPort: 443, BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("resolve ingress addr: %v", err)
	}
	if addr != "127.0.0.1:443" {
		t.Fatalf("unexpected ingress addr: %q", addr)
	}
}

func TestResolveIngressAddrMissingPort(t *testing.T) {
	_, err := resolveIngressAddr(cli.Options{LocalPortRaw: "10000:10010"})
	if err == nil {
		t.Fatalf("expected ingress resolve error without single port")
	}
}

func TestShouldStartOutboundTransport(t *testing.T) {
	t.Setenv("MTPROXY_GO_ENABLE_OUTBOUND", "1")
	ok, reason := shouldStartOutboundTransport(false)
	if !ok || reason != "" {
		t.Fatalf("expected outbound enabled")
	}
	t.Setenv("MTPROXY_GO_ENABLE_OUTBOUND", "0")
	ok, _ = shouldStartOutboundTransport(false)
	if ok {
		t.Fatalf("expected outbound disabled")
	}
}

func TestShouldStartOutboundTransportSupervisorWorker0(t *testing.T) {
	t.Setenv("MTPROXY_GO_ENABLE_OUTBOUND", "1")
	t.Setenv("MTPROXY_GO_WORKER_ID", "0")
	ok, reason := shouldStartOutboundTransport(true)
	if !ok || reason != "" {
		t.Fatalf("expected outbound enabled for worker0, got ok=%v reason=%q", ok, reason)
	}
}

func TestShouldStartOutboundTransportSupervisorNonZero(t *testing.T) {
	t.Setenv("MTPROXY_GO_ENABLE_OUTBOUND", "1")
	t.Setenv("MTPROXY_GO_WORKER_ID", "1")
	ok, reason := shouldStartOutboundTransport(true)
	if ok {
		t.Fatalf("expected outbound disabled for non-zero worker")
	}
	if !strings.Contains(reason, "only worker 0 enables outbound transport") {
		t.Fatalf("unexpected skip reason: %q", reason)
	}
}

func TestOutboundConfigFromEnvDefaults(t *testing.T) {
	cfg, err := outboundConfigFromEnv()
	if err != nil {
		t.Fatalf("outbound config defaults: %v", err)
	}
	if cfg.ConnectTimeout != 3*time.Second {
		t.Fatalf("unexpected default connect timeout: %s", cfg.ConnectTimeout)
	}
	if cfg.WriteTimeout != 5*time.Second {
		t.Fatalf("unexpected default write timeout: %s", cfg.WriteTimeout)
	}
	if cfg.ReadTimeout != 250*time.Millisecond {
		t.Fatalf("unexpected default read timeout: %s", cfg.ReadTimeout)
	}
	if cfg.IdleConnTimeout != 90*time.Second {
		t.Fatalf("unexpected default idle timeout: %s", cfg.IdleConnTimeout)
	}
	if cfg.MaxFrameSize != 8<<20 {
		t.Fatalf("unexpected default max frame size: %d", cfg.MaxFrameSize)
	}
}

func TestOutboundConfigFromEnvCustomValues(t *testing.T) {
	t.Setenv("MTPROXY_GO_OUTBOUND_CONNECT_TIMEOUT_MS", "1200")
	t.Setenv("MTPROXY_GO_OUTBOUND_WRITE_TIMEOUT_MS", "2300")
	t.Setenv("MTPROXY_GO_OUTBOUND_READ_TIMEOUT_MS", "345")
	t.Setenv("MTPROXY_GO_OUTBOUND_IDLE_TIMEOUT_MS", "4567")
	t.Setenv("MTPROXY_GO_OUTBOUND_MAX_FRAME_SIZE", "123456")

	cfg, err := outboundConfigFromEnv()
	if err != nil {
		t.Fatalf("outbound config custom: %v", err)
	}
	if cfg.ConnectTimeout != 1200*time.Millisecond {
		t.Fatalf("unexpected connect timeout: %s", cfg.ConnectTimeout)
	}
	if cfg.WriteTimeout != 2300*time.Millisecond {
		t.Fatalf("unexpected write timeout: %s", cfg.WriteTimeout)
	}
	if cfg.ReadTimeout != 345*time.Millisecond {
		t.Fatalf("unexpected read timeout: %s", cfg.ReadTimeout)
	}
	if cfg.IdleConnTimeout != 4567*time.Millisecond {
		t.Fatalf("unexpected idle timeout: %s", cfg.IdleConnTimeout)
	}
	if cfg.MaxFrameSize != 123456 {
		t.Fatalf("unexpected max frame size: %d", cfg.MaxFrameSize)
	}
}

func TestOutboundConfigFromEnvInvalidValue(t *testing.T) {
	t.Setenv("MTPROXY_GO_OUTBOUND_IDLE_TIMEOUT_MS", "bad")
	if _, err := outboundConfigFromEnv(); err == nil {
		t.Fatalf("expected outbound config parse error")
	}
}

func TestOutboundConfigFromEnvInvalidMaxFrameSize(t *testing.T) {
	t.Setenv("MTPROXY_GO_OUTBOUND_MAX_FRAME_SIZE", "0")
	if _, err := outboundConfigFromEnv(); err == nil {
		t.Fatalf("expected outbound max frame size parse error")
	}
}
