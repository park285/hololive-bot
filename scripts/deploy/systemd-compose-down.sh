#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${HOLOLIVE_BOT_ROOT:-/opt/hololive-bot/compose/current}"
cd "$ROOT_DIR"

if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  exec_tree=(
    "${ROOT_DIR}/scripts/deploy/systemd-compose-down.sh"
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
    exit 1
  fi
fi

export COMPOSE_ENV_FILE=/run/hololive-bot/compose.env

COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  -f deploy/compose/docker-compose.main-ap.yml \
  -f deploy/compose/docker-compose.main-ap.live-compat.yml \
  down
