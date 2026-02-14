package protocol_test

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/TelegramMessenger/MTProxy/internal/protocol"
)

// This harness compares Go parser output against C forward_mtproto_packet rules:
// mtproto/mtproto-proxy.c:1917..1940.
func TestForwardMTProtoTraceParityWithCLogic(t *testing.T) {
	traces := []struct {
		name  string
		frame []byte
	}{
		{name: "reject_short_24", frame: make([]byte, 24)},
		{name: "reject_misaligned_30", frame: make([]byte, 30)},
		{name: "accept_handshake_req_pq", frame: makeHandshakeFrame(40, 20, protocol.CodeReqPQ)},
		{name: "accept_handshake_req_dh_params", frame: makeHandshakeFrame(40, 20, protocol.CodeReqDHParams)},
		{name: "accept_handshake_extra_tail", frame: makeHandshakeFrame(44, 20, protocol.CodeReqPQMulti)},
		{name: "reject_handshake_inner_too_big", frame: makeHandshakeFrame(40, 64, protocol.CodeReqPQ)},
		{name: "reject_handshake_inner_too_small", frame: makeHandshakeFrame(40, 16, protocol.CodeReqPQ)},
		{name: "reject_handshake_unknown_fn", frame: makeHandshakeFrame(40, 20, 0x12345678)},
		{name: "reject_handshake_negative_inner", frame: makeHandshakeFrame(40, -1, protocol.CodeReqPQ)},
		{name: "accept_encrypted_min_56", frame: makeEncryptedFrame(56, 0x1122334455667788)},
		{name: "reject_encrypted_short_52", frame: makeEncryptedFrame(52, 0x1122334455667788)},
		{name: "reject_encrypted_misaligned_58", frame: makeEncryptedFrame(58, 0x1122334455667788)},
	}

	for _, tc := range traces {
		t.Run(tc.name, func(t *testing.T) {
			want, wantErr := classifyByCForwardRules(tc.frame)
			got, gotErr := protocol.ParseMTProtoPacket(tc.frame)

			if (wantErr != nil) != (gotErr != nil) {
				t.Fatalf(
					"parity mismatch: c_err=%v go_err=%v frame_len=%d",
					wantErr,
					gotErr,
					len(tc.frame),
				)
			}
			if wantErr != nil {
				return
			}

			if got.Kind != want.Kind {
				t.Fatalf("kind mismatch: got=%v want=%v", got.Kind, want.Kind)
			}
			if got.AuthKeyID != want.AuthKeyID {
				t.Fatalf("auth key mismatch: got=%016x want=%016x", got.AuthKeyID, want.AuthKeyID)
			}
			if got.InnerLength != want.InnerLength {
				t.Fatalf("inner length mismatch: got=%d want=%d", got.InnerLength, want.InnerLength)
			}
			if got.Function != want.Function {
				t.Fatalf("function mismatch: got=0x%08x want=0x%08x", got.Function, want.Function)
			}
			if got.Length != want.Length {
				t.Fatalf("length mismatch: got=%d want=%d", got.Length, want.Length)
			}
		})
	}
}

func classifyByCForwardRules(frame []byte) (protocol.PacketInfo, error) {
	// C: if (len < sizeof(header) || (len & 3)) return 0; where sizeof(header)=28.
	if len(frame) < 28 || (len(frame)&3) != 0 {
		return protocol.PacketInfo{}, fmt.Errorf("c reject: invalid frame length")
	}

	authKeyID := binary.LittleEndian.Uint64(frame[:8])
	if authKeyID != 0 {
		// C: forward_mtproto_enc_packet() rejects if len < offsetof(struct encrypted_message, message).
		// In packed MTProxy layout this offset is 56 bytes.
		if len(frame) < 56 {
			return protocol.PacketInfo{}, fmt.Errorf("c reject: encrypted frame too short")
		}
		return protocol.PacketInfo{
			Kind:      protocol.PacketKindEncrypted,
			AuthKeyID: authKeyID,
			Length:    len(frame),
		}, nil
	}

	innerLen := int32(binary.LittleEndian.Uint32(frame[16:20]))
	// C: if (inner_len + 20 > len) return 0; if (inner_len < 20) return 0.
	if int(innerLen)+20 > len(frame) {
		return protocol.PacketInfo{}, fmt.Errorf("c reject: bad inner len")
	}
	if innerLen < 20 {
		return protocol.PacketInfo{}, fmt.Errorf("c reject: inner len too small")
	}

	function := binary.LittleEndian.Uint32(frame[20:24])
	if !protocol.IsDHHandshakeFunction(function) {
		return protocol.PacketInfo{}, fmt.Errorf("c reject: unexpected handshake function")
	}

	return protocol.PacketInfo{
		Kind:        protocol.PacketKindDHHandshake,
		InnerLength: innerLen,
		Function:    function,
		Length:      len(frame),
	}, nil
}

func makeHandshakeFrame(totalLen int, innerLen int32, function uint32) []byte {
	frame := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(frame[16:20], uint32(innerLen))
	binary.LittleEndian.PutUint32(frame[20:24], function)
	return frame
}

func makeEncryptedFrame(totalLen int, authKeyID uint64) []byte {
	frame := make([]byte, totalLen)
	if totalLen >= 8 {
		binary.LittleEndian.PutUint64(frame[:8], authKeyID)
	}
	return frame
}
