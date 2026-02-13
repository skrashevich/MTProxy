package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
)

const (
	CryptoTempDHParamsMagic uint32 = 0xab45ccd3
	RPCDHGen                uint32 = 3
	RPCParamHash            uint32 = 0x00620b93
)

type TempDHParams struct {
	Magic        uint32
	DHParamsHash uint32
	A            [256]byte
}

type DH struct {
	prime     *big.Int
	generator *big.Int
}

func NewDH() *DH {
	prime := new(big.Int).SetBytes(rpcDHPrime[:])
	return &DH{
		prime:     prime,
		generator: big.NewInt(int64(RPCDHGen)),
	}
}

func DefaultDH() *DH {
	return NewDH()
}

func (d *DH) Prime() *big.Int {
	return new(big.Int).Set(d.prime)
}

func (d *DH) Generator() int {
	return int(RPCDHGen)
}

func (d *DH) ParamsSelect() uint32 {
	buf := make([]byte, 8+len(rpcDHPrime))
	binary.LittleEndian.PutUint32(buf[0:4], RPCDHGen)
	binary.LittleEndian.PutUint32(buf[4:8], 0x000100fe)
	copy(buf[8:], rpcDHPrime[:])
	h := SHA1(buf)
	return binary.LittleEndian.Uint32(h[:4])
}

func (d *DH) IsGoodPublicValue(data []byte) bool {
	if len(data) != 256 {
		return false
	}

	ok := false
	for i := 0; i < 8; i++ {
		if data[i] != 0 {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}

	for i := 0; i < 8; i++ {
		if data[i] > rpcDHPrime[i] {
			return false
		}
		if data[i] < rpcDHPrime[i] {
			return true
		}
	}
	return false
}

func (d *DH) PublicFromPrivate(private [256]byte) ([256]byte, error) {
	pow := new(big.Int).SetBytes(private[:])
	value := new(big.Int).Exp(d.generator, pow, d.prime)
	return toDHBlock(value)
}

func (d *DH) SharedSecret(peer [256]byte, private [256]byte) ([256]byte, error) {
	if !d.IsGoodPublicValue(peer[:]) {
		return [256]byte{}, fmt.Errorf("bad dh public value")
	}

	base := new(big.Int).SetBytes(peer[:])
	pow := new(big.Int).SetBytes(private[:])
	key := new(big.Int).Exp(base, pow, d.prime)
	return toDHBlock(key)
}

func (d *DH) FirstRound(r io.Reader) (public [256]byte, params TempDHParams, err error) {
	if r == nil {
		r = rand.Reader
	}
	params.Magic = CryptoTempDHParamsMagic
	params.DHParamsHash = d.ParamsSelect()

	const maxAttempts = 1024
	for i := 0; i < maxAttempts; i++ {
		if _, err = io.ReadFull(r, params.A[:]); err != nil {
			return [256]byte{}, TempDHParams{}, err
		}
		public, err = d.PublicFromPrivate(params.A)
		if err != nil {
			continue
		}
		if d.IsGoodPublicValue(public[:]) {
			return public, params, nil
		}
	}
	return [256]byte{}, TempDHParams{}, fmt.Errorf("unable to generate good dh public value")
}

func (d *DH) SecondRound(peer [256]byte, r io.Reader) (shared [256]byte, public [256]byte, err error) {
	if !d.IsGoodPublicValue(peer[:]) {
		return [256]byte{}, [256]byte{}, fmt.Errorf("bad dh public value")
	}
	if r == nil {
		r = rand.Reader
	}

	var private [256]byte
	found := false
	const maxAttempts = 1024
	for i := 0; i < maxAttempts; i++ {
		if _, err = io.ReadFull(r, private[:]); err != nil {
			return [256]byte{}, [256]byte{}, err
		}
		public, err = d.PublicFromPrivate(private)
		if err != nil {
			continue
		}
		if d.IsGoodPublicValue(public[:]) {
			found = true
			break
		}
	}
	if !found {
		return [256]byte{}, [256]byte{}, fmt.Errorf("unable to generate good dh public value")
	}

	shared, err = d.SharedSecret(peer, private)
	if err != nil {
		return [256]byte{}, [256]byte{}, err
	}
	for i := range private {
		private[i] = 0
	}
	return shared, public, nil
}

func (d *DH) ThirdRound(peer [256]byte, params TempDHParams) ([256]byte, error) {
	if !d.IsGoodPublicValue(peer[:]) {
		return [256]byte{}, fmt.Errorf("bad dh public value")
	}
	if params.Magic != CryptoTempDHParamsMagic {
		return [256]byte{}, fmt.Errorf("invalid dh params magic: %08x", params.Magic)
	}
	return d.SharedSecret(peer, params.A)
}

func toDHBlock(v *big.Int) ([256]byte, error) {
	lenV := len(v.Bytes())
	if lenV <= 240 || lenV > 256 {
		return [256]byte{}, fmt.Errorf("invalid dh value length: %d", lenV)
	}
	var out [256]byte
	b := v.Bytes()
	copy(out[256-len(b):], b)
	return out, nil
}

var rpcDHPrime = [256]byte{0x89, 0x52, 0x13, 0x1b, 0x1e, 0x3a, 0x69, 0xba, 0x5f, 0x85, 0xcf, 0x8b, 0xd2, 0x66, 0xc1, 0x2b, 0x13, 0x83, 0x16, 0x13, 0xbd, 0x2a, 0x4e, 0xf8, 0x35, 0xa4, 0xd5, 0x3f, 0x9d, 0xbb, 0x42, 0x48, 0x2d, 0xbd, 0x46, 0x2b, 0x31, 0xd8, 0x6c, 0x81, 0x6c, 0x59, 0x77, 0x52, 0x0f, 0x11, 0x70, 0x73, 0x9e, 0xd2, 0xdd, 0xd6, 0xd8, 0x1b, 0x9e, 0xb6, 0x5f, 0xaa, 0xac, 0x14, 0x87, 0x53, 0xc9, 0xe4, 0xf0, 0x72, 0xdc, 0x11, 0xa4, 0x92, 0x73, 0x06, 0x83, 0xfa, 0x00, 0x67, 0x82, 0x6b, 0x18, 0xc5, 0x1d, 0x7e, 0xcb, 0xa5, 0x2b, 0x82, 0x60, 0x75, 0xc0, 0xb9, 0x55, 0xe5, 0xac, 0xaf, 0xdd, 0x74, 0xc3, 0x79, 0x5f, 0xd9, 0x52, 0x0b, 0x48, 0x0f, 0x3b, 0xe3, 0xba, 0x06, 0x65, 0x33, 0x8a, 0x49, 0x8c, 0xa5, 0xda, 0xf1, 0x01, 0x76, 0x05, 0x09, 0xa3, 0x8c, 0x49, 0xe3, 0x00, 0x74, 0x64, 0x08, 0x77, 0x4b, 0xb3, 0xed, 0x26, 0x18, 0x1a, 0x64, 0x55, 0x76, 0x6a, 0xe9, 0x49, 0x7b, 0xb9, 0xc3, 0xa3, 0xad, 0x5c, 0xba, 0xf7, 0x6b, 0x73, 0x84, 0x5f, 0xbb, 0x96, 0xbb, 0x6d, 0x0f, 0x68, 0x4f, 0x95, 0xd2, 0xd3, 0x9c, 0xcb, 0xb4, 0xa9, 0x04, 0xfa, 0xb1, 0xde, 0x43, 0x49, 0xce, 0x1c, 0x20, 0x87, 0xb6, 0xc9, 0x51, 0xed, 0x99, 0xf9, 0x52, 0xe3, 0x4f, 0xd1, 0xa3, 0xfd, 0x14, 0x83, 0x35, 0x75, 0x41, 0x47, 0x29, 0xa3, 0x8b, 0xe8, 0x68, 0xa4, 0xf9, 0xec, 0x62, 0x3a, 0x5d, 0x24, 0x62, 0x1a, 0xba, 0x01, 0xb2, 0x55, 0xc7, 0xe8, 0x38, 0x5d, 0x16, 0xac, 0x93, 0xb0, 0x2d, 0x2a, 0x54, 0x0a, 0x76, 0x42, 0x98, 0x2d, 0x22, 0xad, 0xa3, 0xcc, 0xde, 0x5c, 0x8d, 0x26, 0x6f, 0xaa, 0x25, 0xdd, 0x2d, 0xe9, 0xf6, 0xd4, 0x91, 0x04, 0x16, 0x2f, 0x68, 0x5c, 0x45, 0xfe, 0x34, 0xdd, 0xab}
