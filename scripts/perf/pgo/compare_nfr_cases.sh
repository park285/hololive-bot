#!/usr/bin/env bash

case_measured_cpu_regression_reject() {
  local dir="${TMP_ROOT}/cpu-reject"
  setup_case "${dir}"
  COMPARE_ARGS+=(--live-cmd "${LIVE_CMD}")
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=900 \
    PGO_TEST_CPU=3.1 PGO_TEST_P95=-2 PGO_TEST_P99=0 PGO_TEST_RSS=1
  assert_status "measured CPU regression -> REJECTED exits 2" 2
  assert_contains "measured CPU reject reason" "CPU delta > +3%"
}

case_measured_p95_regression_reject() {
  local dir="${TMP_ROOT}/p95-reject"
  setup_case "${dir}"
  COMPARE_ARGS+=(--live-cmd "${LIVE_CMD}")
  run_compare \
    PGO_TEST_OFF_BYTES=100000 PGO_TEST_ON_BYTES=101000 \
    PGO_TEST_BENCH_OFF_NS_LIST=1000 PGO_TEST_BENCH_ON_NS_LIST=900 \
    PGO_TEST_CPU=-5 PGO_TEST_P95=3.1 PGO_TEST_P99=0 PGO_TEST_RSS=1
  assert_status "measured p95 regression -> REJECTED exits 2" 2
  assert_contains "measured p95 reject reason" "p95 latency regression > 3%"
}
