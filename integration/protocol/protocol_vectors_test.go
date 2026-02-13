package protocol_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/TelegramMessenger/MTProxy/internal/protocol"
)

func TestParseMTProtoPacketHandshake(t *testing.T) {
	frame := make([]byte, 40)
	binary.LittleEndian.PutUint32(frame[16:20], 20)
	binary.LittleEndian.PutUint32(frame[20:24], uint32(protocol.CodeReqPQ))

	info, err := protocol.ParseMTProtoPacket(frame)
	if err != nil {
		t.Fatalf("parse handshake packet: %v", err)
	}
	if info.Kind != protocol.PacketKindDHHandshake {
		t.Fatalf("unexpected kind: %v", info.Kind)
	}
	if info.Function != protocol.CodeReqPQ {
		t.Fatalf("unexpected function: 0x%08x", uint32(info.Function))
	}
}

func TestParseMTProtoPacketEncrypted(t *testing.T) {
	frame := make([]byte, 56)
	binary.LittleEndian.PutUint64(frame[:8], 0x1122334455667788)

	info, err := protocol.ParseMTProtoPacket(frame)
	if err != nil {
		t.Fatalf("parse encrypted packet: %v", err)
	}
	if info.Kind != protocol.PacketKindEncrypted {
		t.Fatalf("unexpected kind: %v", info.Kind)
	}
	if info.AuthKeyID != 0x1122334455667788 {
		t.Fatalf("unexpected auth key id: %016x", info.AuthKeyID)
	}
}

func TestParseMTProtoPacketErrors(t *testing.T) {
	if _, err := protocol.ParseMTProtoPacket(make([]byte, 24)); err == nil {
		t.Fatalf("expected short frame error")
	}

	badInner := make([]byte, 40)
	binary.LittleEndian.PutUint32(badInner[16:20], 64)
	binary.LittleEndian.PutUint32(badInner[20:24], uint32(protocol.CodeReqPQ))
	if _, err := protocol.ParseMTProtoPacket(badInner); err == nil {
		t.Fatalf("expected bad inner length error")
	}

	badFn := make([]byte, 40)
	binary.LittleEndian.PutUint32(badFn[16:20], 20)
	binary.LittleEndian.PutUint32(badFn[20:24], 0x12345678)
	if _, err := protocol.ParseMTProtoPacket(badFn); err == nil {
		t.Fatalf("expected bad function error")
	}
}

func TestSessionStateTransitions(t *testing.T) {
	s := protocol.NewSession()
	if s.State() != protocol.SessionStateInit {
		t.Fatalf("unexpected initial state: %v", s.State())
	}

	handshake := make([]byte, 40)
	binary.LittleEndian.PutUint32(handshake[16:20], 20)
	binary.LittleEndian.PutUint32(handshake[20:24], uint32(protocol.CodeReqDHParams))
	if _, err := s.AcceptPacket(handshake); err != nil {
		t.Fatalf("accept handshake: %v", err)
	}
	if s.State() != protocol.SessionStateHandshake {
		t.Fatalf("expected handshake state, got %v", s.State())
	}

	encrypted := make([]byte, 56)
	binary.LittleEndian.PutUint64(encrypted[:8], 1)
	if _, err := s.AcceptPacket(encrypted); err != nil {
		t.Fatalf("accept encrypted: %v", err)
	}
	if s.State() != protocol.SessionStateEncrypted {
		t.Fatalf("expected encrypted state, got %v", s.State())
	}
}

func TestControlFramesParseAndBuild(t *testing.T) {
	proxyAnsPayload := []byte{1, 2, 3, 4}
	proxyAns := protocol.BuildProxyAns(7, 0x0102030405060708, proxyAnsPayload)
	parsedAns, err := protocol.ParseControlFrame(proxyAns)
	if err != nil {
		t.Fatalf("parse proxy ans: %v", err)
	}
	if parsedAns.Kind != protocol.ControlFrameProxyAns {
		t.Fatalf("unexpected proxy ans kind: %v", parsedAns.Kind)
	}
	if parsedAns.Flags != 7 || parsedAns.OutConnID != 0x0102030405060708 {
		t.Fatalf("unexpected proxy ans fields: %+v", parsedAns)
	}
	if !bytes.Equal(parsedAns.Payload, proxyAnsPayload) {
		t.Fatalf("unexpected proxy ans payload: %x", parsedAns.Payload)
	}

	ack := protocol.BuildSimpleAck(0x0807060504030201, 0x11223344)
	parsedAck, err := protocol.ParseControlFrame(ack)
	if err != nil {
		t.Fatalf("parse simple ack: %v", err)
	}
	if parsedAck.Kind != protocol.ControlFrameSimpleAck {
		t.Fatalf("unexpected simple ack kind: %v", parsedAck.Kind)
	}
	if parsedAck.Confirm != 0x11223344 {
		t.Fatalf("unexpected confirm: %08x", uint32(parsedAck.Confirm))
	}

	closeExt := protocol.BuildCloseExt(0x1112131415161718)
	parsedClose, err := protocol.ParseControlFrame(closeExt)
	if err != nil {
		t.Fatalf("parse close ext: %v", err)
	}
	if parsedClose.Kind != protocol.ControlFrameCloseExt {
		t.Fatalf("unexpected close kind: %v", parsedClose.Kind)
	}
	if parsedClose.OutConnID != 0x1112131415161718 {
		t.Fatalf("unexpected out conn id: %016x", uint64(parsedClose.OutConnID))
	}
}

func TestProxyReqParseAndBuild(t *testing.T) {
	var remote [20]byte
	var our [20]byte
	for i := 0; i < 20; i++ {
		remote[i] = byte(i + 1)
		our[i] = byte(20 - i)
	}

	req := protocol.ProxyRequestFrame{
		Flags:      12,
		ExtConnID:  0x0102030405060708,
		RemoteIP:   remote,
		OurIP:      our,
		ExtraBytes: []byte{0xaa, 0xbb, 0xcc},
		Payload:    []byte("payload"),
	}

	frame := protocol.BuildProxyReq(req)
	parsed, err := protocol.ParseProxyReq(frame)
	if err != nil {
		t.Fatalf("parse proxy req: %v", err)
	}
	if parsed.Flags != req.Flags || parsed.ExtConnID != req.ExtConnID {
		t.Fatalf("parsed req mismatch: %+v", parsed)
	}
	if !bytes.Equal(parsed.RemoteIP[:], req.RemoteIP[:]) {
		t.Fatalf("remote ip mismatch")
	}
	if !bytes.Equal(parsed.OurIP[:], req.OurIP[:]) {
		t.Fatalf("our ip mismatch")
	}
	if !bytes.Equal(parsed.ExtraBytes, req.ExtraBytes) {
		t.Fatalf("extra mismatch: %x vs %x", parsed.ExtraBytes, req.ExtraBytes)
	}
	if !bytes.Equal(parsed.Payload, req.Payload) {
		t.Fatalf("payload mismatch: %q vs %q", string(parsed.Payload), string(req.Payload))
	}
}
