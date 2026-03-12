package proxy

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/nicholasgasior/mtproxy/internal/protocol"
)

// DataPlane обрабатывает MTProto-пакеты от клиентов.
// Соответствует forward_mtproto_packet() из mtproto-proxy.c.
type DataPlane struct {
	router   *Router
	outbound *OutboundProxy
	stats    *Stats
	proxyTag []byte // 16 байт или nil
}

// NewDataPlane создаёт DataPlane.
func NewDataPlane(router *Router, outbound *OutboundProxy, stats *Stats, proxyTag []byte) *DataPlane {
	return &DataPlane{
		router:   router,
		outbound: outbound,
		stats:    stats,
		proxyTag: proxyTag,
	}
}

// HandlePacket классифицирует и перенаправляет MTProto-пакет к целевому DC.
//
// Соответствует forward_mtproto_packet() из mtproto-proxy.c:
//   auth_key_id (первые 8 байт) == 0 → DH handshake, flags = FlagDH
//   auth_key_id != 0              → зашифрованный пакет, flags = FlagExtNode
func (dp *DataPlane) HandlePacket(pkt IncomingPacket) error {
	data := pkt.Data
	if len(data) < 28 || len(data)&3 != 0 {
		dp.stats.IncDroppedQuery()
		return fmt.Errorf("dataplane: packet too short or unaligned: %d bytes", len(data))
	}

	authKeyID := int64(binary.LittleEndian.Uint64(data[0:8]))

	var flags uint32
	if authKeyID == 0 {
		if err := validateDHPacket(data); err != nil {
			dp.stats.IncDroppedQuery()
			return fmt.Errorf("dataplane: invalid DH packet: %w", err)
		}
		flags = protocol.FlagDH // 0x2
	} else {
		flags = protocol.FlagExtNode // 0x1000
	}

	if len(dp.proxyTag) == 16 {
		flags |= protocol.FlagProxyTag // 0x8
	}

	target, err := dp.router.Route(int(pkt.TargetDC))
	if err != nil {
		dp.stats.IncDroppedQuery()
		return fmt.Errorf("dataplane: route dc=%d: %w", pkt.TargetDC, err)
	}

	remoteIPv6 := ipToIPv6Wire(pkt.ClientIP)

	req := protocol.BuildProxyReq(
		flags,
		0, // ext_conn_id assigned by ingress
		remoteIPv6,
		uint32(pkt.ClientPort),
		[16]byte{},
		0,
		dp.proxyTag,
		data,
	)

	resp, err := dp.outbound.ForwardPacket(target.Addr, req)
	if err != nil {
		dp.stats.IncDroppedQuery()
		return fmt.Errorf("dataplane: forward to %s: %w", target.Addr, err)
	}

	dp.stats.IncForwardedQuery()
	dp.stats.AddBytesIn(int64(len(data)))
	dp.stats.AddBytesOut(int64(len(resp)))

	return nil
}

// validateDHPacket проверяет, что нешифрованный пакет является допустимым DH-запросом.
func validateDHPacket(data []byte) error {
	if len(data) < 24 {
		return fmt.Errorf("too short for DH packet")
	}
	innerLen := binary.LittleEndian.Uint32(data[16:20])
	if int(innerLen)+20 > len(data) {
		return fmt.Errorf("bad inner length: %d (packet %d)", innerLen, len(data))
	}
	if innerLen < 20 {
		return fmt.Errorf("inner length too small: %d", innerLen)
	}

	function := binary.LittleEndian.Uint32(data[20:24])
	switch function {
	case protocol.CodeReqPQ,
		protocol.CodeReqPQMulti,
		protocol.CodeReqDHParams,
		protocol.CodeSetClientDH:
		return nil
	}
	return fmt.Errorf("unknown DH function: 0x%08x", function)
}

// ipToIPv6Wire конвертирует net.IP в 16-байтный wire-формат.
// IPv4 адреса кодируются как IPv4-mapped IPv6.
func ipToIPv6Wire(ip net.IP) [16]byte {
	var result [16]byte
	if ip == nil {
		return result
	}
	if ip4 := ip.To4(); ip4 != nil {
		result[10] = 0xFF
		result[11] = 0xFF
		copy(result[12:16], ip4)
	} else if ip6 := ip.To16(); ip6 != nil {
		copy(result[:], ip6)
	}
	return result
}
