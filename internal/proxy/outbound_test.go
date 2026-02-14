package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/config"
)

func TestOutboundProxyExchangeSuccessWithResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	payload := []byte("hello-outbound")
	response := []byte("backend-response")
	recvCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		frame, err := readLenPrefixedFrame(conn)
		if err != nil {
			errCh <- err
			return
		}
		recvCh <- frame
		if err := writeLenPrefixedFrame(conn, response); err != nil {
			errCh <- err
		}
	}()

	out := NewOutboundProxy(OutboundConfig{
		ConnectTimeout: time.Second,
		WriteTimeout:   time.Second,
		ReadTimeout:    time.Second,
	})
	target := config.Target{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := out.Exchange(ctx, target, payload)
	if err != nil {
		t.Fatalf("exchange outbound: %v", err)
	}
	if string(resp) != string(response) {
		t.Fatalf("response mismatch: got=%q want=%q", string(resp), string(response))
	}

	select {
	case err := <-errCh:
		t.Fatalf("accept/read frame: %v", err)
	case got := <-recvCh:
		if string(got) != string(payload) {
			t.Fatalf("payload mismatch: got=%q want=%q", string(got), string(payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting outbound payload")
	}

	s := out.Stats()
	if s.Dials != 1 || s.Sends != 1 || s.DialErrors != 0 || s.SendErrors != 0 {
		t.Fatalf("unexpected outbound stats: %+v", s)
	}
	if s.BytesSent != uint64(4+len(payload)) {
		t.Fatalf("unexpected bytes sent: %d", s.BytesSent)
	}
	if s.Responses != 1 || s.ResponseErrors != 0 || s.ResponseBytes != uint64(len(response)) {
		t.Fatalf("unexpected response stats: %+v", s)
	}
	_ = out.Close()
}

func TestOutboundProxyExchangeNoResponseIsNotError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = readLenPrefixedFrame(conn)
		// Close without response.
	}()

	out := NewOutboundProxy(OutboundConfig{
		ConnectTimeout: time.Second,
		WriteTimeout:   time.Second,
		ReadTimeout:    150 * time.Millisecond,
	})
	target := config.Target{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := out.Exchange(ctx, target, []byte("x"))
	if err != nil {
		t.Fatalf("unexpected exchange error: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected empty response, got %x", resp)
	}

	s := out.Stats()
	if s.Sends != 1 || s.Responses != 0 || s.ResponseErrors != 0 {
		t.Fatalf("unexpected stats: %+v", s)
	}
	_ = out.Close()
}

func TestOutboundProxyExchangeDialError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	out := NewOutboundProxy(OutboundConfig{
		ConnectTimeout: 100 * time.Millisecond,
		WriteTimeout:   100 * time.Millisecond,
	})
	target := config.Target{Host: "127.0.0.1", Port: port}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := out.Exchange(ctx, target, []byte("x")); err == nil {
		t.Fatalf("expected dial error")
	}

	s := out.Stats()
	if s.Dials != 1 || s.DialErrors != 1 || s.Sends != 0 {
		t.Fatalf("unexpected outbound stats: %+v", s)
	}
	_ = out.Close()
}

func TestOutboundProxyExchangeRejectsOversizePayload(t *testing.T) {
	out := NewOutboundProxy(OutboundConfig{
		MaxFrameSize: 4,
	})
	target := config.Target{Host: "127.0.0.1", Port: 1}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := out.Exchange(ctx, target, []byte("12345")); err == nil {
		t.Fatalf("expected oversize payload error")
	} else if !errors.Is(err, ErrOutboundPayloadTooLarge) {
		t.Fatalf("expected ErrOutboundPayloadTooLarge, got: %v", err)
	}

	s := out.Stats()
	if s.Sends != 0 || s.SendErrors != 1 || s.Dials != 0 {
		t.Fatalf("unexpected outbound stats: %+v", s)
	}
}

func TestOutboundProxyExchangeReusesConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	framesCh := make(chan []byte, 2)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		for i := 0; i < 2; i++ {
			frame, err := readLenPrefixedFrame(conn)
			if err != nil {
				errCh <- err
				return
			}
			framesCh <- frame
		}
	}()

	out := NewOutboundProxy(OutboundConfig{
		ConnectTimeout: time.Second,
		WriteTimeout:   time.Second,
		ReadTimeout:    50 * time.Millisecond,
	})
	target := config.Target{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := out.Exchange(ctx, target, []byte("first")); err != nil {
		t.Fatalf("exchange #1: %v", err)
	}
	if _, err := out.Exchange(ctx, target, []byte("second")); err != nil {
		t.Fatalf("exchange #2: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("backend exchange err: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	got1 := <-framesCh
	got2 := <-framesCh
	if string(got1) != "first" || string(got2) != "second" {
		t.Fatalf("unexpected frames: %q, %q", string(got1), string(got2))
	}

	s := out.Stats()
	if s.Dials != 1 || s.Sends != 2 {
		t.Fatalf("expected one dial and two sends, got %+v", s)
	}
	if s.PoolHits < 1 || s.PoolMisses != 1 {
		t.Fatalf("unexpected pool stats: %+v", s)
	}
	_ = out.Close()
}

func TestOutboundProxyExchangeReconnectAfterPeerClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := ln.Accept()
			if err != nil {
				errCh <- err
				return
			}
			_, err = readLenPrefixedFrame(conn)
			_ = conn.Close()
			if err != nil {
				errCh <- err
				return
			}
		}
	}()

	out := NewOutboundProxy(OutboundConfig{
		ConnectTimeout: time.Second,
		WriteTimeout:   time.Second,
		ReadTimeout:    100 * time.Millisecond,
	})
	target := config.Target{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := out.Exchange(ctx, target, []byte("first")); err != nil {
		t.Fatalf("exchange #1: %v", err)
	}
	if _, err := out.Exchange(ctx, target, []byte("second")); err != nil {
		t.Fatalf("exchange #2: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("backend exchange err: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	s := out.Stats()
	if s.Dials < 2 || s.Reconnects < 1 || s.Sends != 2 {
		t.Fatalf("expected reconnect path, got %+v", s)
	}
	_ = out.Close()
}

func TestOutboundProxyExchangeEvictsIdleConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	errCh := make(chan error, 2)
	acceptedCh := make(chan struct{}, 2)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := ln.Accept()
			if err != nil {
				errCh <- err
				return
			}
			acceptedCh <- struct{}{}
			go func(c net.Conn) {
				defer c.Close()
				if _, err := readLenPrefixedFrame(c); err != nil {
					errCh <- err
					return
				}
				// Keep the socket open long enough for client-side idle eviction.
				time.Sleep(400 * time.Millisecond)
			}(conn)
		}
	}()

	out := NewOutboundProxy(OutboundConfig{
		ConnectTimeout:  time.Second,
		WriteTimeout:    time.Second,
		ReadTimeout:     50 * time.Millisecond,
		IdleConnTimeout: 60 * time.Millisecond,
	})
	target := config.Target{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := out.Exchange(ctx, target, []byte("first")); err != nil {
		t.Fatalf("exchange #1: %v", err)
	}
	time.Sleep(120 * time.Millisecond)
	if _, err := out.Exchange(ctx, target, []byte("second")); err != nil {
		t.Fatalf("exchange #2: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	accepted := 0
	for accepted < 2 && time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("backend exchange err: %v", err)
		case <-acceptedCh:
			accepted++
		case <-time.After(20 * time.Millisecond):
		}
	}
	if accepted < 2 {
		t.Fatalf("expected two backend accepts due idle eviction, got %d", accepted)
	}

	s := out.Stats()
	if s.Dials < 2 || s.IdleEvictions < 1 || s.Sends != 2 {
		t.Fatalf("expected idle eviction path, got %+v", s)
	}
	_ = out.Close()
}

func readLenPrefixedFrame(conn net.Conn) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.LittleEndian.Uint32(hdr[:]))
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeLenPrefixedFrame(conn net.Conn, payload []byte) error {
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := conn.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := conn.Write(payload)
	return err
}
