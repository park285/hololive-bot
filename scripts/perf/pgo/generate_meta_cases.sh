#!/usr/bin/env bash

case_validate_meta_rejects_missing_required() {
  local dir="${TMP_ROOT}/meta-invalid"
  mkdir -p "${dir}"
  printf '{"schemaVersion":1,"service":"hololive-bot"}\n' >"${dir}/default.pgo.meta.json"
  run_validate_meta "${dir}/default.pgo.meta.json"
  assert_failure "validate-meta rejects missing required"
  assert_contains "validate-meta rejects missing required" "missing field"
}

case_validate_meta_rejects_replayed_bad_verdict() {
  local source="${TMP_ROOT}/accept/default.pgo.meta.json"
  local dir="${TMP_ROOT}/meta-bad-verdict"
  mkdir -p "${dir}"
  cp "${source}" "${dir}/default.pgo.meta.json"
  python3 - "${dir}/default.pgo.meta.json" <<'PY'
import json, sys
path = sys.argv[1]
data = json.load(open(path))
data["comparison"]["p99LatencyDelta"] = 1.0
with open(path, "w") as output:
    json.dump(data, output, indent=2)
    output.write("\n")
PY
  run_validate_meta "${dir}/default.pgo.meta.json"
  assert_failure "validate-meta rejects replayed bad performance verdict"
  assert_contains "bad verdict rejection uses common verdict" "metadata comparison verdict REJECTED"
}

case_validate_meta_rejects_nonfinite_delta() {
  local source="${TMP_ROOT}/accept/default.pgo.meta.json"
  local dir="${TMP_ROOT}/meta-nonfinite"
  mkdir -p "${dir}"
  cp "${source}" "${dir}/default.pgo.meta.json"
  python3 - "${dir}/default.pgo.meta.json" <<'PY'
import json, sys
path = sys.argv[1]
data = json.load(open(path))
data["comparison"]["p99LatencyDelta"] = float("nan")
with open(path, "w") as output:
    json.dump(data, output, indent=2)
    output.write("\n")
PY
  run_validate_meta "${dir}/default.pgo.meta.json"
  assert_failure "validate-meta rejects non-finite performance delta"
  assert_contains "non-finite delta rejection is explicit" "invalid numeric constant"
}

case_validate_meta_rejects_naive_generated_at() {
  local source="${TMP_ROOT}/accept/default.pgo.meta.json"
  local dir="${TMP_ROOT}/meta-naive-time"
  mkdir -p "${dir}"
  cp "${source}" "${dir}/default.pgo.meta.json"
  python3 - "${dir}/default.pgo.meta.json" <<'PY'
import datetime as dt, json, sys
path = sys.argv[1]
data = json.load(open(path))
data["generatedAt"] = dt.datetime.now().replace(microsecond=0).isoformat()
with open(path, "w") as output:
    json.dump(data, output, indent=2, allow_nan=False)
    output.write("\n")
PY
  run_validate_meta "${dir}/default.pgo.meta.json"
  assert_failure "validate-meta rejects timezone-naive generatedAt"
  assert_contains "naive generatedAt rejection requires offset" "must include a timezone offset"
}

case_validate_meta_rejects_future_generated_at() {
  local source="${TMP_ROOT}/accept/default.pgo.meta.json"
  local dir="${TMP_ROOT}/meta-future-time"
  mkdir -p "${dir}"
  cp "${source}" "${dir}/default.pgo.meta.json"
  python3 - "${dir}/default.pgo.meta.json" <<'PY'
import datetime as dt, json, sys
path = sys.argv[1]
data = json.load(open(path))
data["generatedAt"] = (dt.datetime.now().astimezone() + dt.timedelta(minutes=10)).isoformat()
with open(path, "w") as output:
    json.dump(data, output, indent=2, allow_nan=False)
    output.write("\n")
PY
  run_validate_meta "${dir}/default.pgo.meta.json"
  assert_failure "validate-meta rejects generatedAt beyond future skew"
  assert_contains "future generatedAt rejection names skew" "more than 5 minutes in the future"
}

case_validate_meta_rejects_untrusted_acceptor() {
  local source="${TMP_ROOT}/accept/default.pgo.meta.json"
  local dir="${TMP_ROOT}/meta-acceptor"
  mkdir -p "${dir}"
  cp "${source}" "${dir}/default.pgo.meta.json"
  sed -i 's|"acceptedBy": "scripts/perf/pgo/generate.sh"|"acceptedBy": "manual"|' "${dir}/default.pgo.meta.json"
  run_validate_meta "${dir}/default.pgo.meta.json"
  assert_failure "validate-meta rejects untrusted acceptedBy"
  assert_contains "acceptedBy rejection names generator" "acceptedBy must be"
}

case_validate_meta_rejects_excessive_expiry() {
  local source="${TMP_ROOT}/accept/default.pgo.meta.json"
  local dir="${TMP_ROOT}/meta-expiry"
  mkdir -p "${dir}"
  cp "${source}" "${dir}/default.pgo.meta.json"
  sed -i 's/"expiresAfterDays": 45/"expiresAfterDays": 46/' "${dir}/default.pgo.meta.json"
  run_validate_meta "${dir}/default.pgo.meta.json"
  assert_failure "validate-meta rejects expiry over 45 days"
  assert_contains "expiry rejection names bound" "between 1 and 45"
}
