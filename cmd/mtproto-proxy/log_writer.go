package main

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// LogWriter is an io.Writer that prepends a prefix to every line written.
// Optionally it can write to a file in addition to the underlying writer.
type LogWriter struct {
	mu      sync.Mutex
	prefix  string
	out     io.Writer
	file    *os.File
}

// NewLogWriter creates a LogWriter with the given prefix writing to out.
func NewLogWriter(prefix string, out io.Writer) *LogWriter {
	return &LogWriter{prefix: prefix, out: out}
}

// OpenFile opens (or creates) a log file and additionally writes there.
// Call Close() to close the file.
func (lw *LogWriter) OpenFile(filename string) error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", filename, err)
	}
	lw.file = f
	return nil
}

// Close closes the underlying log file, if any.
func (lw *LogWriter) Close() error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.file != nil {
		err := lw.file.Close()
		lw.file = nil
		return err
	}
	return nil
}

// Write implements io.Writer, prepending lw.prefix to each call.
func (lw *LogWriter) Write(p []byte) (n int, err error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	line := lw.prefix + string(p)
	b := []byte(line)
	if _, err = lw.out.Write(b); err != nil {
		return 0, err
	}
	if lw.file != nil {
		_, _ = lw.file.Write(b)
	}
	return len(p), nil
}
