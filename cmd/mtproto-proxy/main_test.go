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
