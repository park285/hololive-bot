#!/usr/bin/env bash

write_extra_driver_workload() {
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
  hidden_extra: { tool: builtin }
EOF
}

case_extra_driver_errors() {
  local dir="${TMP_ROOT}/extra-driver"
  local workload="${dir}/workload.yaml"
  write_extra_driver_workload "${workload}"
  run_generator "${dir}/artifacts/perf/pgo" "2026-06-12" \
    --service hololive-bot --main "${MAIN_PACKAGE}" --duration 600s \
    --workload "${workload}" --output "${dir}/default.pgo"
  assert_failure "driver without traffic key errors"
  assert_contains "extra driver rejection is explicit" "driver without traffic key"
}

case_cpu_regression_rejects() {
  local dir="${TMP_ROOT}/cpu-reject"
  local workload="${dir}/workload.yaml"
  local collector="${dir}/collector.sh"
  write_workload "${workload}"
  write_collector "${collector}"
  run_generator_with_metrics "${dir}/artifacts/perf/pgo" "2026-06-12" \
    3.1 -2 0 1 2 4 \
    --service hololive-bot --main "${MAIN_PACKAGE}" --duration 600s \
    --workload "${workload}" --output "${dir}/default.pgo" --collect-cmd "${collector}"
  assert_failure "CPU regression rejects candidate"
  assert_contains "CPU regression rejection reason" "CPU delta > +3%"
}

case_p95_regression_rejects() {
  local dir="${TMP_ROOT}/p95-reject"
  local workload="${dir}/workload.yaml"
  local collector="${dir}/collector.sh"
  write_workload "${workload}"
  write_collector "${collector}"
  run_generator_with_metrics "${dir}/artifacts/perf/pgo" "2026-06-12" \
    -3 3.1 0 1 2 4 \
    --service hololive-bot --main "${MAIN_PACKAGE}" --duration 600s \
    --workload "${workload}" --output "${dir}/default.pgo" --collect-cmd "${collector}"
  assert_failure "p95 regression rejects candidate"
  assert_contains "p95 regression rejection reason" "p95 latency regression > 3%"
}

case_profile_collection_time_is_bound() {
  local source="${TMP_ROOT}/accept/default.pgo"
  local dir="${TMP_ROOT}/profile-time-mismatch"
  mkdir -p "${dir}"
  cp "${source}" "${dir}/default.pgo"
  cp "${source}.meta.json" "${dir}/default.pgo.meta.json"
  python3 - "${dir}/default.pgo.meta.json" <<'PY'
import datetime as dt, json, sys
path = sys.argv[1]
data = json.load(open(path))
data["profileCollectedAt"] = (dt.datetime.fromisoformat(data["profileCollectedAt"]) - dt.timedelta(hours=1)).isoformat()
with open(path, "w") as output:
    json.dump(data, output, indent=2, allow_nan=False)
    output.write("\n")
PY
  run_validate_profile "${dir}/default.pgo" "${dir}/default.pgo.meta.json"
  assert_failure "profile collection time mismatch is rejected"
  assert_contains "profile collection time mismatch is explicit" "profileCollectedAt mismatch"
}
