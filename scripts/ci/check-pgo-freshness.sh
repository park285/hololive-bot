#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

DEFAULT_PROFILES=(
  "hololive/hololive-api/cmd/hololive-api/default.pgo"
  "hololive/hololive-alarm-worker/cmd/alarm-worker/default.pgo"
)
APPROVED_DEFAULT="${ROOT_DIR}/scripts/perf/pgo/approved-workloads"

PROFILES=()
APPROVED_FILE="${APPROVED_DEFAULT}"
STRICT=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      PROFILES+=("$2")
      shift 2
      ;;
    --approved-workloads)
      APPROVED_FILE="$2"
      shift 2
      ;;
    --strict)
      STRICT=1
      shift
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ ${#PROFILES[@]} -eq 0 ]]; then
  PROFILES=("${DEFAULT_PROFILES[@]}")
fi

GLOB_THRESHOLD="${PGO_FRESHNESS_GLOB_THRESHOLD:-20}"
WARN_COUNT=0

warn() {
  echo "[pgo-freshness] WARN: $1"
  WARN_COUNT=$((WARN_COUNT + 1))
}

info() {
  echo "[pgo-freshness] $1"
}

read_meta_field() {
  local meta="$1"
  local field="$2"
  python3 - "$meta" "$field" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(3)
val = data.get(sys.argv[2])
print("" if val is None else val)
PY
}

current_toolchain() {
  go env GOVERSION 2>/dev/null || echo ""
}

meta_expired() {
  local generated_at="$1"
  local expires_days="$2"
  python3 - "$generated_at" "$expires_days" <<'PY'
import datetime as dt
import sys
generated_at, expires = sys.argv[1], sys.argv[2]
try:
    gen = dt.datetime.fromisoformat(generated_at)
except ValueError:
    print("PARSE_ERROR")
    sys.exit(0)
if gen.tzinfo is None:
    now = dt.datetime.now()
else:
    now = dt.datetime.now(gen.tzinfo)
try:
    days = int(expires)
except ValueError:
    days = 45
age = (now - gen).days
print("EXPIRED" if age > days else "FRESH", age, days)
PY
}

glob_change_count() {
  local meta_sha="$1"
  local globs_file="$2"
  if [[ -n "${PGO_FRESHNESS_GLOB_COUNT_CMD:-}" ]]; then
    bash -c "${PGO_FRESHNESS_GLOB_COUNT_CMD}"
    return
  fi
  if ! git rev-parse "${meta_sha}^{commit}" >/dev/null 2>&1; then
    echo "UNKNOWN_SHA"
    return
  fi
  local paths=()
  local line
  while IFS= read -r line; do
    [[ -z "${line}" ]] && continue
    paths+=("${line%%\*\*}")
  done <"${globs_file}"
  if [[ ${#paths[@]} -eq 0 ]]; then
    echo 0
    return
  fi
  git log --oneline "${meta_sha}..HEAD" -- "${paths[@]}" 2>/dev/null | wc -l | tr -d ' '
}

check_profile() {
  local profile="$1"
  local meta="${profile}.meta.json"
  local globs="${profile}.hotpaths"

  info "checking ${profile}"

  if [[ ! -f "${profile}" ]]; then
    warn "${profile}: default.pgo missing"
    return
  fi
  if [[ ! -f "${meta}" ]]; then
    warn "${profile}: meta sibling missing (${meta})"
    return
  fi

  local generated_at expires workload go_version sha
  generated_at="$(read_meta_field "${meta}" generatedAt || true)"
  expires="$(read_meta_field "${meta}" expiresAfterDays || true)"
  workload="$(read_meta_field "${meta}" workloadName || true)"
  go_version="$(read_meta_field "${meta}" goVersion || true)"
  sha="$(read_meta_field "${meta}" sourceGitSha || true)"

  if [[ -z "${generated_at}" || -z "${expires}" ]]; then
    warn "${profile}: meta missing generatedAt/expiresAfterDays"
  else
    local expiry_result
    expiry_result="$(meta_expired "${generated_at}" "${expires}")"
    case "${expiry_result}" in
      EXPIRED*)
        warn "${profile}: profile expired (${expiry_result})"
        ;;
      PARSE_ERROR)
        warn "${profile}: generatedAt unparseable: ${generated_at}"
        ;;
    esac
  fi

  if [[ -z "${workload}" ]]; then
    warn "${profile}: meta missing workloadName"
  elif ! grep -qxF "${workload}" "${APPROVED_FILE}" 2>/dev/null; then
    warn "${profile}: workload '${workload}' not in approved list (${APPROVED_FILE})"
  elif [[ "${workload}" == "retroactive-v0" ]]; then
    warn "${profile}: workload retroactive-v0 — 갱신 권고 (소급 profile, 대표성 미문서화)"
  fi

  local toolchain
  toolchain="$(current_toolchain)"
  if [[ -n "${go_version}" && -n "${toolchain}" && "${go_version}" != "${toolchain}" ]]; then
    warn "${profile}: meta goVersion ${go_version} != current toolchain ${toolchain}"
  fi

  if [[ "${PGO_FRESHNESS_SKIP_GLOB:-0}" != "1" ]]; then
    if [[ ! -f "${globs}" ]]; then
      warn "${profile}: hot-path glob file missing (${globs})"
    elif [[ -n "${sha}" ]]; then
      local count
      count="$(glob_change_count "${sha}" "${globs}")"
      if [[ "${count}" == "UNKNOWN_SHA" ]]; then
        warn "${profile}: sourceGitSha ${sha} not in history — cannot count hot path changes"
      elif [[ "${count}" =~ ^[0-9]+$ ]] && (( count > GLOB_THRESHOLD )); then
        warn "${profile}: ${count} commits touched hot path globs since profile (threshold ${GLOB_THRESHOLD})"
      fi
    fi
  fi
}

check_stamp() {
  if [[ "${PGO_FRESHNESS_SKIP_STAMP:-0}" == "1" ]]; then
    return
  fi
  if [[ ! -x "${SCRIPT_DIR}/check-pgo-default.sh" ]]; then
    warn "check-pgo-default.sh not found; cannot verify -pgo build stamp"
    return
  fi
  if ! "${SCRIPT_DIR}/check-pgo-default.sh" >/dev/null 2>&1; then
    warn "-pgo build stamp check failed (see check-pgo-default.sh)"
  fi
}

for profile in "${PROFILES[@]}"; do
  check_profile "${profile}"
done
check_stamp

if [[ "${WARN_COUNT}" -eq 0 ]]; then
  info "OK: all PGO profiles fresh, approved, and stamped"
  exit 0
fi

info "${WARN_COUNT} warning(s) (warning mode: not a hard failure until PR-P2-01 completes)"
if [[ "${STRICT}" -eq 1 ]]; then
  info "strict mode: promoting warnings to failure"
  exit 1
fi
exit 0
