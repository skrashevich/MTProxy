package crypto_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	mtcrypto "github.com/TelegramMessenger/MTProxy/internal/crypto"
)

func TestHashAndCRCVectors(t *testing.T) {
	sha1Want := "a9993e364706816aba3e25717850c26c9cd0d89d"
	sha1Sum := mtcrypto.SHA1([]byte("abc"))
	sha1Got := hex.EncodeToString(sha1Sum[:])
	if sha1Got != sha1Want {
		t.Fatalf("sha1 mismatch: got=%s want=%s", sha1Got, sha1Want)
	}

	sha1TwoSum := mtcrypto.SHA1TwoChunks([]byte("a"), []byte("bc"))
	sha1Two := hex.EncodeToString(sha1TwoSum[:])
	if sha1Two != sha1Want {
		t.Fatalf("sha1 two-chunk mismatch: got=%s want=%s", sha1Two, sha1Want)
	}

	sha256Want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	sha256Sum := mtcrypto.SHA256([]byte("abc"))
	sha256Got := hex.EncodeToString(sha256Sum[:])
	if sha256Got != sha256Want {
		t.Fatalf("sha256 mismatch: got=%s want=%s", sha256Got, sha256Want)
	}

	hmacWant := "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
	hmacSum := mtcrypto.SHA256HMAC([]byte("key"), []byte("The quick brown fox jumps over the lazy dog"))
	hmacGot := hex.EncodeToString(hmacSum[:])
	if hmacGot != hmacWant {
		t.Fatalf("hmac-sha256 mismatch: got=%s want=%s", hmacGot, hmacWant)
	}

	crcData := []byte("123456789")
	if got, want := mtcrypto.ComputeCRC32(crcData), uint32(0xcbf43926); got != want {
		t.Fatalf("crc32 mismatch: got=%08x want=%08x", got, want)
	}
	if got, want := mtcrypto.ComputeCRC32C(crcData), uint32(0xe3069283); got != want {
		t.Fatalf("crc32c mismatch: got=%08x want=%08x", got, want)
	}

	seed := ^uint32(0)
	p1 := mtcrypto.CRC32Partial([]byte("1234"), seed)
	p2 := mtcrypto.CRC32Partial([]byte("56789"), p1)
	if got, want := p2^uint32(0xffffffff), mtcrypto.ComputeCRC32(crcData); got != want {
		t.Fatalf("crc32 partial mismatch: got=%08x want=%08x", got, want)
	}

	pc1 := mtcrypto.CRC32CPartial([]byte("1234"), seed)
	pc2 := mtcrypto.CRC32CPartial([]byte("56789"), pc1)
	if got, want := pc2^uint32(0xffffffff), mtcrypto.ComputeCRC32C(crcData); got != want {
		t.Fatalf("crc32c partial mismatch: got=%08x want=%08x", got, want)
	}
}

func TestAESModesVectors(t *testing.T) {
	key := mustDecode32Hex(t, "603deb1015ca71be2b73aef0857d77811f352c073b6108d72d9810a30914dff4")
	ivCBC := mustDecode16Hex(t, "000102030405060708090a0b0c0d0e0f")
	plain := mustDecodeHex(t, "6bc1bee22e409f96e93d7e117393172a")
	cipherWant := mustDecodeHex(t, "f58c4c04d6e5f1ba779eabfb5f7bfbd6")

	cipherGot, err := mtcrypto.EncryptCBC(key, ivCBC, plain)
	if err != nil {
		t.Fatalf("encrypt cbc: %v", err)
	}
	if !bytes.Equal(cipherGot, cipherWant) {
		t.Fatalf("cbc vector mismatch:\n got=%x\nwant=%x", cipherGot, cipherWant)
	}

	plainGot, err := mtcrypto.DecryptCBC(key, ivCBC, cipherGot)
	if err != nil {
		t.Fatalf("decrypt cbc: %v", err)
	}
	if !bytes.Equal(plainGot, plain) {
		t.Fatalf("cbc decrypt mismatch:\n got=%x\nwant=%x", plainGot, plain)
	}

	ivCTR := mustDecode16Hex(t, "f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff")
	ctrWant := mustDecodeHex(t, "601ec313775789a5b7a7f504bbf3d228")
	ctrGot, err := mtcrypto.ApplyCTR(key, ivCTR, plain)
	if err != nil {
		t.Fatalf("apply ctr: %v", err)
	}
	if !bytes.Equal(ctrGot, ctrWant) {
		t.Fatalf("ctr vector mismatch:\n got=%x\nwant=%x", ctrGot, ctrWant)
	}
	ctrPlain, err := mtcrypto.ApplyCTR(key, ivCTR, ctrGot)
	if err != nil {
		t.Fatalf("apply ctr decrypt: %v", err)
	}
	if !bytes.Equal(ctrPlain, plain) {
		t.Fatalf("ctr decrypt mismatch:\n got=%x\nwant=%x", ctrPlain, plain)
	}
}

func TestCreateAESKeysVector(t *testing.T) {
	secret := bytes.Repeat([]byte{0x11}, 32)
	tempKey := bytes.Repeat([]byte{0x22}, 64)

	nonceServer := [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe}
	nonceClient := [16]byte{0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10, 0xef, 0xcd, 0xab, 0x89, 0x67, 0x45, 0x23, 0x01}
	serverIPv6 := [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6}
	clientIPv6 := [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 7, 0, 8, 0, 9, 0, 0x0a, 0, 0x0b, 0, 0x0c}

	keys, err := mtcrypto.CreateAESKeys(
		true,
		nonceServer,
		nonceClient,
		1700000000,
		0,
		443,
		serverIPv6,
		0,
		50000,
		clientIPv6,
		secret,
		tempKey,
	)
	if err != nil {
		t.Fatalf("create keys: %v", err)
	}

	const (
		writeKeyWant = "47986228a9895175677e239e34c4068224d9474db714cdc2c09b3efef03d6b46"
		writeIVWant  = "4c827365a6e2fda7e5138e748ee536f4"
		readKeyWant  = "b9498025e2def176527b99b2a44530025e25208e53c310141e8bcbb99ac15107"
		readIVWant   = "368be3c4a61873e82bd998428f7a494e"
	)

	if got := hex.EncodeToString(keys.WriteKey[:]); got != writeKeyWant {
		t.Fatalf("write key mismatch: got=%s want=%s", got, writeKeyWant)
	}
	if got := hex.EncodeToString(keys.WriteIV[:]); got != writeIVWant {
		t.Fatalf("write iv mismatch: got=%s want=%s", got, writeIVWant)
	}
	if got := hex.EncodeToString(keys.ReadKey[:]); got != readKeyWant {
		t.Fatalf("read key mismatch: got=%s want=%s", got, readKeyWant)
	}
	if got := hex.EncodeToString(keys.ReadIV[:]); got != readIVWant {
		t.Fatalf("read iv mismatch: got=%s want=%s", got, readIVWant)
	}
}

func TestDHVectors(t *testing.T) {
	dh := mtcrypto.NewDH()
	if got := dh.ParamsSelect(); got != mtcrypto.RPCParamHash {
		t.Fatalf("params select mismatch: got=%08x want=%08x", got, mtcrypto.RPCParamHash)
	}

	allZero := make([]byte, 256)
	if dh.IsGoodPublicValue(allZero) {
		t.Fatalf("all-zero public value must be invalid")
	}

	rA := newDeterministicReader(0x41)
	rB := newDeterministicReader(0x42)
	pubA, tempA, err := dh.FirstRound(rA)
	if err != nil {
		t.Fatalf("first round A: %v", err)
	}
	pubB, tempB, err := dh.FirstRound(rB)
	if err != nil {
		t.Fatalf("first round B: %v", err)
	}

	sharedA, err := dh.ThirdRound(pubB, tempA)
	if err != nil {
		t.Fatalf("third round A: %v", err)
	}
	sharedB, err := dh.ThirdRound(pubA, tempB)
	if err != nil {
		t.Fatalf("third round B: %v", err)
	}
	if !bytes.Equal(sharedA[:], sharedB[:]) {
		t.Fatalf("dh shared mismatch between third-round paths")
	}

	sharedSecond, pubSecond, err := dh.SecondRound(pubA, newDeterministicReader(0x43))
	if err != nil {
		t.Fatalf("second round: %v", err)
	}
	sharedThird, err := dh.ThirdRound(pubSecond, tempA)
	if err != nil {
		t.Fatalf("third round from second-round pub: %v", err)
	}
	if !bytes.Equal(sharedSecond[:], sharedThird[:]) {
		t.Fatalf("second-round/third-round shared mismatch")
	}

	var badPeer [256]byte
	if _, _, err := dh.SecondRound(badPeer, newDeterministicReader(0x44)); err == nil {
		t.Fatalf("expected error for invalid peer in second round")
	}
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode hex %q: %v", s, err)
	}
	return b
}

func mustDecode16Hex(t *testing.T, s string) [16]byte {
	t.Helper()
	b := mustDecodeHex(t, s)
	if len(b) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(b))
	}
	var out [16]byte
	copy(out[:], b)
	return out
}

func mustDecode32Hex(t *testing.T, s string) [32]byte {
	t.Helper()
	b := mustDecodeHex(t, s)
	if len(b) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(b))
	}
	var out [32]byte
	copy(out[:], b)
	return out
}

type deterministicReader struct {
	state uint64
}

func newDeterministicReader(seed byte) *deterministicReader {
	return &deterministicReader{state: uint64(seed) + 1}
}

func (r *deterministicReader) Read(p []byte) (int, error) {
	for i := range p {
		r.state = r.state*6364136223846793005 + 1
		p[i] = byte(r.state >> 56)
	}
	return len(p), nil
}

func (r *deterministicReader) String() string {
	return fmt.Sprintf("deterministicReader{%d}", r.state)
}
