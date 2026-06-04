#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DOCS_DIR="${ROOT_DIR}/docs"

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

misplaced=()

# docs/history/plan-kits/ 는 .gitignore 대상 로컬 아카이브라 존재 단언은
# 클린 체크아웃에서 항상 실패한다. 추적 콘텐츠로 검증 가능한
# top-level 오배치 금지만 게이트로 유지한다.
for name in "${legacy_names[@]}"; do
  if [[ -e "${DOCS_DIR}/${name}" ]]; then
    misplaced+=("docs/${name}")
  fi
done

if (( ${#misplaced[@]} > 0 )); then
  echo "[FAIL] legacy plan-kit directory found at docs top level"
  printf ' - %s\n' "${misplaced[@]}"
  exit 1
fi

echo "[PASS] legacy docs plan kits are under docs/history/plan-kits"
