package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
)

// AESKeyData holds the derived read/write keys and IVs.
// Equivalent to C struct aes_key_data.
type AESKeyData struct {
	ReadKey  [32]byte
	ReadIV   [16]byte
	WriteKey [32]byte
	WriteIV  [16]byte
}

// AESState holds AES-256-CTR cipher state for a connection direction.
type AESState struct {
	stream cipher.Stream
}

// NewAESCTRState creates a new AES-256-CTR cipher state.
// key must be 32 bytes, iv must be 16 bytes.
// AES-CTR uses encrypt=true for both directions (matching C: EVP_aes_256_ctr with is_encrypt=1).
func NewAESCTRState(key [32]byte, iv [16]byte) (*AESState, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	stream := cipher.NewCTR(block, iv[:])
	return &AESState{stream: stream}, nil
}

// Encrypt encrypts (or decrypts — CTR is symmetric) data in-place.
func (s *AESState) Encrypt(dst, src []byte) {
	s.stream.XORKeyStream(dst, src)
}

// Decrypt is an alias for Encrypt in CTR mode (XOR is self-inverse).
func (s *AESState) Decrypt(dst, src []byte) {
	s.stream.XORKeyStream(dst, src)
}

// AESCreateKeys derives AES keys from the MTProxy handshake parameters.
// This is a direct port of C function aes_create_keys() from net/net-crypto-aes.c.
//
// str := nonce_server || nonce_client || client_timestamp || server_ip || client_port ||
//        ("SERVER"/"CLIENT") || client_ip || server_port || secret || nonce_server ||
//        [client_ipv6 || server_ipv6] || nonce_client
// write_key := SUBSTR(MD5(str+1), 0, 12) || SHA1(str)[0:20]   => 32 bytes
// write_iv  := MD5(str+2)                                       => 16 bytes
// (flip CLIENT/SERVER in str bytes 42-47)
// read_key  := same derivation with flipped str
// read_iv   := MD5(str+2) with flipped str
//
// amClient: true if we are the client side
// nonceServer, nonceClient: 16-byte nonces
// clientTimestamp: 4-byte timestamp (little-endian in wire format)
// serverIP, clientIP: IPv4 addresses (0 for IPv6-only)
// serverPort, clientPort: port numbers
// serverIPv6, clientIPv6: 16-byte IPv6 addresses (used when serverIP==0)
// secret: the shared secret
// tempKey: optional temp key XOR'd into str (may be nil)
func AESCreateKeys(
	amClient bool,
	nonceServer, nonceClient [16]byte,
	clientTimestamp uint32,
	serverIP uint32, serverPort uint16,
	serverIPv6 [16]byte,
	clientIP uint32, clientPort uint16,
	clientIPv6 [16]byte,
	secret []byte,
	tempKey []byte,
) (*AESKeyData, error) {
	// Build str buffer: max size as in C
	buf := make([]byte, 0, 16+16+4+4+2+6+4+2+256+16+32+16+256)

	buf = append(buf, nonceServer[:]...)       // 0..15
	buf = append(buf, nonceClient[:]...)        // 16..31
	ts := make([]byte, 4)
	binary.LittleEndian.PutUint32(ts, clientTimestamp)
	buf = append(buf, ts...)                   // 32..35
	sip := make([]byte, 4)
	binary.LittleEndian.PutUint32(sip, serverIP)
	// C stores client_port at offset 40 (after server_ip at 36): wait — re-read C carefully
	// str+36 = server_ip (uint), str+40 = client_port (ushort), str+42 = CLIENT/SERVER, str+48 = client_ip, str+52 = server_port
	buf = append(buf, sip...)                  // 36..39 server_ip
	cp := make([]byte, 2)
	binary.LittleEndian.PutUint16(cp, clientPort)
	buf = append(buf, cp...)                   // 40..41 client_port
	if amClient {
		buf = append(buf, []byte("CLIENT")...)  // 42..47
	} else {
		buf = append(buf, []byte("SERVER")...)  // 42..47
	}
	cip := make([]byte, 4)
	binary.LittleEndian.PutUint32(cip, clientIP)
	buf = append(buf, cip...)                  // 48..51 client_ip
	sp := make([]byte, 2)
	binary.LittleEndian.PutUint16(sp, serverPort)
	buf = append(buf, sp...)                   // 52..53 server_port
	buf = append(buf, secret...)               // 54..54+len(secret)-1
	buf = append(buf, nonceServer[:]...)       // nonce_server again

	strLen := len(buf)

	if serverIP == 0 {
		buf = append(buf, clientIPv6[:]...)
		buf = append(buf, serverIPv6[:]...)
		strLen += 32
	}

	buf = append(buf, nonceClient[:]...)
	strLen += 16

	// XOR with tempKey if provided (matching C behavior)
	if len(tempKey) > 0 {
		firstLen := strLen
		if len(tempKey) < firstLen {
			firstLen = len(tempKey)
		}
		for i := 0; i < firstLen; i++ {
			buf[i] ^= tempKey[i]
		}
		if len(tempKey) > strLen {
			for i := strLen; i < len(tempKey) && i < len(buf); i++ {
				buf[i] = tempKey[i]
			}
			if len(tempKey) > strLen {
				strLen = len(tempKey)
			}
		}
	}

	data := buf[:strLen]

	// Derive write keys: key = MD5(str+1)[0:12] || SHA1(str)[0:20], iv = MD5(str+2)
	writeKey, writeIV := deriveKeyIV(data)

	// Flip CLIENT/SERVER bytes at positions 42-47
	// C code: str[42] ^= 'C'^'S', str[43] ^= 'L'^'E', str[44] ^= 'I'^'R', str[45] ^= 'E'^'V', str[46] ^= 'N'^'E', str[47] ^= 'T'^'R'
	if strLen > 47 {
		data[42] ^= 'C' ^ 'S'
		data[43] ^= 'L' ^ 'E'
		data[44] ^= 'I' ^ 'R'
		data[45] ^= 'E' ^ 'V'
		data[46] ^= 'N' ^ 'E'
		data[47] ^= 'T' ^ 'R'
	}

	// Derive read keys with flipped str
	readKey, readIV := deriveKeyIV(data)

	// Zero the buffer for security
	for i := range buf {
		buf[i] = 0
	}

	result := &AESKeyData{}
	copy(result.WriteKey[:], writeKey)
	copy(result.WriteIV[:], writeIV)
	copy(result.ReadKey[:], readKey)
	copy(result.ReadIV[:], readIV)

	return result, nil
}

// deriveKeyIV derives a 32-byte key and 16-byte IV from data.
// key = MD5(data[1:])[0:12] || SHA1(data)[0:20]  => 32 bytes total
// iv  = MD5(data[2:])                             => 16 bytes
func deriveKeyIV(data []byte) (key []byte, iv []byte) {
	if len(data) < 1 {
		return make([]byte, 32), make([]byte, 16)
	}

	md5Key := md5.Sum(data[1:])   // MD5(str+1)
	sha1Key := sha1.Sum(data)     // SHA1(str)

	key = make([]byte, 32)
	copy(key[0:12], md5Key[:12])  // first 12 bytes of MD5
	copy(key[12:32], sha1Key[:])  // all 20 bytes of SHA1

	if len(data) < 2 {
		return key, make([]byte, 16)
	}
	md5IV := md5.Sum(data[2:])    // MD5(str+2)
	iv = md5IV[:]
	return key, iv
}
