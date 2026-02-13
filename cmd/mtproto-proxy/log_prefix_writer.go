package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

type linePrefixWriter struct {
	w      io.Writer
	prefix string

	mu            sync.Mutex
	atLineStart   bool
	pendingPrefix []byte
}

type reopenable interface {
	Reopen() error
}

func newLinePrefixWriter(w io.Writer, prefix string) *linePrefixWriter {
	return &linePrefixWriter{
		w:             w,
		prefix:        prefix,
		atLineStart:   true,
		pendingPrefix: []byte(prefix),
	}
}

func (p *linePrefixWriter) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var out bytes.Buffer
	consumed := 0
	for len(data) > 0 {
		if p.atLineStart {
			out.Write(p.pendingPrefix)
			p.atLineStart = false
		}

		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			out.Write(data)
			consumed += len(data)
			break
		}

		out.Write(data[:i+1])
		consumed += i + 1
		data = data[i+1:]
		p.atLineStart = true
	}

	if _, err := p.w.Write(out.Bytes()); err != nil {
		return consumed, err
	}
	return consumed, nil
}

func (p *linePrefixWriter) Reopen() error {
	r, ok := p.w.(reopenable)
	if !ok {
		return fmt.Errorf("reopen not supported by wrapped writer")
	}
	return r.Reopen()
}

func maybeWrapWorkerLogWriter(logw io.Writer) io.Writer {
	id, ok := currentWorkerID()
	if !ok {
		return logw
	}
	return newLinePrefixWriter(logw, fmt.Sprintf("[worker %d] ", id))
}

func currentWorkerID() (int, bool) {
	raw := os.Getenv("MTPROXY_GO_WORKER_ID")
	if raw == "" {
		return 0, false
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id < 0 {
		return 0, false
	}
	return id, true
}
