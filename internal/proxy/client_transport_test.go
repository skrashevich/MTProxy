package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

// buildRawHeader constructs a valid 64-byte obfuscated2 header for the given
// secret and transport magic, using deterministic key material so tests are
// reproducible.
//
// Algorithm mirrors C net-tcp-rpc-ext-server.c (tcp_rpcs_compact_parse_execute):
//  1. Fill raw[0:64] with test bytes.
//  2. Derive read key: sha256(raw[8:40] || secret[0:16])
//  3. IV = raw[40:56]
//  4. Encrypt raw[0:64] with AES-CTR(readKey, readIV) to produce ciphertext.
//  5. But we want the plaintext at [56:60] to be the magic, so we work
//     backwards: start from desired plaintext, encrypt it to get the wire form.
func buildRawHeader(t *testing.T, secret []byte, transportMagic uint32, targetDC int16) [64]byte {
	t.Helper()

	// Choose deterministic "random" bytes for the nonce/key material areas.
	var plain [64]byte
	for i := range plain {
		plain[i] = byte(i + 1)
	}
	// Set transport magic and targetDC in the plaintext positions.
	binary.LittleEndian.PutUint32(plain[56:60], transportMagic)
	binary.LittleEndian.PutUint16(plain[60:62], uint16(targetDC))

	// Derive read key from plain (which is what ParseObfuscated2Header will use).
	var kBuf [48]byte
	copy(kBuf[0:32], plain[8:40])
	if len(secret) >= 16 {
		copy(kBuf[32:48], secret[0:16])
	}
	readKey := sha256.Sum256(kBuf[:])
	var readIV [16]byte
	copy(readIV[:], plain[40:56])

	// Encrypt plaintext → wire ciphertext using the same stream.
	encStream, err := newAESCTRStream(readKey, readIV)
	if err != nil {
		t.Fatalf("buildRawHeader: newAESCTRStream: %v", err)
	}
	var raw [64]byte
	encStream.XORKeyStream(raw[:], plain[:])

	// The key material bytes (8..39) and IV bytes (40..55) in the wire header
	// must match what ParseObfuscated2Header will use to derive its read key.
	// Because we encrypted plain→raw using plain[8:40] as key material, the
	// parser will correctly derive the same key from raw[8:40].
	// However the parser reads raw[8:40] directly (not decrypted), so we must
	// ensure raw[8:40] == plain[8:40] used in the key derivation — which means
	// the plaintext key-material area must survive in cleartext in the wire
	// header.  But we just encrypted everything... re-read the C code:
	//
	//   memcpy(k, random_header + 8, 32);   ← raw bytes, NOT decrypted
	//   memcpy(k + 32, ext_secret, 16);
	//   sha256(k, 48, key_data.read_key);
	//   memcpy(key_data.read_iv, random_header + 40, 16);  ← raw bytes
	//
	// So the key and IV are derived from the RAW (encrypted) bytes, and then
	// the whole header is decrypted with that key. This is a self-referential
	// construction: the key is derived from the ciphertext, not the plaintext.
	//
	// Therefore our helper must:
	//   1. Pick arbitrary raw[8:40] and raw[40:56] (the "key material" wire bytes).
	//   2. Derive readKey = sha256(raw[8:40] || secret).
	//   3. Derive readIV  = raw[40:56].
	//   4. Decrypt raw[0:64] with AES-CTR to get plain.
	//   5. Set plain[56:60]=magic, plain[60:62]=dc.
	//   6. Re-encrypt plain[0:64] to get the corrected raw.
	//
	// Step 6 gives raw where raw[8:40] and raw[40:56] must match what we used
	// in step 2, but encrypting step changes raw[8:40] (since we're XOR-ing).
	// This is a chicken-and-egg problem in the test helper.
	//
	// Correct approach: treat raw[8:40] and raw[40:56] as fixed "wire" values
	// and compute what raw[56:64] must be such that decrypt(raw)[56:60]==magic.
	// decrypt(raw)[i] = raw[i] XOR keystream[i]
	// So raw[56:60] = magic_bytes XOR keystream[56:60].

	// --- Restart with correct approach ---

	// Fix key material area to deterministic values (arbitrary).
	for i := 0; i < 64; i++ {
		raw[i] = byte(i + 0x10)
	}

	// Derive read key/iv from the fixed raw bytes.
	copy(kBuf[0:32], raw[8:40])
	if len(secret) >= 16 {
		copy(kBuf[32:48], secret[0:16])
	}
	readKey = sha256.Sum256(kBuf[:])
	copy(readIV[:], raw[40:56])

	// Generate keystream for positions 0..63.
	keystream := make([]byte, 64)
	ks, err := newAESCTRStream(readKey, readIV)
	if err != nil {
		t.Fatalf("buildRawHeader: keystream: %v", err)
	}
	ks.XORKeyStream(keystream, keystream) // XOR zeros → keystream

	// Set raw[56:60] so that decrypt gives transportMagic.
	magicBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(magicBytes, transportMagic)
	raw[56] = magicBytes[0] ^ keystream[56]
	raw[57] = magicBytes[1] ^ keystream[57]
	raw[58] = magicBytes[2] ^ keystream[58]
	raw[59] = magicBytes[3] ^ keystream[59]

	// Set raw[60:62] so that decrypt gives targetDC.
	dcBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(dcBytes, uint16(targetDC))
	raw[60] = dcBytes[0] ^ keystream[60]
	raw[61] = dcBytes[1] ^ keystream[61]

	return raw
}

func TestParseObfuscated2Header_Abridged(t *testing.T) {
	secret := make([]byte, 16)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	raw := buildRawHeader(t, secret, TransportMagicAbridged, 1)
	hdr, dec, enc, err := ParseObfuscated2Header(raw, secret)
	if err != nil {
		t.Fatalf("ParseObfuscated2Header: %v", err)
	}
	if hdr.Transport != TransportAbridged {
		t.Errorf("Transport = %d, want %d (Abridged)", hdr.Transport, TransportAbridged)
	}
	if hdr.TargetDC != 1 {
		t.Errorf("TargetDC = %d, want 1", hdr.TargetDC)
	}
	if dec == nil || enc == nil {
		t.Error("dec or enc stream is nil")
	}
}

func TestParseObfuscated2Header_Intermediate(t *testing.T) {
	secret := make([]byte, 16)
	raw := buildRawHeader(t, secret, TransportMagicIntermediate, -1)
	hdr, _, _, err := ParseObfuscated2Header(raw, secret)
	if err != nil {
		t.Fatalf("ParseObfuscated2Header: %v", err)
	}
	if hdr.Transport != TransportIntermediate {
		t.Errorf("Transport = %d, want %d (Intermediate)", hdr.Transport, TransportIntermediate)
	}
	if hdr.TargetDC != -1 {
		t.Errorf("TargetDC = %d, want -1 (media DC)", hdr.TargetDC)
	}
}

func TestParseObfuscated2Header_Padded(t *testing.T) {
	secret := make([]byte, 16)
	raw := buildRawHeader(t, secret, TransportMagicPadded, 2)
	hdr, _, _, err := ParseObfuscated2Header(raw, secret)
	if err != nil {
		t.Fatalf("ParseObfuscated2Header: %v", err)
	}
	if hdr.Transport != TransportPadded {
		t.Errorf("Transport = %d, want %d (Padded)", hdr.Transport, TransportPadded)
	}
}

func TestParseObfuscated2Header_WrongSecret(t *testing.T) {
	secret := make([]byte, 16)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	raw := buildRawHeader(t, secret, TransportMagicAbridged, 1)

	// Use a different secret — should produce wrong magic and fail.
	badSecret := make([]byte, 16)
	_, _, _, err := ParseObfuscated2Header(raw, badSecret)
	if err == nil {
		t.Error("expected error for wrong secret, got nil")
	}
}

func TestParseObfuscated2Header_NoSecret(t *testing.T) {
	// Legacy mode: no secret (nil). The C code uses raw[8:40] directly as key.
	raw := buildRawHeader(t, nil, TransportMagicIntermediate, 3)
	hdr, _, _, err := ParseObfuscated2Header(raw, nil)
	if err != nil {
		t.Fatalf("ParseObfuscated2Header (no secret): %v", err)
	}
	if hdr.Transport != TransportIntermediate {
		t.Errorf("Transport = %d, want Intermediate", hdr.Transport)
	}
	if hdr.TargetDC != 3 {
		t.Errorf("TargetDC = %d, want 3", hdr.TargetDC)
	}
}

// --- ReadPacket / WritePacket round-trip tests ---

func roundTripPacket(t *testing.T, transport TransportType, payload []byte) {
	t.Helper()

	var buf bytes.Buffer

	// For round-trip we need matching enc/dec streams (same key+IV).
	// ParseObfuscated2Header returns enc (proxy→client) and dec (client→proxy)
	// which use DIFFERENT keys. Instead, create a matched pair from the same key.
	key := sha256.Sum256([]byte("test-round-trip-key"))
	var iv [16]byte
	copy(iv[:], key[16:])

	encStream, err := newAESCTRStream(key, iv)
	if err != nil {
		t.Fatalf("newAESCTRStream (enc): %v", err)
	}
	decStream, err := newAESCTRStream(key, iv)
	if err != nil {
		t.Fatalf("newAESCTRStream (dec): %v", err)
	}
	enc := &AESStreamState{stream: encStream}
	dec := &AESStreamState{stream: decStream}

	// Write packet encrypted.
	if err := WritePacket(&buf, payload, enc, transport); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}

	// Read packet decrypted.
	got, err := ReadPacket(&buf, dec, transport)
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Errorf("round-trip mismatch:\n  sent %x\n   got %x", payload, got)
	}
}

func transportMagicForType(t TransportType) uint32 {
	switch t {
	case TransportAbridged:
		return TransportMagicAbridged
	case TransportIntermediate:
		return TransportMagicIntermediate
	case TransportPadded:
		return TransportMagicPadded
	}
	return 0
}

func TestReadWritePacket_Abridged(t *testing.T) {
	// 12 bytes = 3 words (fits in 1-byte length prefix)
	payload := bytes.Repeat([]byte{0xAB}, 12)
	roundTripPacket(t, TransportAbridged, payload)
}

func TestReadWritePacket_Abridged_LargeLength(t *testing.T) {
	// 0x7f * 4 = 508 bytes requires 4-byte length prefix (0x7f marker)
	payload := bytes.Repeat([]byte{0xCD}, 508)
	roundTripPacket(t, TransportAbridged, payload)
}

func TestReadWritePacket_Intermediate(t *testing.T) {
	payload := bytes.Repeat([]byte{0x12, 0x34, 0x56, 0x78}, 8)
	roundTripPacket(t, TransportIntermediate, payload)
}

func TestReadWritePacket_Padded(t *testing.T) {
	payload := bytes.Repeat([]byte{0xFF}, 32)
	roundTripPacket(t, TransportPadded, payload)
}

func TestReadWritePacket_Unencrypted(t *testing.T) {
	// nil enc/dec — plaintext mode
	payload := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 4)
	var buf bytes.Buffer
	if err := WritePacket(&buf, payload, nil, TransportIntermediate); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	got, err := ReadPacket(&buf, nil, TransportIntermediate)
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("unencrypted round-trip mismatch")
	}
}

func TestReadWritePacket_MultiplePackets(t *testing.T) {
	// Verify stream state is correctly maintained across multiple packets.
	key := sha256.Sum256([]byte("test-multiple-packets-key"))
	var iv [16]byte
	copy(iv[:], key[16:])
	encStream, err := newAESCTRStream(key, iv)
	if err != nil {
		t.Fatalf("newAESCTRStream (enc): %v", err)
	}
	decStream, err := newAESCTRStream(key, iv)
	if err != nil {
		t.Fatalf("newAESCTRStream (dec): %v", err)
	}
	enc := &AESStreamState{stream: encStream}
	dec := &AESStreamState{stream: decStream}

	var buf bytes.Buffer
	packets := [][]byte{
		bytes.Repeat([]byte{0x01}, 8),
		bytes.Repeat([]byte{0x02}, 16),
		bytes.Repeat([]byte{0x03}, 4),
	}
	for _, p := range packets {
		if err := WritePacket(&buf, p, enc, TransportIntermediate); err != nil {
			t.Fatalf("WritePacket: %v", err)
		}
	}
	for i, want := range packets {
		got, err := ReadPacket(&buf, dec, TransportIntermediate)
		if err != nil {
			t.Fatalf("ReadPacket[%d]: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("packet[%d] mismatch", i)
		}
	}
}

// TestCryptoHelpers_SHA256 verifies sha256Raw delegates correctly.
func TestCryptoHelpers_SHA256(t *testing.T) {
	input := []byte("hello world")
	got := sha256Raw(input)
	want := sha256.Sum256(input)
	if got != want {
		t.Errorf("sha256Raw mismatch: got %x want %x", got, want)
	}
}
