package protocol

import (
	"encoding/binary"
	"errors"
)

// PacketType классифицирует MTProto-пакет по auth_key_id.
type PacketType int

const (
	PacketUnencrypted PacketType = iota // auth_key_id == 0 (DH-рукопожатие)
	PacketEncrypted                     // auth_key_id != 0 (зашифрованное сообщение)
)

// MTProtoPacket — результат классификации входящего MTProto-пакета.
type MTProtoPacket struct {
	Type      PacketType
	AuthKeyID int64
	// Для нешифрованных — функция DH (req_pq и др.)
	DHFunction uint32
	// Полный payload пакета (включая заголовок)
	Data []byte
}

// ParseMTProtoPacket классифицирует MTProto-пакет.
//
// Минимальный размер: 28 байт (из forward_mtproto_packet в mtproto-proxy.c):
//   header[7] = 28 байт = auth_key_id(8) + msg_key_or_msg_id(16) + msg_len(4)
//
// Для нешифрованного пакета:
//   [8B auth_key_id=0][8B msg_id][4B msg_len][4B inner_len][4B function_id]...
//
// Для зашифрованного:
//   [8B auth_key_id!=0][16B msg_key][...]
func ParseMTProtoPacket(data []byte) (*MTProtoPacket, error) {
	if len(data) < 28 {
		return nil, errors.New("mtproto: packet too short")
	}
	if len(data)&3 != 0 {
		return nil, errors.New("mtproto: packet length not aligned to 4")
	}

	authKeyID := int64(binary.LittleEndian.Uint64(data[0:8]))

	pkt := &MTProtoPacket{
		AuthKeyID: authKeyID,
		Data:      data,
	}

	if authKeyID != 0 {
		pkt.Type = PacketEncrypted
		return pkt, nil
	}

	// Нешифрованный пакет — DH-рукопожатие
	// Структура: auth_key_id(8) + msg_id(8) + msg_len(4) + inner_len(4) + function(4) + ...
	if len(data) < 28 {
		return nil, errors.New("mtproto: unencrypted packet too short")
	}

	innerLen := int(binary.LittleEndian.Uint32(data[20:24]))
	if innerLen+20 > len(data) {
		return nil, errors.New("mtproto: bad inner length")
	}
	if innerLen < 20 {
		return nil, errors.New("mtproto: inner too short for DH")
	}

	function := binary.LittleEndian.Uint32(data[24:28])
	if !IsDHFunction(function) {
		return nil, errors.New("mtproto: unknown DH function")
	}

	pkt.Type = PacketUnencrypted
	pkt.DHFunction = function
	return pkt, nil
}

// IsDHFunction возвращает true если code является одним из DH-кодов MTProto.
func IsDHFunction(code uint32) bool {
	switch code {
	case CodeReqPQ, CodeReqPQMulti, CodeReqDHParams, CodeSetClientDH:
		return true
	}
	return false
}

// IsEncrypted возвращает true если пакет зашифрован (auth_key_id != 0).
func IsEncrypted(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	return binary.LittleEndian.Uint64(data[0:8]) != 0
}

// EncryptedMessage представляет структуру зашифрованного MTProto-сообщения.
//
// Соответствует struct encrypted_message из mtproto-common.h.
type EncryptedMessage struct {
	AuthKeyID  int64
	MsgKey     [16]byte
	ServerSalt int64
	SessionID  int64
	MsgID      int64
	SeqNo      int32
	MsgLen     int32
	Message    []byte
}

// ParseEncryptedHeader разбирает только незашифрованный заголовок (auth_key_id + msg_key).
func ParseEncryptedHeader(data []byte) (authKeyID int64, msgKey [16]byte, err error) {
	if len(data) < 24 {
		err = errors.New("encrypted header: too short")
		return
	}
	authKeyID = int64(binary.LittleEndian.Uint64(data[0:8]))
	copy(msgKey[:], data[8:24])
	return
}
