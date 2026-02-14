package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestPhase8CutoverRollbackDrill(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("phase8 cutover drill is enabled only on Linux")
	}
	if runtime.GOARCH != "amd64" {
		t.Skip("phase8 cutover drill currently requires amd64 (C build flags are x86-specific)")
	}
	if os.Getenv("MTPROXY_PHASE8") != "1" {
		t.Skip("set MTPROXY_PHASE8=1 to run phase8 cutover/rollback drill")
	}

	goBin := testutil.BuildProxyBinary(t)
	cBin := testutil.BuildCProxyBinary(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backend.conf")
	if err := os.WriteFile(cfgPath, []byte("proxy 149.154.175.50:8888;"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	statsPort := findFreeLocalPort(t)
	activePath := filepath.Join(dir, "mtproxy-active")
	if err := switchExecutableSymlink(activePath, cBin); err != nil {
		t.Fatalf("init active symlink to C binary: %v", err)
	}

	steps := []phase8Step{
		{Name: "baseline_c", TargetBinary: cBin},
		{Name: "cutover_go", TargetBinary: goBin},
		{Name: "rollback_c", TargetBinary: cBin},
	}
	results := make([]phase8StepResult, 0, len(steps))

	for _, step := range steps {
		if err := switchExecutableSymlink(activePath, step.TargetBinary); err != nil {
			t.Fatalf("switch symlink for step %s: %v", step.Name, err)
		}
		res := runPhase8Step(t, phase8StepRunConfig{
			StepName:     step.Name,
			Executable:   activePath,
			ConfigPath:   cfgPath,
			LogPath:      filepath.Join(dir, step.Name+".log"),
			StatsPort:    statsPort,
			CStyleExit:   filepath.Base(step.TargetBinary) == "mtproto-proxy",
			ResolvedName: filepath.Base(step.TargetBinary),
		})
		results = append(results, res)
	}

	if len(results) != 3 {
		t.Fatalf("unexpected step count: %d", len(results))
	}

	baseline := results[0]
	cutover := results[1]
	rollback := results[2]

	// Generous guardrails: cutover/rollback should stay within sane startup/shutdown envelopes.
	startupThreshold := 3*baseline.StartupLatency + 500*time.Millisecond
	shutdownThreshold := 3*baseline.ShutdownLatency + 500*time.Millisecond

	if cutover.StartupLatency > startupThreshold {
		t.Fatalf("cutover startup regression: go=%s threshold=%s baseline_c=%s", cutover.StartupLatency, startupThreshold, baseline.StartupLatency)
	}
	if rollback.StartupLatency > startupThreshold {
		t.Fatalf("rollback startup regression: rollback_c=%s threshold=%s baseline_c=%s", rollback.StartupLatency, startupThreshold, baseline.StartupLatency)
	}
	if cutover.ShutdownLatency > shutdownThreshold {
		t.Fatalf("cutover shutdown regression: go=%s threshold=%s baseline_c=%s", cutover.ShutdownLatency, shutdownThreshold, baseline.ShutdownLatency)
	}
	if rollback.ShutdownLatency > shutdownThreshold {
		t.Fatalf("rollback shutdown regression: rollback_c=%s threshold=%s baseline_c=%s", rollback.ShutdownLatency, shutdownThreshold, baseline.ShutdownLatency)
	}

	writePhase8DrillReport(t, phase8DrillReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		GoOS:        runtime.GOOS,
		GoArch:      runtime.GOARCH,
		Thresholds: map[string]string{
			"startup":  "cutover_go and rollback_c <= baseline_c*3 + 500ms",
			"shutdown": "cutover_go and rollback_c <= baseline_c*3 + 500ms",
		},
		Steps: results,
	})
}

type phase8Step struct {
	Name         string
	TargetBinary string
}

type phase8StepRunConfig struct {
	StepName     string
	Executable   string
	ConfigPath   string
	LogPath      string
	StatsPort    int
	CStyleExit   bool
	ResolvedName string
}

type phase8StepResult struct {
	Name              string        `json:"name"`
	Binary            string        `json:"binary"`
	StartupLatencyMS  float64       `json:"startup_latency_ms"`
	ShutdownLatencyMS float64       `json:"shutdown_latency_ms"`
	StatsLineCount    int           `json:"stats_line_count"`
	StatsSample       string        `json:"stats_sample"`
	StartupLatency    time.Duration `json:"-"`
	ShutdownLatency   time.Duration `json:"-"`
}

type phase8DrillReport struct {
	GeneratedAt string             `json:"generated_at"`
	GoOS        string             `json:"go_os"`
	GoArch      string             `json:"go_arch"`
	Thresholds  map[string]string  `json:"thresholds"`
	Steps       []phase8StepResult `json:"steps"`
}

func runPhase8Step(t *testing.T, cfg phase8StepRunConfig) phase8StepResult {
	t.Helper()

	logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open phase8 log %s: %v", cfg.LogPath, err)
	}
	defer logFile.Close()

	args := []string{"-u", "root", "-p", fmt.Sprintf("%d", cfg.StatsPort), "--http-stats", "-l", cfg.LogPath, cfg.ConfigPath}
	cmd := exec.Command(cfg.Executable, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		t.Fatalf("start step %s (%s): %v", cfg.StepName, cfg.ResolvedName, err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	waitCtx, cancel := context.WithTimeout(context.Background(), 14*time.Second)
	defer cancel()

	startupStart := time.Now()
	if err := waitForProcessAlive(waitCtx, cmd, 250*time.Millisecond); err != nil {
		logData, _ := os.ReadFile(cfg.LogPath)
		t.Fatalf("wait process alive (%s): %v\nlogs:\n%s", cfg.StepName, err, string(logData))
	}
	statsBody, err := waitForStatsBody(waitCtx, cfg.StatsPort)
	if err != nil {
		logData, _ := os.ReadFile(cfg.LogPath)
		t.Fatalf("wait stats endpoint (%s): %v\nlogs:\n%s", cfg.StepName, err, string(logData))
	}
	startupLatency := time.Since(startupStart)

	if !strings.Contains(statsBody, "stats_generated_at\t") && !strings.Contains(statsBody, "current_time\t") {
		t.Fatalf("unexpected stats body for step %s: %s", cfg.StepName, statsBody)
	}

	shutdownStart := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM (%s): %v", cfg.StepName, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if !isExpectedPhase8Exit(cfg.CStyleExit, err) {
			logData, _ := os.ReadFile(cfg.LogPath)
			t.Fatalf("step %s (%s) exit error: %v\nlogs:\n%s", cfg.StepName, cfg.ResolvedName, err, string(logData))
		}
	case <-time.After(8 * time.Second):
		logData, _ := os.ReadFile(cfg.LogPath)
		t.Fatalf("timeout waiting step %s exit\nlogs:\n%s", cfg.StepName, string(logData))
	}
	shutdownLatency := time.Since(shutdownStart)

	lines := strings.Split(strings.TrimSpace(statsBody), "\n")
	statsSample := ""
	if len(lines) > 0 {
		statsSample = lines[0]
	}

	return phase8StepResult{
		Name:              cfg.StepName,
		Binary:            cfg.ResolvedName,
		StartupLatencyMS:  durationToMillis(startupLatency),
		ShutdownLatencyMS: durationToMillis(shutdownLatency),
		StatsLineCount:    len(lines),
		StatsSample:       statsSample,
		StartupLatency:    startupLatency,
		ShutdownLatency:   shutdownLatency,
	}
}

func switchExecutableSymlink(linkPath, targetPath string) error {
	tmp := linkPath + ".next"
	_ = os.Remove(tmp)
	if err := os.Symlink(targetPath, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, linkPath)
}

func isExpectedPhase8Exit(cStyle bool, err error) bool {
	if err == nil {
		return true
	}
	code := testutil.ExitCode(err)
	if cStyle {
		return code == 1 || code == 143
	}
	return false
}

func writePhase8DrillReport(t *testing.T, report phase8DrillReport) {
	t.Helper()
	path := os.Getenv("MTPROXY_PHASE8_REPORT")
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create phase8 report dir: %v", err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal phase8 report: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write phase8 report %s: %v", path, err)
	}
}
