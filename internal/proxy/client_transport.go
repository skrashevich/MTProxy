package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Transport magic bytes — from net-tcp-rpc-ext-server.c, tag values after decryption.
const (
	TransportMagicAbridged     uint32 = 0xefefefef // RPC_F_COMPACT
	TransportMagicIntermediate uint32 = 0xeeeeeeee // RPC_F_MEDIUM
	TransportMagicPadded       uint32 = 0xdddddddd // RPC_F_MEDIUM | RPC_F_PAD
)

// TransportType identifies the MTProto framing used on a connection.
type TransportType int

const (
	TransportAbridged     TransportType = iota // 1-byte length prefix (or 4-byte if >=0x7f)
	TransportIntermediate                      // 4-byte LE length prefix
	TransportPadded                            // 4-byte LE length prefix, trailing pad allowed
)

// Obfuscated2Header is the parsed result of the 64-byte obfuscated2 handshake.
//
// Wire layout (C source net-tcp-rpc-ext-server.c, tcp_rpcs_compact_parse_execute):
//   bytes  0- 7: random nonce (not used for crypto)
//   bytes  8-39: key material — sha256(secret[0:16] + header[8:40]) → read_key
//   bytes 40-55: IV — header[40:56] → read_iv
//   bytes 56-59: transport magic (after decryption)
//   bytes 60-61: target DC id, int16 LE (may be negative for media DCs)
//   bytes 62-63: padding / unused
//
// Write key derivation (reversed from C):
//   write_key[i] = header[55-i]  for i in 0..31   (bytes 55..24, reversed)
//   write_iv[i]  = header[23-i]  for i in 0..15   (bytes 23..8, reversed)
//   if secrets present: sha256(write_key[0:32] || secret[0:16]) → write_key
type Obfuscated2Header struct {
	Transport TransportType
	TargetDC  int16
}

// ParseObfuscated2Header decrypts and parses the 64-byte obfuscated2 header.
// secret is the 16-byte proxy secret. raw is the 64 raw bytes from the wire.
// Returns the derived AES states for ongoing stream crypto plus the parsed header.
//
// The caller must use encState to encrypt data sent to the client and
// decState to decrypt data received from the client.
func ParseObfuscated2Header(raw [64]byte, secret []byte) (
	hdr Obfuscated2Header,
	decState *AESStreamState,
	encState *AESStreamState,
	err error,
) {
	// --- derive read (client→proxy) key/iv ---
	// key = sha256(header[8:40] || secret[0:16])   (C: memcpy(k, header+8, 32); memcpy(k+32, secret, 16); sha256(k, 48, key))
	var kBuf [48]byte
	copy(kBuf[0:32], raw[8:40])
	if len(secret) >= 16 {
		copy(kBuf[32:48], secret[0:16])
	}
	readKey := sha256Raw(kBuf[:])
	var readIV [16]byte
	copy(readIV[:], raw[40:56])

	// --- derive write (proxy→client) key/iv ---
	// write_key[i] = raw[55-i] for i in 0..31
	var writeKeyRaw [32]byte
	for i := 0; i < 32; i++ {
		writeKeyRaw[i] = raw[55-i]
	}
	var writeIV [16]byte
	for i := 0; i < 16; i++ {
		writeIV[i] = raw[23-i]
	}
	// if secret present: writeKey = sha256(writeKeyRaw || secret[0:16])
	var writeBuf [48]byte
	copy(writeBuf[0:32], writeKeyRaw[:])
	if len(secret) >= 16 {
		copy(writeBuf[32:48], secret[0:16])
	}
	writeKey := sha256Raw(writeBuf[:])

	// --- decrypt the raw header to check magic ---
	decCipher, err := newAESCTRStream(readKey, readIV)
	if err != nil {
		return hdr, nil, nil, fmt.Errorf("obfuscated2: init decrypt: %w", err)
	}
	var decrypted [64]byte
	decCipher.XORKeyStream(decrypted[:], raw[:])

	// bytes 56-59 = transport magic
	tag := binary.LittleEndian.Uint32(decrypted[56:60])
	switch tag {
	case TransportMagicAbridged:
		hdr.Transport = TransportAbridged
	case TransportMagicIntermediate:
		hdr.Transport = TransportIntermediate
	case TransportMagicPadded:
		hdr.Transport = TransportPadded
	default:
		return hdr, nil, nil, fmt.Errorf("obfuscated2: unknown transport magic 0x%08x", tag)
	}

	// bytes 60-61 = target DC, int16 LE
	hdr.TargetDC = int16(binary.LittleEndian.Uint16(decrypted[60:62]))

	// Build ongoing stream states.
	// Decrypt stream: already positioned at byte 64 (after header).
	decStream, err := newAESCTRStreamAt(readKey, readIV, 64)
	if err != nil {
		return hdr, nil, nil, fmt.Errorf("obfuscated2: init decStream: %w", err)
	}
	encStream, err := newAESCTRStreamAt(writeKey, writeIV, 64)
	if err != nil {
		return hdr, nil, nil, fmt.Errorf("obfuscated2: init encStream: %w", err)
	}

	decState = &AESStreamState{stream: decStream}
	encState = &AESStreamState{stream: encStream}
	return hdr, decState, encState, nil
}

// ReadPacket reads one MTProto packet from r, decrypting with dec if non-nil.
// Returns the plaintext payload (without length prefix).
func ReadPacket(r io.Reader, dec *AESStreamState, transport TransportType) ([]byte, error) {
	switch transport {
	case TransportAbridged:
		return readAbridged(r, dec)
	case TransportIntermediate, TransportPadded:
		return readIntermediate(r, dec, transport == TransportPadded)
	default:
		return nil, fmt.Errorf("ReadPacket: unknown transport %d", transport)
	}
}

// WritePacket writes one MTProto packet to w, encrypting with enc if non-nil.
func WritePacket(w io.Writer, data []byte, enc *AESStreamState, transport TransportType) error {
	switch transport {
	case TransportAbridged:
		return writeAbridged(w, data, enc)
	case TransportIntermediate, TransportPadded:
		return writeIntermediate(w, data, enc, transport == TransportPadded)
	default:
		return fmt.Errorf("WritePacket: unknown transport %d", transport)
	}
}

// --- Abridged transport ---

func readAbridged(r io.Reader, dec *AESStreamState) ([]byte, error) {
	var b [1]byte
	if err := transportReadFull(r, dec, b[:]); err != nil {
		return nil, err
	}
	length := int(b[0])
	if length == 0x7f {
		var lb [3]byte
		if err := transportReadFull(r, dec, lb[:]); err != nil {
			return nil, err
		}
		length = int(lb[0]) | int(lb[1])<<8 | int(lb[2])<<16
	}
	length *= 4
	if length <= 0 || length > maxPacketSize {
		return nil, fmt.Errorf("abridged: invalid length %d", length)
	}
	buf := make([]byte, length)
	if err := transportReadFull(r, dec, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeAbridged(w io.Writer, data []byte, enc *AESStreamState) error {
	n := len(data)
	if n%4 != 0 {
		return fmt.Errorf("writeAbridged: data length %d not multiple of 4", n)
	}
	words := n / 4
	var header []byte
	if words < 0x7f {
		header = []byte{byte(words)}
	} else {
		header = []byte{
			0x7f,
			byte(words),
			byte(words >> 8),
			byte(words >> 16),
		}
	}
	return transportWriteFull(w, enc, header, data)
}

// --- Intermediate / Padded transport ---

func readIntermediate(r io.Reader, dec *AESStreamState, padded bool) ([]byte, error) {
	var lb [4]byte
	if err := transportReadFull(r, dec, lb[:]); err != nil {
		return nil, err
	}
	length := int(binary.LittleEndian.Uint32(lb[:]))
	// strip quickack flag (top bit in C: RPC_F_QUICKACK = 0x8000000)
	length &^= 0x80000000
	if padded {
		// padded: actual data is length rounded down to multiple of 4
		length = length &^ 3
	}
	if length <= 0 || length > maxPacketSize {
		return nil, fmt.Errorf("intermediate: invalid length %d", length)
	}
	buf := make([]byte, length)
	if err := transportReadFull(r, dec, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeIntermediate(w io.Writer, data []byte, enc *AESStreamState, padded bool) error {
	n := len(data)
	var lb [4]byte
	binary.LittleEndian.PutUint32(lb[:], uint32(n))
	return transportWriteFull(w, enc, lb[:], data)
}

// --- helpers ---

const maxPacketSize = 16 * 1024 * 1024 // 16 MiB sanity cap

// transportReadFull reads exactly len(buf) bytes from r, decrypting in-place if dec != nil.
func transportReadFull(r io.Reader, dec *AESStreamState, buf []byte) error {
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	if dec != nil {
		dec.stream.XORKeyStream(buf, buf)
	}
	return nil
}

// transportWriteFull encrypts (if enc != nil) and writes parts to w.
// Encrypts into a temporary buffer to avoid modifying the caller's data.
func transportWriteFull(w io.Writer, enc *AESStreamState, parts ...[]byte) error {
	for _, p := range parts {
		out := p
		if enc != nil {
			out = make([]byte, len(p))
			enc.stream.XORKeyStream(out, p)
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
	}
	return nil
}
