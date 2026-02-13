package main

import (
	"fmt"
	"io"
	"os"
	"sync"
)

var _ io.Writer = (*reopenableLogWriter)(nil)

type reopenableLogWriter struct {
	path string

	mu sync.Mutex
	f  *os.File
}

func newReopenableLogWriter(path string) (*reopenableLogWriter, error) {
	f, err := openLogFile(path)
	if err != nil {
		return nil, err
	}
	return &reopenableLogWriter{
		path: path,
		f:    f,
	}, nil
}

func (w *reopenableLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, fmt.Errorf("log writer is closed")
	}
	return w.f.Write(p)
}

func (w *reopenableLogWriter) Reopen() error {
	next, err := openLogFile(w.path)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	prev := w.f
	w.f = next
	if prev != nil {
		return prev.Close()
	}
	return nil
}

func (w *reopenableLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func openLogFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", path, err)
	}
	return f, nil
}
