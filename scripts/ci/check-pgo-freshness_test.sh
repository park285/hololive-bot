#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
GATE="${SCRIPT_DIR}/check-pgo-freshness.sh"

cd "${REPO_ROOT}"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

LAST_STATUS=0
LAST_OUTPUT=""
PASSED=0

if [[ ! -f "${GATE}" ]]; then
  echo "not ok - freshness gate missing: ${GATE}" >&2
  exit 1
fi

write_meta() {
  local path="$1"
  local generated_at="$2"
  local workload="$3"
  local expires="${4:-45}"
  local go_version="${5:-go1.26.4}"
  local sha="${6:-de9cee40612cc671430e813bfbf89b1114506458}"
  mkdir -p "$(dirname "${path}")"
  cat >"${path}" <<EOF
{
  "schemaVersion": 1,
  "service": "hololive-bot",
  "mainPackage": "./cmd/bot",
  "generatedAt": "${generated_at}",
  "sourceGitSha": "${sha}",
  "goVersion": "${go_version}",
  "profileDurationSeconds": 0,
  "workloadName": "${workload}",
  "workloadDescription": "fixture",
  "trafficMix": {},
  "comparison": {
    "cpuPercentDelta": null,
    "p95LatencyDelta": null,
    "p99LatencyDelta": null,
    "rssDelta": null,
    "binarySizeDelta": null
  },
  "acceptedBy": "fixture",
  "expiresAfterDays": ${expires}
}
EOF
}

write_approved() {
  local path="$1"
  mkdir -p "$(dirname "${path}")"
  cat >"${path}" <<'EOF'
bot-mixed-v1
retroactive-v0
EOF
}

write_globs() {
  local path="$1"
  mkdir -p "$(dirname "${path}")"
  cat >"${path}" <<'EOF'
hololive/hololive-kakao-bot-go/internal/service/matcher/**
EOF
}

run_gate() {
  set +e
  LAST_OUTPUT="$(env "$@" bash "${GATE}" "${GATE_ARGS[@]}" 2>&1)"
  LAST_STATUS=$?
  set -e
}

assert_status() {
  local name="$1"
  local want="$2"
  if [[ "${LAST_STATUS}" -ne "${want}" ]]; then
    printf 'not ok - %s want status %s got %s\n%s\n' "${name}" "${want}" "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_contains() {
  local name="$1"
  local needle="$2"
  if [[ "${LAST_OUTPUT}" != *"${needle}"* ]]; then
    printf 'not ok - %s missing %q\n%s\n' "${name}" "${needle}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

setup_profile() {
  local dir="$1"
  PROFILE="${dir}/cmd/bot/default.pgo"
  META="${dir}/cmd/bot/default.pgo.meta.json"
  GLOBS="${dir}/cmd/bot/default.pgo.hotpaths"
  APPROVED="${dir}/approved-workloads"
  mkdir -p "$(dirname "${PROFILE}")"
  printf 'profile-bytes\n' >"${PROFILE}"
  write_globs "${GLOBS}"
  write_approved "${APPROVED}"
  GATE_ARGS=(
    --profile "${PROFILE}"
    --approved-workloads "${APPROVED}"
  )
}

# Fresh, approved, non-expired meta -> warning-free OK at exit 0.
case_healthy_ok() {
  local dir="${TMP_ROOT}/healthy"
  setup_profile "${dir}"
  write_meta "${META}" "$(date -d '-1 day' --iso-8601=seconds)" "bot-mixed-v1"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "healthy meta exits 0" 0
  assert_contains "healthy meta reports ok" "OK"
}

# Expired meta -> warning, still exit 0 (warning mode).
case_expired_warns() {
  local dir="${TMP_ROOT}/expired"
  setup_profile "${dir}"
  write_meta "${META}" "$(date -d '-60 days' --iso-8601=seconds)" "bot-mixed-v1" 45
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "expired meta warns exits 0" 0
  assert_contains "expired meta warning" "WARN"
  assert_contains "expired meta mentions expiry" "expired"
}

# Unapproved workload name -> warning, exit 0.
case_unapproved_workload_warns() {
  local dir="${TMP_ROOT}/unapproved"
  setup_profile "${dir}"
  write_meta "${META}" "$(date -d '-1 day' --iso-8601=seconds)" "ghost-workload-v9"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "unapproved workload warns exits 0" 0
  assert_contains "unapproved workload warning" "WARN"
  assert_contains "unapproved workload not approved" "not in approved list"
}

# retroactive-v0 is approved but always advises regeneration.
case_retroactive_advises() {
  local dir="${TMP_ROOT}/retro"
  setup_profile "${dir}"
  write_meta "${META}" "$(date -d '-1 day' --iso-8601=seconds)" "retroactive-v0"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "retroactive advises exits 0" 0
  assert_contains "retroactive advises warning" "WARN"
  assert_contains "retroactive advises regeneration" "갱신 권고"
}

# go version mismatch -> warning, exit 0.
case_goversion_mismatch_warns() {
  local dir="${TMP_ROOT}/goversion"
  setup_profile "${dir}"
  write_meta "${META}" "$(date -d '-1 day' --iso-8601=seconds)" "bot-mixed-v1" 45 "go1.24.0"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "go version mismatch warns exits 0" 0
  assert_contains "go version mismatch warning" "WARN"
  assert_contains "go version mismatch detail" "goVersion"
}

# Missing meta sibling -> warning, exit 0.
case_missing_meta_warns() {
  local dir="${TMP_ROOT}/missing-meta"
  setup_profile "${dir}"
  rm -f "${META}"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "missing meta warns exits 0" 0
  assert_contains "missing meta warning" "WARN"
  assert_contains "missing meta detail" "meta"
}

# Glob change count over threshold -> warning, exit 0.
case_glob_threshold_warns() {
  local dir="${TMP_ROOT}/glob"
  setup_profile "${dir}"
  write_meta "${META}" "$(date -d '-1 day' --iso-8601=seconds)" "bot-mixed-v1"
  # Inject a fake git-log count via hook env (script consults PGO_FRESHNESS_GLOB_COUNT_CMD).
  local counter="${dir}/count.sh"
  cat >"${counter}" <<'EOF'
#!/usr/bin/env bash
echo 25
EOF
  chmod +x "${counter}"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_GLOB_COUNT_CMD="${counter}" PGO_FRESHNESS_GLOB_THRESHOLD=20
  assert_status "glob threshold warns exits 0" 0
  assert_contains "glob threshold warning" "WARN"
  assert_contains "glob threshold detail" "hot path"
}

# --strict promotes any warning to nonzero exit.
case_strict_nonzero() {
  local dir="${TMP_ROOT}/strict"
  setup_profile "${dir}"
  write_meta "${META}" "$(date -d '-60 days' --iso-8601=seconds)" "bot-mixed-v1" 45
  GATE_ARGS+=(--strict)
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  if [[ "${LAST_STATUS}" -eq 0 ]]; then
    printf 'not ok - strict mode should exit nonzero on warning\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - strict mode exits nonzero on warning\n'
  assert_contains "strict mode still prints warning" "WARN"
}

case_healthy_ok
case_expired_warns
case_unapproved_workload_warns
case_retroactive_advises
case_goversion_mismatch_warns
case_missing_meta_warns
case_glob_threshold_warns
case_strict_nonzero

printf 'ok - %s pgo freshness fixture checks passed\n' "${PASSED}"
