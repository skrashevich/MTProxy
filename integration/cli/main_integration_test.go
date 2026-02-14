package cli_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/integration/testutil"
)

func TestHelpExitCodeParity(t *testing.T) {
	bin := testutil.BuildProxyBinary(t)
	cmd := exec.Command(bin, "--help")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	code := testutil.ExitCode(err)
	if code != 2 {
		t.Fatalf("unexpected exit code: got=%d err=%v output=%s", code, err, out.String())
	}
	if !strings.Contains(out.String(), "usage:") {
		t.Fatalf("usage marker not found in output:\n%s", out.String())
	}
}

func TestDefaultRuntimeStartAndShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "runtime-default.log")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(bin, "-l", logPath, cfgPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start default runtime process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := waitForFileContains(waitCtx, logPath, "runtime initialized:"); err != nil {
		t.Fatalf("wait for runtime init: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runtime process exit error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read runtime log: %v", err)
	}
	logs := string(logData)
	for _, marker := range []string{
		"Go runtime enabled:",
		"runtime initialized:",
		"Terminated by SIGTERM.",
	} {
		if !strings.Contains(logs, marker) {
			t.Fatalf("missing marker %q in logs:\n%s", marker, logs)
		}
	}
}

func TestSignalLoopReloadAndShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "runtime.log")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(bin, "-l", logPath, cfgPath)
	cmd.Env = append(os.Environ(), "MTPROXY_GO_SIGNAL_LOOP=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start loop process: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() {
		_ = cmd.Process.Kill()
	}()

	if err := waitForFileContains(waitCtx, logPath, "runtime initialized:"); err != nil {
		t.Fatalf("wait for runtime init: %v", err)
	}

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.51:8888;"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop process exit error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read runtime log: %v", err)
	}
	logs := string(logData)
	for _, marker := range []string{
		"runtime initialized:",
		"re-read successfully",
		"SIGUSR1 received: log file reopened.",
		"Terminated by SIGTERM.",
	} {
		if !strings.Contains(logs, marker) {
			t.Fatalf("missing marker %q in logs:\n%s", marker, logs)
		}
	}
}

func TestSignalLoopReloadFailureDoesNotAbortProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "runtime.log")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(bin, "-l", logPath, cfgPath)
	cmd.Env = append(os.Environ(), "MTPROXY_GO_SIGNAL_LOOP=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start loop process: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() {
		_ = cmd.Process.Kill()
	}()

	if err := waitForFileContains(waitCtx, logPath, "runtime initialized:"); err != nil {
		t.Fatalf("wait for runtime init: %v", err)
	}

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:99999;"), 0o600); err != nil {
		t.Fatalf("rewrite config with invalid port: %v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}

	if err := waitForFileContains(waitCtx, logPath, "configuration reload failed:"); err != nil {
		t.Fatalf("wait for reload failure marker: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop process exit error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read runtime log: %v", err)
	}
	logs := string(logData)
	for _, marker := range []string{
		"runtime initialized:",
		"configuration reload failed:",
		"Terminated by SIGTERM.",
	} {
		if !strings.Contains(logs, marker) {
			t.Fatalf("missing marker %q in logs:\n%s", marker, logs)
		}
	}
}

func TestSignalLoopSIGUSR1WithoutLogFileIsNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	outPath := filepath.Join(dir, "runtime.out")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open output file: %v", err)
	}
	defer outFile.Close()

	cmd := exec.Command(bin, cfgPath)
	cmd.Env = append(os.Environ(), "MTPROXY_GO_SIGNAL_LOOP=1")
	cmd.Stdout = outFile
	cmd.Stderr = outFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start loop process: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() {
		_ = cmd.Process.Kill()
	}()

	if err := waitForFileContains(waitCtx, outPath, "runtime initialized:"); err != nil {
		t.Fatalf("wait for runtime init: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop process exit error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}

	logData, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read runtime output: %v", err)
	}
	logs := string(logData)
	for _, marker := range []string{
		"runtime initialized:",
		"SIGUSR1 received: no log file configured, skipping reopen.",
		"Terminated by SIGTERM.",
	} {
		if !strings.Contains(logs, marker) {
			t.Fatalf("missing marker %q in logs:\n%s", marker, logs)
		}
	}
	if strings.Contains(logs, "SIGUSR1 log reopen failed:") {
		t.Fatalf("unexpected reopen failure in logs:\n%s", logs)
	}
}

func TestSignalLoopSupervisorModeShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "supervisor.log")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(bin, "-M", "2", "-l", logPath, cfgPath)
	cmd.Env = append(os.Environ(), "MTPROXY_GO_SIGNAL_LOOP=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start supervisor process: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	defer func() {
		_ = cmd.Process.Kill()
	}()

	if err := waitForFileContains(waitCtx, logPath, "supervisor started worker id=1"); err != nil {
		t.Fatalf("wait for supervisor start: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervisor process exit error: %v", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read supervisor log: %v", err)
	}
	logs := string(logData)
	for _, marker := range []string{
		"Go bootstrap supervisor enabled: workers=2",
		"supervisor started worker id=0",
		"supervisor started worker id=1",
		"supervisor received SIGTERM, shutting down workers",
	} {
		if !strings.Contains(logs, marker) {
			t.Fatalf("missing marker %q in logs:\n%s", marker, logs)
		}
	}
}

func TestSignalLoopSupervisorForwardsReloadAndReopen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "supervisor-forward.log")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(bin, "-M", "2", "-l", logPath, cfgPath)
	cmd.Env = append(os.Environ(), "MTPROXY_GO_SIGNAL_LOOP=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start supervisor process: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	defer func() {
		_ = cmd.Process.Kill()
	}()

	if err := waitForFileContains(waitCtx, logPath, "supervisor started worker id=1"); err != nil {
		t.Fatalf("wait for supervisor start: %v", err)
	}

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.51:8888;"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}
	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"[worker 0] configuration file",
		"[worker 1] configuration file",
		"re-read successfully",
	}); err != nil {
		t.Fatalf("wait for reload markers: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1: %v", err)
	}
	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"supervisor SIGUSR1: log file reopened.",
		"[worker 0] SIGUSR1 received: log file reopened.",
		"[worker 1] SIGUSR1 received: log file reopened.",
	}); err != nil {
		t.Fatalf("wait for reopen markers: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervisor process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopSupervisorWorkerCrashReaction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "supervisor-crash.log")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(bin, "-M", "2", "-l", logPath, cfgPath)
	cmd.Env = append(os.Environ(), "MTPROXY_GO_SIGNAL_LOOP=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start supervisor process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	pid, err := waitForWorkerPID(waitCtx, logPath, 0)
	if err != nil {
		t.Fatalf("wait for worker pid: %v", err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill worker pid=%d: %v", pid, err)
	}

	err = cmd.Wait()
	if code := testutil.ExitCode(err); code != 1 {
		t.Fatalf("unexpected supervisor exit code: %d err=%v", code, err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read crash log: %v", err)
	}
	logs := string(logData)
	if !strings.Contains(logs, "exited unexpectedly") {
		t.Fatalf("expected crash reaction marker in logs:\n%s", logs)
	}
	if !strings.Contains(logs, "supervisor error: worker") {
		t.Fatalf("expected supervisor error marker in logs:\n%s", logs)
	}
}

func TestSupervisedWorkerParentMismatchExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "worker-parent-mismatch.log")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(bin, "-l", logPath, cfgPath)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_SIGNAL_LOOP=1",
		"MTPROXY_GO_SUPERVISED_WORKER=1",
		"MTPROXY_GO_WORKER_ID=0",
		"MTPROXY_GO_SUPERVISOR_PID=999999",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start worker process: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if code := testutil.ExitCode(err); code != 1 {
			t.Fatalf("unexpected worker exit code: %d err=%v", code, err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("timeout waiting worker exit on parent mismatch")
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read worker mismatch log: %v", err)
	}
	logs := string(logData)
	for _, marker := range []string{
		"[worker 0] runtime initialized:",
		"[worker 0] supervised worker parent changed:",
		"[worker 0] signal loop error: context canceled",
	} {
		if !strings.Contains(logs, marker) {
			t.Fatalf("missing marker %q in logs:\n%s", marker, logs)
		}
	}
}

func TestSupervisedWorkerInvalidSupervisorPIDExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "worker-invalid-parent.log")

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(bin, "-l", logPath, cfgPath)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_SIGNAL_LOOP=1",
		"MTPROXY_GO_SUPERVISED_WORKER=1",
		"MTPROXY_GO_WORKER_ID=0",
		"MTPROXY_GO_SUPERVISOR_PID=bad",
	)
	err := cmd.Run()
	if code := testutil.ExitCode(err); code != 1 {
		t.Fatalf("unexpected worker exit code: %d err=%v", code, err)
	}

	logData, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read invalid parent log: %v", readErr)
	}
	logs := string(logData)
	for _, marker := range []string{
		"[worker 0] supervised worker startup error: invalid MTPROXY_GO_SUPERVISOR_PID=",
		"[worker 0] signal loop error: context canceled",
	} {
		if !strings.Contains(logs, marker) {
			t.Fatalf("missing marker %q in logs:\n%s", marker, logs)
		}
	}
}

func TestSignalLoopSupervisorHTTPStatsSingleWorkerBinder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "supervisor-http-stats.log")
	statsPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"-M", "2",
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(os.Environ(), "MTPROXY_GO_SIGNAL_LOOP=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start supervisor process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"[worker 0] stats server listening on 127.0.0.1:",
		"[worker 1] http-stats requested in supervisor mode, only worker 0 serves stats",
	}); err != nil {
		t.Fatalf("wait for stats binding markers: %v", err)
	}

	statsBody, err := waitForStatsBody(waitCtx, statsPort)
	if err != nil {
		t.Fatalf("wait for stats endpoint: %v", err)
	}
	if !strings.Contains(statsBody, "stats_generated_at\t") {
		t.Fatalf("unexpected stats payload: %s", statsBody)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read stats log: %v", err)
	}
	if strings.Contains(string(logData), "failed to start stats server on") {
		t.Fatalf("unexpected stats bind failure in logs:\n%s", string(logData))
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervisor process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopSupervisorIngressOutboundSingleWorkerBinder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "supervisor-ingress-outbound.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"-M", "2",
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_ENABLE_INGRESS=1",
		"MTPROXY_GO_ENABLE_OUTBOUND=1",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start supervisor process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"[worker 0] stats server listening on 127.0.0.1:",
		fmt.Sprintf("[worker 0] ingress server listening on 127.0.0.1:%d", ingressPort),
		"[worker 0] outbound transport enabled.",
		"[worker 1] ingress requested in supervisor mode, only worker 0 serves ingress",
		"[worker 1] outbound requested in supervisor mode, only worker 0 enables outbound transport",
	}); err != nil {
		t.Fatalf("wait for ingress/outbound binding markers: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervisor process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopIngressProcessesFrames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "ingress.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_SIGNAL_LOOP=1",
		"MTPROXY_GO_ENABLE_INGRESS=1",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start ingress loop process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
		"stats server listening on 127.0.0.1:",
	}); err != nil {
		t.Fatalf("wait for ingress+stats markers: %v", err)
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	if err := writeIngressFrame(conn, buildHandshakeFrameForIngress(0x60469778)); err != nil {
		t.Fatalf("write handshake frame: %v", err)
	}
	if err := writeIngressFrame(conn, buildEncryptedFrameForIngress(0x0102030405060708)); err != nil {
		t.Fatalf("write encrypted frame: %v", err)
	}
	_ = conn.Close()

	var statsBody string
	deadline := time.Now().Add(5 * time.Second)
	for {
		body, err := waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait for stats endpoint: %v", err)
		}
		statsBody = body
		if statInt(statsBody, "dataplane_packets_total") >= 2 &&
			statInt(statsBody, "dataplane_packets_encrypted") >= 1 &&
			statInt(statsBody, "dataplane_packets_handshake") >= 1 &&
			statInt(statsBody, "ingress_frames_handled") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting ingress counters in stats:\n%s", statsBody)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopIngressAppliesMaxAcceptRate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "ingress-max-accept-rate.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"--max-accept-rate", "1",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_SIGNAL_LOOP=1",
		"MTPROXY_GO_ENABLE_INGRESS=1",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start ingress loop process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
		"stats server listening on 127.0.0.1:",
	}); err != nil {
		t.Fatalf("wait for ingress+stats markers: %v", err)
	}

	for i := 0; i < 8; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
		if err != nil {
			t.Fatalf("dial ingress #%d: %v", i+1, err)
		}
		_ = conn.Close()
	}

	var statsBody string
	deadline := time.Now().Add(5 * time.Second)
	for {
		body, err := waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait for stats endpoint: %v", err)
		}
		statsBody = body
		if statInt(statsBody, "ingress_accept_rate_limited") >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting accept-rate-limited counters in stats:\n%s", statsBody)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopIngressAppliesMaxDHAcceptRate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "ingress-max-dh-rate.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"--max-dh-accept-rate", "1",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_SIGNAL_LOOP=1",
		"MTPROXY_GO_ENABLE_INGRESS=1",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start ingress loop process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
		"stats server listening on 127.0.0.1:",
	}); err != nil {
		t.Fatalf("wait for ingress+stats markers: %v", err)
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	for i := 0; i < 8; i++ {
		if err := writeIngressFrame(conn, buildHandshakeFrameForIngress(0x60469778)); err != nil {
			t.Fatalf("write handshake frame #%d: %v", i+1, err)
		}
	}
	_ = conn.Close()

	var statsBody string
	deadline := time.Now().Add(5 * time.Second)
	for {
		statsBody, err = waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait for stats endpoint: %v", err)
		}
		if statInt(statsBody, "dataplane_packets_rejected_dh_rate") >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting dh-rate-limited counters in stats:\n%s", statsBody)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopIngressOutboundDeliversToBackend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start backend listener: %v", err)
	}
	defer backendLn.Close()
	backendPort := backendLn.Addr().(*net.TCPAddr).Port

	recvPayloadCh := make(chan []byte, 1)
	backendErrCh := make(chan error, 1)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			backendErrCh <- err
			return
		}
		defer conn.Close()
		payload, err := readIngressFrame(conn)
		if err != nil {
			backendErrCh <- err
			return
		}
		recvPayloadCh <- payload
	}()

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "ingress-outbound.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("proxy 127.0.0.1:%d;", backendPort)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_SIGNAL_LOOP=1",
		"MTPROXY_GO_ENABLE_INGRESS=1",
		"MTPROXY_GO_ENABLE_OUTBOUND=1",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		"outbound transport enabled.",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
		"stats server listening on 127.0.0.1:",
	}); err != nil {
		t.Fatalf("wait for startup markers: %v", err)
	}

	frame := buildHandshakeFrameForIngress(0x60469778)
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	if err := writeIngressFrame(conn, frame); err != nil {
		t.Fatalf("write ingress frame: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-backendErrCh:
		t.Fatalf("backend error: %v", err)
	case payload := <-recvPayloadCh:
		if !bytes.Equal(payload, frame) {
			t.Fatalf("backend payload mismatch: got=%x want=%x", payload, frame)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting backend payload")
	}

	var statsBody string
	deadline := time.Now().Add(5 * time.Second)
	for {
		statsBody, err = waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait stats: %v", err)
		}
		if statInt(statsBody, "outbound_sends") >= 1 &&
			statInt(statsBody, "outbound_bytes_sent") >= int64(4+len(frame)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting outbound stats:\n%s", statsBody)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopIngressReturnsBackendResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start backend listener: %v", err)
	}
	defer backendLn.Close()
	backendPort := backendLn.Addr().(*net.TCPAddr).Port

	requestPayload := buildHandshakeFrameForIngress(0x60469778)
	responsePayload := []byte("backend-response-frame")

	backendErrCh := make(chan error, 1)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			backendErrCh <- err
			return
		}
		defer conn.Close()
		payload, err := readIngressFrame(conn)
		if err != nil {
			backendErrCh <- err
			return
		}
		if !bytes.Equal(payload, requestPayload) {
			backendErrCh <- fmt.Errorf("unexpected backend request payload")
			return
		}
		if err := writeIngressFrame(conn, responsePayload); err != nil {
			backendErrCh <- err
			return
		}
		backendErrCh <- nil
	}()

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "ingress-response.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("proxy 127.0.0.1:%d;", backendPort)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_SIGNAL_LOOP=1",
		"MTPROXY_GO_ENABLE_INGRESS=1",
		"MTPROXY_GO_ENABLE_OUTBOUND=1",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		"outbound transport enabled.",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
	}); err != nil {
		t.Fatalf("wait startup markers: %v", err)
	}

	clientConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer clientConn.Close()

	if err := writeIngressFrame(clientConn, requestPayload); err != nil {
		t.Fatalf("write ingress frame: %v", err)
	}
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	gotResp, err := readIngressFrame(clientConn)
	if err != nil {
		t.Fatalf("read ingress response: %v", err)
	}
	if !bytes.Equal(gotResp, responsePayload) {
		t.Fatalf("response payload mismatch: got=%x want=%x", gotResp, responsePayload)
	}

	if backendErr := <-backendErrCh; backendErr != nil {
		t.Fatalf("backend exchange error: %v", backendErr)
	}

	var statsBody string
	deadline := time.Now().Add(5 * time.Second)
	for {
		statsBody, err = waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait stats: %v", err)
		}
		if statInt(statsBody, "outbound_responses") >= 1 &&
			statInt(statsBody, "ingress_frames_returned") >= 1 &&
			statInt(statsBody, "ingress_bytes_returned") >= int64(4+len(responsePayload)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting response stats:\n%s", statsBody)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopIngressOutboundBurstStability(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	const burstFrames = 100

	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start backend listener: %v", err)
	}
	defer backendLn.Close()
	backendPort := backendLn.Addr().(*net.TCPAddr).Port

	backendErrCh := make(chan error, 1)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			backendErrCh <- err
			return
		}
		defer conn.Close()
		for i := 0; i < burstFrames; i++ {
			payload, err := readIngressFrame(conn)
			if err != nil {
				backendErrCh <- err
				return
			}
			if err := writeIngressFrame(conn, payload); err != nil {
				backendErrCh <- err
				return
			}
		}
		backendErrCh <- nil
	}()

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "ingress-burst.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("proxy 127.0.0.1:%d;", backendPort)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_ENABLE_INGRESS=1",
		"MTPROXY_GO_ENABLE_OUTBOUND=1",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		"outbound transport enabled.",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
	}); err != nil {
		t.Fatalf("wait startup markers: %v", err)
	}

	clientConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer clientConn.Close()

	for i := 0; i < burstFrames; i++ {
		frame := buildHandshakeFrameForIngress(protocolFuncForIndex(i))
		if err := writeIngressFrame(clientConn, frame); err != nil {
			t.Fatalf("write ingress frame #%d: %v", i+1, err)
		}
		gotResp, err := readIngressFrame(clientConn)
		if err != nil {
			t.Fatalf("read ingress response #%d: %v", i+1, err)
		}
		if !bytes.Equal(gotResp, frame) {
			t.Fatalf("response mismatch on frame #%d", i+1)
		}
	}

	if backendErr := <-backendErrCh; backendErr != nil {
		t.Fatalf("backend exchange error: %v", backendErr)
	}

	var statsBody string
	deadline := time.Now().Add(6 * time.Second)
	for {
		statsBody, err = waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait stats: %v", err)
		}
		if statInt(statsBody, "dataplane_packets_total") >= burstFrames &&
			statInt(statsBody, "outbound_sends") >= burstFrames &&
			statInt(statsBody, "outbound_responses") >= burstFrames &&
			statInt(statsBody, "ingress_frames_handled") >= burstFrames &&
			statInt(statsBody, "ingress_frames_returned") >= burstFrames &&
			statInt(statsBody, "dataplane_packets_outbound_errors") == 0 &&
			statInt(statsBody, "ingress_frames_failed") == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting burst counters:\n%s", statsBody)
		}
		time.Sleep(120 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopOutboundIdleEvictionMetrics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start backend listener: %v", err)
	}
	defer backendLn.Close()
	backendPort := backendLn.Addr().(*net.TCPAddr).Port

	backendErrCh := make(chan error, 2)
	go func() {
		conn1, err := backendLn.Accept()
		if err != nil {
			backendErrCh <- err
			return
		}
		go func() {
			defer conn1.Close()
			payload, err := readIngressFrame(conn1)
			if err != nil {
				backendErrCh <- err
				return
			}
			if err := writeIngressFrame(conn1, payload); err != nil {
				backendErrCh <- err
				return
			}
			// Keep first backend connection alive to force client-side idle eviction path.
			time.Sleep(2 * time.Second)
			backendErrCh <- nil
		}()

		conn2, err := backendLn.Accept()
		if err != nil {
			backendErrCh <- err
			return
		}
		go func() {
			defer conn2.Close()
			payload, err := readIngressFrame(conn2)
			if err != nil {
				backendErrCh <- err
				return
			}
			if err := writeIngressFrame(conn2, payload); err != nil {
				backendErrCh <- err
				return
			}
			backendErrCh <- nil
		}()
	}()

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "outbound-idle-eviction.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("proxy 127.0.0.1:%d;", backendPort)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_ENABLE_INGRESS=1",
		"MTPROXY_GO_ENABLE_OUTBOUND=1",
		"MTPROXY_GO_OUTBOUND_IDLE_TIMEOUT_MS=80",
		"MTPROXY_GO_OUTBOUND_READ_TIMEOUT_MS=1000",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		"outbound transport enabled.",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
	}); err != nil {
		t.Fatalf("wait startup markers: %v", err)
	}

	clientConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer clientConn.Close()

	frame1 := buildHandshakeFrameForIngress(0x60469778)
	if err := writeIngressFrame(clientConn, frame1); err != nil {
		t.Fatalf("write frame #1: %v", err)
	}
	if _, err := readIngressFrame(clientConn); err != nil {
		t.Fatalf("read frame #1 response: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	frame2 := buildHandshakeFrameForIngress(0xbe7e8ef1)
	if err := writeIngressFrame(clientConn, frame2); err != nil {
		t.Fatalf("write frame #2: %v", err)
	}
	if _, err := readIngressFrame(clientConn); err != nil {
		t.Fatalf("read frame #2 response: %v", err)
	}

	for i := 0; i < 2; i++ {
		if backendErr := <-backendErrCh; backendErr != nil {
			t.Fatalf("backend exchange error: %v", backendErr)
		}
	}

	var statsBody string
	deadline := time.Now().Add(6 * time.Second)
	for {
		statsBody, err = waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait stats: %v", err)
		}
		if statInt(statsBody, "outbound_idle_evictions") >= 1 &&
			statInt(statsBody, "outbound_dials") >= 2 &&
			statInt(statsBody, "outbound_sends") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting idle eviction stats:\n%s", statsBody)
		}
		time.Sleep(120 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopOutboundMaxFrameSizeRejectsOversizedPayload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start backend listener: %v", err)
	}
	defer backendLn.Close()
	backendPort := backendLn.Addr().(*net.TCPAddr).Port

	backendAcceptedCh := make(chan struct{}, 1)
	go func() {
		conn, err := backendLn.Accept()
		if err == nil {
			backendAcceptedCh <- struct{}{}
			_ = conn.Close()
		}
	}()

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "outbound-max-frame.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("proxy 127.0.0.1:%d;", backendPort)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_ENABLE_INGRESS=1",
		"MTPROXY_GO_ENABLE_OUTBOUND=1",
		"MTPROXY_GO_OUTBOUND_MAX_FRAME_SIZE=64",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		"outbound transport enabled.",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
	}); err != nil {
		t.Fatalf("wait startup markers: %v", err)
	}

	clientConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer clientConn.Close()

	oversized := buildHandshakeFrameForIngressWithLen(84, 64, 0x60469778)
	if err := writeIngressFrame(clientConn, oversized); err != nil {
		t.Fatalf("write oversized frame: %v", err)
	}

	var statsBody string
	deadline := time.Now().Add(5 * time.Second)
	for {
		statsBody, err = waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait stats: %v", err)
		}
		if statInt(statsBody, "dataplane_packets_outbound_errors") >= 1 &&
			statInt(statsBody, "ingress_frames_failed") >= 1 &&
			statInt(statsBody, "outbound_send_errors") >= 1 &&
			statInt(statsBody, "outbound_dials") == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting oversized-frame stats:\n%s", statsBody)
		}
		time.Sleep(120 * time.Millisecond)
	}

	select {
	case <-backendAcceptedCh:
		t.Fatalf("backend should not be dialed for oversized payload")
	default:
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func TestSignalLoopIngressOutboundSoakLoadFDAndMemoryGuards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	const (
		totalFrames     = 220
		largeFrameEvery = 11
		largeFrameBytes = 1 << 20 // 1 MiB
		fdLeakBudget    = 32
		rssGrowthBudget = 160 << 20 // 160 MiB
	)

	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start backend listener: %v", err)
	}
	defer backendLn.Close()
	backendPort := backendLn.Addr().(*net.TCPAddr).Port

	backendErrCh := make(chan error, 1)
	go func() {
		for {
			conn, err := backendLn.Accept()
			if err != nil {
				backendErrCh <- err
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					payload, err := readIngressFrame(c)
					if err != nil {
						return
					}
					if err := writeIngressFrame(c, payload); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	logPath := filepath.Join(dir, "soak-load-fd-mem.log")
	statsPort := findFreeLocalPort(t)
	ingressPort := findFreeLocalPort(t)

	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("proxy 127.0.0.1:%d;", backendPort)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(
		bin,
		"--http-stats",
		"-p", fmt.Sprintf("%d", statsPort),
		"-l", logPath,
		cfgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"MTPROXY_GO_ENABLE_INGRESS=1",
		"MTPROXY_GO_ENABLE_OUTBOUND=1",
		"MTPROXY_GO_OUTBOUND_READ_TIMEOUT_MS=1200",
		fmt.Sprintf("MTPROXY_GO_INGRESS_ADDR=127.0.0.1:%d", ingressPort),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := waitForFileContainsAll(waitCtx, logPath, []string{
		"runtime initialized:",
		"outbound transport enabled.",
		fmt.Sprintf("ingress server listening on 127.0.0.1:%d", ingressPort),
	}); err != nil {
		t.Fatalf("wait startup markers: %v", err)
	}

	clientConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingressPort))
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer clientConn.Close()

	// Warmup request to stabilize baseline after outbound/backend connection establishment.
	warmup := buildHandshakeFrameForIngress(0x60469778)
	if err := writeIngressFrame(clientConn, warmup); err != nil {
		t.Fatalf("write warmup frame: %v", err)
	}
	if _, err := readIngressFrame(clientConn); err != nil {
		t.Fatalf("read warmup response: %v", err)
	}

	startFDs, fdErr := processOpenFDCount(cmd.Process.Pid)
	if fdErr != nil {
		t.Logf("fd baseline unavailable: %v", fdErr)
	}
	startRSS, rssErr := processRSSBytes(cmd.Process.Pid)
	if rssErr != nil {
		t.Logf("rss baseline unavailable: %v", rssErr)
	}

	for i := 0; i < totalFrames; i++ {
		var frame []byte
		if i > 0 && i%largeFrameEvery == 0 {
			frame = buildHandshakeFrameForIngressWithLen(largeFrameBytes, 20, protocolFuncForIndex(i))
		} else {
			frame = buildHandshakeFrameForIngress(protocolFuncForIndex(i))
		}
		if err := writeIngressFrame(clientConn, frame); err != nil {
			t.Fatalf("write frame #%d: %v", i+1, err)
		}
		resp, err := readIngressFrame(clientConn)
		if err != nil {
			t.Fatalf("read response #%d: %v", i+1, err)
		}
		if !bytes.Equal(resp, frame) {
			t.Fatalf("payload mismatch on frame #%d", i+1)
		}
	}

	var statsBody string
	deadline := time.Now().Add(8 * time.Second)
	for {
		statsBody, err = waitForStatsBody(waitCtx, statsPort)
		if err != nil {
			t.Fatalf("wait stats: %v", err)
		}
		if statInt(statsBody, "dataplane_packets_total") >= int64(totalFrames+1) &&
			statInt(statsBody, "outbound_sends") >= int64(totalFrames+1) &&
			statInt(statsBody, "outbound_responses") >= int64(totalFrames+1) &&
			statInt(statsBody, "ingress_frames_handled") >= int64(totalFrames+1) &&
			statInt(statsBody, "ingress_frames_returned") >= int64(totalFrames+1) &&
			statInt(statsBody, "dataplane_packets_outbound_errors") == 0 &&
			statInt(statsBody, "ingress_frames_failed") == 0 &&
			statInt(statsBody, "ingress_read_errors") == 0 &&
			statInt(statsBody, "ingress_write_errors") == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting soak counters:\n%s", statsBody)
		}
		time.Sleep(120 * time.Millisecond)
	}

	_ = clientConn.Close()
	time.Sleep(350 * time.Millisecond)

	endFDs, fdEndErr := processOpenFDCount(cmd.Process.Pid)
	if fdErr == nil && fdEndErr != nil {
		t.Fatalf("fd final unavailable: %v", fdEndErr)
	}
	endRSS, rssEndErr := processRSSBytes(cmd.Process.Pid)
	if rssErr == nil && rssEndErr != nil {
		t.Fatalf("rss final unavailable: %v", rssEndErr)
	}

	if fdErr == nil && fdEndErr == nil && endFDs > startFDs+fdLeakBudget {
		t.Fatalf("possible fd leak: start=%d end=%d budget=%d", startFDs, endFDs, fdLeakBudget)
	}
	if rssErr == nil && rssEndErr == nil && endRSS > startRSS+rssGrowthBudget {
		t.Fatalf(
			"rss growth exceeds budget: start=%d end=%d growth=%d budget=%d",
			startRSS,
			endRSS,
			endRSS-startRSS,
			rssGrowthBudget,
		)
	}

	select {
	case be := <-backendErrCh:
		if be != nil {
			t.Fatalf("backend error: %v", be)
		}
	default:
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exit error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting process exit")
	}
}

func waitForFileContains(ctx context.Context, path, needle string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), needle) {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %q in %s: %w", needle, path, ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitForFileContainsAll(ctx context.Context, path string, needles []string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		data, err := os.ReadFile(path)
		if err == nil {
			text := string(data)
			ok := true
			for _, needle := range needles {
				if !strings.Contains(text, needle) {
					ok = false
					break
				}
			}
			if ok {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting markers %v in %s: %w", needles, path, ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitForWorkerPID(ctx context.Context, path string, workerID int) (int, error) {
	re := regexp.MustCompile(fmt.Sprintf(`supervisor started worker id=%d pid=(\d+)`, workerID))
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		data, err := os.ReadFile(path)
		if err == nil {
			m := re.FindStringSubmatch(string(data))
			if len(m) == 2 {
				pid, convErr := strconv.Atoi(m[1])
				if convErr == nil && pid > 0 {
					return pid, nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("timeout waiting worker id=%d pid in %s: %w", workerID, path, ctx.Err())
		case <-ticker.C:
		}
	}
}

func findFreeLocalPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForStatsBody(ctx context.Context, port int) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/stats", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK {
				return string(body), nil
			}
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting stats endpoint %s: %w", url, ctx.Err())
		case <-ticker.C:
		}
	}
}

func writeIngressFrame(conn net.Conn, frame []byte) error {
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(frame)))
	if _, err := conn.Write(hdr[:]); err != nil {
		return err
	}
	_, err := conn.Write(frame)
	return err
}

func buildHandshakeFrameForIngress(fn uint32) []byte {
	frame := make([]byte, 40)
	binary.LittleEndian.PutUint32(frame[16:20], 20)
	binary.LittleEndian.PutUint32(frame[20:24], fn)
	return frame
}

func buildEncryptedFrameForIngress(authKeyID uint64) []byte {
	frame := make([]byte, 56)
	binary.LittleEndian.PutUint64(frame[:8], authKeyID)
	return frame
}

func buildHandshakeFrameForIngressWithLen(totalLen int, innerLen uint32, fn uint32) []byte {
	frame := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(frame[16:20], innerLen)
	binary.LittleEndian.PutUint32(frame[20:24], fn)
	return frame
}

func protocolFuncForIndex(i int) uint32 {
	switch i % 4 {
	case 0:
		return 0x60469778
	case 1:
		return 0xbe7e8ef1
	case 2:
		return 0xd712e4be
	default:
		return 0xf5045f1f
	}
}

func statInt(statsBody string, key string) int64 {
	prefix := key + "\t"
	for _, line := range strings.Split(statsBody, "\n") {
		if strings.HasPrefix(line, prefix) {
			v := strings.TrimPrefix(line, prefix)
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err == nil {
				return n
			}
		}
	}
	return 0
}

func readIngressFrame(conn net.Conn) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, err
	}
	frameLen := int(binary.LittleEndian.Uint32(hdr[:]))
	if frameLen < 0 || frameLen > (8<<20) {
		return nil, fmt.Errorf("bad frame length: %d", frameLen)
	}
	frame := make([]byte, frameLen)
	if _, err := io.ReadFull(conn, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

func processOpenFDCount(pid int) (int, error) {
	switch runtime.GOOS {
	case "linux":
		entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		if err != nil {
			return 0, err
		}
		return len(entries), nil
	case "darwin":
		out, err := exec.Command("lsof", "-n", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return 0, err
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) == 0 {
			return 0, fmt.Errorf("empty lsof output")
		}
		return len(lines) - 1, nil
	default:
		return 0, fmt.Errorf("unsupported os for fd count: %s", runtime.GOOS)
	}
}

func processRSSBytes(pid int) (int64, error) {
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return 0, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "VmRSS:") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, fmt.Errorf("bad VmRSS format: %q", line)
			}
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return kb * 1024, nil
		}
		return 0, fmt.Errorf("VmRSS not found")
	case "darwin":
		out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return 0, err
		}
		raw := strings.TrimSpace(string(out))
		if raw == "" {
			return 0, fmt.Errorf("empty ps rss output")
		}
		kb, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, err
		}
		return kb * 1024, nil
	default:
		return 0, fmt.Errorf("unsupported os for rss: %s", runtime.GOOS)
	}
}
