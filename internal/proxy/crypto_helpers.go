package proxy

import (
	"crypto/cipher"
	"fmt"

	"github.com/skrashevich/MTProxy/internal/crypto"
)

// AESStreamState wraps an AES-256-CTR cipher.Stream for a single direction.
// This is a thin proxy-layer wrapper; the underlying crypto is provided by
// internal/crypto (crypto.AESState). The extra functionality here is
// newAESCTRStreamAt — advancing the stream to a given byte offset — which
// is needed for obfuscated2 where the stream starts at position 64 (after
// the header that was already processed).
//
// Refactor note: crypto.AESState.Encrypt/Decrypt map directly to
// AESStreamState; once integration is complete callers can be migrated to
// use crypto.AESState directly if desired.
type AESStreamState struct {
	stream cipher.Stream
}

// sha256Raw delegates to internal/crypto.SHA256.
func sha256Raw(data []byte) [32]byte {
	return crypto.SHA256(data)
}

// newAESCTRStream creates an AES-256-CTR stream at byte position 0.
// Delegates key/cipher creation to internal/crypto.NewAESCTRState.
func newAESCTRStream(key [32]byte, iv [16]byte) (cipher.Stream, error) {
	state, err := crypto.NewAESCTRState(key, iv)
	if err != nil {
		return nil, fmt.Errorf("newAESCTRStream: %w", err)
	}
	// Expose the underlying stream. We use a one-shot encrypt of a zero-length
	// slice to get the cipher.Stream interface from the state without copying.
	return aesStateStream{state}, nil
}

// newAESCTRStreamAt creates an AES-256-CTR stream pre-advanced by skipBytes.
// This is required for obfuscated2: the 64-byte header is processed with the
// same key/iv but the ongoing data stream must start at position 64.
func newAESCTRStreamAt(key [32]byte, iv [16]byte, skipBytes int) (cipher.Stream, error) {
	state, err := crypto.NewAESCTRState(key, iv)
	if err != nil {
		return nil, fmt.Errorf("newAESCTRStreamAt: %w", err)
	}
	s := aesStateStream{state}
	if skipBytes > 0 {
		dummy := make([]byte, skipBytes)
		s.XORKeyStream(dummy, dummy)
	}
	return s, nil
}

// aesStateStream adapts crypto.AESState to the cipher.Stream interface so
// that it can be stored inside AESStreamState.stream without an extra
// allocation tier.
type aesStateStream struct {
	s *crypto.AESState
}

func (a aesStateStream) XORKeyStream(dst, src []byte) {
	a.s.Encrypt(dst, src)
}
