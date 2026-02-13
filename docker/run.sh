#!/bin/bash
if [ ! -z "$DEBUG" ]; then set -x; fi
mkdir /data 2>/dev/null >/dev/null
RANDOM=$(printf "%d" "0x$(head -c4 /dev/urandom | od -t x1 -An | tr -d ' ')")

if [ -z "$WORKERS" ]; then
  WORKERS=2
fi

echo "####"
echo "#### Telegram Proxy"
echo "####"
echo
MAX_SECRETS=128

load_secrets_from_file () {
  local file="$1"
  local count=0
  local parsed=""
  local token

  if [ ! -f "$file" ]; then
    echo "[F] Secret file '$file' does not exist."
    return 1
  fi

  while IFS= read -r token; do
    [ -z "$token" ] && continue
    if ! echo "$token" | grep -qE '^[0-9a-fA-F]{32}$'; then
      echo "[F] Bad secret format in '$file': '$token'. Should be 32 hex chars."
      return 1
    fi
    token="$(echo "$token" | tr '[:upper:]' '[:lower:]')"
    if [ -z "$parsed" ]; then
      parsed="$token"
    else
      parsed="$parsed,$token"
    fi
    count=$((count+1))
    if [ "$count" -gt "$MAX_SECRETS" ]; then
      echo "[F] Too many secrets in '$file'. Can use between 1 and $MAX_SECRETS secrets."
      return 1
    fi
  done < <(awk '{ sub(/#.*/, ""); gsub(/,/, " "); for (i = 1; i <= NF; i++) print $i; }' "$file")

  if [ "$count" -eq 0 ]; then
    echo "[F] Secret file '$file' does not contain secrets."
    return 1
  fi

  SECRET="$parsed"
  return 0
}

SECRET_FILE_CMD="--mtproto-secret-file /data/secret"
if [ ! -z "$SECRET_FILE" ]; then
  echo "[+] Using secrets from file: '$SECRET_FILE'."
  load_secrets_from_file "$SECRET_FILE" || exit 1
elif [ ! -z "$SECRET" ]; then
  echo "[+] Using the explicitly passed secret list from SECRET."
  if ! echo "$SECRET" | grep -qE '^[0-9a-fA-F]{32}(,[0-9a-fA-F]{32}){0,127}$'; then
    echo '[F] Bad secret format: should be 32 hex chars (for 16 bytes) for every secret; secrets should be comma-separated.'
    exit 1
  fi
  SECRET="$(echo "$SECRET" | tr '[:upper:]' '[:lower:]')"
elif [ -f /data/secret ]; then
  echo "[+] Using secrets from /data/secret."
  load_secrets_from_file /data/secret || exit 1
else
  if [[ ! -z "$SECRET_COUNT" ]]; then
    if [[ ! ( "$SECRET_COUNT" -ge 1 &&  "$SECRET_COUNT" -le "$MAX_SECRETS" ) ]]; then
      echo "[F] Can generate between 1 and $MAX_SECRETS secrets."
      exit 5
    fi
  else
    SECRET_COUNT="1"
  fi

  echo "[+] No secret passed. Will generate $SECRET_COUNT random ones."
  SECRET="$(dd if=/dev/urandom bs=16 count=1 2>&1 | od -tx1  | head -n1 | tail -c +9 | tr -d ' ')"
  for pass in $(seq 2 $SECRET_COUNT); do
    SECRET="$SECRET,$(dd if=/dev/urandom bs=16 count=1 2>&1 | od -tx1  | head -n1 | tail -c +9 | tr -d ' ')"
  done
fi

if echo "$SECRET" | grep -qE '^[0-9a-fA-F]{32}(,[0-9a-fA-F]{32}){0,127}$'; then
  echo "$SECRET" > /data/secret
  echo -- "$SECRET_FILE_CMD" > /data/secret_cmd
else
  echo '[F] Bad secret format: should be 32 hex chars (for 16 bytes) for every secret; secrets should be comma-separated.'
  exit 1
fi

if [ ! -z "$TAG" ]; then
  echo "[+] Using the explicitly passed tag: '$TAG'."
fi

TAG_CMD=""
if [[ ! -z "$TAG" ]]; then
  if echo "$TAG" | grep -qE '^[0-9a-fA-F]{32}$'; then
    TAG="$(echo "$TAG" | tr '[:upper:]' '[:lower:]')"
    TAG_CMD="-P $TAG"
  else
    echo '[!] Bad tag format: should be 32 hex chars (for 16 bytes).'
    echo '[!] Continuing.'
  fi
fi

curl -s https://core.telegram.org/getProxyConfig -o /etc/telegram/backend.conf || {
  echo '[F] Cannot download proxy configuration from Telegram servers.'
  exit 2
}
CONFIG=/etc/telegram/backend.conf

IP="$(curl -s -4 "https://digitalresistance.dog/myIp")"
INTERNAL_IP="$(ip -4 route get 8.8.8.8 | grep '^8\.8\.8\.8\s' | grep -Po 'src\s+\d+\.\d+\.\d+\.\d+' | awk '{print $2}')"

if [[ -z "$IP" ]]; then
  echo "[F] Cannot determine external IP address."
  exit 3
fi

if [[ -z "$INTERNAL_IP" ]]; then
  echo "[F] Cannot determine internal IP address."
  exit 4
fi

echo "[*] Final configuration:"
I=1
echo "$SECRET" | tr ',' '\n' | while IFS= read -r S; do
  echo "[*]   Secret $I: $S"
  echo "[*]   tg:// link for secret $I auto configuration: tg://proxy?server=${IP}&port=443&secret=${S}"
  echo "[*]   t.me link for secret $I: https://t.me/proxy?server=${IP}&port=443&secret=${S}"
  I=$(($I+1))
done

[ ! -z "$TAG" ] && echo "[*]   Tag: $TAG" || echo "[*]   Tag: no tag"
echo "[*]   External IP: $IP"
echo "[*]   Make sure to fix the links in case you run the proxy on a different port."
echo
echo '[+] Starting proxy...'
sleep 1
exec /usr/local/bin/mtproto-proxy -p 2398 --http-stats -H 443 -M "$WORKERS" -C 60000 --aes-pwd /etc/telegram/hello-explorers-how-are-you-doing -u root $CONFIG --allow-skip-dh --nat-info "$INTERNAL_IP:$IP" $SECRET_FILE_CMD $TAG_CMD
