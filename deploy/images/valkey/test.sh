#!/usr/bin/env bash
set -euo pipefail

image=${1:?테스트할 로컬 Valkey 이미지가 필요합니다}
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
umask 077
openssl req -x509 -newkey rsa:2048 -noenc -days 1 -subj /CN=localhost -addext subjectAltName=IP:127.0.0.1 \
  -keyout "$fixture/key.pem" -out "$fixture/cert.pem" >/dev/null 2>&1
openssl rand -hex 32 | tr -d '\n' >"$fixture/health-password"
digest=$(sha256sum "$fixture/health-password")
printf 'user default off\nuser health on #%s -@all +ping +info\n' "${digest%% *}" >"$fixture/users.acl"

for aof in no yes; do
  podman run --rm --pull=never --network none --userns keep-id --user "$(id -u):$(id -g)" \
    --read-only --security-opt label=disable --cap-drop all --pids-limit 64 --memory 128m \
    --mount "type=bind,src=$fixture,dst=/fixture,readonly" --tmpfs /data:rw,mode=1777 --entrypoint sh "$image" -eu -c '
      valkey-server --port 0 --tls-port 16379 --tls-auth-clients no --tls-ca-cert-file /fixture/cert.pem \
        --tls-cert-file /fixture/cert.pem --tls-key-file /fixture/key.pem --aclfile /fixture/users.acl \
        --save "" --appendonly "$1" --dir /data &
      server=$!
      trap '\''kill "$server" 2>/dev/null || :; wait "$server" || :'\'' EXIT
      for attempt in $(seq 1 50); do
        if valkey-runtime health /fixture/health-password /fixture/cert.pem 16379 >/dev/null 2>&1; then break; fi
        kill -0 "$server"
        sleep 0.1
      done
      result=$(valkey-runtime observe /fixture/health-password /fixture/cert.pem 16379)
      case "$1:$result" in no:0|yes:1) ;; *) exit 1 ;; esac
    ' test "$aof"
done
