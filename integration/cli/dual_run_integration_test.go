package cli_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/integration/testutil"
)

func TestDualRunControlPlaneSLO(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dual-run harness is enabled only on Linux")
	}
	if runtime.GOARCH != "amd64" {
		t.Skip("dual-run harness currently requires amd64 (C build flags are x86-specific)")
	}
	if os.Getenv("MTPROXY_DUAL_RUN") != "1" {
		t.Skip("set MTPROXY_DUAL_RUN=1 to run C vs Go dual-run harness")
	}

	goBin := testutil.BuildProxyBinary(t)
	cBin := testutil.BuildCProxyBinary(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	goRes := runControlPlaneCycle(t, goBin, cfgPath, filepath.Join(dir, "go.log"), "runtime initialized:")
	cRes := runControlPlaneCycle(t, cBin, cfgPath, filepath.Join(dir, "c.log"), "config_filename")

	t.Logf("dual-run cycle: go_shutdown=%s c_shutdown=%s", goRes.shutdownLatency, cRes.shutdownLatency)

	// Control-plane SLO guard: Go shutdown should not be significantly worse than C.
	if goRes.shutdownLatency > 2*cRes.shutdownLatency+200*time.Millisecond {
		t.Fatalf(
			"go shutdown latency regressed: go=%s c=%s threshold=%s",
			goRes.shutdownLatency,
			cRes.shutdownLatency,
			2*cRes.shutdownLatency+200*time.Millisecond,
		)
	}
}

type controlPlaneResult struct {
	shutdownLatency time.Duration
}

func runControlPlaneCycle(t *testing.T, bin, cfgPath, logPath, startupMarker string) controlPlaneResult {
	t.Helper()

	cmd := exec.Command(bin, "-l", logPath, cfgPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContains(waitCtx, logPath, startupMarker); err != nil {
		logData, _ := os.ReadFile(logPath)
		t.Fatalf("wait startup marker %q for %s: %v\nlogs:\n%s", startupMarker, bin, err, string(logData))
	}

	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP to %s: %v", bin, err)
	}
	time.Sleep(150 * time.Millisecond)
	assertProcessAlive(t, cmd, "after SIGHUP")

	if err := cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatalf("send SIGUSR1 to %s: %v", bin, err)
	}
	time.Sleep(150 * time.Millisecond)
	assertProcessAlive(t, cmd, "after SIGUSR1")

	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to %s: %v", bin, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			logData, _ := os.ReadFile(logPath)
			t.Fatalf("%s exited with error: %v\nlogs:\n%s", bin, err, string(logData))
		}
	case <-time.After(8 * time.Second):
		logData, _ := os.ReadFile(logPath)
		t.Fatalf("timeout waiting process exit for %s\nlogs:\n%s", bin, string(logData))
	}

	return controlPlaneResult{
		shutdownLatency: time.Since(start),
	}
}

func assertProcessAlive(t *testing.T, cmd *exec.Cmd, phase string) {
	t.Helper()
	if cmd.Process == nil {
		t.Fatalf("process handle missing %s", phase)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("process is not alive %s: %v", phase, err)
	}
}
