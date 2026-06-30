#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

compose_config() {
  local env_file="${COMPOSE_ENV_FILE:-}"
  if [[ -z "${env_file}" && -e /run/hololive-bot/compose.env ]]; then
    env_file=/run/hololive-bot/compose.env
  fi

  local env_args=()
  if [[ -n "${env_file}" ]]; then
    env_args+=("COMPOSE_ENV_FILE=${env_file}")
  fi

  if [[ -n "${env_file}" && ! -r "${env_file}" ]]; then
    sudo -n env "${env_args[@]}" "${ROOT_DIR}/scripts/deploy/compose.sh" "$@"
    return
  fi
  env "${env_args[@]}" "${ROOT_DIR}/scripts/deploy/compose.sh" "$@"
}

echo "[SMOKE] docker compose production config"
compose_config -f "${ROOT_DIR}/deploy/compose/docker-compose.prod.yml" config --no-interpolate >/dev/null
echo "[PASS] docker compose production config is valid"
