#!/usr/bin/env bash
set -euo pipefail

url="${DISPATCHER_READY_URL:-http://127.0.0.1:30020/ready}"

echo "[SMOKE] dispatcher readiness ${url}"
curl -fsS --max-time 5 "${url}" >/dev/null
echo "[PASS] dispatcher readiness endpoint is reachable"
