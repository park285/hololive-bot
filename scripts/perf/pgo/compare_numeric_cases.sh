#!/usr/bin/env bash

case_nonfinite_live_metric_rejects() {
  local dir="${TMP_ROOT}/nonfinite-live"
  setup_case "${dir}"
  COMPARE_ARGS+=(--live-cmd "${LIVE_CMD}")
  run_compare PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=900 \
    PGO_TEST_CPU=-5 PGO_TEST_P95=0 PGO_TEST_P99=NaN PGO_TEST_RSS=1
  if [[ "${LAST_STATUS}" -eq 0 || "${LAST_STATUS}" -eq 3 ]]; then
    printf 'not ok - non-finite live metric must error, got status %s\n%s\n' "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - non-finite live metric rejected\n'
  assert_contains "non-finite live metric error" "invalid numeric constant: NaN"
}

case_overflowing_live_metric_rejects() {
  local dir="${TMP_ROOT}/overflow-live"
  setup_case "${dir}"
  COMPARE_ARGS+=(--live-cmd "${LIVE_CMD}")
  run_compare PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=900 \
    PGO_TEST_CPU=-5 PGO_TEST_P95=0 PGO_TEST_P99=1e400 PGO_TEST_RSS=1
  if [[ "${LAST_STATUS}" -eq 0 || "${LAST_STATUS}" -eq 3 ]]; then
    printf 'not ok - overflowing live metric must error, got status %s\n%s\n' "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - overflowing live metric rejected\n'
  assert_contains "overflowing live metric error" "must be finite or null"
}

case_400_digit_benchmark_rejects() {
  local dir="${TMP_ROOT}/overflow-bench-parse"
  local huge
  setup_case "${dir}"
  huge="$(printf '9%.0s' {1..400})"
  run_compare PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST="${huge}" PGO_TEST_BENCH_ON_NS_LIST=900
  if [[ "${LAST_STATUS}" -eq 0 || "${LAST_STATUS}" -eq 3 ]]; then
    printf 'not ok - 400-digit ns/op must error, got status %s\n%s\n' "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - 400-digit ns/op rejected\n'
  assert_contains "400-digit ns/op rejection is finite-bound" "positive finite number"
}

case_benchmark_mean_overflow_rejects() {
  local dir="${TMP_ROOT}/overflow-bench-mean"
  local huge
  setup_case "${dir}"
  huge="1$(printf '0%.0s' {1..308})"
  run_compare PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST="${huge},${huge}" PGO_TEST_BENCH_ON_NS_LIST=900
  if [[ "${LAST_STATUS}" -eq 0 || "${LAST_STATUS}" -eq 3 ]]; then
    printf 'not ok - benchmark mean overflow must error, got status %s\n%s\n' "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - benchmark mean overflow rejected\n'
  assert_contains "benchmark mean overflow error" "mean ns/op overflowed"
}

case_bench_mean_parsing() {
  local dir="${TMP_ROOT}/mean"
  setup_case "${dir}"
  run_compare PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST="1000,1100" PGO_TEST_BENCH_ON_NS_LIST="900,950"
  local hb
  hb="$(json_field "${OUTPUT_DIR}/comparison.json" hotBenchmarkPercentDelta)"
  assert_close "hot bench delta uses mean (11.9048)" "${hb}" "11.904761904761905"
}
