package proxy

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMTProtoClientTransportCompactPlain(t *testing.T) {
	payload := makeHandshakeFrameForTransport(0x60469778)
	frame := make([]byte, 1+len(payload))
	frame[0] = byte(len(payload) >> 2)
	copy(frame[1:], payload)

	var input bytes.Buffer
	input.WriteByte(0xef)
	input.Write(frame)

	tr := newMTProtoClientTransport(1 << 20)
	rd := bufio.NewReader(bytes.NewReader(input.Bytes()))
	if err := tr.init(rd, nil); err != nil {
		t.Fatalf("init transport: %v", err)
	}
	if tr.mode != mtprotoTransportCompact {
		t.Fatalf("unexpected mode: %v", tr.mode)
	}
	got, err := tr.readPacket(rd)
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}

	var out bytes.Buffer
	if err := tr.writePacket(&out, payload); err != nil {
		t.Fatalf("write packet: %v", err)
	}
	written := out.Bytes()
	if len(written) != len(frame) || written[0] != frame[0] || !bytes.Equal(written[1:], payload) {
		t.Fatalf("unexpected compact write: %x", written)
	}
}

func TestMTProtoClientTransportObfuscatedPadded(t *testing.T) {
	secret := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	header, readKey, readIV, writeKey, writeIV := buildObfuscatedHeaderForTest(t, &secret, mtprotoTagPadded, 3)

	inPayload := makeHandshakeFrameForTransport(0x60469778)
	paddedLen := len(inPayload) + 2
	plainInFrame := make([]byte, 4+paddedLen)
	binary.LittleEndian.PutUint32(plainInFrame[:4], uint32(paddedLen))
	copy(plainInFrame[4:], inPayload)
	plainInFrame[len(plainInFrame)-2] = 0xaa
	plainInFrame[len(plainInFrame)-1] = 0x55

	cipherInFrame := encryptObfuscatedPayloadForTest(t, readKey, readIV, plainInFrame)

	var input bytes.Buffer
	input.Write(header)
	input.Write(cipherInFrame)

	tr := newMTProtoClientTransport(1 << 20)
	rd := bufio.NewReader(bytes.NewReader(input.Bytes()))
	if err := tr.init(rd, [][16]byte{secret}); err != nil {
		t.Fatalf("init transport: %v", err)
	}
	if !tr.obfuscated {
		t.Fatalf("expected obfuscated transport")
	}
	if tr.mode != mtprotoTransportPadded {
		t.Fatalf("unexpected mode: %v", tr.mode)
	}
	if tr.targetDC != 3 {
		t.Fatalf("unexpected target dc: %d", tr.targetDC)
	}

	got, err := tr.readPacket(rd)
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if !bytes.Equal(got, inPayload) {
		t.Fatalf("decoded payload mismatch")
	}

	outPayload := makeEncryptedFrameForTransport(0x0102030405060708)
	var out bytes.Buffer
	if err := tr.writePacket(&out, outPayload); err != nil {
		t.Fatalf("write packet: %v", err)
	}
	decrypted := decryptObfuscatedPayloadForTest(t, writeKey, writeIV, out.Bytes())
	if len(decrypted) < 4 {
		t.Fatalf("too short encrypted output: %x", decrypted)
	}
	outLen := int(binary.LittleEndian.Uint32(decrypted[:4]))
	if outLen < len(outPayload) || outLen > len(outPayload)+3 {
		t.Fatalf("unexpected output length: %d", outLen)
	}
	if !bytes.Equal(decrypted[4:4+len(outPayload)], outPayload) {
		t.Fatalf("output payload mismatch")
	}
}

func TestMTProtoClientTransportRejectsInvalidHeader(t *testing.T) {
	raw := bytes.Repeat([]byte{0x11}, 64)
	tr := newMTProtoClientTransport(1 << 20)
	rd := bufio.NewReader(bytes.NewReader(raw))
	if err := tr.init(rd, nil); err == nil {
		t.Fatalf("expected init error for invalid header")
	}
}

func buildObfuscatedHeaderForTest(
	t *testing.T,
	secret *[16]byte,
	tag uint32,
	targetDC int16,
) ([]byte, [32]byte, [16]byte, [32]byte, [16]byte) {
	t.Helper()

	header := make([]byte, 64)
	for i := 0; i < 56; i++ {
		header[i] = byte(17 + i*3)
	}
	header[0], header[1], header[2], header[3] = 0x39, 0x7a, 0x13, 0x42

	readKey, readIV, writeKey, writeIV := deriveObfuscatedServerKeys(header, secret)
	ks := xorWithCTRStream(t, readKey, readIV, make([]byte, 64))

	var plainTail [8]byte
	binary.LittleEndian.PutUint32(plainTail[0:4], tag)
	binary.LittleEndian.PutUint16(plainTail[4:6], uint16(targetDC))
	plainTail[6] = 0x6a
	plainTail[7] = 0x33
	for i := 0; i < 8; i++ {
		header[56+i] = plainTail[i] ^ ks[56+i]
	}
	return header, readKey, readIV, writeKey, writeIV
}

func encryptObfuscatedPayloadForTest(t *testing.T, key [32]byte, iv [16]byte, plain []byte) []byte {
	t.Helper()
	streamInput := make([]byte, 64+len(plain))
	cipherOut := xorWithCTRStream(t, key, iv, streamInput)
	out := make([]byte, len(plain))
	copy(out, plain)
	for i := range out {
		out[i] ^= cipherOut[64+i]
	}
	return out
}

func decryptObfuscatedPayloadForTest(t *testing.T, key [32]byte, iv [16]byte, cipherText []byte) []byte {
	t.Helper()
	return xorWithCTRStream(t, key, iv, cipherText)
}

func xorWithCTRStream(t *testing.T, key [32]byte, iv [16]byte, data []byte) []byte {
	t.Helper()
	stream, err := newCTRStream(key, iv)
	if err != nil {
		t.Fatalf("new ctr stream: %v", err)
	}
	out := make([]byte, len(data))
	stream.XORKeyStream(out, data)
	return out
}

func makeHandshakeFrameForTransport(function uint32) []byte {
	frame := make([]byte, 40)
	binary.LittleEndian.PutUint32(frame[16:20], 20)
	binary.LittleEndian.PutUint32(frame[20:24], function)
	return frame
}

func makeEncryptedFrameForTransport(authKeyID uint64) []byte {
	frame := make([]byte, 56)
	binary.LittleEndian.PutUint64(frame[:8], authKeyID)
	return frame
}
