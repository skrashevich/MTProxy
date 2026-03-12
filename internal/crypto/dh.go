package crypto

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// rpcDHPrimeBin is the 2048-bit DH prime used by MTProxy RPC.
// Copied directly from C: net/net-crypto-dh.c rpc_dh_prime_bin[256]
var rpcDHPrimeBin = []byte{
	0x89, 0x52, 0x13, 0x1b, 0x1e, 0x3a, 0x69, 0xba, 0x5f, 0x85, 0xcf, 0x8b, 0xd2, 0x66, 0xc1, 0x2b,
	0x13, 0x83, 0x16, 0x13, 0xbd, 0x2a, 0x4e, 0xf8, 0x35, 0xa4, 0xd5, 0x3f, 0x9d, 0xbb, 0x42, 0x48,
	0x2d, 0xbd, 0x46, 0x2b, 0x31, 0xd8, 0x6c, 0x81, 0x6c, 0x59, 0x77, 0x52, 0x0f, 0x11, 0x70, 0x73,
	0x9e, 0xd2, 0xdd, 0xd6, 0xd8, 0x1b, 0x9e, 0xb6, 0x5f, 0xaa, 0xac, 0x14, 0x87, 0x53, 0xc9, 0xe4,
	0xf0, 0x72, 0xdc, 0x11, 0xa4, 0x92, 0x73, 0x06, 0x83, 0xfa, 0x00, 0x67, 0x82, 0x6b, 0x18, 0xc5,
	0x1d, 0x7e, 0xcb, 0xa5, 0x2b, 0x82, 0x60, 0x75, 0xc0, 0xb9, 0x55, 0xe5, 0xac, 0xaf, 0xdd, 0x74,
	0xc3, 0x79, 0x5f, 0xd9, 0x52, 0x0b, 0x48, 0x0f, 0x3b, 0xe3, 0xba, 0x06, 0x65, 0x33, 0x8a, 0x49,
	0x8c, 0xa5, 0xda, 0xf1, 0x01, 0x76, 0x05, 0x09, 0xa3, 0x8c, 0x49, 0xe3, 0x00, 0x74, 0x64, 0x08,
	0x77, 0x4b, 0xb3, 0xed, 0x26, 0x18, 0x1a, 0x64, 0x55, 0x76, 0x6a, 0xe9, 0x49, 0x7b, 0xb9, 0xc3,
	0xa3, 0xad, 0x5c, 0xba, 0xf7, 0x6b, 0x73, 0x84, 0x5f, 0xbb, 0x96, 0xbb, 0x6d, 0x0f, 0x68, 0x4f,
	0x95, 0xd2, 0xd3, 0x9c, 0xcb, 0xb4, 0xa9, 0x04, 0xfa, 0xb1, 0xde, 0x43, 0x49, 0xce, 0x1c, 0x20,
	0x87, 0xb6, 0xc9, 0x51, 0xed, 0x99, 0xf9, 0x52, 0xe3, 0x4f, 0xd1, 0xa3, 0xfd, 0x14, 0x83, 0x35,
	0x75, 0x41, 0x47, 0x29, 0xa3, 0x8b, 0xe8, 0x68, 0xa4, 0xf9, 0xec, 0x62, 0x3a, 0x5d, 0x24, 0x62,
	0x1a, 0xba, 0x01, 0xb2, 0x55, 0xc7, 0xe8, 0x38, 0x5d, 0x16, 0xac, 0x93, 0xb0, 0x2d, 0x2a, 0x54,
	0x0a, 0x76, 0x42, 0x98, 0x2d, 0x22, 0xad, 0xa3, 0xcc, 0xde, 0x5c, 0x8d, 0x26, 0x6f, 0xaa, 0x25,
	0xdd, 0x2d, 0xe9, 0xf6, 0xd4, 0x91, 0x04, 0x16, 0x2f, 0x68, 0x5c, 0x45, 0xfe, 0x34, 0xdd, 0xab,
}

// RPC_DH_GEN is the DH generator (g=3) used in MTProxy.
// Equivalent to C: #define RPC_DH_GEN 3
const RPCDHGen = 3

var (
	dhPrime     *big.Int
	dhGenerator *big.Int
)

func init() {
	dhPrime = new(big.Int).SetBytes(rpcDHPrimeBin)
	dhGenerator = big.NewInt(RPCDHGen)
}

// isGoodDHBin checks that a 256-byte value is a valid DH public value:
// non-zero in the first 8 bytes and less than the prime in those bytes.
// Equivalent to C: is_good_rpc_dh_bin()
func isGoodDHBin(data []byte) bool {
	if len(data) < 256 {
		return false
	}
	hasNonZero := false
	for i := 0; i < 8; i++ {
		if data[i] != 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		return false
	}
	for i := 0; i < 8; i++ {
		if data[i] > rpcDHPrimeBin[i] {
			return false
		}
		if data[i] < rpcDHPrimeBin[i] {
			return true
		}
	}
	return false
}

// bigIntTo256 serializes a big.Int to a 256-byte big-endian buffer.
func bigIntTo256(n *big.Int) [256]byte {
	var buf [256]byte
	b := n.Bytes()
	if len(b) > 256 {
		b = b[len(b)-256:]
	}
	copy(buf[256-len(b):], b)
	return buf
}

// DHParams holds a generated DH private exponent and the corresponding public value.
type DHParams struct {
	// a is the private exponent (256 bytes)
	a [256]byte
	// GA is g^a mod p (256 bytes) — the public value to send to the peer
	GA [256]byte
}

// GenerateDHParams generates DH parameters: random private exponent a and public g^a mod p.
// Equivalent to C: create_g_a() used in dh_first_round / dh_second_round.
func GenerateDHParams() (*DHParams, error) {
	for {
		aBytes := make([]byte, 256)
		if _, err := rand.Read(aBytes); err != nil {
			return nil, fmt.Errorf("rand.Read: %w", err)
		}

		a := new(big.Int).SetBytes(aBytes)
		ga := new(big.Int).Exp(dhGenerator, a, dhPrime)

		gaBytes := ga.Bytes()
		if len(gaBytes) < 240 || len(gaBytes) > 256 {
			continue
		}

		ga256 := bigIntTo256(ga)
		if !isGoodDHBin(ga256[:]) {
			continue
		}

		p := &DHParams{}
		copy(p.a[:], aBytes)
		p.GA = ga256
		return p, nil
	}
}

// DHComputeSharedSecret computes the DH shared secret: g_b^a mod p.
// gB is the peer's public value (256 bytes).
// Equivalent to C: dh_inner_round()
// Returns the 256-byte shared secret or error if gB is invalid.
func DHComputeSharedSecret(gB []byte, a [256]byte) ([256]byte, error) {
	if !isGoodDHBin(gB) {
		return [256]byte{}, fmt.Errorf("invalid DH public value g_b")
	}

	gBInt := new(big.Int).SetBytes(gB)
	aInt := new(big.Int).SetBytes(a[:])
	shared := new(big.Int).Exp(gBInt, aInt, dhPrime)

	sharedBytes := shared.Bytes()
	if len(sharedBytes) < 240 || len(sharedBytes) > 256 {
		return [256]byte{}, fmt.Errorf("DH shared secret has unexpected length %d", len(sharedBytes))
	}

	return bigIntTo256(shared), nil
}

// DHFirstRound generates g^a and stores the private exponent for later.
// Equivalent to C: dh_first_round()
// Returns (g_a[256], DHParams) where DHParams must be kept for dh_third_round.
func DHFirstRound() ([256]byte, *DHParams, error) {
	params, err := GenerateDHParams()
	if err != nil {
		return [256]byte{}, nil, err
	}
	return params.GA, params, nil
}

// DHSecondRound is used by the server-side: given client's g_b, generate a new g_a
// and compute the shared secret g_ab = g_b^a mod p.
// Equivalent to C: dh_second_round()
// Returns (g_ab[256], g_a[256], error).
func DHSecondRound(gB []byte) ([256]byte, [256]byte, error) {
	if !isGoodDHBin(gB) {
		return [256]byte{}, [256]byte{}, fmt.Errorf("invalid DH public value g_b")
	}

	params, err := GenerateDHParams()
	if err != nil {
		return [256]byte{}, [256]byte{}, err
	}

	shared, err := DHComputeSharedSecret(gB, params.a)
	if err != nil {
		return [256]byte{}, [256]byte{}, err
	}

	// zero private exponent
	for i := range params.a {
		params.a[i] = 0
	}

	return shared, params.GA, nil
}

// DHThirdRound computes the shared secret using a previously stored private exponent.
// Equivalent to C: dh_third_round()
// gB is the peer's public value, params is from DHFirstRound.
func DHThirdRound(gB []byte, params *DHParams) ([256]byte, error) {
	shared, err := DHComputeSharedSecret(gB, params.a)
	// zero private exponent after use
	for i := range params.a {
		params.a[i] = 0
	}
	return shared, err
}
