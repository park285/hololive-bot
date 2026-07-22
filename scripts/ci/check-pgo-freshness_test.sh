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
SOURCE_SHA="$(git rev-parse HEAD)"
PROFILE_TEMPLATE="${TMP_ROOT}/profile-fixture/default.pgo"
PROFILE_BINARY="${TMP_ROOT}/profile-fixture/profile-app"
PROFILE_SOURCE="${TMP_ROOT}/profile-source"
PROFILE_SHA=""
PROFILE_BUILD_ID=""
PROFILE_BINARY_SHA=""
PROFILE_MAIN=""
PROFILE_DURATION=""
PROFILE_COLLECTED_AT=""

if [[ ! -f "${GATE}" ]]; then
  echo "not ok - freshness gate missing: ${GATE}" >&2
  exit 1
fi

write_meta() {
  local path="$1"
  local generated_at="$2"
  local workload="$3"
  local expires="${4:-45}"
  local go_version="${5:-go1.26.5}"
  local sha="${6:-${SOURCE_SHA}}"
  mkdir -p "$(dirname "${path}")"
  cat >"${path}" <<EOF
{
  "schemaVersion": 1,
  "service": "hololive-bot",
  "mainPackage": "${PROFILE_SOURCE}",
  "generatedAt": "${generated_at}",
  "profileCollectedAt": "${PROFILE_COLLECTED_AT}",
  "sourceGitSha": "${sha}",
  "goVersion": "${go_version}",
  "profileDurationSeconds": ${PROFILE_DURATION},
  "profileSha256": "${PROFILE_SHA}",
  "profileExecutable": "profile-app",
  "profileBuildId": "${PROFILE_BUILD_ID}",
  "collectionBinarySha256": "${PROFILE_BINARY_SHA}",
  "profileMainPackage": "${PROFILE_MAIN}",
  "workloadName": "${workload}",
  "workloadDescription": "fixture",
  "trafficMix": {"fixture": 1.0},
  "comparison": {
    "cpuPercentDelta": -3.0,
    "p95LatencyDelta": -1.0,
    "p99LatencyDelta": 0.0,
    "rssDelta": 1.0,
    "binarySizeDelta": 1.0,
    "hotBenchmarkPercentDelta": 4.0
  },
  "acceptedBy": "scripts/perf/pgo/generate.sh",
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
hololive/hololive-api/internal/planes/bot/internal/service/matcher/**
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
  cp "${PROFILE_TEMPLATE}" "${PROFILE}"
  write_globs "${GLOBS}"
  write_approved "${APPROVED}"
  GATE_ARGS=(
    --profile "${PROFILE}"
    --approved-workloads "${APPROVED}"
  )
}

prepare_profile_fixture() {
  local duration repeat_count merged i
  mkdir -p "$(dirname "${PROFILE_TEMPLATE}")"
  mkdir -p "${PROFILE_SOURCE}"
  cp scripts/perf/pgo/testdata/profile-app/main.go.txt "${PROFILE_SOURCE}/main.go"
  cp scripts/perf/pgo/testdata/profile-app/go.mod.txt "${PROFILE_SOURCE}/go.mod"
  (cd "${PROFILE_SOURCE}" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "${PROFILE_BINARY}" .)
  "${PROFILE_BINARY}" 2s 3>"${PROFILE_TEMPLATE}"
  duration="$(go tool pprof -raw "${PROFILE_TEMPLATE}" 2>/dev/null | awk '$1 == "Duration:" {print $2; exit}')"
  repeat_count="$(python3 -c 'import math,sys; print(math.ceil(601 / float(sys.argv[1])))' "${duration}")"
  merged="${PROFILE_TEMPLATE}.merged"
  inputs=()
  for ((i = 0; i < repeat_count; i++)); do
    inputs+=("${PROFILE_TEMPLATE}")
  done
  go tool pprof -proto -output="${merged}" "${inputs[@]}" >/dev/null 2>&1
  mv "${merged}" "${PROFILE_TEMPLATE}"
  raw_duration="$(go tool pprof -raw "${PROFILE_TEMPLATE}" 2>/dev/null | awk '$1 == "Duration:" {print $2; exit}')"
  PROFILE_DURATION="$(python3 - "${raw_duration}" <<'PY'
import re, sys

raw = sys.argv[1]
if re.fullmatch(r"\d+(?:\.\d*)?", raw):
    print(round(float(raw)))
    raise SystemExit
match = re.fullmatch(r"(?:(\d+(?:\.\d*)?)h)?(?:(\d+(?:\.\d*)?)m)?(\d+(?:\.\d*)?)s?", raw)
if not match:
    raise SystemExit(f"invalid pprof duration: {raw}")
hours, minutes, seconds = (float(value or 0) for value in match.groups())
print(round(hours * 3600 + minutes * 60 + seconds))
PY
)"
  PROFILE_SHA="$(sha256sum "${PROFILE_TEMPLATE}" | awk '{print $1}')"
  PROFILE_BUILD_ID="$(go tool pprof -top "${PROFILE_TEMPLATE}" 2>&1 | sed -n 's/^Build ID: //p')"
  profile_time="$(TZ=UTC go tool pprof -top "${PROFILE_TEMPLATE}" 2>&1 | sed -n 's/^Time: //p')"
  PROFILE_COLLECTED_AT="$(date -u -d "${profile_time}" '+%Y-%m-%dT%H:%M:%S+00:00')"
  PROFILE_BINARY_SHA="$(sha256sum "${PROFILE_BINARY}" | awk '{print $1}')"
  PROFILE_MAIN="$(cd "${PROFILE_SOURCE}" && go list -buildvcs=false -f '{{.ImportPath}}' .)"
}

# Fresh, approved, non-expired meta -> warning-free OK at exit 0.
case_healthy_ok() {
  local dir="${TMP_ROOT}/healthy"
  setup_profile "${dir}"
  write_meta "${META}" "$(date --iso-8601=seconds)" "bot-mixed-v1"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "healthy meta exits 0" 0
  assert_contains "healthy meta reports ok" "OK"
}

# Expired meta -> warning, still exit 0 (warning mode).
case_expired_warns() {
  local dir="${TMP_ROOT}/expired"
  setup_profile "${dir}"
  write_meta "${META}" "$(date --iso-8601=seconds)" "bot-mixed-v1" 45
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1 PGO_FRESHNESS_NOW="$(date -d '+60 days' --iso-8601=seconds)"
  assert_status "expired meta warns exits 0" 0
  assert_contains "expired meta warning" "WARN"
  assert_contains "expired meta mentions expiry" "expired"
}

# Unapproved workload name -> warning, exit 0.
case_unapproved_workload_warns() {
  local dir="${TMP_ROOT}/unapproved"
  setup_profile "${dir}"
  write_meta "${META}" "$(date --iso-8601=seconds)" "ghost-workload-v9"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "unapproved workload warns exits 0" 0
  assert_contains "unapproved workload warning" "WARN"
  assert_contains "unapproved workload not approved" "not in approved list"
}

# retroactive-v0 is approved but always advises regeneration.
case_retroactive_advises() {
  local dir="${TMP_ROOT}/retro"
  setup_profile "${dir}"
  write_meta "${META}" "$(date --iso-8601=seconds)" "retroactive-v0"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "retroactive advises exits 0" 0
  assert_contains "retroactive advises warning" "WARN"
  assert_contains "retroactive advises regeneration" "갱신 권고"
}

# go version mismatch -> warning, exit 0.
case_goversion_mismatch_warns() {
  local dir="${TMP_ROOT}/goversion"
  setup_profile "${dir}"
  write_meta "${META}" "$(date --iso-8601=seconds)" "bot-mixed-v1" 45 "go1.24.0"
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
  write_meta "${META}" "$(date --iso-8601=seconds)" "bot-mixed-v1"
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

case_nonancestor_source_sha_warns() {
  local dir="${TMP_ROOT}/nonancestor"
  setup_profile "${dir}"
  write_meta "${META}" "$(date --iso-8601=seconds)" "bot-mixed-v1"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1 PGO_FRESHNESS_ANCESTOR_CMD=false
  assert_status "nonancestor source SHA warns exits 0" 0
  assert_contains "nonancestor source SHA warning" "not an ancestor of HEAD"
}

case_unknown_source_sha_warns() {
  local dir="${TMP_ROOT}/unknown-sha"
  setup_profile "${dir}"
  write_meta "${META}" "$(date --iso-8601=seconds)" "bot-mixed-v1" 45 "$(go env GOVERSION)" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1
  assert_status "unknown source SHA warns exits 0" 0
  assert_contains "unknown source SHA warning" "not in history"
}

# --strict promotes any warning to nonzero exit.
case_strict_nonzero() {
  local dir="${TMP_ROOT}/strict"
  setup_profile "${dir}"
  write_meta "${META}" "$(date --iso-8601=seconds)" "bot-mixed-v1" 45
  GATE_ARGS+=(--strict)
  run_gate PGO_FRESHNESS_SKIP_STAMP=1 PGO_FRESHNESS_SKIP_GLOB=1 PGO_FRESHNESS_NOW="$(date -d '+60 days' --iso-8601=seconds)"
  if [[ "${LAST_STATUS}" -eq 0 ]]; then
    printf 'not ok - strict mode should exit nonzero on warning\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - strict mode exits nonzero on warning\n'
  assert_contains "strict mode still prints warning" "WARN"
}

prepare_profile_fixture
case_healthy_ok
case_expired_warns
case_unapproved_workload_warns
case_retroactive_advises
case_goversion_mismatch_warns
case_missing_meta_warns
case_glob_threshold_warns
case_nonancestor_source_sha_warns
case_unknown_source_sha_warns
case_strict_nonzero

printf 'ok - %s pgo freshness fixture checks passed\n' "${PASSED}"
