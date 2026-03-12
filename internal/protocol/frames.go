package protocol

import (
	"encoding/binary"
	"errors"
)

// WriteTLInt записывает 32-битное целое в little-endian в буфер TL.
func WriteTLInt(buf []byte, v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return append(buf, b[:]...)
}

// WriteTLLong записывает 64-битное целое в little-endian в буфер TL.
func WriteTLLong(buf []byte, v uint64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return append(buf, b[:]...)
}

// WriteTLString сериализует байтовый срез как TL-строку с выравниванием по 4 байта.
//
// Формат:
//   - len < 254:  [1B len][data][padding до кратного 4]
//   - len >= 254: [0xFE][3B len LE][data][padding до кратного 4]
func WriteTLString(data []byte) []byte {
	n := len(data)
	var buf []byte

	if n < 254 {
		buf = append(buf, byte(n))
		buf = append(buf, data...)
		// Выравнивание: (1 + n) должно быть кратно 4
		padded := 1 + n
		if rem := padded & 3; rem != 0 {
			buf = append(buf, make([]byte, 4-rem)...)
		}
	} else {
		// [0xFE][3 байта длины в LE]
		buf = append(buf, 0xfe,
			byte(n),
			byte(n>>8),
			byte(n>>16),
		)
		buf = append(buf, data...)
		// Выравнивание: (4 + n) должно быть кратно 4
		padded := 4 + n
		if rem := padded & 3; rem != 0 {
			buf = append(buf, make([]byte, 4-rem)...)
		}
	}
	return buf
}

// BuildProxyReq формирует пакет RPC_PROXY_REQ бинарно-идентичный C-коду.
//
// Структура пакета (из forward_tcp_query в mtproto-proxy.c):
//   [4B RPC_PROXY_REQ][4B flags][8B ext_conn_id]
//   [16B remote_ipv6][4B remote_port]
//   [16B our_ipv6][4B our_port]
//   [4B extra_bytes_size][extra_bytes...]  (только если flags & 0xC != 0)
//   [data...]
//
// IPv4-mapped формат для remote/our IP:
//   [8B нули][4B 0xFFFF0000 (= -0x10000 как int32)][4B IPv4 в big-endian][4B port в little-endian]
//
// proxyTag — 16 байт proxy-тега (nil если не задан). Если задан, flags должен содержать FlagProxyTag.
func BuildProxyReq(flags uint32, extConnID int64, remoteIP [16]byte, remotePort uint32, ourIP [16]byte, ourPort uint32, proxyTag []byte, data []byte) []byte {
	buf := make([]byte, 0, 64+len(data)+32)

	buf = WriteTLInt(buf, RPCProxyReq)
	buf = WriteTLInt(buf, flags)
	buf = WriteTLLong(buf, uint64(extConnID))

	// remote IP + port (20 байт = 16 + 4)
	buf = append(buf, remoteIP[:]...)
	buf = WriteTLInt(buf, remotePort)

	// our IP + port (20 байт = 16 + 4)
	buf = append(buf, ourIP[:]...)
	buf = WriteTLInt(buf, ourPort)

	// extra bytes (только если есть proxy_tag или HTTP-данные)
	if flags&0xC != 0 {
		// Собираем extra bytes
		var extra []byte
		if flags&FlagProxyTag != 0 && len(proxyTag) == 16 {
			extra = WriteTLInt(extra, TLProxyTag)
			extra = append(extra, WriteTLString(proxyTag)...)
		}
		// Записываем размер extra bytes, затем сами bytes
		buf = WriteTLInt(buf, uint32(len(extra)))
		buf = append(buf, extra...)
	}

	buf = append(buf, data...)
	return buf
}

// MakeIPv4Mapped формирует 16-байтный IPv4-mapped адрес из 4-байтного IPv4 (big-endian).
//
// Формат: [8B нули][4B 0xFFFF0000][4B IPv4 BE]
// Соответствует C-коду:
//   tl_store_long(0);
//   tl_store_int(-0x10000);  // = 0xFFFF0000
//   tl_store_int(htonl(ip));
func MakeIPv4Mapped(ipv4BE uint32) [16]byte {
	var addr [16]byte
	// bytes 0-7: нули (уже нули)
	// bytes 8-11: 0xFFFF0000 в little-endian = {0x00, 0x00, 0xFF, 0xFF}
	binary.LittleEndian.PutUint32(addr[8:12], 0xFFFF0000)
	// bytes 12-15: IPv4 в big-endian
	binary.BigEndian.PutUint32(addr[12:16], ipv4BE)
	return addr
}

// ParseProxyAns разбирает пакет RPC_PROXY_ANS.
//
// Структура (из mtproto-common.h):
//   [4B RPC_PROXY_ANS][4B flags][8B ext_conn_id][data...]
func ParseProxyAns(data []byte) (connID int64, ansFlags uint32, payload []byte, err error) {
	// Минимум: type(4) + flags(4) + ext_conn_id(8) = 16 байт
	if len(data) < 16 {
		err = errors.New("proxy ans: too short")
		return
	}

	msgType := binary.LittleEndian.Uint32(data[0:4])
	if msgType != RPCProxyAns {
		err = errors.New("proxy ans: wrong type")
		return
	}

	ansFlags = binary.LittleEndian.Uint32(data[4:8])
	connID = int64(binary.LittleEndian.Uint64(data[8:16]))
	payload = data[16:]
	return
}

// ParseCloseConn разбирает пакет RPC_CLOSE_CONN.
//
// Структура: [4B RPC_CLOSE_CONN][8B ext_conn_id]
func ParseCloseConn(data []byte) (connID int64, err error) {
	if len(data) < 12 {
		err = errors.New("close conn: too short")
		return
	}
	msgType := binary.LittleEndian.Uint32(data[0:4])
	if msgType != RPCCloseConn {
		err = errors.New("close conn: wrong type")
		return
	}
	connID = int64(binary.LittleEndian.Uint64(data[4:12]))
	return
}

// ParseSimpleAck разбирает пакет RPC_SIMPLE_ACK.
//
// Структура: [4B RPC_SIMPLE_ACK][8B ext_conn_id][4B confirm_key]
func ParseSimpleAck(data []byte) (connID int64, confirmKey uint32, err error) {
	if len(data) < 16 {
		err = errors.New("simple ack: too short")
		return
	}
	msgType := binary.LittleEndian.Uint32(data[0:4])
	if msgType != RPCSimpleAck {
		err = errors.New("simple ack: wrong type")
		return
	}
	connID = int64(binary.LittleEndian.Uint64(data[4:12]))
	confirmKey = binary.LittleEndian.Uint32(data[12:16])
	return
}
