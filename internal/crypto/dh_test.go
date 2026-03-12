package crypto

import (
	"math/big"
	"testing"
)

func TestDHPrimeIsValid(t *testing.T) {
	// Prime must be 256 bytes (2048-bit)
	if len(rpcDHPrimeBin) != 256 {
		t.Fatalf("prime length = %d, want 256", len(rpcDHPrimeBin))
	}
	// First byte must be non-zero (i.e., actually 2048-bit number)
	if rpcDHPrimeBin[0] == 0 {
		t.Error("prime leading byte is zero, not a 2048-bit number")
	}
}

func TestGenerateDHParams(t *testing.T) {
	params, err := GenerateDHParams()
	if err != nil {
		t.Fatalf("GenerateDHParams: %v", err)
	}

	// GA must be 256 bytes and a valid DH value
	if !isGoodDHBin(params.GA[:]) {
		t.Error("generated GA is not a valid DH value")
	}

	// Verify: g^a mod p == GA
	a := new(big.Int).SetBytes(params.a[:])
	expected := new(big.Int).Exp(dhGenerator, a, dhPrime)
	expectedBytes := bigIntTo256(expected)
	if expectedBytes != params.GA {
		t.Error("GA != g^a mod p")
	}
}

func TestDHFirstAndThirdRound(t *testing.T) {
	// Simulate: party A does first round, party B computes shared secret and sends g_b
	// party A completes with third round — both get same shared secret.

	gaBytes, paramsA, err := DHFirstRound()
	if err != nil {
		t.Fatalf("DHFirstRound: %v", err)
	}

	// Party B: generate g_b and compute shared secret g_ab = g_a^b mod p
	paramsB, err := GenerateDHParams()
	if err != nil {
		t.Fatalf("GenerateDHParams B: %v", err)
	}

	sharedB, err := DHComputeSharedSecret(gaBytes[:], paramsB.a)
	if err != nil {
		t.Fatalf("DHComputeSharedSecret B: %v", err)
	}

	// Party A: complete with g_b from B
	sharedA, err := DHThirdRound(paramsB.GA[:], paramsA)
	if err != nil {
		t.Fatalf("DHThirdRound A: %v", err)
	}

	if sharedA != sharedB {
		t.Errorf("DH shared secrets differ:\nA: %x\nB: %x", sharedA, sharedB)
	}
}

func TestDHSecondRound(t *testing.T) {
	// party A generates g_a
	gaBytes, paramsA, err := DHFirstRound()
	if err != nil {
		t.Fatalf("DHFirstRound: %v", err)
	}
	_ = gaBytes

	// party B uses second round (server scenario): given g_a, generate g_b, compute shared
	sharedB, gbBytes, err := DHSecondRound(paramsA.GA[:])
	if err != nil {
		t.Fatalf("DHSecondRound: %v", err)
	}

	// party A completes with g_b
	sharedA, err := DHThirdRound(gbBytes[:], paramsA)
	if err != nil {
		t.Fatalf("DHThirdRound: %v", err)
	}

	if sharedA != sharedB {
		t.Errorf("DH shared secrets differ in second round scenario:\nA: %x\nB: %x", sharedA, sharedB)
	}
}

func TestIsGoodDHBin(t *testing.T) {
	// All zeros: invalid (no non-zero in first 8 bytes)
	var zeros [256]byte
	if isGoodDHBin(zeros[:]) {
		t.Error("all-zero value should not be valid")
	}

	// Value == prime: invalid (not less than prime in first 8 bytes)
	if isGoodDHBin(rpcDHPrimeBin) {
		t.Error("prime itself should not be valid")
	}

	// A valid generated value should pass
	params, err := GenerateDHParams()
	if err != nil {
		t.Fatalf("GenerateDHParams: %v", err)
	}
	if !isGoodDHBin(params.GA[:]) {
		t.Error("generated GA should be valid")
	}
}
