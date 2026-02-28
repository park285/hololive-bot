#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"

FIXTURE_PATH="${1:-${ROOT_DIR}/crates/scraper/service/testdata/date_extractor_cross_validation_cases.json}"
if [[ ! -f "${FIXTURE_PATH}" ]]; then
  echo "[cross-validate] fixture not found: ${FIXTURE_PATH}" >&2
  exit 1
fi

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/date-extractor-cross-validate.XXXXXX")"
GO_OUTPUT="${TMP_DIR}/go_results.json"
RUST_OUTPUT="${TMP_DIR}/rust_results.json"

echo "[cross-validate] fixture: ${FIXTURE_PATH}"
echo "[cross-validate] tmp dir: ${TMP_DIR}"

pushd "${MONOREPO_ROOT}/hololive/hololive-kakao-bot-go" >/dev/null
go run ./cmd/tools/date_extractor_cross_validate \
  -fixture "${FIXTURE_PATH}" \
  -output "${GO_OUTPUT}"
popd >/dev/null

pushd "${ROOT_DIR}" >/dev/null
CROSS_VALIDATE_OUTPUT="${RUST_OUTPUT}" \
  cargo test -p scraper-service --test date_extractor_cross_validation -- --nocapture
popd >/dev/null

if command -v jq >/dev/null 2>&1; then
  if diff \
    <(jq -S '.results | sort_by(.name)' "${GO_OUTPUT}") \
    <(jq -S '.results | sort_by(.name)' "${RUST_OUTPUT}"); then
    echo "[cross-validate] ✅ Go/Rust results matched"
  else
    echo "[cross-validate] ❌ Go/Rust results mismatch" >&2
    echo "[cross-validate] see: ${GO_OUTPUT} ${RUST_OUTPUT}" >&2
    exit 1
  fi
else
  if diff "${GO_OUTPUT}" "${RUST_OUTPUT}"; then
    echo "[cross-validate] ✅ Go/Rust outputs matched (raw diff)"
  else
    echo "[cross-validate] ❌ Go/Rust outputs mismatch" >&2
    echo "[cross-validate] jq not found; raw diff was used" >&2
    exit 1
  fi
fi

echo "[cross-validate] go result:   ${GO_OUTPUT}"
echo "[cross-validate] rust result: ${RUST_OUTPUT}"
