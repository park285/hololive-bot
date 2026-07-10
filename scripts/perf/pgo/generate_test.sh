#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
GENERATOR="${SCRIPT_DIR}/generate.sh"

cd "${REPO_ROOT}"
source scripts/perf/pgo/generate_meta_cases.sh
source scripts/perf/pgo/generate_nfr_cases.sh

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT
MAIN_PACKAGE="${TMP_ROOT}/profile-app"

LAST_STATUS=0
LAST_OUTPUT=""
PASSED=0

if [[ ! -f "${GENERATOR}" ]]; then
  echo "not ok - generator missing: ${GENERATOR}" >&2
  exit 1
fi

materialize_profile_app() {
  mkdir -p "${MAIN_PACKAGE}"
  cp scripts/perf/pgo/testdata/profile-app/main.go.txt "${MAIN_PACKAGE}/main.go"
  cp scripts/perf/pgo/testdata/profile-app/go.mod.txt "${MAIN_PACKAGE}/go.mod"
}

write_workload() {
  local path="$1"
  local extra="${2:-}"
  mkdir -p "$(dirname "${path}")"
  cat >"${path}" <<EOF
schemaVersion: 1
name: bot-mixed-v1
service: hololive-bot
duration: 600s
traffic:
  kakao_message: 40
  iris_reply: 25
  llm_parse: 20
  db_valkey: 10
  admin: 5
drivers:
  kakao_message: { tool: tools/load/iris-webhook-replay, args: "--rate 20/s" }
  iris_reply: { tool: tools/load/iris-reply-burst, args: "--rooms 20 --rate 10/s" }
  llm_parse: { tool: mock, fixture: testdata/llm-responses/ }
  db_valkey: { tool: builtin }
  admin: { tool: curl-script, path: scripts/perf/pgo/admin-mix.sh }
${extra}
EOF
}

write_bad_traffic_workload() {
  local path="$1"
  mkdir -p "$(dirname "${path}")"
  cat >"${path}" <<'EOF'
schemaVersion: 1
name: bot-mixed-v1
service: hololive-bot
duration: 600s
traffic:
  kakao_message: 90
  iris_reply: 9
drivers:
  kakao_message: { tool: tools/load/iris-webhook-replay, args: "--rate 20/s" }
  iris_reply: { tool: tools/load/iris-reply-burst, args: "--rooms 20 --rate 10/s" }
EOF
}

write_missing_driver_workload() {
  local path="$1"
  mkdir -p "$(dirname "${path}")"
  cat >"${path}" <<'EOF'
schemaVersion: 1
name: bot-mixed-v1
service: hololive-bot
duration: 600s
traffic:
  kakao_message: 40
  iris_reply: 60
drivers:
  kakao_message: { tool: tools/load/iris-webhook-replay, args: "--rate 20/s" }
EOF
}

write_collector() {
  local path="$1"
  cat >"${path}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

: "${PGO_CANDIDATE_PROFILE:?}"
: "${PGO_PROFILE_BINARY:?}"
: "${PGO_COMPARISON_JSON:?}"
: "${PGO_ARTIFACT_DIR:?}"
: "${PGO_TEST_CPU_DELTA:?}"
: "${PGO_TEST_P95_DELTA:?}"
: "${PGO_TEST_P99_DELTA:?}"
: "${PGO_TEST_RSS_DELTA:?}"
: "${PGO_TEST_BINARY_DELTA:?}"
: "${PGO_TEST_HOT_BENCH_DELTA:?}"

mkdir -p "$(dirname "${PGO_CANDIDATE_PROFILE}")" "${PGO_ARTIFACT_DIR}"
(cd "${PGO_MAIN}" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "${PGO_PROFILE_BINARY}" .)
"${PGO_PROFILE_BINARY}" 2s 3>"${PGO_CANDIDATE_PROFILE}"
duration="$(go tool pprof -raw "${PGO_CANDIDATE_PROFILE}" 2>/dev/null | awk '$1 == "Duration:" {print $2; exit}')"
repeat_count="$(python3 -c 'import math,sys; print(math.ceil(601 / float(sys.argv[1])))' "${duration}")"
inputs=()
for ((i = 0; i < repeat_count; i++)); do
  inputs+=("${PGO_CANDIDATE_PROFILE}")
done
go tool pprof -proto -output="${PGO_CANDIDATE_PROFILE}.merged" "${inputs[@]}" >/dev/null 2>&1
mv "${PGO_CANDIDATE_PROFILE}.merged" "${PGO_CANDIDATE_PROFILE}"
printf 'before-pprof\n' >"${PGO_ARTIFACT_DIR}/pprof-before.pb.gz"
printf 'after-pprof\n' >"${PGO_ARTIFACT_DIR}/pprof-after.pb.gz"
printf 'before-bench\n' >"${PGO_ARTIFACT_DIR}/bench-before.txt"
printf 'after-bench\n' >"${PGO_ARTIFACT_DIR}/bench-after.txt"
cat >"${PGO_COMPARISON_JSON}" <<EOFJSON
{
  "cpuPercentDelta": ${PGO_TEST_CPU_DELTA},
  "p95LatencyDelta": ${PGO_TEST_P95_DELTA},
  "p99LatencyDelta": ${PGO_TEST_P99_DELTA},
  "rssDelta": ${PGO_TEST_RSS_DELTA},
  "binarySizeDelta": ${PGO_TEST_BINARY_DELTA},
  "hotBenchmarkPercentDelta": ${PGO_TEST_HOT_BENCH_DELTA}
}
EOFJSON
EOF
  chmod +x "${path}"
}

run_generator() {
  local artifact_root="$1"
  local artifact_date="$2"
  shift 2
  set +e
  LAST_OUTPUT="$(
    PGO_ARTIFACT_ROOT="${artifact_root}" \
    PGO_ARTIFACT_DATE="${artifact_date}" \
    bash "${GENERATOR}" "$@" 2>&1
  )"
  LAST_STATUS=$?
  set -e
}

run_generator_with_metrics() {
  local artifact_root="$1"
  local artifact_date="$2"
  local cpu_delta="$3"
  local p95_delta="$4"
  local p99_delta="$5"
  local rss_delta="$6"
  local binary_delta="$7"
  local hot_bench_delta="$8"
  shift 8
  set +e
  LAST_OUTPUT="$(
    PGO_ARTIFACT_ROOT="${artifact_root}" \
    PGO_ARTIFACT_DATE="${artifact_date}" \
    PGO_TEST_CPU_DELTA="${cpu_delta}" \
    PGO_TEST_P95_DELTA="${p95_delta}" \
    PGO_TEST_P99_DELTA="${p99_delta}" \
    PGO_TEST_RSS_DELTA="${rss_delta}" \
    PGO_TEST_BINARY_DELTA="${binary_delta}" \
    PGO_TEST_HOT_BENCH_DELTA="${hot_bench_delta}" \
    bash "${GENERATOR}" "$@" 2>&1
  )"
  LAST_STATUS=$?
  set -e
}

run_validate_meta() {
  set +e
  LAST_OUTPUT="$(bash "${GENERATOR}" validate-meta "$1" 2>&1)"
  LAST_STATUS=$?
  set -e
}

run_validate_profile() {
  set +e
  LAST_OUTPUT="$(bash "${GENERATOR}" validate-profile "$1" "$2" 2>&1)"
  LAST_STATUS=$?
  set -e
}

assert_success() {
  local name="$1"
  if [[ "${LAST_STATUS}" -ne 0 ]]; then
    printf 'not ok - %s\nstatus=%s\n%s\n' "${name}" "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_failure() {
  local name="$1"
  if [[ "${LAST_STATUS}" -eq 0 ]]; then
    printf 'not ok - %s unexpectedly succeeded\n%s\n' "${name}" "${LAST_OUTPUT}" >&2
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
}

assert_file_exists() {
  local name="$1"
  local path="$2"
  if [[ ! -f "${path}" ]]; then
    printf 'not ok - %s missing file: %s\n' "${name}" "${path}" >&2
    exit 1
  fi
}

case_duration_below_600_errors() {
  local dir="${TMP_ROOT}/duration"
  local workload="${dir}/workload.yaml"
  write_workload "${workload}"
  run_generator "${dir}/artifacts/perf/pgo" "2026-06-12" \
    --service hololive-bot \
    --main "${MAIN_PACKAGE}" \
    --duration 599s \
    --workload "${workload}" \
    --output "${dir}/default.pgo"
  assert_failure "duration below 600s errors"
  assert_contains "duration below 600s errors" "대표성 미달"
}

case_traffic_sum_errors() {
  local dir="${TMP_ROOT}/traffic-sum"
  local workload="${dir}/workload.yaml"
  write_bad_traffic_workload "${workload}"
  run_generator "${dir}/artifacts/perf/pgo" "2026-06-12" \
    --service hololive-bot \
    --main "${MAIN_PACKAGE}" \
    --duration 600s \
    --workload "${workload}" \
    --output "${dir}/default.pgo"
  assert_failure "traffic sum errors"
  assert_contains "traffic sum errors" "traffic ratios sum"
}

case_missing_driver_errors() {
  local dir="${TMP_ROOT}/missing-driver"
  local workload="${dir}/workload.yaml"
  write_missing_driver_workload "${workload}"
  run_generator "${dir}/artifacts/perf/pgo" "2026-06-12" \
    --service hololive-bot \
    --main "${MAIN_PACKAGE}" \
    --duration 600s \
    --workload "${workload}" \
    --output "${dir}/default.pgo"
  assert_failure "missing driver errors"
  assert_contains "missing driver errors" "missing driver"
}

case_unknown_workload_field_errors() {
  local dir="${TMP_ROOT}/unknown-field"
  local workload="${dir}/workload.yaml"
  write_workload "${workload}" "unexpected: true"
  run_generator "${dir}/artifacts/perf/pgo" "2026-06-12" \
    --service hololive-bot \
    --main "${MAIN_PACKAGE}" \
    --duration 600s \
    --workload "${workload}" \
    --output "${dir}/default.pgo"
  assert_failure "unknown workload field errors"
  assert_contains "unknown workload field errors" "unknown field: workload.unexpected"
}

case_rejection_preserves_default_pgo() {
  local dir="${TMP_ROOT}/reject"
  local workload="${dir}/workload.yaml"
  local collector="${dir}/collector.sh"
  local output="${dir}/default.pgo"
  write_workload "${workload}"
  write_collector "${collector}"
  printf 'existing-profile\n' >"${output}"
  cp "${output}" "${dir}/before-default.pgo"
  run_generator_with_metrics "${dir}/artifacts/perf/pgo" "2026-06-12" \
    -3 0 1 1 2 0 \
    --service hololive-bot \
    --main "${MAIN_PACKAGE}" \
    --duration 600s \
    --workload "${workload}" \
    --output "${output}" \
    --collect-cmd "${collector}"
  assert_failure "rejection verdict exits nonzero"
  assert_contains "rejection verdict exits nonzero" "REJECTED"
  if ! cmp -s "${output}" "${dir}/before-default.pgo"; then
    printf 'not ok - rejection preserved default.pgo bytes\n' >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - rejection preserved default.pgo bytes\n'
}

case_acceptance_writes_output_contract() {
  local dir="${TMP_ROOT}/accept"
  local workload="${dir}/workload.yaml"
  local collector="${dir}/collector.sh"
  local output="${dir}/default.pgo"
  local artifact_dir="${dir}/artifacts/perf/pgo/hololive-bot/2026-06-12"
  write_workload "${workload}"
  write_collector "${collector}"
  run_generator_with_metrics "${dir}/artifacts/perf/pgo" "2026-06-12" \
    -3 0 0 1 2 0 \
    --service hololive-bot \
    --main "${MAIN_PACKAGE}" \
    --duration 600s \
    --workload "${workload}" \
    --output "${output}" \
    --collect-cmd "${collector}"
  assert_success "acceptance verdict succeeds"
  assert_contains "acceptance verdict succeeds" "ACCEPTED"
  assert_file_exists "acceptance output" "${output}"
  assert_file_exists "acceptance meta" "${output}.meta.json"
  assert_file_exists "acceptance report" "${artifact_dir}/pgo-compare-report.md"
  assert_file_exists "acceptance pprof before" "${artifact_dir}/pprof-before.pb.gz"
  assert_file_exists "acceptance pprof after" "${artifact_dir}/pprof-after.pb.gz"
  assert_file_exists "acceptance bench before" "${artifact_dir}/bench-before.txt"
  assert_file_exists "acceptance bench after" "${artifact_dir}/bench-after.txt"
  python3 - "${output}.meta.json" <<'PY'
import json, re, sys

data = json.load(open(sys.argv[1]))
for field in ("profileSha256", "collectionBinarySha256"):
    assert re.fullmatch(r"[0-9a-f]{64}", data[field]), field
for field in ("profileExecutable", "profileBuildId", "profileMainPackage"):
    assert data[field], field
PY
  PASSED=$((PASSED + 1))
  printf 'ok - acceptance metadata records bound profile provenance\n'
}

case_hot_benchmark_regression_rejects() {
  local dir="${TMP_ROOT}/hot-bench-reject"
  local workload="${dir}/workload.yaml"
  local collector="${dir}/collector.sh"
  local output="${dir}/default.pgo"
  write_workload "${workload}"
  write_collector "${collector}"
  run_generator_with_metrics "${dir}/artifacts/perf/pgo" "2026-06-12" \
    -5 -2 0 1 2 -3.1 \
    --service hololive-bot \
    --main "${MAIN_PACKAGE}" \
    --duration 600s \
    --workload "${workload}" \
    --output "${output}" \
    --collect-cmd "${collector}"
  assert_failure "hot benchmark regression rejects candidate"
  assert_contains "hot benchmark regression rejection reason" "hot benchmark regression > 3%"
  if [[ -e "${output}" ]]; then
    printf 'not ok - rejected hot benchmark candidate must not be installed\n' >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - rejected hot benchmark candidate was not installed\n'
}

materialize_profile_app
case_duration_below_600_errors
case_traffic_sum_errors
case_missing_driver_errors
case_extra_driver_errors
case_unknown_workload_field_errors
case_rejection_preserves_default_pgo
case_acceptance_writes_output_contract
case_hot_benchmark_regression_rejects
case_cpu_regression_rejects
case_p95_regression_rejects
case_profile_collection_time_is_bound
case_validate_meta_rejects_missing_required
case_validate_meta_rejects_replayed_bad_verdict
case_validate_meta_rejects_nonfinite_delta
case_validate_meta_rejects_naive_generated_at
case_validate_meta_rejects_future_generated_at
case_validate_meta_rejects_untrusted_acceptor
case_validate_meta_rejects_excessive_expiry

printf 'ok - %s pgo generator fixture checks passed\n' "${PASSED}"
