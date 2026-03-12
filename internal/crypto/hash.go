// Package crypto provides cryptographic primitives ported from MTProxy C sources.
package crypto

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
)

// MD5 computes MD5 hash of input data.
// Equivalent to C: void md5(unsigned char *input, int ilen, unsigned char output[16])
func MD5(input []byte) [16]byte {
	return md5.Sum(input)
}

// MD5Slice computes MD5 hash and returns a slice.
func MD5Slice(input []byte) []byte {
	sum := md5.Sum(input)
	return sum[:]
}

// SHA1 computes SHA1 hash of input data.
// Equivalent to C: void sha1(const unsigned char *input, int ilen, unsigned char output[20])
func SHA1(input []byte) [20]byte {
	return sha1.Sum(input)
}

// SHA1Slice computes SHA1 hash and returns a slice.
func SHA1Slice(input []byte) []byte {
	sum := sha1.Sum(input)
	return sum[:]
}

// SHA1TwoChunks computes SHA1 hash of two concatenated chunks.
// Equivalent to C: void sha1_two_chunks(input1, ilen1, input2, ilen2, output[20])
func SHA1TwoChunks(input1, input2 []byte) [20]byte {
	h := sha1.New()
	h.Write(input1)
	h.Write(input2)
	var out [20]byte
	copy(out[:], h.Sum(nil))
	return out
}

// SHA256 computes SHA256 hash of input data.
// Equivalent to C: void sha256(const unsigned char *input, int ilen, unsigned char output[32])
func SHA256(input []byte) [32]byte {
	return sha256.Sum256(input)
}

// SHA256Slice computes SHA256 hash and returns a slice.
func SHA256Slice(input []byte) []byte {
	sum := sha256.Sum256(input)
	return sum[:]
}

// SHA256TwoChunks computes SHA256 hash of two concatenated chunks.
// Equivalent to C: void sha256_two_chunks(input1, ilen1, input2, ilen2, output[32])
func SHA256TwoChunks(input1, input2 []byte) [32]byte {
	h := sha256.New()
	h.Write(input1)
	h.Write(input2)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// SHA256HMAC computes HMAC-SHA256.
// Equivalent to C: void sha256_hmac(key, keylen, input, ilen, output[32])
func SHA256HMAC(key, input []byte) [32]byte {
	// Standard HMAC-SHA256 implementation matching OpenSSL HMAC(EVP_sha256(), ...)
	blockSize := 64
	if len(key) > blockSize {
		sum := sha256.Sum256(key)
		key = sum[:]
	}
	ipad := make([]byte, blockSize)
	opad := make([]byte, blockSize)
	copy(ipad, key)
	copy(opad, key)
	for i := range ipad {
		ipad[i] ^= 0x36
		opad[i] ^= 0x5c
	}
	inner := sha256.New()
	inner.Write(ipad)
	inner.Write(input)
	innerSum := inner.Sum(nil)

	outer := sha256.New()
	outer.Write(opad)
	outer.Write(innerSum)
	var out [32]byte
	copy(out[:], outer.Sum(nil))
	return out
}
