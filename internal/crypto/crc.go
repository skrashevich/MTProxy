package crypto

import "hash/crc32"

var (
	crc32IEEETable       = crc32.MakeTable(crc32.IEEE)
	crc32CastagnoliTable = crc32.MakeTable(crc32.Castagnoli)
)

func CRC32Partial(data []byte, crc uint32) uint32 {
	// C code keeps the internal (non-finalized) CRC register.
	// Go's crc32.Update takes/returns finalized state, so we invert at both ends.
	return ^crc32.Update(^crc, crc32IEEETable, data)
}

func ComputeCRC32(data []byte) uint32 {
	return CRC32Partial(data, ^uint32(0)) ^ ^uint32(0)
}

func CRC32CPartial(data []byte, crc uint32) uint32 {
	return ^crc32.Update(^crc, crc32CastagnoliTable, data)
}

func ComputeCRC32C(data []byte) uint32 {
	return CRC32CPartial(data, ^uint32(0)) ^ ^uint32(0)
}
