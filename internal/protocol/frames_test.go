package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestWriteTLString_ShortLen проверяет TL-сериализацию строк длиной < 254.
func TestWriteTLString_ShortLen(t *testing.T) {
	// 16-байтный proxy_tag (наиболее частый случай)
	tag := make([]byte, 16)
	for i := range tag {
		tag[i] = byte(i + 1)
	}

	out := WriteTLString(tag)

	// Ожидаемый формат: [1B len=16][16B data][3B padding] = 20 байт
	if len(out) != 20 {
		t.Fatalf("expected 20 bytes, got %d", len(out))
	}
	if out[0] != 16 {
		t.Fatalf("expected len byte 16, got %d", out[0])
	}
	if !bytes.Equal(out[1:17], tag) {
		t.Fatal("data mismatch")
	}
	// Padding должен быть нулями
	if out[17] != 0 || out[18] != 0 || out[19] != 0 {
		t.Fatal("padding not zero")
	}
	// Проверка кратности 4
	if len(out)&3 != 0 {
		t.Fatal("result not aligned to 4")
	}
}

// TestWriteTLString_LongLen проверяет TL-сериализацию строк длиной >= 254.
func TestWriteTLString_LongLen(t *testing.T) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}

	out := WriteTLString(data)

	// Ожидаемый формат: [0xFE][3B len=256 LE][256B data] = 4+256=260 байт, кратно 4
	if len(out) != 260 {
		t.Fatalf("expected 260 bytes, got %d", len(out))
	}
	if out[0] != 0xfe {
		t.Fatalf("expected 0xfe marker, got 0x%02x", out[0])
	}
	// 3 байта длины в LE
	length := int(out[1]) | int(out[2])<<8 | int(out[3])<<16
	if length != 256 {
		t.Fatalf("expected length 256, got %d", length)
	}
	if !bytes.Equal(out[4:260], data) {
		t.Fatal("data mismatch")
	}
	if len(out)&3 != 0 {
		t.Fatal("result not aligned to 4")
	}
}

// TestWriteTLString_EmptyString проверяет пустую строку.
func TestWriteTLString_EmptyString(t *testing.T) {
	out := WriteTLString([]byte{})
	// [1B len=0][3B padding] = 4 байта
	if len(out) != 4 {
		t.Fatalf("expected 4 bytes, got %d", len(out))
	}
	if out[0] != 0 {
		t.Fatal("expected len byte 0")
	}
	if len(out)&3 != 0 {
		t.Fatal("result not aligned to 4")
	}
}

// TestBuildProxyReq_Structure проверяет бинарную структуру пакета RPC_PROXY_REQ.
func TestBuildProxyReq_Structure(t *testing.T) {
	tag := make([]byte, 16)
	for i := range tag {
		tag[i] = byte(i + 0xAA)
	}

	var remoteIP [16]byte
	remoteIP = MakeIPv4Mapped(0xC0A80101) // 192.168.1.1
	var ourIP [16]byte
	ourIP = MakeIPv4Mapped(0x0A000001) // 10.0.0.1

	flags := uint32(FlagExtNode | FlagProxyTag)
	connID := int64(0x1234567890ABCDE)
	remotePort := uint32(1234)
	ourPort := uint32(443)
	data := []byte{0x01, 0x02, 0x03, 0x04}

	pkt := BuildProxyReq(flags, connID, remoteIP, remotePort, ourIP, ourPort, tag, data)

	offset := 0

	// [4B] RPC_PROXY_REQ
	if got := binary.LittleEndian.Uint32(pkt[offset:]); got != RPCProxyReq {
		t.Fatalf("type: expected 0x%08x, got 0x%08x", RPCProxyReq, got)
	}
	offset += 4

	// [4B] flags
	if got := binary.LittleEndian.Uint32(pkt[offset:]); got != flags {
		t.Fatalf("flags: expected 0x%08x, got 0x%08x", flags, got)
	}
	offset += 4

	// [8B] ext_conn_id
	if got := int64(binary.LittleEndian.Uint64(pkt[offset:])); got != connID {
		t.Fatalf("conn_id: expected %x, got %x", connID, got)
	}
	offset += 8

	// [16B] remote_ipv6
	if !bytes.Equal(pkt[offset:offset+16], remoteIP[:]) {
		t.Fatal("remote_ipv6 mismatch")
	}
	offset += 16

	// [4B] remote_port
	if got := binary.LittleEndian.Uint32(pkt[offset:]); got != remotePort {
		t.Fatalf("remote_port: expected %d, got %d", remotePort, got)
	}
	offset += 4

	// [16B] our_ipv6
	if !bytes.Equal(pkt[offset:offset+16], ourIP[:]) {
		t.Fatal("our_ipv6 mismatch")
	}
	offset += 16

	// [4B] our_port
	if got := binary.LittleEndian.Uint32(pkt[offset:]); got != ourPort {
		t.Fatalf("our_port: expected %d, got %d", ourPort, got)
	}
	offset += 4

	// [4B] extra_bytes_size
	extraSize := int(binary.LittleEndian.Uint32(pkt[offset:]))
	offset += 4

	// extra bytes: [4B TL_PROXY_TAG][TL-string(16 bytes)] = 4 + 20 = 24 байта
	if extraSize != 24 {
		t.Fatalf("extra_bytes_size: expected 24, got %d", extraSize)
	}

	// [4B] TL_PROXY_TAG
	if got := binary.LittleEndian.Uint32(pkt[offset:]); got != TLProxyTag {
		t.Fatalf("TL_PROXY_TAG: expected 0x%08x, got 0x%08x", TLProxyTag, got)
	}
	offset += 4

	// TL-string для proxy_tag: [1B len=16][16B data][3B padding]
	if pkt[offset] != 16 {
		t.Fatalf("proxy_tag len byte: expected 16, got %d", pkt[offset])
	}
	offset++
	if !bytes.Equal(pkt[offset:offset+16], tag) {
		t.Fatal("proxy_tag data mismatch")
	}
	offset += 16
	offset += 3 // padding

	// data
	if !bytes.Equal(pkt[offset:], data) {
		t.Fatal("payload data mismatch")
	}
}

// TestBuildProxyReq_NoExtraBytes проверяет пакет без extra bytes.
func TestBuildProxyReq_NoExtraBytes(t *testing.T) {
	var remoteIP, ourIP [16]byte
	flags := uint32(FlagDH)
	pkt := BuildProxyReq(flags, 42, remoteIP, 80, ourIP, 443, nil, []byte{0xAA, 0xBB, 0xCC, 0xDD})

	// Без extra bytes: 4+4+8+16+4+16+4 = 56 байт заголовок + 4 байта данных
	expectedLen := 4 + 4 + 8 + 16 + 4 + 16 + 4 + 4
	if len(pkt) != expectedLen {
		t.Fatalf("expected %d bytes, got %d", expectedLen, len(pkt))
	}
}

// TestMakeIPv4Mapped проверяет формирование IPv4-mapped адреса.
func TestMakeIPv4Mapped(t *testing.T) {
	// 192.168.1.1 = 0xC0A80101
	addr := MakeIPv4Mapped(0xC0A80101)

	// bytes 0-7 нули
	for i := 0; i < 8; i++ {
		if addr[i] != 0 {
			t.Fatalf("byte[%d] should be 0, got %d", i, addr[i])
		}
	}

	// bytes 8-11: 0xFFFF0000 в LE = {0x00, 0x00, 0xFF, 0xFF}
	expected8_11 := []byte{0x00, 0x00, 0xFF, 0xFF}
	if !bytes.Equal(addr[8:12], expected8_11) {
		t.Fatalf("IPv4-mapped marker: expected %v, got %v", expected8_11, addr[8:12])
	}

	// bytes 12-15: IPv4 в big-endian
	expectedIPv4 := []byte{0xC0, 0xA8, 0x01, 0x01}
	if !bytes.Equal(addr[12:16], expectedIPv4) {
		t.Fatalf("IPv4 bytes: expected %v, got %v", expectedIPv4, addr[12:16])
	}
}

// TestParseProxyAns проверяет разбор пакета RPC_PROXY_ANS.
func TestParseProxyAns(t *testing.T) {
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint32(buf[0:4], RPCProxyAns)
	binary.LittleEndian.PutUint32(buf[4:8], RPCProxyAnsFlushNow)
	const testConnID = uint64(0xDEADBEEFCAFEBABE)
	binary.LittleEndian.PutUint64(buf[8:16], testConnID)
	buf[16] = 0xAA
	buf[17] = 0xBB
	buf[18] = 0xCC
	buf[19] = 0xDD

	connID, flags, payload, err := ParseProxyAns(buf)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(connID) != testConnID {
		t.Fatalf("connID mismatch: got 0x%x", connID)
	}
	if flags != RPCProxyAnsFlushNow {
		t.Fatalf("flags mismatch: got 0x%x", flags)
	}
	if !bytes.Equal(payload, []byte{0xAA, 0xBB, 0xCC, 0xDD}) {
		t.Fatal("payload mismatch")
	}
}

// TestParseProxyAns_WrongType проверяет ошибку при неверном типе пакета.
func TestParseProxyAns_WrongType(t *testing.T) {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], 0xDEADBEEF) // неверный тип

	_, _, _, err := ParseProxyAns(buf)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

// TestProxyTagExtraBytes проверяет, что proxy_tag extra bytes = 24 байта.
// [4B TL_PROXY_TAG][1B len=16][16B data][3B pad] = 4+1+16+3 = 24
func TestProxyTagExtraBytes(t *testing.T) {
	tag := make([]byte, 16)
	extra := WriteTLInt(nil, TLProxyTag)
	extra = append(extra, WriteTLString(tag)...)
	if len(extra) != ProxyTagExtraBytes {
		t.Fatalf("proxy_tag extra bytes: expected %d, got %d", ProxyTagExtraBytes, len(extra))
	}
}
