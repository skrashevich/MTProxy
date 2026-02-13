package cli_test

import (
	"bytes"
	"context"
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

func TestConfigCheckExitCodeParity(t *testing.T) {
	bin := testutil.BuildProxyBinary(t)
	repoRoot := testutil.RepoRoot(t)
	cfg := filepath.Join(repoRoot, "docker", "telegram", "backend.conf")

	cmd := exec.Command(bin, cfg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	code := testutil.ExitCode(err)
	if code != 2 {
		t.Fatalf("unexpected exit code: got=%d err=%v output=%s", code, err, out.String())
	}
	if !strings.Contains(out.String(), "runtime is not implemented yet") {
		t.Fatalf("expected bootstrap marker in output:\n%s", out.String())
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
