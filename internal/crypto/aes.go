package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
)

const (
	MinSecretLen = 32
	MaxSecretLen = 256
)

const maxCreateKeysBufferLen = 16 + 16 + 4 + 4 + 2 + 6 + 4 + 2 + MaxSecretLen + 16 + 16 + 4 + 16*2 + 256

type AESKeyData struct {
	ReadKey  [32]byte
	ReadIV   [16]byte
	WriteKey [32]byte
	WriteIV  [16]byte
}

type CipherSuite interface {
	EncryptCBC(key [32]byte, iv [16]byte, plaintext []byte) ([]byte, error)
	DecryptCBC(key [32]byte, iv [16]byte, ciphertext []byte) ([]byte, error)
	ApplyCTR(key [32]byte, iv [16]byte, data []byte) ([]byte, error)
}

type StandardCipherSuite struct{}

func (StandardCipherSuite) EncryptCBC(key [32]byte, iv [16]byte, plaintext []byte) ([]byte, error) {
	return EncryptCBC(key, iv, plaintext)
}

func (StandardCipherSuite) DecryptCBC(key [32]byte, iv [16]byte, ciphertext []byte) ([]byte, error) {
	return DecryptCBC(key, iv, ciphertext)
}

func (StandardCipherSuite) ApplyCTR(key [32]byte, iv [16]byte, data []byte) ([]byte, error) {
	return ApplyCTR(key, iv, data)
}

var DefaultCipherSuite CipherSuite = StandardCipherSuite{}

func CreateAESKeys(
	amClient bool,
	nonceServer [16]byte,
	nonceClient [16]byte,
	clientTimestamp int32,
	serverIP uint32,
	serverPort uint16,
	serverIPv6 [16]byte,
	clientIP uint32,
	clientPort uint16,
	clientIPv6 [16]byte,
	secret []byte,
	tempKey []byte,
) (AESKeyData, error) {
	if err := ValidateSecret(secret); err != nil {
		return AESKeyData{}, err
	}

	str := make([]byte, 0, 96+len(secret)+len(tempKey))
	str = append(str, nonceServer[:]...)
	str = append(str, nonceClient[:]...)

	var b4 [4]byte
	binary.LittleEndian.PutUint32(b4[:], uint32(clientTimestamp))
	str = append(str, b4[:]...)
	binary.LittleEndian.PutUint32(b4[:], serverIP)
	str = append(str, b4[:]...)

	var b2 [2]byte
	binary.LittleEndian.PutUint16(b2[:], clientPort)
	str = append(str, b2[:]...)
	if amClient {
		str = append(str, []byte("CLIENT")...)
	} else {
		str = append(str, []byte("SERVER")...)
	}
	binary.LittleEndian.PutUint32(b4[:], clientIP)
	str = append(str, b4[:]...)
	binary.LittleEndian.PutUint16(b2[:], serverPort)
	str = append(str, b2[:]...)

	str = append(str, secret...)
	str = append(str, nonceServer[:]...)
	if serverIP == 0 {
		str = append(str, clientIPv6[:]...)
		str = append(str, serverIPv6[:]...)
	}
	str = append(str, nonceClient[:]...)

	tempKeyLen := len(tempKey)
	if tempKeyLen > maxCreateKeysBufferLen {
		tempKeyLen = maxCreateKeysBufferLen
	}
	if tempKeyLen > len(str) {
		grow := make([]byte, tempKeyLen-len(str))
		str = append(str, grow...)
	}
	firstLen := tempKeyLen
	if firstLen > len(str) {
		firstLen = len(str)
	}
	for i := 0; i < firstLen; i++ {
		str[i] ^= tempKey[i]
	}

	var out AESKeyData
	wmd5 := MD5(str[1:])
	copy(out.WriteKey[:12], wmd5[:12])
	wsha1 := SHA1(str)
	copy(out.WriteKey[12:], wsha1[:])
	wiv := MD5(str[2:])
	copy(out.WriteIV[:], wiv[:])

	toggleClientServerMarker(str)

	rmd5 := MD5(str[1:])
	copy(out.ReadKey[:12], rmd5[:12])
	rsha1 := SHA1(str)
	copy(out.ReadKey[12:], rsha1[:])
	riv := MD5(str[2:])
	copy(out.ReadIV[:], riv[:])

	for i := range str {
		str[i] = 0
	}

	return out, nil
}

func ValidateSecret(secret []byte) error {
	if len(secret) < MinSecretLen || len(secret) > MaxSecretLen {
		return fmt.Errorf("secret length out of range: %d (expected %d..%d)", len(secret), MinSecretLen, MaxSecretLen)
	}
	return nil
}

func EncryptCBC(key [32]byte, iv [16]byte, plaintext []byte) ([]byte, error) {
	if len(plaintext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("cbc plaintext length must be multiple of %d", aes.BlockSize)
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(plaintext))
	cbc := cipher.NewCBCEncrypter(block, iv[:])
	cbc.CryptBlocks(out, plaintext)
	return out, nil
}

func DecryptCBC(key [32]byte, iv [16]byte, ciphertext []byte) ([]byte, error) {
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("cbc ciphertext length must be multiple of %d", aes.BlockSize)
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(ciphertext))
	cbc := cipher.NewCBCDecrypter(block, iv[:])
	cbc.CryptBlocks(out, ciphertext)
	return out, nil
}

func ApplyCTR(key [32]byte, iv [16]byte, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	stream := cipher.NewCTR(block, iv[:])
	stream.XORKeyStream(out, data)
	return out, nil
}

func toggleClientServerMarker(str []byte) {
	if len(str) < 48 {
		return
	}
	str[42] ^= 'C' ^ 'S'
	str[43] ^= 'L' ^ 'E'
	str[44] ^= 'I' ^ 'R'
	str[45] ^= 'E' ^ 'V'
	str[46] ^= 'N' ^ 'E'
	str[47] ^= 'T' ^ 'R'
}
