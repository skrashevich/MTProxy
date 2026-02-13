package protocol

import (
	"encoding/binary"
	"fmt"
)

type PacketKind int

const (
	PacketKindUnknown PacketKind = iota
	PacketKindEncrypted
	PacketKindDHHandshake
)

type PacketInfo struct {
	Kind        PacketKind
	AuthKeyID   uint64
	InnerLength int32
	Function    uint32
	Length      int
}

func ParseMTProtoPacket(frame []byte) (PacketInfo, error) {
	if len(frame) < 28 || (len(frame)&3) != 0 {
		return PacketInfo{}, fmt.Errorf("invalid frame length: %d", len(frame))
	}

	authKeyID := binary.LittleEndian.Uint64(frame[:8])
	if authKeyID != 0 {
		if len(frame) < encryptedMessageMinSize {
			return PacketInfo{}, fmt.Errorf("invalid encrypted frame length: %d", len(frame))
		}
		return PacketInfo{
			Kind:      PacketKindEncrypted,
			AuthKeyID: authKeyID,
			Length:    len(frame),
		}, nil
	}

	innerLen := int32(binary.LittleEndian.Uint32(frame[16:20]))
	if int(innerLen)+20 > len(frame) {
		return PacketInfo{}, fmt.Errorf("bad inner length: %d (max %d)", innerLen, len(frame)-20)
	}
	if innerLen < 20 {
		return PacketInfo{}, fmt.Errorf("bad inner length: %d", innerLen)
	}

	function := binary.LittleEndian.Uint32(frame[20:24])
	if !IsDHHandshakeFunction(function) {
		return PacketInfo{}, fmt.Errorf("unexpected handshake function: 0x%08x", function)
	}

	return PacketInfo{
		Kind:        PacketKindDHHandshake,
		InnerLength: innerLen,
		Function:    function,
		Length:      len(frame),
	}, nil
}
