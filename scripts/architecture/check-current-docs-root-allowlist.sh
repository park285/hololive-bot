#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CURRENT_DIR="${ROOT_DIR}/docs/current"

echo "[CHECK] docs/current root contains only approved entrypoint files"

if [[ ! -d "${CURRENT_DIR}" ]]; then
  echo "[FAIL] current docs directory not found: ${CURRENT_DIR}"
  exit 1
fi

allowed_files=(
  "README.md"
  "PROJECT_MAP.md"
  "DEPLOYMENT_BASELINE.md"
  "SERVICE_OWNERSHIP.md"
  "CONTRACT_MAP.md"
  "CONTRACT_MANIFEST.txt"
  "ERROR_CONTRACT.md"
  "QUEUE_AND_PUBSUB_CONTRACTS.md"
  "ALARM_DISPATCH_REMEDIATION_20260414.md"
  "RUNTIME_SPLIT_HANDOFF_20260416.md"
  "RUNTIME_SPLIT_PR07_BLOCKERS_20260416.md"
  "CRITICAL_REVIEW_RESIDUAL_ISSUES_20260415.md"
  "REMAINING_RISKS_EXECUTION_GUIDE_20260415.md"
  "LEGACY_LINT_RESUME_20260415.md"
)

declare -A allowed=()
for file in "${allowed_files[@]}"; do
  allowed["${file}"]=1
done

misplaced=()
while IFS= read -r file; do
  if [[ -z "${allowed[${file}]+x}" ]]; then
    misplaced+=("docs/current/${file}")
  fi
done < <(find "${CURRENT_DIR}" -maxdepth 1 -type f -printf '%f\n' | sort)

if (( ${#misplaced[@]} > 0 )); then
  echo "[FAIL] unclassified root-level docs/current files found"
  printf ' - %s\n' "${misplaced[@]}"
  echo "Move current runbooks, service docs, contracts, architecture guidance, review policy, or history records into their purpose-specific subdirectories."
  exit 1
fi

echo "[PASS] docs/current root contains only approved entrypoint files"
