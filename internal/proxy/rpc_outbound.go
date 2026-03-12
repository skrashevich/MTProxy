// Package proxy implements the outbound RPC client to Telegram DC servers.
// Ported from C sources: net/net-tcp-rpc-client.c, net/net-crypto-dh.c,
// mtproto/mtproto-proxy.c.
package proxy

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nicholasgasior/mtproxy/internal/crypto"
	"github.com/nicholasgasior/mtproxy/internal/protocol"
)

// RPC nonce/handshake packet types (from net/net-tcp-rpc-common.h)
const (
	rpcNonce        = 0x7acb87aa
	rpcHandshake    = 0x7682eef5

	rpccCryptoNone  = 0
	rpccCryptoAES   = 1
	rpccCryptoAESDH = 3

	// DH prime hash (RPC_PARAM_HASH from net/net-crypto-dh.c)
	rpcDHParamsSelect = 0x00620b93

	pingInterval = 5 * time.Second
)

// rpcDHPrime is the 2048-bit safe prime used for DH key exchange.
// From net/net-crypto-dh.c: rpc_dh_prime_bin[256].
var rpcDHPrime = []byte{
	0x89, 0x52, 0x13, 0x1b, 0x1e, 0x3a, 0x69, 0xba, 0x5f, 0x85, 0xcf, 0x8b, 0xd2, 0x66, 0xc1, 0x2b,
	0x13, 0x83, 0x16, 0x13, 0xbd, 0x2a, 0x4e, 0xf8, 0x35, 0xa4, 0xd5, 0x3f, 0x9d, 0xbb, 0x42, 0x48,
	0x2d, 0xbd, 0x46, 0x2b, 0x31, 0xd8, 0x6c, 0x81, 0x6c, 0x59, 0x77, 0x52, 0x0f, 0x11, 0x70, 0x73,
	0x9e, 0xd2, 0xdd, 0xd6, 0xd8, 0x1b, 0x9e, 0xb6, 0x5f, 0xaa, 0xac, 0x14, 0x87, 0x53, 0xc9, 0xe4,
	0xf0, 0x72, 0xdc, 0x11, 0xa4, 0x92, 0x73, 0x06, 0x83, 0xfa, 0x00, 0x67, 0x82, 0x6b, 0x18, 0xc5,
	0x1d, 0x7e, 0xcb, 0xa5, 0x2b, 0x82, 0x60, 0x75, 0xc0, 0xb9, 0x55, 0xe5, 0xac, 0xaf, 0xdd, 0x74,
	0xc3, 0x79, 0x5f, 0xd9, 0x52, 0x0b, 0x48, 0x0f, 0x3b, 0xe3, 0xba, 0x06, 0x65, 0x33, 0x8a, 0x49,
	0x8c, 0xa5, 0xda, 0xf1, 0x01, 0x76, 0x05, 0x09, 0xa3, 0x8c, 0x49, 0xe3, 0x00, 0x74, 0x64, 0x08,
	0x77, 0x4b, 0xb3, 0xed, 0x26, 0x18, 0x1a, 0x64, 0x55, 0x76, 0x6a, 0xe9, 0x49, 0x7b, 0xb9, 0xc3,
	0xa3, 0xad, 0x5c, 0xba, 0xf7, 0x6b, 0x73, 0x84, 0x5f, 0xbb, 0x96, 0xbb, 0x6d, 0x0f, 0x68, 0x4f,
	0x95, 0xd2, 0xd3, 0x9c, 0xcb, 0xb4, 0xa9, 0x04, 0xfa, 0xb1, 0xde, 0x43, 0x49, 0xce, 0x1c, 0x20,
	0x87, 0xb6, 0xc9, 0x51, 0xed, 0x99, 0xf9, 0x52, 0xe3, 0x4f, 0xd1, 0xa3, 0xfd, 0x14, 0x83, 0x35,
	0x75, 0x41, 0x47, 0x29, 0xa3, 0x8b, 0xe8, 0x68, 0xa4, 0xf9, 0xec, 0x62, 0x3a, 0x5d, 0x24, 0x62,
	0x1a, 0xba, 0x01, 0xb2, 0x55, 0xc7, 0xe8, 0x38, 0x5d, 0x16, 0xac, 0x93, 0xb0, 0x2d, 0x2a, 0x54,
	0x0a, 0x76, 0x42, 0x98, 0x2d, 0x22, 0xad, 0xa3, 0xcc, 0xde, 0x5c, 0x8d, 0x26, 0x6f, 0xaa, 0x25,
	0xdd, 0x2d, 0xe9, 0xf6, 0xd4, 0x91, 0x04, 0x16, 0x2f, 0x68, 0x5c, 0x45, 0xfe, 0x34, 0xdd, 0xab,
}

// rpcDHGenerator is g=3 from C source.
var rpcDHGenerator = big.NewInt(3)

// ProxyResponse holds a response received from Telegram DC for a given connection.
type ProxyResponse struct {
	Flags     int32
	ConnID    int64
	Data      []byte
}

// rpcOutboundConn represents a single encrypted RPC connection to a Telegram DC.
// It handles the full lifecycle: TCP dial → handshake → encrypted framing → async read.
//
// Corresponds to C tcp_rpcc_* functions in net/net-tcp-rpc-client.c.
type rpcOutboundConn struct {
	addr   string
	secret []byte // AES secret (proxy password)

	conn     net.Conn
	writeMu  sync.Mutex
	outSeqno int32 // atomic; starts at -2 per C protocol

	// AES-256-CBC encrypt/decrypt state (set after handshake).
	// RPC client connections use CBC (not CTR). CTR is only for client-facing ext-server.
	// Matches C: aes_crypto_init → EVP_aes_256_cbc().
	cbcEnc    *crypto.AESCBCEncryptor
	cbcDec    *crypto.AESCBCDecryptor
	cbcReader *cbcDecryptReader // wraps conn with transparent CBC decryption

	// pending response channels keyed by ext_conn_id
	pendingMu sync.Mutex
	pending   map[int64]chan<- ProxyResponse

	// closed signals the read loop to exit
	closed chan struct{}

	// forceDH requests DH handshake (--force-dh flag)
	forceDH bool

	// natInfo maps local IPv4 → public IPv4 for NAT traversal in key derivation
	natInfo map[uint32]uint32
}

// newRPCOutboundConn creates a new unconnected outbound RPC connection.
func newRPCOutboundConn(addr string, secret []byte, forceDH bool, natInfo map[uint32]uint32) *rpcOutboundConn {
	c := &rpcOutboundConn{
		addr:    addr,
		secret:  secret,
		forceDH: forceDH,
		natInfo: natInfo,
		pending: make(map[int64]chan<- ProxyResponse),
		closed:  make(chan struct{}),
	}
	// C protocol: out_packet_num starts at -2 (tcp_rpcc_connected, line 455)
	atomic.StoreInt32(&c.outSeqno, -2)
	return c
}

// Connect dials the target, performs the RPC handshake, and starts the read loop.
func (c *rpcOutboundConn) Connect() error {
	conn, err := net.DialTimeout("tcp", c.addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.addr, err)
	}
	c.conn = conn

	if err := c.handshake(); err != nil {
		conn.Close()
		return fmt.Errorf("handshake with %s: %w", c.addr, err)
	}

	go c.readLoop()
	go c.pingLoop()
	return nil
}

// Close shuts down the connection gracefully.
func (c *rpcOutboundConn) Close() {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	if c.conn != nil {
		c.conn.Close()
	}
}

// handshake performs the full RPC nonce/crypto handshake.
//
// Protocol (from tcp_rpcc_init_crypto and tcp_rpcc_process_nonce_packet in C):
//   Client sends:  RPC_NONCE packet (type=0x7acb87aa, key_select, crypto_schema, ts, nonce[16])
//                  + optional DH g_a[256]           — UNENCRYPTED (seqno -2)
//   Server sends:  RPC_NONCE packet back             — UNENCRYPTED (seqno -2)
//   Both sides derive AES-256-CBC keys from nonces + secret + IPs/ports.
//   Client sends:  RPC_HANDSHAKE packet              — ENCRYPTED with CBC (seqno -1)
//   Server sends:  RPC_HANDSHAKE packet              — ENCRYPTED with CBC (seqno -1)
//   → connection is now fully encrypted with AES-256-CBC
func (c *rpcOutboundConn) handshake() error {
	var clientNonce [16]byte
	if _, err := rand.Read(clientNonce[:]); err != nil {
		return err
	}

	nonceTSSec := uint32(time.Now().Unix())

	// --- send RPC_NONCE (unencrypted) ---
	var dhPriv [256]byte
	var dhPub [256]byte // g^a mod p

	if c.forceDH {
		if err := dhFirstRound(dhPub[:], dhPriv[:]); err != nil {
			return fmt.Errorf("dh first round: %w", err)
		}
		if err := c.sendNonceDH(clientNonce, nonceTSSec, dhPub); err != nil {
			return err
		}
	} else {
		if err := c.sendNonceAES(clientNonce, nonceTSSec); err != nil {
			return err
		}
	}

	// --- read server RPC_NONCE (unencrypted) ---
	pktLen, pktData, err := c.readRawFrame()
	if err != nil {
		return fmt.Errorf("read nonce response: %w", err)
	}
	if pktLen < 32 {
		return fmt.Errorf("nonce packet too short: %d", pktLen)
	}

	pktType := int32(binary.LittleEndian.Uint32(pktData[0:4]))
	if pktType != rpcNonce {
		return fmt.Errorf("expected RPC_NONCE (0x%08x), got 0x%08x", rpcNonce, pktType)
	}

	cryptoSchema := int32(binary.LittleEndian.Uint32(pktData[8:12]))
	serverTS := binary.LittleEndian.Uint32(pktData[12:16])
	var serverNonce [16]byte
	copy(serverNonce[:], pktData[16:32])

	// Validate timestamp drift ≤ 30s (matching C: abs(P.s.crypto_ts - D->nonce_time) > 30)
	diff := int64(serverTS) - int64(nonceTSSec)
	if diff < -30 || diff > 30 {
		return fmt.Errorf("nonce timestamp drift too large: %d seconds", diff)
	}

	var tempKey []byte

	switch cryptoSchema {
	case rpccCryptoAES:
		// Standard AES: derive keys from client nonce + server nonce
	case rpccCryptoAESDH:
		// DH: server sent g_b[256] after the nonce packet fields
		if len(pktData) < 32+4+8*4+4+256 {
			return fmt.Errorf("DH nonce packet too short: %d", len(pktData))
		}
		dhParamsBase := 32 + 4 + 8*4
		serverDHParamsSelect := binary.LittleEndian.Uint32(pktData[dhParamsBase : dhParamsBase+4])
		if serverDHParamsSelect != rpcDHParamsSelect {
			return fmt.Errorf("DH params mismatch: got 0x%08x, expected 0x%08x", serverDHParamsSelect, rpcDHParamsSelect)
		}
		gBOffset := dhParamsBase + 4
		if len(pktData) < gBOffset+256 {
			return fmt.Errorf("DH nonce packet missing g_b: len=%d", len(pktData))
		}
		gB := pktData[gBOffset : gBOffset+256]

		shared, err := dhThirdRound(gB, dhPriv[:])
		if err != nil {
			return fmt.Errorf("DH third round: %w", err)
		}
		tempKey = shared
	default:
		return fmt.Errorf("unsupported crypto schema: %d", cryptoSchema)
	}

	// --- extract actual IPs/ports from the TCP connection ---
	// C uses nat_translate_ip(c->remote_ip), c->remote_port, c->our_ip, c->our_port
	// for key derivation. Both sides must use the same values.
	serverIP, serverPort, serverIPv6 := extractConnAddr(c.conn.RemoteAddr())
	clientIP, clientPort, clientIPv6 := extractConnAddr(c.conn.LocalAddr())
	serverIP = c.natTranslateIP(serverIP)
	clientIP = c.natTranslateIP(clientIP)

	// --- derive AES-256-CBC keys BEFORE sending handshake ---
	// In C: tcp_rpcc_process_nonce_packet calls rpc_start_crypto (sets up AES-CBC),
	// then tcp_rpcc_send_handshake_packet is sent THROUGH the crypto layer.
	aesKeys, err := crypto.AESCreateKeys(
		true,
		serverNonce, clientNonce,
		nonceTSSec,
		serverIP, serverPort, serverIPv6,
		clientIP, clientPort, clientIPv6,
		c.secret,
		tempKey,
	)
	if err != nil {
		return fmt.Errorf("AES key derivation: %w", err)
	}

	// Set up AES-256-CBC (NOT CTR!) for outbound RPC connections.
	// C: aes_crypto_init → evp_cipher_ctx_init(EVP_aes_256_cbc(), ..., padding=0)
	enc, err := crypto.NewAESCBCEncryptor(aesKeys.WriteKey, aesKeys.WriteIV)
	if err != nil {
		return err
	}
	dec, err := crypto.NewAESCBCDecryptor(aesKeys.ReadKey, aesKeys.ReadIV)
	if err != nil {
		return err
	}

	c.cbcEnc = enc
	c.cbcDec = dec
	c.cbcReader = &cbcDecryptReader{r: c.conn, dec: dec}

	// --- send RPC_HANDSHAKE (ENCRYPTED — crypto is now active) ---
	if err := c.sendHandshake(); err != nil {
		return err
	}

	// --- read server RPC_HANDSHAKE (ENCRYPTED) ---
	_, hsData, err := c.readEncryptedFrame()
	if err != nil {
		return fmt.Errorf("read handshake response: %w", err)
	}
	if len(hsData) < 4 {
		return fmt.Errorf("handshake response too short")
	}
	hsType := int32(binary.LittleEndian.Uint32(hsData[0:4]))
	if hsType != rpcHandshake {
		return fmt.Errorf("expected RPC_HANDSHAKE (0x%08x), got 0x%08x", rpcHandshake, hsType)
	}

	log.Printf("rpc_handshake: connected to %s (crypto=CBC, schema=%d)", c.addr, cryptoSchema)
	return nil
}

// extractConnAddr extracts IPv4 address (as uint32), port (as uint16), and IPv6 address
// from a net.Addr. Returns (ipv4, port, ipv6). If the address is IPv4, ipv4 is set and
// ipv6 is zero. If IPv6, ipv4 is 0 and ipv6 is set (triggering the IPv6 key derivation path).
func extractConnAddr(addr net.Addr) (uint32, uint16, [16]byte) {
	var ipv6 [16]byte
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return 0, 0, ipv6
	}
	port := uint16(tcp.Port)
	ip4 := tcp.IP.To4()
	if ip4 != nil {
		// IPv4: pack as little-endian uint32 (matching C's in_addr network byte order)
		ipv4 := uint32(ip4[0]) | uint32(ip4[1])<<8 | uint32(ip4[2])<<16 | uint32(ip4[3])<<24
		return ipv4, port, ipv6
	}
	// IPv6
	ip16 := tcp.IP.To16()
	if ip16 != nil {
		copy(ipv6[:], ip16)
	}
	return 0, port, ipv6
}

// keySignature returns the first 4 bytes of the AES secret as a LE uint32.
// In C this is main_secret.key_signature — a union of secret[] and int,
// so it's simply *(int*)secret, i.e. the first 4 bytes.
func (c *rpcOutboundConn) keySignature() uint32 {
	if len(c.secret) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(c.secret[0:4])
}

// sendNonceAES sends a RPC_NONCE packet requesting AES encryption.
// Corresponds to tcp_rpcc_init_crypto() in C with RPC_CRYPTO_AES schema.
func (c *rpcOutboundConn) sendNonceAES(clientNonce [16]byte, ts uint32) error {
	// Packet layout: [type(4)][key_select(4)][crypto_schema(4)][crypto_ts(4)][nonce(16)] = 32 bytes
	pkt := make([]byte, 32)
	binary.LittleEndian.PutUint32(pkt[0:4], rpcNonce)
	// key_select = first 4 bytes of secret as LE uint32 (C: main_secret.key_signature,
	// which is a union with secret[] — i.e., *(int*)secret)
	binary.LittleEndian.PutUint32(pkt[4:8], c.keySignature())
	binary.LittleEndian.PutUint32(pkt[8:12], rpccCryptoAES)
	binary.LittleEndian.PutUint32(pkt[12:16], ts)
	copy(pkt[16:32], clientNonce[:])
	return c.writeRawFrame(pkt)
}

// sendNonceDH sends a RPC_NONCE packet with DH params (g^a mod p).
// Corresponds to tcp_rpcc_init_crypto() in C with RPC_CRYPTO_AES_DH schema.
func (c *rpcOutboundConn) sendNonceDH(clientNonce [16]byte, ts uint32, gA [256]byte) error {
	// Layout: [type(4)][key_select(4)][crypto_schema(4)][crypto_ts(4)][nonce(16)]
	//         [extra_keys_count(4)][extra_key_select[8](32)][dh_params_select(4)][g_a(256)]
	// = 32 + 4 + 32 + 4 + 256 = 328 bytes
	pkt := make([]byte, 328)
	binary.LittleEndian.PutUint32(pkt[0:4], rpcNonce)
	binary.LittleEndian.PutUint32(pkt[4:8], c.keySignature())
	binary.LittleEndian.PutUint32(pkt[8:12], rpccCryptoAESDH)
	binary.LittleEndian.PutUint32(pkt[12:16], ts)
	copy(pkt[16:32], clientNonce[:])
	binary.LittleEndian.PutUint32(pkt[32:36], 0) // extra_keys_count = 0
	// extra_key_select[8] = zeros (offset 36..68)
	binary.LittleEndian.PutUint32(pkt[68:72], rpcDHParamsSelect)
	copy(pkt[72:328], gA[:])
	return c.writeRawFrame(pkt)
}

// sendHandshake sends a RPC_HANDSHAKE packet.
// Corresponds to tcp_rpcc_send_handshake_packet() in C.
// IMPORTANT: This is sent AFTER crypto is set up, so it must be encrypted.
//
// Payload layout (32 bytes, matching C struct tcp_rpc_handshake_packet):
//   [type(4)][flags(4)][sender_pid(12)][peer_pid(12)]
//
// struct process_id (12 bytes, #pragma pack(4)):
//   [ip(4)][port(2)][pid(2)][utime(4)]
func (c *rpcOutboundConn) sendHandshake() error {
	pkt := make([]byte, 32)
	binary.LittleEndian.PutUint32(pkt[0:4], rpcHandshake)
	// flags = 0 (no CRC32C extension)

	// sender_pid: our process identity (offset 8)
	clientIP, clientPort, _ := extractConnAddr(c.conn.LocalAddr())
	binary.LittleEndian.PutUint32(pkt[8:12], clientIP)
	binary.LittleEndian.PutUint16(pkt[12:14], clientPort)
	pid := uint16(uint32(os.Getpid()) & 0xFFFF)
	if pid == 0 {
		pid = 1
	}
	binary.LittleEndian.PutUint16(pkt[14:16], pid)
	binary.LittleEndian.PutUint32(pkt[16:20], uint32(time.Now().Unix()))

	// peer_pid: remote DC identity (offset 20)
	serverIP, serverPort, _ := extractConnAddr(c.conn.RemoteAddr())
	if serverIP == 0x7f000001 { // loopback → 0 (matching C)
		serverIP = 0
	}
	binary.LittleEndian.PutUint32(pkt[20:24], serverIP)
	binary.LittleEndian.PutUint16(pkt[24:26], serverPort)
	// peer_pid.pid and peer_pid.utime = 0 (unknown, matching C)

	return c.writeEncryptedFrame(pkt)
}

// writeRawFrame writes an unencrypted RPC frame.
// RPC frame layout: [4B total_len LE][4B seqno LE][payload][4B CRC32 of (len+seqno+payload)]
// Used only during handshake (before encryption is established).
func (c *rpcOutboundConn) writeRawFrame(payload []byte) error {
	seqno := atomic.AddInt32(&c.outSeqno, 1) - 1
	totalLen := uint32(4 + 4 + len(payload) + 4) // len + seqno + payload + crc

	frame := make([]byte, int(totalLen))
	binary.LittleEndian.PutUint32(frame[0:4], totalLen)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(seqno))
	copy(frame[8:8+len(payload)], payload)

	crc := crc32.ChecksumIEEE(frame[:8+len(payload)])
	binary.LittleEndian.PutUint32(frame[8+len(payload):], crc)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.conn.Write(frame)
	return err
}

// writeEncryptedFrame writes an AES-256-CBC encrypted RPC frame.
// After building the frame, it adds padding to align to 16-byte boundary
// (matching C's tcp_rpc_flush which pads with skip-packets of value 4),
// then encrypts the full aligned buffer with CBC.
func (c *rpcOutboundConn) writeEncryptedFrame(payload []byte) error {
	seqno := atomic.AddInt32(&c.outSeqno, 1) - 1
	totalLen := uint32(4 + 4 + len(payload) + 4)

	frame := make([]byte, int(totalLen))
	binary.LittleEndian.PutUint32(frame[0:4], totalLen)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(seqno))
	copy(frame[8:8+len(payload)], payload)

	crc := crc32.ChecksumIEEE(frame[:8+len(payload)])
	binary.LittleEndian.PutUint32(frame[8+len(payload):], crc)

	// Pad to 16-byte alignment for CBC (matching C's tcp_rpc_flush).
	// Padding consists of 4-byte words with value 4 (LE uint32).
	// The parser recognizes packet_len==4 as a skip-packet.
	padBytes := (16 - (len(frame) % 16)) % 16
	for i := 0; i < padBytes; i += 4 {
		frame = append(frame, 4, 0, 0, 0)
	}

	// Encrypt with AES-256-CBC
	encrypted := make([]byte, len(frame))
	c.cbcEnc.Encrypt(encrypted, frame)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.conn.Write(encrypted)
	return err
}

// readRawFrame reads one RPC frame from the connection (unencrypted, used during handshake).
// Returns (payloadLen, payloadBytes, error).
func (c *rpcOutboundConn) readRawFrame() (int, []byte, error) {
	return readRawFrame(c.conn)
}

// readEncryptedFrame reads and decrypts one CBC-encrypted RPC frame.
// Skips padding packets (packet_len == 4) automatically.
func (c *rpcOutboundConn) readEncryptedFrame() (int, []byte, error) {
	return readCBCFrame(c.cbcReader)
}

// readRawFrame reads one unencrypted RPC frame.
// Frame layout: [4B total_len LE][4B seqno LE][payload][4B CRC32]
func readRawFrame(r io.Reader) (int, []byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return 0, nil, err
	}

	totalLen := binary.LittleEndian.Uint32(lenBuf[:])
	if totalLen < 16 || totalLen > 4*1024*1024 {
		return 0, nil, fmt.Errorf("invalid frame length: %d", totalLen)
	}

	rest := make([]byte, totalLen-4)
	if _, err := io.ReadFull(r, rest); err != nil {
		return 0, nil, err
	}

	fullFrame := make([]byte, totalLen)
	copy(fullFrame[0:4], lenBuf[:])
	copy(fullFrame[4:], rest)

	payloadEnd := int(totalLen) - 4
	expectedCRC := crc32.ChecksumIEEE(fullFrame[:payloadEnd])
	gotCRC := binary.LittleEndian.Uint32(fullFrame[payloadEnd:])
	if expectedCRC != gotCRC {
		return 0, nil, fmt.Errorf("CRC32 mismatch: expected 0x%08x got 0x%08x", expectedCRC, gotCRC)
	}

	payload := fullFrame[8:payloadEnd]
	return len(payload), payload, nil
}

// readCBCFrame reads one frame from a CBC-decrypted stream,
// skipping padding packets (packet_len == 4) automatically.
func readCBCFrame(r io.Reader) (int, []byte, error) {
	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return 0, nil, err
		}

		totalLen := binary.LittleEndian.Uint32(lenBuf[:])

		// Skip padding packets (matching C: if packet_len == 4, skip and continue)
		if totalLen == 4 {
			continue
		}

		if totalLen < 16 || totalLen > 4*1024*1024 {
			return 0, nil, fmt.Errorf("invalid frame length: %d", totalLen)
		}

		rest := make([]byte, totalLen-4)
		if _, err := io.ReadFull(r, rest); err != nil {
			return 0, nil, err
		}

		fullFrame := make([]byte, totalLen)
		copy(fullFrame[0:4], lenBuf[:])
		copy(fullFrame[4:], rest)

		payloadEnd := int(totalLen) - 4
		expectedCRC := crc32.ChecksumIEEE(fullFrame[:payloadEnd])
		gotCRC := binary.LittleEndian.Uint32(fullFrame[payloadEnd:])
		if expectedCRC != gotCRC {
			return 0, nil, fmt.Errorf("CRC32 mismatch: expected 0x%08x got 0x%08x", expectedCRC, gotCRC)
		}

		payload := fullFrame[8:payloadEnd]
		return len(payload), payload, nil
	}
}

// cbcDecryptReader provides a transparent decrypted byte stream over an
// AES-256-CBC encrypted TCP connection. It reads encrypted data from the
// underlying reader, decrypts full 16-byte blocks, and serves decrypted
// bytes to callers. Works with io.ReadFull for frame parsing.
type cbcDecryptReader struct {
	r      io.Reader
	dec    *crypto.AESCBCDecryptor
	rawBuf []byte // encrypted bytes not yet forming a full 16-byte block
	decBuf []byte // decrypted bytes ready to consume
}

func (cr *cbcDecryptReader) Read(p []byte) (int, error) {
	// Serve from decrypted buffer first
	if len(cr.decBuf) > 0 {
		n := copy(p, cr.decBuf)
		cr.decBuf = cr.decBuf[n:]
		return n, nil
	}

	// Keep reading until we have at least one full block to decrypt
	for {
		buf := make([]byte, 4096)
		n, err := cr.r.Read(buf)
		if n > 0 {
			cr.rawBuf = append(cr.rawBuf, buf[:n]...)
		}

		blocks := (len(cr.rawBuf) / 16) * 16
		if blocks > 0 {
			decrypted := make([]byte, blocks)
			cr.dec.Decrypt(decrypted, cr.rawBuf[:blocks])
			cr.rawBuf = cr.rawBuf[blocks:]

			nn := copy(p, decrypted)
			if nn < len(decrypted) {
				cr.decBuf = decrypted[nn:]
			}
			return nn, nil
		}

		if err != nil {
			return 0, err
		}
	}
}

// SendProxyRequest builds and sends a RPC_PROXY_REQ frame to the Telegram DC.
//
// This is a port of the forwarding logic from mtproto/mtproto-proxy.c
// (the tl_store_int(RPC_PROXY_REQ) block).
//
// flags: typically FlagExtNode (0x1000) | FlagProxyTag (0x8) if proxyTag != nil
// extConnID: identifies the upstream client connection
// remoteIP: client's IP as seen by the proxy (16 bytes, IPv6 or IPv4-mapped)
// remotePort: client port
// ourIP: proxy's IP (16 bytes)
// ourPort: proxy's port
// proxyTag: 16-byte proxy tag, or nil
// mtprotoData: the raw MTProto payload from the client
func (c *rpcOutboundConn) SendProxyRequest(
	flags int32,
	extConnID int64,
	remoteIP [16]byte,
	remotePort uint32,
	ourIP [16]byte,
	ourPort uint32,
	proxyTag []byte,
	mtprotoData []byte,
) error {
	// Calculate extra bytes size for proxy tag
	// Format: [4B TL_PROXY_TAG][1B len=16][16B tag][3B padding] = 24 bytes
	// Preceded by [4B extra_size]
	var extraBuf []byte
	if len(proxyTag) == 16 {
		flags |= protocol.FlagProxyTag
		extraBuf = buildProxyTagExtra(proxyTag)
	}

	// Build RPC_PROXY_REQ payload:
	// [type(4)][flags(4)][ext_conn_id(8)][remote_ip(16)][remote_port(4)][our_ip(16)][our_port(4)]
	// [extra_size(4)][extra_bytes(N)][mtproto_data]
	hdrSize := 4 + 4 + 8 + 16 + 4 + 16 + 4
	extraSizeField := 4
	totalSize := hdrSize + extraSizeField + len(extraBuf) + len(mtprotoData)

	pkt := make([]byte, totalSize)
	off := 0

	binary.LittleEndian.PutUint32(pkt[off:], uint32(protocol.RPCProxyReq))
	off += 4
	binary.LittleEndian.PutUint32(pkt[off:], uint32(flags))
	off += 4
	binary.LittleEndian.PutUint64(pkt[off:], uint64(extConnID))
	off += 8
	copy(pkt[off:off+16], remoteIP[:])
	off += 16
	binary.LittleEndian.PutUint32(pkt[off:], remotePort)
	off += 4
	copy(pkt[off:off+16], ourIP[:])
	off += 16
	binary.LittleEndian.PutUint32(pkt[off:], ourPort)
	off += 4
	binary.LittleEndian.PutUint32(pkt[off:], uint32(len(extraBuf)))
	off += 4
	copy(pkt[off:], extraBuf)
	off += len(extraBuf)
	copy(pkt[off:], mtprotoData)

	return c.writeEncryptedFrame(pkt)
}

// buildProxyTagExtra builds the TL-serialized proxy tag extra bytes.
// Format from mtproto-proxy.c: tl_store_int(TL_PROXY_TAG) + tl_store_string(proxy_tag, 16)
// TL string: [1B len=16][16B data][3B padding to align to 4 bytes] = 20 bytes
// Total: 4 + 20 = 24 bytes
func buildProxyTagExtra(tag []byte) []byte {
	buf := make([]byte, 24)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(protocol.TLProxyTag))
	buf[4] = 16 // string length byte
	copy(buf[5:21], tag[:16])
	// 3 padding bytes are already zero
	return buf
}

// RegisterPending registers a response channel for a given ext_conn_id.
// The channel will receive exactly one ProxyResponse when the server responds.
func (c *rpcOutboundConn) RegisterPending(connID int64, ch chan<- ProxyResponse) {
	c.pendingMu.Lock()
	c.pending[connID] = ch
	c.pendingMu.Unlock()
}

// UnregisterPending removes a pending channel without sending to it.
func (c *rpcOutboundConn) UnregisterPending(connID int64) {
	c.pendingMu.Lock()
	delete(c.pending, connID)
	c.pendingMu.Unlock()
}

// readLoop is the goroutine that continuously reads encrypted RPC frames from the server
// and dispatches responses to waiting goroutines via the pending map.
//
// Handles: RPC_PROXY_ANS, RPC_SIMPLE_ACK, RPC_CLOSE_EXT, RPC_PONG (keepalive).
func (c *rpcOutboundConn) readLoop() {
	for {
		select {
		case <-c.closed:
			return
		default:
		}

		_, payload, err := c.readEncryptedFrame()
		if err != nil {
			select {
			case <-c.closed:
			default:
				// connection error — signal closure
				close(c.closed)
				c.conn.Close()
			}
			return
		}

		if len(payload) < 4 {
			continue
		}

		opcode := int32(binary.LittleEndian.Uint32(payload[0:4]))
		c.handleFrame(opcode, payload)
	}
}

// handleFrame dispatches a received frame by opcode.
// Corresponds to the execute() dispatch in mtproto-proxy.c.
func (c *rpcOutboundConn) handleFrame(opcode int32, payload []byte) {
	switch uint32(opcode) {
	case protocol.RPCProxyAns:
		c.handleProxyAns(payload)
	case protocol.RPCSimpleAck:
		c.handleSimpleAck(payload)
	case protocol.RPCCloseExt:
		c.handleCloseExt(payload)
	case protocol.RPCPong:
		// keepalive response — no action needed
	}
}

// handleProxyAns processes RPC_PROXY_ANS.
// Layout: [type(4)][flags(4)][ext_conn_id(8)][data...]
func (c *rpcOutboundConn) handleProxyAns(payload []byte) {
	if len(payload) < 16 {
		return
	}
	flags := int32(binary.LittleEndian.Uint32(payload[4:8]))
	connID := int64(binary.LittleEndian.Uint64(payload[8:16]))
	data := make([]byte, len(payload)-16)
	copy(data, payload[16:])

	resp := ProxyResponse{Flags: flags, ConnID: connID, Data: data}

	c.pendingMu.Lock()
	ch, ok := c.pending[connID]
	if ok {
		delete(c.pending, connID)
	}
	c.pendingMu.Unlock()

	if ok {
		select {
		case ch <- resp:
		default:
		}
	}
}

// handleSimpleAck processes RPC_SIMPLE_ACK.
// Layout: [type(4)][ext_conn_id(8)][confirm(4)]
func (c *rpcOutboundConn) handleSimpleAck(payload []byte) {
	if len(payload) < 16 {
		return
	}
	connID := int64(binary.LittleEndian.Uint64(payload[4:12]))

	c.pendingMu.Lock()
	ch, ok := c.pending[connID]
	if ok {
		delete(c.pending, connID)
	}
	c.pendingMu.Unlock()

	if ok {
		resp := ProxyResponse{Flags: 0, ConnID: connID}
		select {
		case ch <- resp:
		default:
		}
	}
}

// handleCloseExt processes RPC_CLOSE_EXT from server — server wants to close a client conn.
// Layout: [type(4)][ext_conn_id(8)]
func (c *rpcOutboundConn) handleCloseExt(payload []byte) {
	if len(payload) < 12 {
		return
	}
	connID := int64(binary.LittleEndian.Uint64(payload[4:12]))

	c.pendingMu.Lock()
	ch, ok := c.pending[connID]
	if ok {
		delete(c.pending, connID)
	}
	c.pendingMu.Unlock()

	if ok {
		// Signal close to the waiting forwarder
		resp := ProxyResponse{Flags: protocol.RPCCloseExt, ConnID: connID}
		select {
		case ch <- resp:
		default:
		}
	}
}

// pingLoop sends RPC_PING frames every pingInterval to keep the connection alive.
// Corresponds to StartPingLoop / tcp_rpc_send_ping in C.
func (c *rpcOutboundConn) pingLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-ticker.C:
			if err := c.sendPing(); err != nil {
				return
			}
		}
	}
}

// sendPing sends a RPC_PING frame.
// Layout: [type(4)][ping_id(8)] = 12 bytes payload
func (c *rpcOutboundConn) sendPing() error {
	var pingID [8]byte
	if _, err := rand.Read(pingID[:]); err != nil {
		return err
	}
	pkt := make([]byte, 12)
	binary.LittleEndian.PutUint32(pkt[0:4], uint32(protocol.RPCPing))
	copy(pkt[4:12], pingID[:])
	return c.writeEncryptedFrame(pkt)
}

// natTranslateIP applies NAT translation to an IPv4 address.
// Matches C: nat_translate_ip() in net/net-connections.c.
func (c *rpcOutboundConn) natTranslateIP(ip uint32) uint32 {
	if c.natInfo != nil {
		if pub, ok := c.natInfo[ip]; ok {
			return pub
		}
	}
	return ip
}

// --- DH helpers (ported from net/net-crypto-dh.c) ---

// dhFirstRound generates a: a random 256-byte exponent, computes g^a mod p.
// Equivalent to C dh_first_round().
func dhFirstRound(gA []byte, a []byte) error {
	p := new(big.Int).SetBytes(rpcDHPrime)

	for {
		if _, err := rand.Read(a[:256]); err != nil {
			return err
		}
		aInt := new(big.Int).SetBytes(a[:256])
		gaInt := new(big.Int).Exp(rpcDHGenerator, aInt, p)

		gaBytes := gaInt.Bytes()
		if len(gaBytes) < 241 || len(gaBytes) > 256 {
			continue
		}
		if !isGoodDHBin(gaBytes) {
			continue
		}
		// Zero-pad to 256 bytes (big-endian)
		result := make([]byte, 256)
		copy(result[256-len(gaBytes):], gaBytes)
		copy(gA[:256], result)
		return nil
	}
}

// dhThirdRound computes shared secret: g_b^a mod p.
// Equivalent to C dh_third_round().
func dhThirdRound(gB []byte, a []byte) ([]byte, error) {
	if !isGoodDHBin(gB) {
		return nil, fmt.Errorf("server DH value gB is not a good DH value")
	}
	p := new(big.Int).SetBytes(rpcDHPrime)
	gBInt := new(big.Int).SetBytes(gB)
	aInt := new(big.Int).SetBytes(a)
	sharedInt := new(big.Int).Exp(gBInt, aInt, p)

	sharedBytes := sharedInt.Bytes()
	if len(sharedBytes) < 241 || len(sharedBytes) > 256 {
		return nil, fmt.Errorf("shared DH value out of range: %d bytes", len(sharedBytes))
	}
	result := make([]byte, 256)
	copy(result[256-len(sharedBytes):], sharedBytes)
	return result, nil
}

// isGoodDHBin checks that a 256-byte big-endian number is non-zero and less than the prime.
// Equivalent to C is_good_rpc_dh_bin().
func isGoodDHBin(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	// must have at least one non-zero byte in first 8
	ok := false
	for i := 0; i < 8; i++ {
		if data[i] != 0 {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	// must be strictly less than prime in first 8 bytes (lexicographic)
	for i := 0; i < 8; i++ {
		if i >= len(rpcDHPrime) {
			break
		}
		if data[i] > rpcDHPrime[i] {
			return false
		}
		if data[i] < rpcDHPrime[i] {
			return true
		}
	}
	return false
}
