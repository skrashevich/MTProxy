package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReopenableLogWriterReopenAndWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.log")

	w, err := newReopenableLogWriter(path)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer func() {
		_ = w.Close()
	}()

	if _, err := w.Write([]byte("line-1\n")); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if err := w.Reopen(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := w.Write([]byte("line-2\n")); err != nil {
		t.Fatalf("write 2: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)
	for _, s := range []string{"line-1", "line-2"} {
		if !strings.Contains(content, s) {
			t.Fatalf("expected %q in log content: %q", s, content)
		}
	}
}

func TestReopenableLogWriterClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.log")

	w, err := newReopenableLogWriter(path)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatalf("expected write error after close")
	}
}
