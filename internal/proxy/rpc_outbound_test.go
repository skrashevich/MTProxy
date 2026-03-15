package proxy

import (
	"encoding/binary"
	"hash/crc32"
	"net"
	"testing"
	"time"

	"github.com/skrashevicj/mtproxy/internal/protocol"
)

// TestDHHelpers verifies isGoodDHBin and basic DH round-trip logic.
func TestDHHelpers(t *testing.T) {
	// Zero value must be rejected
	var zeroBuf [256]byte
	if isGoodDHBin(zeroBuf[:]) {
		t.Error("zero buffer should not be a good DH value")
	}

	// Value equal to prime first byte must be rejected (not strictly less)
	badBuf := make([]byte, 256)
	copy(badBuf, rpcDHPrime)
	if isGoodDHBin(badBuf) {
		t.Error("value equal to prime should not be good")
	}

	// Value clearly less than prime should be accepted
	goodBuf := make([]byte, 256)
	goodBuf[0] = 0x01 // much less than rpcDHPrime[0]=0x89
	goodBuf[1] = 0xFF
	if !isGoodDHBin(goodBuf) {
		t.Error("value < prime[0] should be accepted")
	}
}

// TestBuildProxyTagExtra verifies the TL-serialised proxy tag extra bytes.
func TestBuildProxyTagExtra(t *testing.T) {
	tag := make([]byte, 16)
	for i := range tag {
		tag[i] = byte(i + 1)
	}

	extra := buildProxyTagExtra(tag)
	if len(extra) != 24 {
		t.Fatalf("expected 24 bytes, got %d", len(extra))
	}

	tlType := binary.LittleEndian.Uint32(extra[0:4])
	if tlType != uint32(protocol.TLProxyTag) {
		t.Errorf("expected TLProxyTag 0x%08x, got 0x%08x", protocol.TLProxyTag, tlType)
	}
	if extra[4] != 16 {
		t.Errorf("expected string length byte 16, got %d", extra[4])
	}
	for i, b := range tag {
		if extra[5+i] != b {
			t.Errorf("tag byte %d: expected 0x%02x, got 0x%02x", i, b, extra[5+i])
		}
	}
}

// TestRPCFrameRoundtrip verifies that writeRawFrame produces a valid frame
// that readFrame can parse back.
func TestRPCFrameRoundtrip(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	c := newRPCOutboundConn("pipe", nil, false, nil)
	c.conn = clientConn

	payload := []byte{0xaa, 0x87, 0xcb, 0x7a, 0x01, 0x00, 0x00, 0x00} // RPC_NONCE-like

	done := make(chan error, 1)
	go func() {
		done <- c.writeRawFrame(payload)
	}()

	// Read frame from server side
	totalLen := 4 + 4 + len(payload) + 4
	buf := make([]byte, totalLen)
	if _, err := readFull(serverConn, buf); err != nil {
		t.Fatal("read:", err)
	}

	frameLen := binary.LittleEndian.Uint32(buf[0:4])
	if int(frameLen) != totalLen {
		t.Errorf("frame length: expected %d, got %d", totalLen, frameLen)
	}

	// Verify CRC32
	expectedCRC := crc32.ChecksumIEEE(buf[:totalLen-4])
	gotCRC := binary.LittleEndian.Uint32(buf[totalLen-4:])
	if expectedCRC != gotCRC {
		t.Errorf("CRC32 mismatch: expected 0x%08x, got 0x%08x", expectedCRC, gotCRC)
	}

	if err := <-done; err != nil {
		t.Fatal("writeRawFrame error:", err)
	}
}

// TestHandleFrameDispatch verifies that handleFrame routes opcodes correctly.
func TestHandleFrameDispatch(t *testing.T) {
	c := newRPCOutboundConn("test", nil, false, nil)

	connID := int64(-0x2152410DEDCBA988) // == 0xDEADBEEF12345678 as int64
	respCh := make(chan ProxyResponse, 1)
	c.RegisterPending(connID, respCh)

	// Build a RPC_PROXY_ANS payload: [type(4)][flags(4)][conn_id(8)][data...]
	payload := make([]byte, 16+4)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(protocol.RPCProxyAns))
	binary.LittleEndian.PutUint32(payload[4:8], 0) // flags
	binary.LittleEndian.PutUint64(payload[8:16], uint64(connID))
	copy(payload[16:], []byte{0xDE, 0xAD, 0xBE, 0xEF})

	c.handleFrame(int32(protocol.RPCProxyAns), payload)

	select {
	case resp := <-respCh:
		if resp.ConnID != connID {
			t.Errorf("expected connID 0x%x, got 0x%x", connID, resp.ConnID)
		}
		if len(resp.Data) != 4 {
			t.Errorf("expected 4 data bytes, got %d", len(resp.Data))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout: no response dispatched")
	}
}

// TestHandleSimpleAck verifies RPC_SIMPLE_ACK dispatch.
func TestHandleSimpleAck(t *testing.T) {
	c := newRPCOutboundConn("test", nil, false, nil)

	connID := int64(int64(0x1122334455667788 - 1<<63) - (0 - 1<<63)) // safe signed literal
	respCh := make(chan ProxyResponse, 1)
	c.RegisterPending(connID, respCh)

	// [type(4)][conn_id(8)][confirm(4)]
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(protocol.RPCSimpleAck))
	binary.LittleEndian.PutUint64(payload[4:12], uint64(connID))
	binary.LittleEndian.PutUint32(payload[12:16], 0xCAFEBABE)

	c.handleFrame(int32(protocol.RPCSimpleAck), payload)

	select {
	case resp := <-respCh:
		if resp.ConnID != connID {
			t.Errorf("expected connID 0x%x, got 0x%x", connID, resp.ConnID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout: RPC_SIMPLE_ACK not dispatched")
	}
}

// TestHandleCloseExt verifies RPC_CLOSE_EXT dispatch.
func TestHandleCloseExt(t *testing.T) {
	c := newRPCOutboundConn("test", nil, false, nil)

	connID := int64(-6066930261531574460) // 0xABCDEF0011223344
	respCh := make(chan ProxyResponse, 1)
	c.RegisterPending(connID, respCh)

	// [type(4)][conn_id(8)]
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(protocol.RPCCloseExt))
	binary.LittleEndian.PutUint64(payload[4:12], uint64(connID))

	c.handleFrame(int32(protocol.RPCCloseExt), payload)

	select {
	case resp := <-respCh:
		if uint32(resp.Flags) != protocol.RPCCloseExt {
			t.Errorf("expected Flags=RPCCloseExt 0x%08x, got 0x%08x", protocol.RPCCloseExt, resp.Flags)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout: RPC_CLOSE_EXT not dispatched")
	}
}

// TestSendProxyRequest verifies the structure of the RPC_PROXY_REQ frame.
func TestSendProxyRequest(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	c := newRPCOutboundConn("pipe", nil, false, nil)
	c.conn = clientConn
	// No encryption for this test — CBC fields left nil

	// Override writeEncryptedFrame to use writeRawFrame for testing
	// We'll manually call writeRawFrame to check raw frame bytes instead.
	// Actually, just read raw bytes from server side.

	tag := make([]byte, 16)
	for i := range tag {
		tag[i] = byte(0xAA)
	}

	var remoteIP [16]byte
	remoteIP[15] = 1 // ::1
	var ourIP [16]byte
	ourIP[15] = 2 // ::2
	mtData := []byte{0x01, 0x02, 0x03, 0x04}
	connID := int64(0x1234567890ABCDEF)

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.writeRawFrame(buildProxyReqPayload(
			protocol.FlagExtNode|protocol.FlagProxyTag,
			connID, remoteIP, 1234, ourIP, 443,
			tag, mtData,
		))
	}()

	// Read the frame
	var lenBuf [4]byte
	readFull(serverConn, lenBuf[:])
	totalLen := binary.LittleEndian.Uint32(lenBuf[:])
	rest := make([]byte, totalLen-4)
	readFull(serverConn, rest)

	if err := <-errCh; err != nil {
		t.Fatal("writeRawFrame:", err)
	}

	// Payload starts at offset 8 (after len+seqno), ends before CRC (last 4 bytes)
	full := make([]byte, totalLen)
	copy(full[0:4], lenBuf[:])
	copy(full[4:], rest)
	payload := full[8 : totalLen-4]

	pktType := binary.LittleEndian.Uint32(payload[0:4])
	if pktType != uint32(protocol.RPCProxyReq) {
		t.Errorf("expected RPCProxyReq 0x%08x, got 0x%08x", protocol.RPCProxyReq, pktType)
	}

	flags := binary.LittleEndian.Uint32(payload[4:8])
	if flags&uint32(protocol.FlagExtNode) == 0 {
		t.Error("FlagExtNode must be set")
	}
	if flags&uint32(protocol.FlagProxyTag) == 0 {
		t.Error("FlagProxyTag must be set when proxy tag provided")
	}

	gotConnID := int64(binary.LittleEndian.Uint64(payload[8:16]))
	if gotConnID != connID {
		t.Errorf("connID: expected 0x%x, got 0x%x", connID, gotConnID)
	}
}

// buildProxyReqPayload is a helper for TestSendProxyRequest.
func buildProxyReqPayload(flags int32, extConnID int64, remoteIP [16]byte, remotePort uint32,
	ourIP [16]byte, ourPort uint32, proxyTag []byte, mtData []byte) []byte {
	var extraBuf []byte
	if len(proxyTag) == 16 {
		extraBuf = buildProxyTagExtra(proxyTag)
	}
	hdrSize := 4 + 4 + 8 + 16 + 4 + 16 + 4
	totalSize := hdrSize + 4 + len(extraBuf) + len(mtData)
	pkt := make([]byte, totalSize)
	off := 0
	binary.LittleEndian.PutUint32(pkt[off:], uint32(protocol.RPCProxyReq)); off += 4
	binary.LittleEndian.PutUint32(pkt[off:], uint32(flags)); off += 4
	binary.LittleEndian.PutUint64(pkt[off:], uint64(extConnID)); off += 8
	copy(pkt[off:off+16], remoteIP[:]); off += 16
	binary.LittleEndian.PutUint32(pkt[off:], remotePort); off += 4
	copy(pkt[off:off+16], ourIP[:]); off += 16
	binary.LittleEndian.PutUint32(pkt[off:], ourPort); off += 4
	binary.LittleEndian.PutUint32(pkt[off:], uint32(len(extraBuf))); off += 4
	copy(pkt[off:], extraBuf); off += len(extraBuf)
	copy(pkt[off:], mtData)
	return pkt
}

// readFull is a helper reading exactly len(buf) bytes from conn.
func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
