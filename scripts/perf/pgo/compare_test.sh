#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
COMPARE="${SCRIPT_DIR}/compare-pgo.sh"

cd "${REPO_ROOT}"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

LAST_STATUS=0
LAST_OUTPUT=""
PASSED=0

if [[ ! -f "${COMPARE}" ]]; then
  echo "not ok - compare script missing: ${COMPARE}" >&2
  exit 1
fi

write_workload() {
  local path="$1"
  mkdir -p "$(dirname "${path}")"
  cat >"${path}" <<'EOF'
schemaVersion: 1
name: bot-mixed-v1
service: hololive-bot
duration: 600s
traffic:
  kakao_message: 100
drivers:
  kakao_message: { tool: builtin }
EOF
}

# Build hook: writes two binaries of controllable size.
write_build_cmd() {
  local path="$1"
  cat >"${path}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${PGO_BUILD_MODE:?}"
: "${PGO_BUILD_OUTPUT:?}"
: "${PGO_TEST_OFF_BYTES:?}"
: "${PGO_TEST_ON_BYTES:?}"
if [[ "${PGO_BUILD_MODE}" == "off" ]]; then
  head -c "${PGO_TEST_OFF_BYTES}" /dev/zero >"${PGO_BUILD_OUTPUT}"
else
  head -c "${PGO_TEST_ON_BYTES}" /dev/zero >"${PGO_BUILD_OUTPUT}"
fi
EOF
  chmod +x "${path}"
}

# Bench hook: emits a multi-line go-test-bench output (one row per repetition)
# so mean parsing is exercised. Off rows = PGO_TEST_BENCH_OFF_NS_LIST (comma list),
# on rows = PGO_TEST_BENCH_ON_NS_LIST.
write_bench_cmd() {
  local path="$1"
  cat >"${path}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${PGO_BENCH_MODE:?}"
: "${PGO_BENCH_OUTPUT:?}"
: "${PGO_BENCH_NAME:?}"
if [[ "${PGO_BENCH_MODE}" == "off" ]]; then
  list="${PGO_TEST_BENCH_OFF_NS_LIST:-1000}"
else
  list="${PGO_TEST_BENCH_ON_NS_LIST:-1000}"
fi
{
  echo "goos: linux"
  echo "goarch: amd64"
  echo "pkg: example/pkg"
  IFS=','
  for ns in ${list}; do
    printf '%s-12   \t   1000\t      %s ns/op\t      32 B/op\t       2 allocs/op\n' "${PGO_BENCH_NAME}" "${ns}"
  done
  echo "PASS"
  echo "ok  	example/pkg	0.020s"
} >"${PGO_BENCH_OUTPUT}"
EOF
  chmod +x "${path}"
}

# Live metrics hook: emits a JSON object with cpu/p95/p99/rss so ACCEPTED/REJECTED
# verdict paths (which require measured rejection metrics) can be exercised.
write_live_cmd() {
  local path="$1"
  cat >"${path}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${PGO_LIVE_OUTPUT:?}"
cat >"${PGO_LIVE_OUTPUT}" <<JSON
{
  "cpuPercentDelta": ${PGO_TEST_CPU:-null},
  "p95LatencyDelta": ${PGO_TEST_P95:-null},
  "p99LatencyDelta": ${PGO_TEST_P99:-null},
  "rssDelta": ${PGO_TEST_RSS:-null}
}
JSON
EOF
  chmod +x "${path}"
}

run_compare() {
  set +e
  LAST_OUTPUT="$(env "$@" bash "${COMPARE}" "${COMPARE_ARGS[@]}" 2>&1)"
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

assert_file_exists() {
  local name="$1"
  local path="$2"
  if [[ ! -f "${path}" ]]; then
    printf 'not ok - %s missing file: %s\n' "${name}" "${path}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_close() {
  local name="$1"
  local got="$2"
  local want="$3"
  if ! python3 -c "import sys; sys.exit(0 if abs(float('${got}') - float('${want}')) < 1e-6 else 1)"; then
    printf 'not ok - %s want %s got %s\n' "${name}" "${want}" "${got}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

json_field() {
  local file="$1"
  local field="$2"
  python3 - "$file" "$field" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
val = data[sys.argv[2]]
print("null" if val is None else val)
PY
}

setup_case() {
  local dir="$1"
  WORKLOAD="${dir}/workload.yaml"
  BUILD_CMD="${dir}/build.sh"
  BENCH_CMD="${dir}/bench.sh"
  LIVE_CMD="${dir}/live.sh"
  OUTPUT_DIR="${dir}/out"
  write_workload "${WORKLOAD}"
  write_build_cmd "${BUILD_CMD}"
  write_bench_cmd "${BENCH_CMD}"
  write_live_cmd "${LIVE_CMD}"
  COMPARE_ARGS=(
    --service hololive-bot
    --main ./hololive/hololive-kakao-bot-go/cmd/bot
    --profile hololive/hololive-kakao-bot-go/cmd/bot/default.pgo
    --workload "${WORKLOAD}"
    --output-dir "${OUTPUT_DIR}"
    --build-cmd "${BUILD_CMD}"
    --bench-cmd "${BENCH_CMD}"
  )
}

# --- I3: without measured rejection metrics, adoption signal -> HELD (not ACCEPTED) ---

# hot bench +5% improvement but p99/RSS unmeasured -> HELD, not ACCEPTED.
case_adoption_signal_but_unmeasured_holds() {
  local dir="${TMP_ROOT}/adopt-held"
  setup_case "${dir}"
  # off 1000 -> on 950 = +5% hot bench improvement.
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=950
  assert_status "adoption signal, p99/RSS unmeasured -> HELD exits 3" 3
  assert_contains "adoption-held verdict" "HELD"
  assert_contains "adoption-held reason" "adoption signal present"
  assert_contains "adoption-held reason mentions unmeasured" "rejection metrics unmeasured"
}

# binary +5.0% exactly is within budget (boundary not exceeded); still HELD without live metrics.
case_binary_boundary_pass() {
  local dir="${TMP_ROOT}/bin-pass"
  setup_case "${dir}"
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=105000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=950
  assert_status "binary +5.0 within budget not rejected (HELD) exits 3" 3
  assert_contains "binary +5.0 not REJECTED" "HELD"
  assert_file_exists "report generated" "${OUTPUT_DIR}/pgo-compare-report.md"
  assert_file_exists "comparison json generated" "${OUTPUT_DIR}/comparison.json"
  local bin
  bin="$(json_field "${OUTPUT_DIR}/comparison.json" binarySizeDelta)"
  assert_close "binarySizeDelta computed as +5" "${bin}" "5.0"
}

# binary +5.1% must be rejected on binary budget regardless of live metrics.
case_binary_boundary_reject() {
  local dir="${TMP_ROOT}/bin-reject"
  setup_case "${dir}"
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=105100 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=900
  assert_status "binary +5.1 rejected exits 2" 2
  assert_contains "binary +5.1 verdict REJECTED" "REJECTED"
  assert_contains "binary +5.1 reason" "binary size delta > +5%"
}

# --- I3: with measured rejection metrics, ACCEPTED/REJECTED become reachable ---

# adoption signal + measured p99/RSS passing -> ACCEPTED.
case_measured_metrics_accept() {
  local dir="${TMP_ROOT}/accept"
  setup_case "${dir}"
  COMPARE_ARGS+=(--live-cmd "${LIVE_CMD}")
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=950 \
    PGO_TEST_CPU=-1 PGO_TEST_P95=-2 PGO_TEST_P99=0 PGO_TEST_RSS=1
  assert_status "measured pass + adoption -> ACCEPTED exits 0" 0
  assert_contains "measured accept verdict" "ACCEPTED"
  local p99 rss
  p99="$(json_field "${OUTPUT_DIR}/comparison.json" p99LatencyDelta)"
  rss="$(json_field "${OUTPUT_DIR}/comparison.json" rssDelta)"
  assert_close "p99 measured recorded" "${p99}" "0"
  assert_close "rss measured recorded" "${rss}" "1"
}

# measured p99 regression -> REJECTED even with hot bench improvement.
case_measured_p99_regression_reject() {
  local dir="${TMP_ROOT}/p99-reject"
  setup_case "${dir}"
  COMPARE_ARGS+=(--live-cmd "${LIVE_CMD}")
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=900 \
    PGO_TEST_CPU=-5 PGO_TEST_P95=0 PGO_TEST_P99=0.5 PGO_TEST_RSS=1
  assert_status "measured p99 regression -> REJECTED exits 2" 2
  assert_contains "measured p99 reject reason" "p99 latency regression > 0%"
}

# --- I4+M8: mean parsing and env value validation ---

# Bench parsing uses mean across repetitions, not min.
case_bench_mean_parsing() {
  local dir="${TMP_ROOT}/mean"
  setup_case "${dir}"
  # off mean = (1000+1100)/2 = 1050 ; on mean = (900+950)/2 = 925
  # improvement = (1050-925)/1050*100 = 11.9048%. min() would give (1000-900)/1000=10%.
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST="1000,1100" PGO_TEST_BENCH_ON_NS_LIST="900,950"
  # verdict HELD (no live metrics) but hot bench delta must reflect mean.
  local hb
  hb="$(json_field "${OUTPUT_DIR}/comparison.json" hotBenchmarkPercentDelta)"
  assert_close "hot bench delta uses mean (11.9048)" "${hb}" "11.904761904761905"
}

# PGO_BENCHCOUNT must be a pure value ("2"); a flag form ("-count=2") is an error.
case_env_value_form_rejects_flag() {
  local dir="${TMP_ROOT}/envflag"
  setup_case "${dir}"
  run_compare \
    PGO_BENCHCOUNT="-count=2" \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=950
  if [[ "${LAST_STATUS}" -eq 0 || "${LAST_STATUS}" -eq 3 ]]; then
    printf 'not ok - flag-form PGO_BENCHCOUNT must error, got status %s\n%s\n' "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - flag-form PGO_BENCHCOUNT rejected\n'
  assert_contains "env value form error message" "PGO_BENCHCOUNT"
}

case_env_value_form_rejects_benchtime_flag() {
  local dir="${TMP_ROOT}/envbtflag"
  setup_case "${dir}"
  run_compare \
    PGO_BENCHTIME="-benchtime=2s" \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=950
  if [[ "${LAST_STATUS}" -eq 0 || "${LAST_STATUS}" -eq 3 ]]; then
    printf 'not ok - flag-form PGO_BENCHTIME must error, got status %s\n%s\n' "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - flag-form PGO_BENCHTIME rejected\n'
  assert_contains "env benchtime value form error message" "PGO_BENCHTIME"
}

# --- I5: generator field + collector-incompatible note ---

case_generator_field_present() {
  local dir="${TMP_ROOT}/generator"
  setup_case "${dir}"
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=950
  local cj="${OUTPUT_DIR}/comparison.json"
  assert_file_exists "comparison json exists" "${cj}"
  local gen
  gen="$(json_field "${cj}" generator)"
  if [[ "${gen}" != "compare-pgo" ]]; then
    printf 'not ok - generator field want compare-pgo got %q\n' "${gen}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - comparison json carries generator=compare-pgo\n'
  local report="${OUTPUT_DIR}/pgo-compare-report.md"
  if ! grep -q 'generate.sh' "${report}"; then
    printf 'not ok - report must note generate.sh collector incompatibility\n' >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - report notes generate.sh collector incompatibility\n'
}

case_help_notes_collector_incompatibility() {
  set +e
  local out
  out="$(bash "${COMPARE}" --help 2>&1)"
  set -e
  if [[ "${out}" != *"generate.sh"* ]]; then
    printf 'not ok - --help must mention generate.sh collector incompatibility\n%s\n' "${out}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - --help notes generate.sh collector incompatibility\n'
}

# Report records live metrics as explicit N/A and the load-tool blocker.
case_report_na_contract() {
  local dir="${TMP_ROOT}/report-na"
  setup_case "${dir}"
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=940
  local report="${OUTPUT_DIR}/pgo-compare-report.md"
  assert_file_exists "report exists" "${report}"
  if ! grep -q 'cpuPercentDelta: N/A' "${report}"; then
    printf 'not ok - report must mark cpuPercentDelta N/A\n%s\n' "$(cat "${report}")" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - report marks cpu N/A\n'
  if ! grep -q '측정 불가' "${report}"; then
    printf 'not ok - report must note 측정 불가 load-tool blocker\n' >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - report notes load-tool blocker\n'
}

# comparison.json carries the full COLLECT schema keys.
case_comparison_schema_keys() {
  local dir="${TMP_ROOT}/schema"
  setup_case "${dir}"
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=940
  local cj="${OUTPUT_DIR}/comparison.json"
  assert_file_exists "comparison json exists" "${cj}"
  python3 - "${cj}" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
required = {
  "cpuPercentDelta","p95LatencyDelta","p99LatencyDelta",
  "rssDelta","binarySizeDelta","hotBenchmarkPercentDelta",
}
missing = required - set(data)
if missing:
    print("MISSING:" + ",".join(sorted(missing)))
    sys.exit(1)
PY
  PASSED=$((PASSED + 1))
  printf 'ok - comparison json carries full COLLECT schema keys\n'
}

# --- I2: alarm-worker hot bench selection via hotpaths glob ---

case_alarm_worker_selects_via_hotpaths() {
  local dir="${TMP_ROOT}/alarm-select"
  mkdir -p "${dir}/cmd/alarm-worker"
  # Synthetic profile + hotpaths sibling.
  local profile="${dir}/cmd/alarm-worker/default.pgo"
  printf 'profile\n' >"${profile}"
  cat >"${profile}.hotpaths" <<'EOF'
hololive/hololive-shared/pkg/service/alarm/**
hololive/hololive-shared/pkg/service/youtube/outbox/**
EOF
  # Synthetic perf-budget where dispatchoutbox benches live under the alarm glob.
  local budget="${dir}/perf-budget.yaml"
  cat >"${budget}" <<'EOF'
schemaVersion: 1
repo: hololive-bot
benchmarks:
  BenchmarkBuildDedupeKey: { package: ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox, class: hotpath, gate: pr }
  BenchmarkBuildEventKey: { package: ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox, class: hotpath, gate: pr }
  BenchmarkFindBestMatchCacheHit: { package: ./hololive/hololive-kakao-bot-go/internal/service/matcher, class: critical, gate: pr }
settings:
  min_count: 6
EOF
  local workload="${dir}/workload.yaml"
  write_workload "${workload}"
  local build_cmd="${dir}/build.sh"
  local bench_cmd="${dir}/bench.sh"
  write_build_cmd "${build_cmd}"
  write_bench_cmd "${bench_cmd}"
  local out="${dir}/out"
  COMPARE_ARGS=(
    --service hololive-alarm-worker
    --main ./hololive/hololive-alarm-worker/cmd/alarm-worker
    --profile "${profile}"
    --workload "${workload}"
    --output-dir "${out}"
    --build-cmd "${build_cmd}"
    --bench-cmd "${bench_cmd}"
  )
  run_compare \
    PGO_PERF_BUDGET="${budget}" \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=950
  # Must NOT be the "no benches matched" empty path: report lists the alarm benches.
  local report="${out}/pgo-compare-report.md"
  assert_file_exists "alarm report exists" "${report}"
  if ! grep -q 'BenchmarkBuildDedupeKey' "${report}"; then
    printf 'not ok - alarm-worker must select BenchmarkBuildDedupeKey via hotpaths glob\n%s\n' "$(cat "${report}")" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - alarm-worker selects BuildDedupeKey via hotpaths glob\n'
  if ! grep -q 'BenchmarkBuildEventKey' "${report}"; then
    printf 'not ok - alarm-worker must also select BenchmarkBuildEventKey\n' >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - alarm-worker selects BuildEventKey via hotpaths glob\n'
  # matcher bench is outside alarm globs and outside alarm main prefix -> excluded.
  if grep -q 'BenchmarkFindBestMatchCacheHit' "${report}"; then
    printf 'not ok - alarm-worker must not select matcher bench (out of glob)\n' >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - alarm-worker excludes out-of-glob matcher bench\n'
}

case_adoption_signal_but_unmeasured_holds
case_binary_boundary_pass
case_binary_boundary_reject
case_measured_metrics_accept
case_measured_p99_regression_reject
case_bench_mean_parsing
case_env_value_form_rejects_flag
case_env_value_form_rejects_benchtime_flag
case_generator_field_present
case_help_notes_collector_incompatibility
case_report_na_contract
case_comparison_schema_keys
case_alarm_worker_selects_via_hotpaths

printf 'ok - %s pgo compare fixture checks passed\n' "${PASSED}"
