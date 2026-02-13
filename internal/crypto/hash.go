package crypto

import (
	"crypto/hmac"
	stdmd5 "crypto/md5"
	stdsha1 "crypto/sha1"
	stdsha256 "crypto/sha256"
)

func MD5(data []byte) [16]byte {
	return stdmd5.Sum(data)
}

func SHA1(data []byte) [20]byte {
	return stdsha1.Sum(data)
}

func SHA1TwoChunks(first, second []byte) [20]byte {
	h := stdsha1.New()
	_, _ = h.Write(first)
	_, _ = h.Write(second)

	var out [20]byte
	copy(out[:], h.Sum(nil))
	return out
}

func SHA256(data []byte) [32]byte {
	return stdsha256.Sum256(data)
}

func SHA256TwoChunks(first, second []byte) [32]byte {
	h := stdsha256.New()
	_, _ = h.Write(first)
	_, _ = h.Write(second)

	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func SHA256HMAC(key, data []byte) [32]byte {
	h := hmac.New(stdsha256.New, key)
	_, _ = h.Write(data)

	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
