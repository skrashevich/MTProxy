package cli

import "fmt"

const ShortDescription = "Simple MT-Proto proxy"

func Usage(progname, fullVersion string) string {
	return fmt.Sprintf(
		"usage: %s [-v] [-6] [-p<port>] [-H<http-port>{,<http-port>}] [-M<workers>] [-u<username>] [-b<backlog>] [-c<max-conn>] [-l<log-name>] [-W<window-size>] <config-file>\n%s\n\t%s\n\t-S, --mtproto-secret\t16-byte secret in hex mode\n\t--mtproto-secret-file\tpath to file with mtproto secrets\n\t-P, --proxy-tag\t16-byte proxy tag in hex mode\n\t-D, --domain\tadds allowed domain for TLS-transport mode\n\t--http-stats\tallow http server to answer on stats queries\n\t-H, --http-ports\tcomma-separated list of client (HTTP) ports to listen\n\t-M, --slaves\tspawn several slave workers\n\t-C, --max-special-connections\tmax accepted client connections per worker\n\t-W, --window-clamp\tsets window clamp for client TCP connections\n\t-T, --ping-interval\tsets ping interval for local TCP connections\n\t-l, --log\tpath to log file\n\t-u, --user\tusername used for privilege drop\n\t-p, --port\tlocal port or port range start:end\n\t-v, --verbosity\tincrease verbosity or set explicit level\n\t--allow-skip-dh\tallow skipping DH during RPC handshake\n\t--disable-tcp\tdo not open listening tcp socket\n\t--crc32c\ttry to use crc32c instead of crc32 in tcp rpc\n\t--force-dh\tforce using DH for all outbound RPC connections\n\t--max-accept-rate\tmax accepted connections per second\n\t--max-dh-accept-rate\tmax DH accepted connections per second\n\t--address\tbind socket only to specified address\n\t--msg-buffers-size\tsets maximal buffers size\n\t--nice\tsets niceness\n\t--nat-info\t<local-addr>:<global-addr>\n",
		progname,
		fullVersion,
		ShortDescription,
	)
}
