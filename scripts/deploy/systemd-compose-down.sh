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
    "${ROOT_DIR}/scripts/deploy/systemd-compose-down.sh"
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

export COMPOSE_ENV_FILE=/run/hololive-bot/compose.env

COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  -f deploy/compose/docker-compose.main-ap.yml \
  -f deploy/compose/docker-compose.main-ap.live-compat.yml \
  down
