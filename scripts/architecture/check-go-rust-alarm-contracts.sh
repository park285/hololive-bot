#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
GO_CONTRACT_FILE="${ROOT_DIR}/hololive/hololive-shared/pkg/contracts/alarm/contracts.go"

if [[ ! -f "${GO_CONTRACT_FILE}" ]]; then
  echo "error: go contract file not found: ${GO_CONTRACT_FILE}" >&2
  exit 1
fi

extract_go_string_const() {
  local name="$1"
  awk -v n="${name}" '
    $1 == n && $2 == "=" {
      gsub(/"/,"",$3);
      print $3;
      exit
    }
  ' "${GO_CONTRACT_FILE}"
}

extract_go_numeric_const() {
  local name="$1"
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
  ' "${GO_CONTRACT_FILE}"
}

require_non_empty() {
  local label="$1"
  local val="$2"
  if [[ -z "${val}" ]]; then
    echo "FAIL: ${label} is empty or missing in ${GO_CONTRACT_FILE}" >&2
    exit 1
  fi
}

go_queue="$(extract_go_string_const "DispatchQueueKey")"
go_claim="$(extract_go_string_const "NotifyClaimKeyPrefix")"
go_logical_claim="$(extract_go_string_const "NotifyLogicalClaimKeyPrefix")"
go_envelope_version="$(extract_go_numeric_const "QueueEnvelopeVersionV1")"

require_non_empty "DispatchQueueKey" "${go_queue}"
require_non_empty "NotifyClaimKeyPrefix" "${go_claim}"
require_non_empty "NotifyLogicalClaimKeyPrefix" "${go_logical_claim}"
require_non_empty "QueueEnvelopeVersionV1" "${go_envelope_version}"

echo "OK: Go alarm contract constants are valid"
echo " - DispatchQueueKey: ${go_queue}"
echo " - NotifyClaimKeyPrefix: ${go_claim}"
echo " - NotifyLogicalClaimKeyPrefix: ${go_logical_claim}"
echo " - QueueEnvelopeVersionV1: ${go_envelope_version}"
