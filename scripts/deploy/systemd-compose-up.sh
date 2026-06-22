#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${HOLOLIVE_BOT_ROOT:-/opt/hololive-bot/compose/current}"
cd "$ROOT_DIR"

verify_bootstrap_root_safe() {
  local target="$1" path dir uid gid perms g o violations=0

  path="$(python3 - "$target" <<'PY'
import os
import sys

print(os.path.abspath(os.path.normpath(sys.argv[1])))
PY
)"
  dir="$(dirname "$path")"
  while [[ "$dir" != "/" ]]; do
    if [[ -L "$dir" ]]; then
      echo "[verify-exec-tree-bootstrap] symlink in verifier path: $dir" >&2
      violations=$((violations + 1))
    elif [[ -e "$dir" ]]; then
      uid="$(stat -c '%u' "$dir")"
      gid="$(stat -c '%g' "$dir")"
      perms="$(printf '%04d' "$((10#$(stat -c '%a' "$dir")))")"
      g="${perms:2:1}"
      o="${perms:3:1}"
      if [[ "$uid" -ne 0 ]] || (( (o & 2) != 0 )) || { (( (g & 2) != 0 )) && [[ "$gid" -ne 0 ]]; }; then
        echo "[verify-exec-tree-bootstrap] unsafe verifier parent: $dir" >&2
        violations=$((violations + 1))
      fi
    else
      echo "[verify-exec-tree-bootstrap] missing verifier parent: $dir" >&2
      violations=$((violations + 1))
    fi
    dir="$(dirname "$dir")"
  done

  if [[ -L "$path" ]]; then
    echo "[verify-exec-tree-bootstrap] verifier is a symlink: $path" >&2
    violations=$((violations + 1))
  elif [[ ! -e "$path" ]]; then
    echo "[verify-exec-tree-bootstrap] verifier is missing: $path" >&2
    violations=$((violations + 1))
  else
    uid="$(stat -c '%u' "$path")"
    gid="$(stat -c '%g' "$path")"
    perms="$(printf '%04d' "$((10#$(stat -c '%a' "$path")))")"
    g="${perms:2:1}"
    o="${perms:3:1}"
    if [[ "$uid" -ne 0 ]] || (( (o & 2) != 0 )) || { (( (g & 2) != 0 )) && [[ "$gid" -ne 0 ]]; }; then
      echo "[verify-exec-tree-bootstrap] unsafe verifier: $path" >&2
      violations=$((violations + 1))
    fi
  fi

  (( violations == 0 ))
}

if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  verifier="${ROOT_DIR}/scripts/deploy/verify-exec-tree-ownership.sh"
  if ! verify_bootstrap_root_safe "$verifier"; then
    echo "[SECURITY] root-executed verifier is writable by a non-root user (03e6dca8)." >&2
    echo "           chown the tree to root (or run this unit as a constrained service user)." >&2
    exit 1
  fi

  exec_tree=(
    "$verifier"
    "${ROOT_DIR}/scripts/deploy/systemd-compose-up.sh"
    "${ROOT_DIR}/scripts/deploy/compose.sh"
    "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
    "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh"
  )
  while IFS= read -r yml; do
    exec_tree+=("${yml}")
  done < <(find "${ROOT_DIR}/deploy/compose" -maxdepth 1 -type f -name 'docker-compose*.yml' 2>/dev/null | sort)

  if ! "$verifier" "${exec_tree[@]}"; then
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
