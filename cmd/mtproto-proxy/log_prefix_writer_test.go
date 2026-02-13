package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestLinePrefixWriterSingleAndMultiLine(t *testing.T) {
	var out bytes.Buffer
	w := newLinePrefixWriter(&out, "[worker 1] ")

	if _, err := w.Write([]byte("line-1\nline-2\nline-3")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := out.String()
	for _, marker := range []string{
		"[worker 1] line-1",
		"[worker 1] line-2",
		"[worker 1] line-3",
	} {
		if !strings.Contains(got, marker) {
			t.Fatalf("missing marker %q in output: %q", marker, got)
		}
	}
}

func TestLinePrefixWriterHandlesSplitWrites(t *testing.T) {
	var out bytes.Buffer
	w := newLinePrefixWriter(&out, "[worker 2] ")

	if _, err := w.Write([]byte("line-1")); err != nil {
		t.Fatalf("write1: %v", err)
	}
	if _, err := w.Write([]byte("\nline-2\n")); err != nil {
		t.Fatalf("write2: %v", err)
	}
	got := out.String()
	for _, marker := range []string{
		"[worker 2] line-1",
		"[worker 2] line-2",
	} {
		if !strings.Contains(got, marker) {
			t.Fatalf("missing marker %q in output: %q", marker, got)
		}
	}
}

func TestCurrentWorkerID(t *testing.T) {
	t.Setenv("MTPROXY_GO_WORKER_ID", "3")
	id, ok := currentWorkerID()
	if !ok || id != 3 {
		t.Fatalf("unexpected worker id parse result: id=%d ok=%v", id, ok)
	}
}

func TestCurrentWorkerIDInvalid(t *testing.T) {
	t.Setenv("MTPROXY_GO_WORKER_ID", "bad")
	if _, ok := currentWorkerID(); ok {
		t.Fatalf("expected invalid worker id parse failure")
	}

	t.Setenv("MTPROXY_GO_WORKER_ID", "-1")
	if _, ok := currentWorkerID(); ok {
		t.Fatalf("expected negative worker id parse failure")
	}

	t.Setenv("MTPROXY_GO_WORKER_ID", "")
	if _, ok := currentWorkerID(); ok {
		t.Fatalf("expected unset worker id parse failure")
	}
}

type reopenMockWriter struct {
	buf         bytes.Buffer
	reopenCalls int
	reopenErr   error
}

func (w *reopenMockWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *reopenMockWriter) Reopen() error {
	w.reopenCalls++
	return w.reopenErr
}

func TestLinePrefixWriterReopenDelegates(t *testing.T) {
	base := &reopenMockWriter{}
	w := newLinePrefixWriter(base, "[worker 0] ")
	if err := w.Reopen(); err != nil {
		t.Fatalf("unexpected reopen error: %v", err)
	}
	if base.reopenCalls != 1 {
		t.Fatalf("unexpected reopen calls: %d", base.reopenCalls)
	}
}

func TestLinePrefixWriterReopenDelegatesError(t *testing.T) {
	base := &reopenMockWriter{reopenErr: errors.New("boom")}
	w := newLinePrefixWriter(base, "[worker 0] ")
	if err := w.Reopen(); err == nil {
		t.Fatalf("expected reopen error")
	}
	if base.reopenCalls != 1 {
		t.Fatalf("unexpected reopen calls: %d", base.reopenCalls)
	}
}
