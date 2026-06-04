#!/usr/bin/env bash
set -euo pipefail

checks=(
  "bot|http://127.0.0.1:30001/health|"
  "admin-api|http://127.0.0.1:30006/health|"
  "alarm-worker|http://127.0.0.1:30007/health|"
  "llm-scheduler|http://127.0.0.1:30003/health|"
  "youtube-producer-c|http://127.0.0.1:30025/health|"
)

echo "[SMOKE] runtime health endpoints"
for check in "${checks[@]}"; do
  IFS="|" read -r name url tls_flag <<<"${check}"
  echo "[CHECK] ${name} ${url}"
  if [[ -n "${tls_flag}" ]]; then
    curl -fsS "${tls_flag}" --max-time 5 "${url}" >/dev/null
  else
    curl -fsS --max-time 5 "${url}" >/dev/null
  fi
  echo "[PASS] ${name}"
done
echo "[PASS] runtime health endpoints are reachable"
