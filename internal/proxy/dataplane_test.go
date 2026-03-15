package proxy

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/skrashevicj/mtproxy/internal/config"
	"github.com/skrashevicj/mtproxy/internal/protocol"
)

func makeDHPacketDP() []byte {
	buf := make([]byte, 48)
	binary.LittleEndian.PutUint64(buf[8:16], 12345678)
	binary.LittleEndian.PutUint32(buf[16:20], 28)
	binary.LittleEndian.PutUint32(buf[20:24], protocol.CodeReqPQ)
	return buf
}

func makeEncPacketDP() []byte {
	buf := make([]byte, 48)
	binary.LittleEndian.PutUint64(buf[0:8], 0x00ADBEEF12345678)
	return buf
}

func makeTestRouterDP() *Router {
	return NewRouter(&config.Config{
		DefaultClusterID: 2,
		Clusters: map[int]*config.Cluster{
			2: {ID: 2, Targets: []config.Target{{Addr: "127.0.0.1", Port: 18888}}},
		},
	})
}

func makeTestDP(proxyTag []byte) *DataPlane {
	out := NewOutboundProxy(OutboundConfig{})
	return NewDataPlane(makeTestRouterDP(), out, NewStats(), proxyTag)
}

func makeIncomingDP(data []byte, dc int16) IncomingPacket {
	return IncomingPacket{
		Data:       data,
		ClientIP:   net.ParseIP("127.0.0.1"),
		ClientPort: 12345,
		TargetDC:   dc,
	}
}

func TestDataPlane_HandlePacket_TooShort(t *testing.T) {
	dp := makeTestDP(nil)
	_, err := dp.HandlePacket(makeIncomingDP([]byte{1, 2, 3}, 2))
	if err == nil {
		t.Error("expected error for too-short packet")
	}
}

func TestDataPlane_HandlePacket_UnalignedSize(t *testing.T) {
	dp := makeTestDP(nil)
	_, err := dp.HandlePacket(makeIncomingDP(make([]byte, 29), 2))
	if err == nil {
		t.Error("expected error for unaligned packet")
	}
}

func TestDataPlane_HandlePacket_BadDHFunction(t *testing.T) {
	dp := makeTestDP(nil)
	buf := make([]byte, 48)
	binary.LittleEndian.PutUint32(buf[16:20], 28)
	binary.LittleEndian.PutUint32(buf[20:24], 0x0EADBEEF)
	_, err := dp.HandlePacket(makeIncomingDP(buf, 2))
	if err == nil {
		t.Error("expected error for unknown DH function")
	}
}

func TestDataPlane_DroppedOnShort(t *testing.T) {
	out := NewOutboundProxy(OutboundConfig{})
	stats := NewStats()
	dp := NewDataPlane(makeTestRouterDP(), out, stats, nil)
	dp.HandlePacket(makeIncomingDP([]byte{1, 2}, 2)) //nolint:errcheck
	if stats.DroppedQueries != 1 {
		t.Errorf("DroppedQueries = %d, want 1", stats.DroppedQueries)
	}
}

func TestDataPlane_DHPacketPassesValidation(t *testing.T) {
	dp := makeTestDP(nil)
	_, err := dp.HandlePacket(makeIncomingDP(makeDHPacketDP(), 2))
	// DH пакет валиден — ошибка может быть только про connect к несуществующему серверу
	if err != nil {
		if err.Error() == "dataplane: invalid DH packet: unknown DH function: 0x60469778" {
			t.Errorf("valid DH packet failed validation: %v", err)
		}
	}
}

func TestValidateDHPacket(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{"valid CodeReqPQ", makeDHPacketDP(), false},
		{"too short", make([]byte, 10), true},
		{
			"bad inner length",
			func() []byte {
				b := make([]byte, 48)
				binary.LittleEndian.PutUint32(b[16:20], 100)
				binary.LittleEndian.PutUint32(b[20:24], protocol.CodeReqPQ)
				return b
			}(),
			true,
		},
		{
			"unknown function",
			func() []byte {
				b := make([]byte, 48)
				binary.LittleEndian.PutUint32(b[16:20], 28)
				binary.LittleEndian.PutUint32(b[20:24], 0x12345678)
				return b
			}(),
			true,
		},
		{
			"valid CodeReqDHParams",
			func() []byte {
				b := make([]byte, 48)
				binary.LittleEndian.PutUint32(b[16:20], 28)
				binary.LittleEndian.PutUint32(b[20:24], protocol.CodeReqDHParams)
				return b
			}(),
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDHPacket(tc.data)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateDHPacket() error=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestIPToIPv6Wire(t *testing.T) {
	result := ipToIPv6Wire(net.ParseIP("1.2.3.4"))
	if result[10] != 0xFF || result[11] != 0xFF {
		t.Errorf("IPv4-mapped prefix [10:12]=%v", result[10:12])
	}
	if result[12] != 1 || result[13] != 2 || result[14] != 3 || result[15] != 4 {
		t.Errorf("IPv4 bytes=%v", result[12:16])
	}
}

func TestIPToIPv6Wire_Nil(t *testing.T) {
	if ipToIPv6Wire(nil) != ([16]byte{}) {
		t.Error("nil IP should give zero result")
	}
}
