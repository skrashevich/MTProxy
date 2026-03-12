package crypto

import (
	"testing"
)

func TestCRC32(t *testing.T) {
	tests := []struct {
		input string
		want  uint32
	}{
		{"", 0x00000000},
		{"123456789", 0xcbf43926},
		{"The quick brown fox jumps over the lazy dog", 0x414fa339},
	}
	for _, tt := range tests {
		got := CRC32([]byte(tt.input))
		if got != tt.want {
			t.Errorf("CRC32(%q) = 0x%08x, want 0x%08x", tt.input, got, tt.want)
		}
	}
}

func TestCRC32Partial(t *testing.T) {
	// CRC32 of two parts should equal CRC32 of whole
	data := []byte("Hello, World!")
	part1 := data[:5]
	part2 := data[5:]

	full := CRC32(data)
	partial := CRC32Partial(part2, CRC32Partial(part1, 0))
	if partial != full {
		t.Errorf("CRC32Partial chained = 0x%08x, want 0x%08x", partial, full)
	}
}

func TestCRC32C(t *testing.T) {
	// CRC32C test vectors (Castagnoli)
	tests := []struct {
		input string
		want  uint32
	}{
		{"", 0x00000000},
		{"123456789", 0xe3069283},
	}
	for _, tt := range tests {
		got := CRC32C([]byte(tt.input))
		if got != tt.want {
			t.Errorf("CRC32C(%q) = 0x%08x, want 0x%08x", tt.input, got, tt.want)
		}
	}
}

func TestCRC32CPartial(t *testing.T) {
	data := []byte("Hello, CRC32C!")
	part1 := data[:7]
	part2 := data[7:]

	full := CRC32C(data)
	partial := CRC32CPartial(part2, CRC32CPartial(part1, 0))
	if partial != full {
		t.Errorf("CRC32CPartial chained = 0x%08x, want 0x%08x", partial, full)
	}
}
