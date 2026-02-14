package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
	"github.com/TelegramMessenger/MTProxy/internal/protocol"
)

func TestClientIngressServerHandlesObfuscatedPaddedFrames(t *testing.T) {
	rt := newDataPlaneRuntimeForTest(t, cli.Options{MaxConn: 8})
	rt.SetOutboundSender(&echoOutboundSender{})

	secret := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	srv, err := StartClientIngressServer(rt, ClientIngressConfig{
		Addr:         "127.0.0.1:0",
		TargetDC:     2,
		MaxFrameSize: 1 << 20,
		IdleTimeout:  3 * time.Second,
		Secrets:      [][16]byte{secret},
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("start client ingress: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	header, readKey, readIV, writeKey, writeIV := buildObfuscatedHeaderForTest(t, &secret, mtprotoTagPadded, 2)
	payload := makeHandshakeFrameForTransport(protocol.CodeReqPQ)

	inPad := []byte{0xaa, 0x55}
	plainInFrame := make([]byte, 4+len(payload)+len(inPad))
	binary.LittleEndian.PutUint32(plainInFrame[:4], uint32(len(payload)+len(inPad)))
	copy(plainInFrame[4:], payload)
	copy(plainInFrame[4+len(payload):], inPad)
	cipherInFrame := encryptObfuscatedPayloadForTest(t, readKey, readIV, plainInFrame)

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Write(header); err != nil {
		t.Fatalf("write obfuscated header: %v", err)
	}
	if _, err := conn.Write(cipherInFrame); err != nil {
		t.Fatalf("write encrypted frame: %v", err)
	}

	clientReadStream, err := newCTRStream(writeKey, writeIV)
	if err != nil {
		t.Fatalf("new ctr stream: %v", err)
	}
	encLen := make([]byte, 4)
	if _, err := io.ReadFull(conn, encLen); err != nil {
		t.Fatalf("read encrypted response len: %v", err)
	}
	decLen := make([]byte, 4)
	clientReadStream.XORKeyStream(decLen, encLen)
	respLen := int(binary.LittleEndian.Uint32(decLen))
	if respLen < len(payload) || respLen > len(payload)+3 {
		t.Fatalf("unexpected response len: %d", respLen)
	}

	encResp := make([]byte, respLen)
	if _, err := io.ReadFull(conn, encResp); err != nil {
		t.Fatalf("read encrypted response payload: %v", err)
	}
	respPlain := make([]byte, respLen)
	clientReadStream.XORKeyStream(respPlain, encResp)
	if !bytes.Equal(respPlain[:len(payload)], payload) {
		t.Fatalf("response payload mismatch")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		stats := srv.Stats()
		if stats.FramesHandled >= 1 && stats.FramesReturned >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting stats update: %+v", stats)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type echoOutboundSender struct{}

func (e *echoOutboundSender) Exchange(_ context.Context, _ config.Target, payload []byte) ([]byte, error) {
	out := make([]byte, len(payload))
	copy(out, payload)
	return out, nil
}

func (e *echoOutboundSender) Stats() OutboundStats {
	return OutboundStats{}
}

func (e *echoOutboundSender) Close() error {
	return nil
}
