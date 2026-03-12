package proxy

import "net"

// ClientMeta содержит метаданные клиентского соединения.
// Аналог полей ext_connection + connection_info из C-кода.
type ClientMeta struct {
	// Сетевые адреса
	RemoteAddr net.Addr
	LocalAddr  net.Addr

	// IPv4/IPv6 в wire-формате (для RPC_PROXY_REQ)
	RemoteIPv6 [16]byte
	RemotePort uint32
	OurIPv6    [16]byte
	OurPort    uint32

	// Идентификатор целевого DC (из obfuscated2 handshake)
	TargetDC int

	// Секрет, которым аутентифицирован клиент (индекс в списке секретов)
	SecretIndex int
	Secret      []byte

	// auth_key_id из первого пакета (0 = DH handshake, non-0 = encrypted)
	AuthKeyID int64

	// ext_conn_id — уникальный ID этого соединения в таблице ext_connections
	// используется как ключ при маппинге client ↔ backend
	ExtConnID int64
}

// Target представляет один backend-адрес Telegram DC.
type Target struct {
	Addr string // "host:port"
	DCID int
}
