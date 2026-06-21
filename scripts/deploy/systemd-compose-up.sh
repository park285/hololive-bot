#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${HOLOLIVE_BOT_ROOT:-/home/kapu/work/iris-stack/hololive-bot}"
cd "$ROOT_DIR"

# root 로 실행될 때, 곧 실행할 트리(entrypoint+sourced lib+compose YAML)가 비root 에게
# 쓰기 가능하면 그 계정 장악만으로 root RCE 가 된다(03e6dca8). 기본은 경고만 — 운영자가
# 트리를 root-owned 로 전환한 뒤 HOLOLIVE_EXEC_TREE_ENFORCE=1 로 강제(start 차단)한다.
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  exec_tree=(
    "${ROOT_DIR}/scripts/deploy/systemd-compose-up.sh"
    "${ROOT_DIR}/scripts/deploy/compose.sh"
    "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
    "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh"
  )
  while IFS= read -r yml; do
    exec_tree+=("${yml}")
  done < <(find "${ROOT_DIR}/deploy/compose" -maxdepth 1 -type f -name 'docker-compose*.yml' 2>/dev/null | sort)

  if ! "${ROOT_DIR}/scripts/deploy/verify-exec-tree-ownership.sh" "${exec_tree[@]}"; then
    echo "[SECURITY] root-executed deploy tree is writable by a non-root user (03e6dca8)." >&2
    echo "           chown the tree to root (or run this unit as a constrained service user)." >&2
    if [[ "${HOLOLIVE_EXEC_TREE_ENFORCE:-0}" == "1" ]]; then
      echo "           HOLOLIVE_EXEC_TREE_ENFORCE=1 -> refusing to start." >&2
      exit 1
    fi
    echo "           set HOLOLIVE_EXEC_TREE_ENFORCE=1 to make this fatal once the tree is root-owned." >&2
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

./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d

COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  -f deploy/compose/docker-compose.main-ap.yml \
  -f deploy/compose/docker-compose.main-ap.live-compat.yml \
  up -d youtube-producer-c
