#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${HOLOLIVE_BOT_ROOT:-/opt/hololive-bot/compose/current}"
cd "$ROOT_DIR"

if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  verifier="${ROOT_DIR}/scripts/deploy/verify-exec-tree-ownership.sh"

  # verifier 를 root 로 실행하기 전에, verifier 와 그 부모 경로가 root-owned·비root 쓰기 불가·
  # 심링크 아님을 repo verifier 에 의존하지 않고 직접 확인한다. 이 self-check 가 없으면
  # verifier 자체를 장악한 비root 사용자가 다음 root 실행에서 임의 코드를 실행한다(4d57f81c).
  bp="${verifier}"
  while :; do
    if [[ -L "${bp}" || ! -e "${bp}" ]]; then
      echo "[SECURITY] root verifier path is a symlink or missing: ${bp} (4d57f81c)." >&2
      exit 1
    fi
    bp_perms="$(printf '%04d' "$((10#$(stat -c '%a' -- "${bp}")))")"
    if [[ "$(stat -c '%u' -- "${bp}")" -ne 0 ]] \
       || (( ( ${bp_perms:3:1} & 2 ) != 0 )) \
       || { (( ( ${bp_perms:2:1} & 2 ) != 0 )) && [[ "$(stat -c '%g' -- "${bp}")" -ne 0 ]]; }; then
      echo "[SECURITY] root verifier is writable by a non-root user: ${bp} (4d57f81c)." >&2
      echo "           chown the deploy tree to root before running deploy units as root." >&2
      exit 1
    fi
    [[ "${bp}" == "/" ]] && break
    bp="$(dirname -- "${bp}")"
  done

  exec_tree=(
    "${verifier}"
    "${ROOT_DIR}/scripts/deploy/systemd-compose-up.sh"
    "${ROOT_DIR}/scripts/deploy/compose.sh"
    "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
    "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh"
  )
  while IFS= read -r yml; do
    exec_tree+=("${yml}")
  done < <(find "${ROOT_DIR}/deploy/compose" -maxdepth 1 -type f -name 'docker-compose*.yml' 2>/dev/null | sort)

  if ! "${verifier}" "${exec_tree[@]}"; then
    echo "[SECURITY] root-executed deploy tree is writable by a non-root user (03e6dca8)." >&2
    echo "           chown the tree to root (or run this unit as a constrained service user)." >&2
    exit 1
  fi
fi

wait_for_tailscale_ip() {
  for _ in $(seq 1 90); do
    if ip -4 addr show tailscale0 2>/dev/null | grep -q '100\.100\.1\.3/32'; then
      return 0
    fi
    sleep 1
  done
  echo "tailscale0 missing 100.100.1.3/32 after 90s" >&2
  return 1
}

wait_for_file() {
  local path="$1"

  for _ in $(seq 1 90); do
    if [ -f "$path" ] && [ -s "$path" ]; then
      return 0
    fi
    if [ -d "$path" ]; then
      echo "$path is a directory; OpenBao render did not complete cleanly" >&2
      return 1
    fi
    sleep 1
  done
  echo "$path was not rendered after 90s" >&2
  return 1
}

wait_for_tailscale_ip
for file in \
  /run/hololive-bot/compose.env \
  /run/hololive-bot/bot.env \
  /run/hololive-bot/alarm-worker.env \
  /run/hololive-bot/youtube-producer.env \
  /run/hololive-bot/certs/hololive-h3.crt \
  /run/hololive-bot/certs/hololive-h3.key \
  /run/hololive-bot/certs/iris-ca.pem \
  /run/hololive-bot/certs/postgres-ca.pem \
  /run/hololive-bot/postgres-tls/server.crt \
  /run/hololive-bot/postgres-tls/server.key
do
  wait_for_file "$file"
done

export COMPOSE_ENV_FILE=/run/hololive-bot/compose.env

base_files=(-f deploy/compose/docker-compose.prod.yml)
main_ap_files=(-f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.main-ap.yml)
if [[ "${HOLOLIVE_ENABLE_LIVE_COMPAT:-}" == "1" ]]; then
  base_files+=(-f deploy/compose/docker-compose.live-compat.yml)
  main_ap_files=(
    -f deploy/compose/docker-compose.prod.yml
    -f deploy/compose/docker-compose.live-compat.yml
    -f deploy/compose/docker-compose.main-ap.yml
    -f deploy/compose/docker-compose.main-ap.live-compat.yml
  )
fi

./scripts/deploy/compose.sh "${base_files[@]}" up -d

COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh "${main_ap_files[@]}" up -d youtube-producer-c
