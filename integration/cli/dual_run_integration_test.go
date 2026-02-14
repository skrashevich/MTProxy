package cli_test

import (
	"context"
	"encoding/json"
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

	writeDualRunReportEntry(t, dualRunReportEntry{
		Name: "control_plane_slo",
		Thresholds: map[string]string{
			"shutdown": "go <= c*2 + 200ms",
		},
		Metrics: map[string]any{
			"go_shutdown_ms": durationToMillis(goRes.shutdownLatency),
			"c_shutdown_ms":  durationToMillis(cRes.shutdownLatency),
		},
	})
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

	writeDualRunReportEntry(t, dualRunReportEntry{
		Name: "dataplane_canary_slo",
		Thresholds: map[string]string{
			"connect_success_rate": "go + 0.02 >= c",
			"connect_p95":          "go <= c*2.0 + 25ms",
			"connect_p99":          "go <= c*3.0 + 60ms",
			"stats_success_rate":   "go + 0.02 >= c",
			"stats_p95":            "go <= c*2.5 + 40ms",
			"stats_p99":            "go <= c*3.5 + 100ms",
			"shutdown":             "go <= c*2 + 250ms",
		},
		Metrics: map[string]any{
			"go_connect":     metricSnapshot(goRes.connect),
			"c_connect":      metricSnapshot(cRes.connect),
			"go_stats":       metricSnapshot(goRes.stats),
			"c_stats":        metricSnapshot(cRes.stats),
			"go_shutdown_ms": durationToMillis(goRes.shutdownLatency),
			"c_shutdown_ms":  durationToMillis(cRes.shutdownLatency),
		},
	})
}

func TestDualRunDataplaneLoadSLO(t *testing.T) {
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
	profile := dataplaneLoadProfile()

	goRes := runDataplaneCycle(
		t,
		goBin,
		cfgPath,
		filepath.Join(dir, "go-dataplane-load.log"),
		"runtime initialized:",
		goPort,
		profile,
	)
	cRes := runDataplaneCycle(
		t,
		cBin,
		cfgPath,
		filepath.Join(dir, "c-dataplane-load.log"),
		"",
		cPort,
		profile,
	)

	t.Logf(
		"dual-run dataplane load (%s):\n"+
			"  go_connect success=%.3f p95=%s p99=%s avg=%s\n"+
			"  c_connect success=%.3f p95=%s p99=%s avg=%s\n"+
			"  go_stats success=%.3f p95=%s p99=%s avg=%s\n"+
			"  c_stats success=%.3f p95=%s p99=%s avg=%s\n"+
			"  go_shutdown=%s c_shutdown=%s",
		profile.name,
		goRes.connect.successRate(), goRes.connect.p95Latency, goRes.connect.p99Latency, goRes.connect.avgLatency,
		cRes.connect.successRate(), cRes.connect.p95Latency, cRes.connect.p99Latency, cRes.connect.avgLatency,
		goRes.stats.successRate(), goRes.stats.p95Latency, goRes.stats.p99Latency, goRes.stats.avgLatency,
		cRes.stats.successRate(), cRes.stats.p95Latency, cRes.stats.p99Latency, cRes.stats.avgLatency,
		goRes.shutdownLatency, cRes.shutdownLatency,
	)

	assertDualRunCanarySLO(t, "load_connect", goRes.connect, cRes.connect, 0.02, 2.0, 25*time.Millisecond)
	assertDualRunCanarySLO(t, "load_stats", goRes.stats, cRes.stats, 0.02, 2.5, 60*time.Millisecond)

	if goRes.connect.p99Latency > time.Duration(float64(cRes.connect.p99Latency)*3.0)+80*time.Millisecond {
		t.Fatalf(
			"load connect p99 regression: go=%s c=%s threshold=%s",
			goRes.connect.p99Latency,
			cRes.connect.p99Latency,
			time.Duration(float64(cRes.connect.p99Latency)*3.0)+80*time.Millisecond,
		)
	}
	if goRes.stats.p99Latency > time.Duration(float64(cRes.stats.p99Latency)*4.0)+150*time.Millisecond {
		t.Fatalf(
			"load stats p99 regression: go=%s c=%s threshold=%s",
			goRes.stats.p99Latency,
			cRes.stats.p99Latency,
			time.Duration(float64(cRes.stats.p99Latency)*4.0)+150*time.Millisecond,
		)
	}
	if goRes.shutdownLatency > 2*cRes.shutdownLatency+300*time.Millisecond {
		t.Fatalf(
			"go shutdown latency regressed in dataplane load: go=%s c=%s threshold=%s",
			goRes.shutdownLatency,
			cRes.shutdownLatency,
			2*cRes.shutdownLatency+300*time.Millisecond,
		)
	}

	writeDualRunReportEntry(t, dualRunReportEntry{
		Name: "dataplane_load_slo",
		Thresholds: map[string]string{
			"profile":              profile.name,
			"connect_success_rate": "go + 0.02 >= c",
			"connect_p95":          "go <= c*2.0 + 25ms",
			"connect_p99":          "go <= c*3.0 + 80ms",
			"stats_success_rate":   "go + 0.02 >= c",
			"stats_p95":            "go <= c*2.5 + 60ms",
			"stats_p99":            "go <= c*4.0 + 150ms",
			"shutdown":             "go <= c*2 + 300ms",
		},
		Metrics: map[string]any{
			"go_connect":     metricSnapshot(goRes.connect),
			"c_connect":      metricSnapshot(cRes.connect),
			"go_stats":       metricSnapshot(goRes.stats),
			"c_stats":        metricSnapshot(cRes.stats),
			"go_shutdown_ms": durationToMillis(goRes.shutdownLatency),
			"c_shutdown_ms":  durationToMillis(cRes.shutdownLatency),
		},
	})
}

type controlPlaneResult struct {
	shutdownLatency time.Duration
}

type canaryLatencyResult struct {
	attempts   int
	successes  int
	failures   int
	avgLatency time.Duration
	p50Latency time.Duration
	p95Latency time.Duration
	p99Latency time.Duration
	maxLatency time.Duration
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

type dualRunDataplaneProfile struct {
	name               string
	connectAttempts    int
	connectConcurrency int
	connectTimeout     time.Duration
	statsAttempts      int
	statsConcurrency   int
	statsTimeout       time.Duration
}

type dualRunReportEntry struct {
	Name       string            `json:"name"`
	Thresholds map[string]string `json:"thresholds,omitempty"`
	Metrics    map[string]any    `json:"metrics,omitempty"`
}

type dualRunReportFile struct {
	GeneratedAt string               `json:"generated_at"`
	GoOS        string               `json:"go_os"`
	GoArch      string               `json:"go_arch"`
	Entries     []dualRunReportEntry `json:"entries"`
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
	return runDataplaneCycle(
		t,
		bin,
		cfgPath,
		logPath,
		startupMarker,
		port,
		dataplaneCanaryProfile(),
	)
}

func runDataplaneCycle(
	t *testing.T,
	bin string,
	cfgPath string,
	logPath string,
	startupMarker string,
	port int,
	profile dualRunDataplaneProfile,
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
	connectRes := runDialCanary(addr, profile.connectAttempts, profile.connectConcurrency, profile.connectTimeout)
	statsRes := runHTTPStatsCanary(port, profile.statsAttempts, profile.statsConcurrency, profile.statsTimeout)

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
	res.p99Latency = percentileDuration(lats, 99)
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

func dataplaneCanaryProfile() dualRunDataplaneProfile {
	return dualRunDataplaneProfile{
		name:               "canary",
		connectAttempts:    160,
		connectConcurrency: 16,
		connectTimeout:     450 * time.Millisecond,
		statsAttempts:      120,
		statsConcurrency:   12,
		statsTimeout:       650 * time.Millisecond,
	}
}

func dataplaneLoadProfile() dualRunDataplaneProfile {
	return dualRunDataplaneProfile{
		name:               "load",
		connectAttempts:    420,
		connectConcurrency: 32,
		connectTimeout:     500 * time.Millisecond,
		statsAttempts:      260,
		statsConcurrency:   20,
		statsTimeout:       900 * time.Millisecond,
	}
}

func metricSnapshot(r canaryLatencyResult) map[string]any {
	return map[string]any{
		"attempts":     r.attempts,
		"successes":    r.successes,
		"failures":     r.failures,
		"success_rate": r.successRate(),
		"avg_ms":       durationToMillis(r.avgLatency),
		"p50_ms":       durationToMillis(r.p50Latency),
		"p95_ms":       durationToMillis(r.p95Latency),
		"p99_ms":       durationToMillis(r.p99Latency),
		"max_ms":       durationToMillis(r.maxLatency),
	}
}

func durationToMillis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func writeDualRunReportEntry(t *testing.T, entry dualRunReportEntry) {
	t.Helper()
	reportPath := os.Getenv("MTPROXY_DUAL_RUN_REPORT")
	if reportPath == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatalf("create dual-run report dir: %v", err)
	}

	report := dualRunReportFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		GoOS:        runtime.GOOS,
		GoArch:      runtime.GOARCH,
	}
	if data, err := os.ReadFile(reportPath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatalf("parse dual-run report %s: %v", reportPath, err)
		}
		report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}

	updated := false
	for i := range report.Entries {
		if report.Entries[i].Name == entry.Name {
			report.Entries[i] = entry
			updated = true
			break
		}
	}
	if !updated {
		report.Entries = append(report.Entries, entry)
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal dual-run report: %v", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(reportPath, encoded, 0o644); err != nil {
		t.Fatalf("write dual-run report %s: %v", reportPath, err)
	}
}
