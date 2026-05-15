#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DOCS_DIR="${ROOT_DIR}/docs"
HISTORY_PLAN_KITS_DIR="${DOCS_DIR}/history/plan-kits"

echo "[CHECK] legacy docs plan kits are under docs/history/plan-kits"

if [[ ! -d "${DOCS_DIR}" ]]; then
  echo "[FAIL] docs directory not found: ${DOCS_DIR}"
  exit 1
fi

legacy_names=(
  "holobot-pg-valkey-hybrid-hardening-plan-v4"
  "holobot-pg-first-logic-hardening-plan-v2"
  "holobot-valkey-plan"
  "hololive-bot-baseline-bigbang-llm-docs-v8"
  "hololive-bot-integrated-refactor-v3"
  "hololive-main-server-logs-mirror-v2"
  "hololive_scraper_plan_v2"
)

missing=()
misplaced=()

for name in "${legacy_names[@]}"; do
  if [[ -e "${DOCS_DIR}/${name}" ]]; then
    misplaced+=("docs/${name}")
  fi
  if [[ ! -d "${HISTORY_PLAN_KITS_DIR}/${name}" ]]; then
    missing+=("docs/history/plan-kits/${name}")
  fi
done

if (( ${#misplaced[@]} > 0 )); then
  echo "[FAIL] legacy plan-kit directory found at docs top level"
  printf ' - %s\n' "${misplaced[@]}"
  exit 1
fi

if (( ${#missing[@]} > 0 )); then
  echo "[FAIL] legacy plan-kit directory missing from docs/history/plan-kits"
  printf ' - %s\n' "${missing[@]}"
  exit 1
fi

echo "[PASS] legacy docs plan kits are under docs/history/plan-kits"
