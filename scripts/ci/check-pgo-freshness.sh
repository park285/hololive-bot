#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

APPROVED_DEFAULT="${ROOT_DIR}/scripts/perf/pgo/approved-workloads"
POLICY_DEFAULT="${ROOT_DIR}/scripts/perf/pgo/default-policy.tsv"

PROFILES=()
APPROVED_FILE="${APPROVED_DEFAULT}"
POLICY_FILE="${POLICY_DEFAULT}"
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
    --policy)
      POLICY_FILE="$2"
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
  while IFS='|' read -r mode _module _pkg _services extra; do
    [[ -z "${mode}" || "${mode}" == \#* ]] && continue
    if [[ "${mode}" != "off" || -n "${extra}" ]]; then
      echo "invalid off-only PGO policy row" >&2
      exit 2
    fi
  done <"${POLICY_FILE}"
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
  local collected_at="$1"
  local expires_days="$2"
  python3 - "$collected_at" "$expires_days" "${PGO_FRESHNESS_NOW:-}" <<'PY'
import datetime as dt
import sys
collected_at, expires, now_override = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    collected = dt.datetime.fromisoformat(collected_at)
except ValueError:
    print("PARSE_ERROR")
    sys.exit(0)
if now_override:
    try:
        now = dt.datetime.fromisoformat(now_override)
    except ValueError:
        print("NOW_PARSE_ERROR")
        sys.exit(0)
elif collected.tzinfo is None:
    now = dt.datetime.now()
else:
    now = dt.datetime.now(collected.tzinfo)
try:
    days = int(expires)
except ValueError:
    days = 45
if collected.tzinfo is not None and now.tzinfo is None:
    now = now.replace(tzinfo=collected.tzinfo)
elif collected.tzinfo is None and now.tzinfo is not None:
    collected = collected.replace(tzinfo=now.tzinfo)
age = (now - collected).days
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

source_sha_is_ancestor() {
  local sha="$1"
  if [[ -n "${PGO_FRESHNESS_ANCESTOR_CMD:-}" ]]; then
    PGO_FRESHNESS_SOURCE_SHA="${sha}" bash -c "${PGO_FRESHNESS_ANCESTOR_CMD}"
    return
  fi
  git merge-base --is-ancestor "${sha}" HEAD >/dev/null 2>&1
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
  if ! "${ROOT_DIR}/scripts/perf/pgo/generate.sh" validate-profile "${profile}" "${meta}" >/dev/null 2>&1; then
    warn "${profile}: profile or metadata contract invalid (${meta})"
    return
  fi

  local collected_at expires workload go_version sha
  collected_at="$(read_meta_field "${meta}" profileCollectedAt || true)"
  expires="$(read_meta_field "${meta}" expiresAfterDays || true)"
  workload="$(read_meta_field "${meta}" workloadName || true)"
  go_version="$(read_meta_field "${meta}" goVersion || true)"
  sha="$(read_meta_field "${meta}" sourceGitSha || true)"

  local source_sha_valid=0
  if [[ -z "${sha}" ]]; then
    warn "${profile}: meta missing sourceGitSha"
  elif ! git rev-parse "${sha}^{commit}" >/dev/null 2>&1; then
    warn "${profile}: sourceGitSha ${sha} not in history"
  elif ! source_sha_is_ancestor "${sha}"; then
    warn "${profile}: sourceGitSha ${sha} is not an ancestor of HEAD"
  else
    source_sha_valid=1
  fi

  if [[ -z "${collected_at}" || -z "${expires}" ]]; then
    warn "${profile}: meta missing profileCollectedAt/expiresAfterDays"
  else
    local expiry_result
    expiry_result="$(meta_expired "${collected_at}" "${expires}")"
    case "${expiry_result}" in
      EXPIRED*)
        warn "${profile}: profile expired (${expiry_result})"
        ;;
      PARSE_ERROR)
        warn "${profile}: profileCollectedAt unparseable: ${collected_at}"
        ;;
      NOW_PARSE_ERROR)
        warn "${profile}: PGO_FRESHNESS_NOW unparseable"
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
    elif [[ "${source_sha_valid}" -eq 1 ]]; then
      local count
      count="$(glob_change_count "${sha}" "${globs}")"
      if [[ "${count}" =~ ^[0-9]+$ ]] && (( count > GLOB_THRESHOLD )); then
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
if [[ ${#PROFILES[@]} -eq 0 ]]; then
  info "no default PGO profiles are adopted; validating PGO-off policy"
fi
check_stamp

if [[ "${WARN_COUNT}" -eq 0 ]]; then
  info "OK: default policy and active PGO profiles are valid"
  exit 0
fi

info "${WARN_COUNT} warning(s)"
if [[ "${STRICT}" -eq 1 ]]; then
  info "strict mode: promoting warnings to failure"
  exit 1
fi
exit 0
