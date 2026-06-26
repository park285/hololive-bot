#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
PROJECT_MAP="${ROOT_DIR}/docs/current/PROJECT_MAP.md"
RUNBOOK_INDEX="${ROOT_DIR}/docs/current/runbooks/README.md"

echo "[CHECK] runtime runbook coverage"

required_runtimes=(
  hololive-api
  alarm-worker
  youtube-producer
)

required_sections=(
  "## Role"
  "## Dependencies"
  "## Common failure modes"
  "## Smoke test"
  "## Rollback"
)

missing=0

if [[ ! -f "${PROJECT_MAP}" ]]; then
  echo "[FAIL] missing project map: ${PROJECT_MAP}"
  exit 1
fi
if [[ ! -f "${RUNBOOK_INDEX}" ]]; then
  echo "[FAIL] missing runbook index: ${RUNBOOK_INDEX}"
  exit 1
fi

for runtime in "${required_runtimes[@]}"; do
  runbook_rel="runbooks/${runtime}.md"
  runbook_file="${ROOT_DIR}/docs/current/${runbook_rel}"

  if ! grep -Fq "\`${runtime}\`" "${PROJECT_MAP}"; then
    echo "[FAIL] project map missing runtime: ${runtime}"
    missing=1
  else
    echo "[PASS] project map lists runtime: ${runtime}"
  fi

  if ! grep -Fq "${runbook_rel}" "${PROJECT_MAP}"; then
    echo "[FAIL] project map missing runbook link: ${runbook_rel}"
    missing=1
  else
    echo "[PASS] project map links runbook: ${runbook_rel}"
  fi

  if [[ ! -f "${runbook_file}" ]]; then
    echo "[FAIL] missing runbook file: docs/current/${runbook_rel}"
    missing=1
  else
    echo "[PASS] found runbook: docs/current/${runbook_rel}"

    for section in "${required_sections[@]}"; do
      if ! grep -Fxq "${section}" "${runbook_file}"; then
        echo "[FAIL] docs/current/${runbook_rel} missing required section: ${section}"
        missing=1
      else
        echo "[PASS] docs/current/${runbook_rel} contains section: ${section}"
      fi
    done
  fi

  if ! grep -Fq "${runtime}.md" "${RUNBOOK_INDEX}"; then
    echo "[FAIL] runbook index missing: ${runtime}.md"
    missing=1
  else
    echo "[PASS] runbook index links: ${runtime}.md"
  fi
done

if [[ "${missing}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] runtime runbook coverage is complete"
