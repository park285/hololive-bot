#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

GO_CONTRACT_FILE="${ROOT_DIR}/hololive/hololive-shared/pkg/contracts/alarm/contracts.go"
RUST_KEYS_FILE_LEGACY="${ROOT_DIR}/hololive/hololive-rs/crates/shared/core/src/keys.rs"
RUST_KEYS_DIR="${ROOT_DIR}/hololive/hololive-rs/crates/shared/core/src/keys"

if [[ ! -f "${GO_CONTRACT_FILE}" ]]; then
  echo "error: go contract file not found: ${GO_CONTRACT_FILE}" >&2
  exit 1
fi

RUST_KEYS_FILES=()
if [[ -f "${RUST_KEYS_FILE_LEGACY}" ]]; then
  RUST_KEYS_FILES+=("${RUST_KEYS_FILE_LEGACY}")
elif [[ -d "${RUST_KEYS_DIR}" ]]; then
  while IFS= read -r -d '' file; do
    RUST_KEYS_FILES+=("${file}")
  done < <(find "${RUST_KEYS_DIR}" -maxdepth 1 -type f -name '*.rs' -print0)
fi

if [[ "${#RUST_KEYS_FILES[@]}" -eq 0 ]]; then
  echo "error: rust keys files not found (checked legacy file + keys/ dir)" >&2
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

extract_rust_const() {
  local name="$1"
  awk -v n="${name}" '
    $1 == "pub" && $2 == "const" && $3 == n ":" {
      gsub(/"/,"",$6);
      gsub(/;/,"",$6);
      print $6;
      exit
    }
  ' "${RUST_KEYS_FILES[@]}"
}

go_queue="$(extract_go_string_const "DispatchQueueKey")"
go_claim="$(extract_go_string_const "NotifyClaimKeyPrefix")"
go_logical_claim="$(extract_go_string_const "NotifyLogicalClaimKeyPrefix")"
go_envelope_version="$(extract_go_numeric_const "QueueEnvelopeVersionV1")"

rust_queue="$(extract_rust_const "ALARM_DISPATCH_QUEUE_KEY")"
rust_claim="$(extract_rust_const "NOTIFY_CLAIM_KEY_PREFIX")"
rust_logical_claim="$(extract_rust_const "NOTIFY_LOGICAL_CLAIM_KEY_PREFIX")"
rust_envelope_version="$(extract_rust_const "ALARM_QUEUE_ENVELOPE_VERSION_V1")"

check_pair() {
  local label="$1"
  local go_val="$2"
  local rust_val="$3"
  if [[ -z "${go_val}" || -z "${rust_val}" ]]; then
    echo "FAIL: ${label} missing constant value (go='${go_val}', rust='${rust_val}')" >&2
    exit 1
  fi
  if [[ "${go_val}" != "${rust_val}" ]]; then
    echo "FAIL: ${label} mismatch" >&2
    echo "  go:   ${go_val}" >&2
    echo "  rust: ${rust_val}" >&2
    exit 1
  fi
}

check_pair "dispatch_queue_key" "${go_queue}" "${rust_queue}"
check_pair "notify_claim_key_prefix" "${go_claim}" "${rust_claim}"
check_pair "notify_logical_claim_key_prefix" "${go_logical_claim}" "${rust_logical_claim}"
check_pair "queue_envelope_version_v1" "${go_envelope_version}" "${rust_envelope_version}"

echo "OK: Go↔Rust alarm contract constants match"
echo " - DispatchQueueKey: ${go_queue}"
echo " - NotifyClaimKeyPrefix: ${go_claim}"
echo " - NotifyLogicalClaimKeyPrefix: ${go_logical_claim}"
echo " - QueueEnvelopeVersionV1: ${go_envelope_version}"
