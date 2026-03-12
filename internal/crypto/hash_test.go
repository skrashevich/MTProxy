package crypto

import (
	"encoding/hex"
	"testing"
)

// Test vectors from RFC 1321 (MD5) and FIPS 180-4 (SHA1, SHA256).

func TestMD5(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "d41d8cd98f00b204e9800998ecf8427e"},
		{"a", "0cc175b9c0f1b6a831c399e269772661"},
		{"abc", "900150983cd24fb0d6963f7d28e17f72"},
		{"message digest", "f96b697d7cb7938d525a2f31aaf161d0"},
		{"abcdefghijklmnopqrstuvwxyz", "c3fcd3d76192e4007dfb496cca67e13b"},
	}
	for _, tt := range tests {
		got := MD5([]byte(tt.input))
		if hex.EncodeToString(got[:]) != tt.want {
			t.Errorf("MD5(%q) = %x, want %s", tt.input, got, tt.want)
		}
	}
}

func TestSHA1(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
		{"abc", "a9993e364706816aba3e25717850c26c9cd0d89d"},
		{"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq", "84983e441c3bd26ebaae4aa1f95129e5e54670f1"},
	}
	for _, tt := range tests {
		got := SHA1([]byte(tt.input))
		if hex.EncodeToString(got[:]) != tt.want {
			t.Errorf("SHA1(%q) = %x, want %s", tt.input, got, tt.want)
		}
	}
}

func TestSHA1TwoChunks(t *testing.T) {
	// SHA1("abc" + "def") == SHA1("abcdef")
	want := SHA1([]byte("abcdef"))
	got := SHA1TwoChunks([]byte("abc"), []byte("def"))
	if got != want {
		t.Errorf("SHA1TwoChunks mismatch: got %x, want %x", got, want)
	}
}

func TestSHA256(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	}
	for _, tt := range tests {
		got := SHA256([]byte(tt.input))
		if hex.EncodeToString(got[:]) != tt.want {
			t.Errorf("SHA256(%q) = %x, want %s", tt.input, got, tt.want)
		}
	}
}

func TestSHA256TwoChunks(t *testing.T) {
	want := SHA256([]byte("helloworld"))
	got := SHA256TwoChunks([]byte("hello"), []byte("world"))
	if got != want {
		t.Errorf("SHA256TwoChunks mismatch: got %x, want %x", got, want)
	}
}

func TestSHA256HMAC(t *testing.T) {
	// HMAC-SHA256 test vector from RFC 4231 Test Case 1:
	// key  = 0x0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b (20 bytes)
	// data = "Hi There"
	// want = b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7
	key, _ := hex.DecodeString("0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b")
	data := []byte("Hi There")
	want := "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"
	got := SHA256HMAC(key, data)
	if hex.EncodeToString(got[:]) != want {
		t.Errorf("SHA256HMAC = %x, want %s", got, want)
	}
}
