# MTProxy (Go)

A complete Go port of the [original MTProxy](https://github.com/TelegramMessenger/MTProxy) — a simple MT-Proto proxy for Telegram.

## Building

Requires Go 1.26+.

```bash
go build -o mtproto-proxy ./cmd/mtproto-proxy
```

## Running

1. Obtain a secret used to connect to Telegram servers:
```bash
curl -s https://core.telegram.org/getProxySecret -o proxy-secret
```

2. Obtain the current Telegram configuration (update daily):
```bash
curl -s https://core.telegram.org/getProxyConfig -o proxy-multi.conf
```

3. Generate a secret for client connections:
```bash
head -c 16 /dev/urandom | xxd -ps
```

4. Run the proxy:
```bash
./mtproto-proxy -H 443 -S <secret> --aes-pwd proxy-secret proxy-multi.conf
```

Or with a secrets file:
```bash
./mtproto-proxy -H 443 --mtproto-secret-file secrets.txt --aes-pwd proxy-secret proxy-multi.conf
```

## Options

| Flag | Description |
|------|-------------|
| `-S`, `--mtproto-secret <hex>` | 16-byte secret in hex (32 chars); repeatable |
| `--mtproto-secret-file <path>` | File with secrets (comma or whitespace separated) |
| `-P`, `--proxy-tag <hex>` | 16-byte proxy tag in hex (32 chars) |
| `-M`, `--slaves <N>` | Number of worker processes (default 1) |
| `-H`, `--http-ports <ports>` | Comma-separated client listen ports |
| `--aes-pwd <path>` | AES secret file for RPC connections |
| `--http-stats` | Enable HTTP stats endpoint |
| `-C`, `--max-special-connections <N>` | Max client connections per worker (0 = unlimited) |
| `-W`, `--window-clamp <N>` | TCP window clamp for client connections |
| `--nat-info <local_ip:public_ip>` | NAT IP translation for key derivation; repeatable |
| `-D`, `--domain <domain>` | TLS domain; disables other transports; repeatable |
| `-T`, `--ping-interval <sec>` | Ping interval in seconds (default 5.0) |
| `-u`, `--user <username>` | Username for setuid |
| `-6` | Prefer IPv6 for outbound connections |
| `-v`, `--verbosity <N>` | Verbosity level |
| `-d`, `--daemonize` | Daemonize the process |

## NAT Support

When running behind NAT, use `--nat-info` to map local IPs to public IPs for correct key derivation:

```bash
./mtproto-proxy -H 443 -S <secret> --aes-pwd proxy-secret \
  --nat-info 10.0.1.10:203.0.113.5 proxy-multi.conf
```

## Random Padding

Random padding is supported to counter DPI detection by some ISPs.
Add the `dd` prefix to the secret on the client side: `cafe...babe` → `ddcafe...babe`.

## Systemd

```ini
[Unit]
Description=MTProxy
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/mtproxy
ExecStart=/opt/mtproxy/mtproto-proxy -H 443 -S <secret> --aes-pwd proxy-secret proxy-multi.conf
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Project Structure

```
cmd/mtproto-proxy/    — entrypoint, supervisor
internal/
  cli/                — CLI argument parsing
  config/             — DC configuration loading and updates
  crypto/             — AES, DH, CRC, SHA
  protocol/           — MTProto constants and frames
  proxy/              — proxy core: ingress, outbound, RPC, stats
c-original/           — original C implementation (reference)
```

## License

GPLv2 — see [GPLv2](GPLv2), [LGPLv2](LGPLv2).
