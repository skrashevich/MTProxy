package crypto

import (
	"hash/crc32"
)

var (
	// ieeeTable is the standard CRC32 (IEEE 802.3) table — same polynomial as C's crc32_table.
	ieeeTable = crc32.IEEETable
	// castagnoliTable is the CRC32C (Castagnoli) table — same polynomial (0x82F63B78) as C's crc32c_table.
	castagnoliTable = crc32.MakeTable(crc32.Castagnoli)
)

// CRC32 computes CRC32 (IEEE) checksum of data with initial value initCRC.
// Equivalent to C: crc32_partial(data, len, crc) — returns partial CRC folded with existing value.
// To compute fresh CRC32: CRC32(data, 0xFFFFFFFF) ^ 0xFFFFFFFF
func CRC32Partial(data []byte, initCRC uint32) uint32 {
	return crc32.Update(initCRC, ieeeTable, data)
}

// CRC32 computes standard CRC32 (IEEE) of data (initial value 0).
func CRC32(data []byte) uint32 {
	return crc32.Checksum(data, ieeeTable)
}

// CRC32C computes CRC32C (Castagnoli) checksum of data with initial value initCRC.
// Equivalent to C: crc32c_partial(data, len, crc)
func CRC32CPartial(data []byte, initCRC uint32) uint32 {
	return crc32.Update(initCRC, castagnoliTable, data)
}

// CRC32C computes standard CRC32C (Castagnoli) of data (initial value 0).
func CRC32C(data []byte) uint32 {
	return crc32.Checksum(data, castagnoliTable)
}
