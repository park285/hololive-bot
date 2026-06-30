#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

checks=(
  "bot|https://127.0.0.1:30001/health|compose-healthcheck:hololive-api"
  "admin-api|https://127.0.0.1:30006/health|compose-healthcheck:hololive-api"
  "alarm-worker|https://127.0.0.1:30007/health|compose-healthcheck:hololive-alarm-worker"
  "alarm-worker-ready|https://127.0.0.1:30007/ready|compose-healthcheck:hololive-alarm-worker"
  "llm-scheduler|https://127.0.0.1:30003/health|compose-healthcheck:hololive-api"
  "youtube-producer-c|https://127.0.0.1:30025/health|compose-healthcheck:youtube-producer-c:main-ap"
)

compose_exec() {
  local profile="$1"
  shift

  local env_file="${COMPOSE_ENV_FILE:-}"
  if [[ -z "${env_file}" && -e /run/hololive-bot/compose.env ]]; then
    env_file=/run/hololive-bot/compose.env
  fi

  local env_args=()
  if [[ -n "${env_file}" ]]; then
    env_args+=("COMPOSE_ENV_FILE=${env_file}")
  fi
  if [[ -n "${profile}" ]]; then
    env_args+=("COMPOSE_PROFILES=${profile}")
  fi

  if [[ -n "${env_file}" && ! -r "${env_file}" ]]; then
    sudo -n env "${env_args[@]}" "${ROOT_DIR}/scripts/deploy/compose.sh" "$@"
    return
  fi
  env "${env_args[@]}" "${ROOT_DIR}/scripts/deploy/compose.sh" "$@"
}

run_check() {
  local url="$1"
  local probe="$2"

  case "${probe}" in
    compose-healthcheck:*)
      local spec="${probe#compose-healthcheck:}"
      local service="${spec%%:*}"
      local mode=""
      if [[ "${spec}" == *:* ]]; then
        mode="${spec#*:}"
      fi
      case "${mode}" in
        "")
          compose_exec "" -f "${ROOT_DIR}/deploy/compose/docker-compose.prod.yml" exec -T "${service}" ./bin/healthcheck "${url}" >/dev/null
          ;;
        main-ap)
          compose_exec main-ap \
            -f "${ROOT_DIR}/deploy/compose/docker-compose.prod.yml" \
            -f "${ROOT_DIR}/deploy/compose/docker-compose.main-ap.yml" \
            exec -T "${service}" ./bin/healthcheck "${url}" >/dev/null
          ;;
        *)
          echo "unsupported compose healthcheck mode: ${mode}" >&2
          return 2
          ;;
      esac
      ;;
    "")
      curl -fsS --max-time 5 "${url}" >/dev/null
      ;;
    *)
      curl -fsS "${probe}" --max-time 5 "${url}" >/dev/null
      ;;
  esac
}

echo "[SMOKE] runtime health endpoints"
for check in "${checks[@]}"; do
  IFS="|" read -r name url probe <<<"${check}"
  echo "[CHECK] ${name} ${url}"
  run_check "${url}" "${probe}"
  echo "[PASS] ${name}"
done
echo "[PASS] runtime health endpoints are reachable"
