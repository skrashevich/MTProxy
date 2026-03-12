package cli

import (
	"flag"
	"fmt"
	"os"
)

const versionStr = "mtproxy-0.02 (Go port)"

// PrintUsage prints formatted help to stderr.
func PrintUsage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, "%s\n", versionStr)
	fmt.Fprintf(os.Stderr, "\tSimple MT-Proto proxy\n\n")
	fmt.Fprintf(os.Stderr, "Usage: %s [options] <config-file>\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Options:\n")
	fmt.Fprintf(os.Stderr, "  -S, --mtproto-secret <hex>      16-byte secret in hex (32 chars); repeatable\n")
	fmt.Fprintf(os.Stderr, "      --mtproto-secret-file <path> file with secrets (comma/whitespace sep)\n")
	fmt.Fprintf(os.Stderr, "  -P, --proxy-tag <hex>           16-byte proxy tag in hex (32 chars)\n")
	fmt.Fprintf(os.Stderr, "  -M, --slaves <N>                spawn N worker processes (default 1)\n")
	fmt.Fprintf(os.Stderr, "  -H, --http-ports <ports>        comma-separated HTTP listen ports\n")
	fmt.Fprintf(os.Stderr, "      --aes-pwd <path>            AES secret file for RPC\n")
	fmt.Fprintf(os.Stderr, "      --http-stats                enable HTTP stats on main port\n")
	fmt.Fprintf(os.Stderr, "  -C, --max-special-connections N max accepted client connections per worker\n")
	fmt.Fprintf(os.Stderr, "  -W, --window-clamp N            TCP window clamp for client connections\n")
	fmt.Fprintf(os.Stderr, "  -D, --domain <domain>           TLS domain; disables other transports; repeatable\n")
	fmt.Fprintf(os.Stderr, "  -T, --ping-interval <sec>       ping interval for local TCP (default 5.0)\n")
	fmt.Fprintf(os.Stderr, "  -u, --user <username>           setuid to this user\n")
	fmt.Fprintf(os.Stderr, "  -6                              prefer IPv6 for outbound\n")
	fmt.Fprintf(os.Stderr, "  -v, --verbosity [N]             increase or set verbosity level\n")
	fmt.Fprintf(os.Stderr, "  -d, --daemonize                 daemonize\n")
	fmt.Fprintf(os.Stderr, "  -h, --help                      print this help\n")
	fmt.Fprintf(os.Stderr, "\nPositional:\n")
	fmt.Fprintf(os.Stderr, "  <config-file>                   path to proxy-multi.conf\n")
}
