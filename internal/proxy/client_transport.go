package proxy

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"

	mtcrypto "github.com/TelegramMessenger/MTProxy/internal/crypto"
)

const (
	mtprotoTagCompact = 0xefefefef
	mtprotoTagMedium  = 0xeeeeeeee
	mtprotoTagPadded  = 0xdddddddd
)

type mtprotoTransportMode uint8

const (
	mtprotoTransportUnknown mtprotoTransportMode = iota
	mtprotoTransportCompact
	mtprotoTransportMedium
	mtprotoTransportPadded
)

type mtprotoClientTransport struct {
	mode         mtprotoTransportMode
	targetDC     int
	maxFrameSize int

	obfuscated  bool
	readStream  cipher.Stream
	writeStream cipher.Stream
}

func newMTProtoClientTransport(maxFrameSize int) *mtprotoClientTransport {
	if maxFrameSize <= 0 {
		maxFrameSize = 4 << 20
	}
	return &mtprotoClientTransport{
		maxFrameSize: maxFrameSize,
	}
}

func (t *mtprotoClientTransport) init(r *bufio.Reader, secrets [][16]byte) error {
	header4, err := r.Peek(4)
	if err != nil {
		return err
	}

	switch {
	case header4[0] == 0xef:
		if _, err := r.Discard(1); err != nil {
			return err
		}
		t.mode = mtprotoTransportCompact
		return nil
	case binary.LittleEndian.Uint32(header4) == mtprotoTagMedium:
		if _, err := r.Discard(4); err != nil {
			return err
		}
		t.mode = mtprotoTransportMedium
		return nil
	case binary.LittleEndian.Uint32(header4) == mtprotoTagPadded:
		if _, err := r.Discard(4); err != nil {
			return err
		}
		t.mode = mtprotoTransportPadded
		return nil
	}

	header64, err := r.Peek(64)
	if err != nil {
		return err
	}

	mode, targetDC, readStream, writeStream, ok := parseObfuscatedClientHeader(header64, secrets)
	if !ok {
		return fmt.Errorf("unsupported transport header")
	}
	if _, err := r.Discard(64); err != nil {
		return err
	}

	t.mode = mode
	t.targetDC = targetDC
	t.obfuscated = true
	t.readStream = readStream
	t.writeStream = writeStream
	return nil
}

func (t *mtprotoClientTransport) readPacket(r *bufio.Reader) ([]byte, error) {
	if t.mode == mtprotoTransportUnknown {
		return nil, fmt.Errorf("transport is not initialized")
	}

	packetLen, err := t.readPacketLen(r)
	if err != nil {
		return nil, err
	}
	if packetLen <= 0 || packetLen > t.maxFrameSize {
		return nil, fmt.Errorf("bad packet length: %d", packetLen)
	}

	payload, err := t.readDecoded(r, packetLen)
	if err != nil {
		return nil, err
	}
	if t.mode == mtprotoTransportPadded {
		payload = payload[:packetLen&^3]
	} else if (packetLen & 3) != 0 {
		return nil, fmt.Errorf("bad packet alignment: %d", packetLen)
	}
	return payload, nil
}

func (t *mtprotoClientTransport) writePacket(w io.Writer, payload []byte) error {
	if t.mode == mtprotoTransportUnknown {
		return fmt.Errorf("transport is not initialized")
	}
	if len(payload) == 0 {
		return nil
	}

	frame, err := t.encodeFrame(payload)
	if err != nil {
		return err
	}
	if t.obfuscated {
		t.writeStream.XORKeyStream(frame, frame)
	}
	_, err = w.Write(frame)
	return err
}

func (t *mtprotoClientTransport) readPacketLen(r *bufio.Reader) (int, error) {
	switch t.mode {
	case mtprotoTransportCompact:
		b0, err := t.readDecoded(r, 1)
		if err != nil {
			return 0, err
		}
		if b0[0]&0x7f == 0x7f {
			brest, err := t.readDecoded(r, 3)
			if err != nil {
				return 0, err
			}
			enc := uint32(b0[0]) | (uint32(brest[0]) << 8) | (uint32(brest[1]) << 16) | (uint32(brest[2]) << 24)
			return int(enc>>8) << 2, nil
		}
		return int(b0[0]&0x7f) << 2, nil
	case mtprotoTransportMedium, mtprotoTransportPadded:
		b4, err := t.readDecoded(r, 4)
		if err != nil {
			return 0, err
		}
		enc := binary.LittleEndian.Uint32(b4)
		return int(enc &^ 0x80000000), nil
	default:
		return 0, fmt.Errorf("unknown transport mode")
	}
}

func (t *mtprotoClientTransport) encodeFrame(payload []byte) ([]byte, error) {
	switch t.mode {
	case mtprotoTransportCompact:
		if (len(payload) & 3) != 0 {
			return nil, fmt.Errorf("compact transport requires 4-byte aligned payload: %d", len(payload))
		}
		if len(payload) <= 0x7e*4 {
			frame := make([]byte, 1+len(payload))
			frame[0] = byte(len(payload) >> 2)
			copy(frame[1:], payload)
			return frame, nil
		}
		frame := make([]byte, 4+len(payload))
		binary.LittleEndian.PutUint32(frame[:4], (uint32(len(payload))<<6)|0x7f)
		copy(frame[4:], payload)
		return frame, nil
	case mtprotoTransportMedium:
		if (len(payload) & 3) != 0 {
			return nil, fmt.Errorf("intermediate transport requires 4-byte aligned payload: %d", len(payload))
		}
		frame := make([]byte, 4+len(payload))
		binary.LittleEndian.PutUint32(frame[:4], uint32(len(payload)))
		copy(frame[4:], payload)
		return frame, nil
	case mtprotoTransportPadded:
		padLen, err := randomPadLen()
		if err != nil {
			return nil, err
		}
		frame := make([]byte, 4+len(payload)+padLen)
		binary.LittleEndian.PutUint32(frame[:4], uint32(len(payload)+padLen))
		copy(frame[4:], payload)
		if padLen > 0 {
			if _, err := io.ReadFull(crand.Reader, frame[4+len(payload):]); err != nil {
				return nil, err
			}
		}
		return frame, nil
	default:
		return nil, fmt.Errorf("unknown transport mode")
	}
}

func (t *mtprotoClientTransport) readDecoded(r *bufio.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	if t.obfuscated {
		t.readStream.XORKeyStream(buf, buf)
	}
	return buf, nil
}

func parseObfuscatedClientHeader(header []byte, secrets [][16]byte) (mtprotoTransportMode, int, cipher.Stream, cipher.Stream, bool) {
	if len(header) < 64 {
		return 0, 0, nil, nil, false
	}

	tryCandidate := func(secret *[16]byte) (mtprotoTransportMode, int, cipher.Stream, cipher.Stream, bool) {
		readKey, readIV, writeKey, writeIV := deriveObfuscatedServerKeys(header, secret)

		readStream, err := newCTRStream(readKey, readIV)
		if err != nil {
			return 0, 0, nil, nil, false
		}
		decrypted := make([]byte, 64)
		readStream.XORKeyStream(decrypted, header[:64])
		tag := binary.LittleEndian.Uint32(decrypted[56:60])

		var mode mtprotoTransportMode
		switch tag {
		case mtprotoTagCompact:
			mode = mtprotoTransportCompact
		case mtprotoTagMedium:
			mode = mtprotoTransportMedium
		case mtprotoTagPadded:
			mode = mtprotoTransportPadded
		default:
			return 0, 0, nil, nil, false
		}

		writeStream, err := newCTRStream(writeKey, writeIV)
		if err != nil {
			return 0, 0, nil, nil, false
		}

		targetDC := int(int16(binary.LittleEndian.Uint16(decrypted[60:62])))
		return mode, targetDC, readStream, writeStream, true
	}

	if len(secrets) == 0 {
		return tryCandidate(nil)
	}
	for i := range secrets {
		if mode, targetDC, rs, ws, ok := tryCandidate(&secrets[i]); ok {
			return mode, targetDC, rs, ws, true
		}
	}
	return 0, 0, nil, nil, false
}

func deriveObfuscatedServerKeys(header []byte, secret *[16]byte) ([32]byte, [16]byte, [32]byte, [16]byte) {
	var readKey [32]byte
	var readIV [16]byte
	var writeKey [32]byte
	var writeIV [16]byte

	if secret == nil {
		copy(readKey[:], header[8:40])
	} else {
		var buf [48]byte
		copy(buf[:32], header[8:40])
		copy(buf[32:], secret[:])
		sum := mtcrypto.SHA256(buf[:])
		copy(readKey[:], sum[:])
	}
	copy(readIV[:], header[40:56])

	for i := 0; i < 32; i++ {
		writeKey[i] = header[55-i]
	}
	for i := 0; i < 16; i++ {
		writeIV[i] = header[23-i]
	}
	if secret != nil {
		var buf [48]byte
		copy(buf[:32], writeKey[:])
		copy(buf[32:], secret[:])
		sum := mtcrypto.SHA256(buf[:])
		copy(writeKey[:], sum[:])
	}

	return readKey, readIV, writeKey, writeIV
}

func newCTRStream(key [32]byte, iv [16]byte) (cipher.Stream, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, iv[:]), nil
}

func randomPadLen() (int, error) {
	var b [1]byte
	if _, err := io.ReadFull(crand.Reader, b[:]); err != nil {
		return 0, err
	}
	return int(b[0] & 3), nil
}
