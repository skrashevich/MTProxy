package cli_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
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
	cRes := runControlPlaneCycle(t, cBin, cfgPath, filepath.Join(dir, "c.log"), "")

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

func TestDualRunDataplaneCanarySLO(t *testing.T) {
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

	goPort := findFreeLocalPort(t)
	cPort := findFreeLocalPort(t)

	goRes := runDataplaneCanaryCycle(
		t,
		goBin,
		cfgPath,
		filepath.Join(dir, "go-dataplane.log"),
		"runtime initialized:",
		goPort,
	)
	cRes := runDataplaneCanaryCycle(
		t,
		cBin,
		cfgPath,
		filepath.Join(dir, "c-dataplane.log"),
		"",
		cPort,
	)

	t.Logf(
		"dual-run dataplane canary:\n"+
			"  go_connect success=%.3f p95=%s avg=%s\n"+
			"  c_connect success=%.3f p95=%s avg=%s\n"+
			"  go_stats success=%.3f p95=%s avg=%s\n"+
			"  c_stats success=%.3f p95=%s avg=%s\n"+
			"  go_shutdown=%s c_shutdown=%s",
		goRes.connect.successRate(), goRes.connect.p95Latency, goRes.connect.avgLatency,
		cRes.connect.successRate(), cRes.connect.p95Latency, cRes.connect.avgLatency,
		goRes.stats.successRate(), goRes.stats.p95Latency, goRes.stats.avgLatency,
		cRes.stats.successRate(), cRes.stats.p95Latency, cRes.stats.avgLatency,
		goRes.shutdownLatency, cRes.shutdownLatency,
	)

	assertDualRunCanarySLO(t, "connect", goRes.connect, cRes.connect, 0.02, 2.0, 25*time.Millisecond)
	assertDualRunCanarySLO(t, "stats", goRes.stats, cRes.stats, 0.02, 2.5, 40*time.Millisecond)

	// Dataplane canary also keeps control-plane shutdown close to C baseline.
	if goRes.shutdownLatency > 2*cRes.shutdownLatency+250*time.Millisecond {
		t.Fatalf(
			"go shutdown latency regressed in dataplane canary: go=%s c=%s threshold=%s",
			goRes.shutdownLatency,
			cRes.shutdownLatency,
			2*cRes.shutdownLatency+250*time.Millisecond,
		)
	}
}

type controlPlaneResult struct {
	shutdownLatency time.Duration
}

type canaryLatencyResult struct {
	attempts    int
	successes   int
	failures    int
	avgLatency  time.Duration
	p50Latency  time.Duration
	p95Latency  time.Duration
	maxLatency  time.Duration
}

func (r canaryLatencyResult) successRate() float64 {
	if r.attempts == 0 {
		return 0
	}
	return float64(r.successes) / float64(r.attempts)
}

type dataplaneCanaryResult struct {
	connect         canaryLatencyResult
	stats           canaryLatencyResult
	shutdownLatency time.Duration
}

func runControlPlaneCycle(t *testing.T, bin, cfgPath, logPath, startupMarker string) controlPlaneResult {
	t.Helper()

	outputFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open log output %s: %v", logPath, err)
	}
	defer outputFile.Close()

	cmd := exec.Command(bin, "-u", "root", "-l", logPath, cfgPath)
	cmd.Stdout = outputFile
	cmd.Stderr = outputFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	waitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if startupMarker != "" {
		if err := waitForFileContains(waitCtx, logPath, startupMarker); err != nil {
			logData, _ := os.ReadFile(logPath)
			t.Fatalf("wait startup marker %q for %s: %v\nlogs:\n%s", startupMarker, bin, err, string(logData))
		}
	} else {
		if err := waitForProcessAlive(waitCtx, cmd, 250*time.Millisecond); err != nil {
			logData, _ := os.ReadFile(logPath)
			t.Fatalf("wait process alive for %s: %v\nlogs:\n%s", bin, err, string(logData))
		}
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
		if !isExpectedTerminateExit(bin, err) {
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

func runDataplaneCanaryCycle(
	t *testing.T,
	bin string,
	cfgPath string,
	logPath string,
	startupMarker string,
	port int,
) dataplaneCanaryResult {
	t.Helper()

	outputFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open log output %s: %v", logPath, err)
	}
	defer outputFile.Close()

	args := []string{"-u", "root", "-p", fmt.Sprintf("%d", port), "--http-stats", "-l", logPath, cfgPath}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = outputFile
	cmd.Stderr = outputFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	waitCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	if startupMarker != "" {
		if err := waitForFileContains(waitCtx, logPath, startupMarker); err != nil {
			logData, _ := os.ReadFile(logPath)
			t.Fatalf("wait startup marker %q for %s: %v\nlogs:\n%s", startupMarker, bin, err, string(logData))
		}
	} else {
		if err := waitForProcessAlive(waitCtx, cmd, 250*time.Millisecond); err != nil {
			logData, _ := os.ReadFile(logPath)
			t.Fatalf("wait process alive for %s: %v\nlogs:\n%s", bin, err, string(logData))
		}
	}
	if _, err := waitForStatsBody(waitCtx, port); err != nil {
		logData, _ := os.ReadFile(logPath)
		t.Fatalf("wait stats endpoint for %s: %v\nlogs:\n%s", bin, err, string(logData))
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	connectRes := runDialCanary(addr, 160, 16, 450*time.Millisecond)
	statsRes := runHTTPStatsCanary(port, 120, 12, 650*time.Millisecond)

	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to %s: %v", bin, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if !isExpectedTerminateExit(bin, err) {
			logData, _ := os.ReadFile(logPath)
			t.Fatalf("%s exited with error: %v\nlogs:\n%s", bin, err, string(logData))
		}
	case <-time.After(8 * time.Second):
		logData, _ := os.ReadFile(logPath)
		t.Fatalf("timeout waiting process exit for %s\nlogs:\n%s", bin, string(logData))
	}

	return dataplaneCanaryResult{
		connect:         connectRes,
		stats:           statsRes,
		shutdownLatency: time.Since(start),
	}
}

func runDialCanary(addr string, attempts int, concurrency int, timeout time.Duration) canaryLatencyResult {
	return runCanaryLatency(attempts, concurrency, func() error {
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return err
		}
		_ = conn.SetDeadline(time.Now().Add(timeout))
		return conn.Close()
	})
}

func runHTTPStatsCanary(port int, attempts int, concurrency int, timeout time.Duration) canaryLatencyResult {
	url := fmt.Sprintf("http://127.0.0.1:%d/stats", port)
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	return runCanaryLatency(attempts, concurrency, func() error {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		_, readErr := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status: %d", resp.StatusCode)
		}
		return readErr
	})
}

func runCanaryLatency(attempts int, concurrency int, op func() error) canaryLatencyResult {
	if attempts <= 0 {
		return canaryLatencyResult{}
	}
	if concurrency <= 0 {
		concurrency = 1
	}

	jobs := make(chan struct{}, attempts)
	latCh := make(chan time.Duration, attempts)
	failCh := make(chan struct{}, attempts)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				start := time.Now()
				if err := op(); err != nil {
					failCh <- struct{}{}
					continue
				}
				latCh <- time.Since(start)
			}
		}()
	}

	for i := 0; i < attempts; i++ {
		jobs <- struct{}{}
	}
	close(jobs)
	wg.Wait()
	close(latCh)
	close(failCh)

	lats := make([]time.Duration, 0, attempts)
	for lat := range latCh {
		lats = append(lats, lat)
	}

	failures := 0
	for range failCh {
		failures++
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })

	res := canaryLatencyResult{
		attempts:  attempts,
		successes: len(lats),
		failures:  failures,
	}
	if len(lats) == 0 {
		return res
	}

	var total time.Duration
	for _, lat := range lats {
		total += lat
	}
	res.avgLatency = total / time.Duration(len(lats))
	res.p50Latency = percentileDuration(lats, 50)
	res.p95Latency = percentileDuration(lats, 95)
	res.maxLatency = lats[len(lats)-1]
	return res
}

func percentileDuration(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := (len(sorted)*p + 99) / 100
	if idx <= 0 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

func assertDualRunCanarySLO(
	t *testing.T,
	label string,
	goRes canaryLatencyResult,
	cRes canaryLatencyResult,
	maxSuccessRateDelta float64,
	latencyFactor float64,
	latencyMargin time.Duration,
) {
	t.Helper()

	if cRes.successes == 0 {
		t.Fatalf("%s canary baseline failed: C has zero successful attempts (%d failures)", label, cRes.failures)
	}
	if goRes.successes == 0 {
		t.Fatalf("%s canary failed: Go has zero successful attempts (%d failures)", label, goRes.failures)
	}

	goRate := goRes.successRate()
	cRate := cRes.successRate()
	if goRate+maxSuccessRateDelta < cRate {
		t.Fatalf(
			"%s success-rate regression: go=%.3f c=%.3f allowed_delta=%.3f",
			label,
			goRate,
			cRate,
			maxSuccessRateDelta,
		)
	}

	threshold := time.Duration(float64(cRes.p95Latency)*latencyFactor) + latencyMargin
	if goRes.p95Latency > threshold {
		t.Fatalf(
			"%s p95 latency regression: go=%s c=%s threshold=%s",
			label,
			goRes.p95Latency,
			cRes.p95Latency,
			threshold,
		)
	}
}

func waitForProcessAlive(ctx context.Context, cmd *exec.Cmd, stableFor time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("missing process handle")
	}
	if stableFor <= 0 {
		stableFor = 200 * time.Millisecond
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var aliveSince time.Time
	for {
		err := cmd.Process.Signal(syscall.Signal(0))
		if err == nil {
			if aliveSince.IsZero() {
				aliveSince = time.Now()
			}
			if time.Since(aliveSince) >= stableFor {
				return nil
			}
		} else {
			aliveSince = time.Time{}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting process alive: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func isExpectedTerminateExit(bin string, err error) bool {
	if err == nil {
		return true
	}
	code := testutil.ExitCode(err)
	if filepath.Base(bin) == "mtproto-proxy" {
		return code == 1 || code == 143
	}
	return false
}
