package crypto

import (
	"bytes"
	"testing"
)

func TestAESCTREncryptDecrypt(t *testing.T) {
	var key [32]byte
	var iv [16]byte
	for i := range key {
		key[i] = byte(i)
	}
	for i := range iv {
		iv[i] = byte(i + 32)
	}

	plaintext := []byte("Hello, MTProxy AES-256-CTR test!")

	enc, err := NewAESCTRState(key, iv)
	if err != nil {
		t.Fatalf("NewAESCTRState enc: %v", err)
	}
	ciphertext := make([]byte, len(plaintext))
	enc.Encrypt(ciphertext, plaintext)

	dec, err := NewAESCTRState(key, iv)
	if err != nil {
		t.Fatalf("NewAESCTRState dec: %v", err)
	}
	recovered := make([]byte, len(ciphertext))
	dec.Decrypt(recovered, ciphertext)

	if !bytes.Equal(recovered, plaintext) {
		t.Errorf("decrypt mismatch: got %q, want %q", recovered, plaintext)
	}
}

func TestAESCTRDifferentFromPlaintext(t *testing.T) {
	var key [32]byte
	var iv [16]byte
	for i := range key {
		key[i] = byte(i + 1)
	}
	plaintext := []byte("test data 12345678")

	enc, _ := NewAESCTRState(key, iv)
	ciphertext := make([]byte, len(plaintext))
	enc.Encrypt(ciphertext, plaintext)

	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext should differ from plaintext")
	}
}

func TestAESCreateKeys_DeterministicOutput(t *testing.T) {
	var nonceServer, nonceClient [16]byte
	var serverIPv6, clientIPv6 [16]byte
	for i := range nonceServer {
		nonceServer[i] = byte(i)
		nonceClient[i] = byte(i + 16)
	}
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 100)
	}

	keys1, err := AESCreateKeys(true, nonceServer, nonceClient, 1234567890,
		0x01020304, 443, serverIPv6,
		0x05060708, 12345, clientIPv6,
		secret, nil)
	if err != nil {
		t.Fatalf("AESCreateKeys: %v", err)
	}

	keys2, err := AESCreateKeys(true, nonceServer, nonceClient, 1234567890,
		0x01020304, 443, serverIPv6,
		0x05060708, 12345, clientIPv6,
		secret, nil)
	if err != nil {
		t.Fatalf("AESCreateKeys second call: %v", err)
	}

	if keys1.WriteKey != keys2.WriteKey {
		t.Error("WriteKey not deterministic")
	}
	if keys1.WriteIV != keys2.WriteIV {
		t.Error("WriteIV not deterministic")
	}
	if keys1.ReadKey != keys2.ReadKey {
		t.Error("ReadKey not deterministic")
	}
	if keys1.ReadIV != keys2.ReadIV {
		t.Error("ReadIV not deterministic")
	}
}

func TestAESCreateKeys_ClientServerSymmetry(t *testing.T) {
	// When the client calls AESCreateKeys(amClient=true),
	// the server calls AESCreateKeys(amClient=false) with swapped roles.
	// The client's write keys must equal server's read keys and vice versa.
	var nonceServer, nonceClient [16]byte
	var serverIPv6, clientIPv6 [16]byte
	for i := range nonceServer {
		nonceServer[i] = byte(i + 1)
		nonceClient[i] = byte(i + 17)
	}
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 50)
	}

	clientKeys, err := AESCreateKeys(true, nonceServer, nonceClient, 111111111,
		0x0a0b0c0d, 8888, serverIPv6,
		0x01020304, 54321, clientIPv6,
		secret, nil)
	if err != nil {
		t.Fatalf("AESCreateKeys client: %v", err)
	}

	serverKeys, err := AESCreateKeys(false, nonceServer, nonceClient, 111111111,
		0x0a0b0c0d, 8888, serverIPv6,
		0x01020304, 54321, clientIPv6,
		secret, nil)
	if err != nil {
		t.Fatalf("AESCreateKeys server: %v", err)
	}

	// client writes → server reads
	if clientKeys.WriteKey != serverKeys.ReadKey {
		t.Errorf("client WriteKey != server ReadKey\nclient: %x\nserver: %x",
			clientKeys.WriteKey, serverKeys.ReadKey)
	}
	if clientKeys.WriteIV != serverKeys.ReadIV {
		t.Errorf("client WriteIV != server ReadIV")
	}
	// server writes → client reads
	if serverKeys.WriteKey != clientKeys.ReadKey {
		t.Errorf("server WriteKey != client ReadKey")
	}
	if serverKeys.WriteIV != clientKeys.ReadIV {
		t.Errorf("server WriteIV != client ReadIV")
	}
}

func TestDeriveKeyIV(t *testing.T) {
	data := []byte("test input for key derivation 12345678")
	key, iv := deriveKeyIV(data)
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}
	if len(iv) != 16 {
		t.Errorf("iv length = %d, want 16", len(iv))
	}
	// Deterministic
	key2, iv2 := deriveKeyIV(data)
	if !bytes.Equal(key, key2) {
		t.Error("deriveKeyIV not deterministic (key)")
	}
	if !bytes.Equal(iv, iv2) {
		t.Error("deriveKeyIV not deterministic (iv)")
	}
}
