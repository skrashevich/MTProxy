package docker_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/integration/testutil"
)

func TestDockerRunStyleArgsBootstrapCheck(t *testing.T) {
	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	secretPath := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretPath, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	cmd := exec.Command(
		bin,
		"-p", "2398",
		"--http-stats",
		"-H", "443",
		"-M", "2",
		"-C", "60000",
		"--aes-pwd", "/etc/telegram/hello-explorers-how-are-you-doing",
		"-u", "root",
		cfgPath,
		"--allow-skip-dh",
		"--nat-info", "10.0.0.2:203.0.113.10",
		"--mtproto-secret-file", secretPath,
		"-P", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	code := testutil.ExitCode(err)
	if code != 2 {
		t.Fatalf("unexpected exit code: got=%d err=%v output=%s", code, err, out.String())
	}
	text := out.String()
	for _, marker := range []string{
		"Go implementation bootstrap: config loaded",
		"runtime is not implemented yet",
		"usage:",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("missing marker %q in output:\n%s", marker, text)
		}
	}
}

func TestDockerRunStyleArgsRejectInvalidSecretFile(t *testing.T) {
	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	secretPath := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretPath, []byte("not-a-secret\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	cmd := exec.Command(
		bin,
		"-p", "2398",
		cfgPath,
		"--mtproto-secret-file", secretPath,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	code := testutil.ExitCode(err)
	if code != 2 {
		t.Fatalf("unexpected exit code: got=%d err=%v output=%s", code, err, out.String())
	}
	if !strings.Contains(out.String(), "Can not parse options") {
		t.Fatalf("expected parse error marker in output:\n%s", out.String())
	}
}

func TestDockerRunStyleArgsLoopSupervisor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are validated only on Unix-like systems")
	}

	bin := testutil.BuildProxyBinary(t)
	dir := t.TempDir()
	statsPort := findFreeLocalPort(t)

	cfgPath := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	secretPath := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretPath, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	logPath := filepath.Join(dir, "docker-loop.log")

	cmd := exec.Command(
		bin,
		"-p", fmt.Sprintf("%d", statsPort),
		"--http-stats",
		"-H", "443",
		"-M", "2",
		"-C", "60000",
		"--aes-pwd", "/etc/telegram/hello-explorers-how-are-you-doing",
		"-u", "root",
		"-l", logPath,
		cfgPath,
		"--allow-skip-dh",
		"--nat-info", "10.0.0.2:203.0.113.10",
		"--mtproto-secret-file", secretPath,
		"-P", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	cmd.Env = append(os.Environ(), "MTPROXY_GO_SIGNAL_LOOP=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start loop supervisor: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := waitForFileContains(waitCtx, logPath, "supervisor started worker id=1"); err != nil {
		t.Fatalf("wait for supervisor startup: %v", err)
	}
	if err := waitForFileContains(waitCtx, logPath, "[worker 0] stats server listening on"); err != nil {
		t.Fatalf("wait for stats bind: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop supervisor exit error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout waiting loop supervisor exit")
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read loop supervisor log: %v", err)
	}
	logs := string(logData)
	for _, marker := range []string{
		"Go bootstrap supervisor enabled: workers=2",
		"[worker 1] http-stats requested in supervisor mode, only worker 0 serves stats",
		"supervisor received SIGTERM, shutting down workers",
	} {
		if !strings.Contains(logs, marker) {
			t.Fatalf("missing marker %q in logs:\n%s", marker, logs)
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
