#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

checks=(
  "bot|http://127.0.0.1:30001/health|"
  "admin-api|http://127.0.0.1:30006/health|"
  "alarm-worker|https://127.0.0.1:30007/health|compose-healthcheck:hololive-alarm-worker"
  "alarm-worker-ready|https://127.0.0.1:30007/ready|compose-healthcheck:hololive-alarm-worker"
  "llm-scheduler|http://127.0.0.1:30003/health|"
  "youtube-producer-c|http://127.0.0.1:30025/health|"
)

run_check() {
  local url="$1"
  local probe="$2"

  case "${probe}" in
    compose-healthcheck:*)
      local service="${probe#compose-healthcheck:}"
      "${ROOT_DIR}/scripts/deploy/compose.sh" -f "${ROOT_DIR}/deploy/compose/docker-compose.prod.yml" exec -T "${service}" ./bin/healthcheck "${url}" >/dev/null
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
