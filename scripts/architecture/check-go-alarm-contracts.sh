#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
GO_CONTRACT_FILE="${ROOT_DIR}/hololive/hololive-shared/pkg/contracts/alarm/contracts.go"
GO_KEYS_FILE="${ROOT_DIR}/hololive/hololive-shared/pkg/service/alarm/keys/keys.go"

for f in "${GO_CONTRACT_FILE}" "${GO_KEYS_FILE}"; do
  if [[ ! -f "${f}" ]]; then
    echo "error: go source file not found: ${f}" >&2
    exit 1
  fi
done

# H5 이후 dispatch/claim 키의 리터럴 SSOT는 keys.go이고 contracts.go는 re-export(`= keyspkg.X`)다.
# 문자열 리터럴은 keys.go에서, envelope version 숫자는 contracts.go에서 추출한다.
# re-export 심볼명을 값으로 오인하던 false-green을 막기 위해 기대값과 정확히 비교한다.
extract_go_string_const() {
  local file="$1"
  local name="$2"
  awk -v n="${name}" '
    $1 == n && $2 == "=" {
      gsub(/"/,"",$3);
      print $3;
      exit
    }
  ' "${file}"
}

extract_go_numeric_const() {
  local file="$1"
  local name="$2"
  awk -v n="${name}" '
    $1 == n {
      for (i = 1; i <= NF; i++) {
        if ($i == "=") {
          v = $(i + 1)
          gsub(/;/,"",v)
          print v
          exit
        }
      }
    }
  ' "${file}"
}

assert_equals() {
  local label="$1"
  local got="$2"
  local want="$3"
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL: ${label} = '${got}', want '${want}'" >&2
    echo "       (literal source: ${GO_KEYS_FILE}; version source: ${GO_CONTRACT_FILE})" >&2
    exit 1
  fi
}

go_queue="$(extract_go_string_const "${GO_KEYS_FILE}" "DispatchQueueKey")"
go_retry_queue="$(extract_go_string_const "${GO_KEYS_FILE}" "DispatchRetryQueueKey")"
go_dlq="$(extract_go_string_const "${GO_KEYS_FILE}" "DispatchDLQKey")"
go_claim="$(extract_go_string_const "${GO_KEYS_FILE}" "NotifyClaimKeyPrefix")"
go_logical_claim="$(extract_go_string_const "${GO_KEYS_FILE}" "NotifyLogicalClaimKeyPrefix")"
go_envelope_version="$(extract_go_numeric_const "${GO_CONTRACT_FILE}" "QueueEnvelopeVersionV1")"

assert_equals "DispatchQueueKey" "${go_queue}" "alarm:dispatch:queue"
assert_equals "DispatchRetryQueueKey" "${go_retry_queue}" "alarm:dispatch:retry"
assert_equals "DispatchDLQKey" "${go_dlq}" "alarm:dispatch:dlq"
assert_equals "NotifyClaimKeyPrefix" "${go_claim}" "notified:claim:"
assert_equals "NotifyLogicalClaimKeyPrefix" "${go_logical_claim}" "notified:claim:event:"
assert_equals "QueueEnvelopeVersionV1" "${go_envelope_version}" "1"

echo "OK: Go alarm contract constants are valid"
echo " - DispatchQueueKey: ${go_queue}"
echo " - DispatchRetryQueueKey: ${go_retry_queue}"
echo " - DispatchDLQKey: ${go_dlq}"
echo " - NotifyClaimKeyPrefix: ${go_claim}"
echo " - NotifyLogicalClaimKeyPrefix: ${go_logical_claim}"
echo " - QueueEnvelopeVersionV1: ${go_envelope_version}"
