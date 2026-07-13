#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="${SCRIPT_DIR}/grep-sensitive-logs.sh"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

run_gate() {
  local target="$1"
  set +e
  LAST_OUTPUT="$(bash "${GATE}" "${target}" 2>&1)"
  LAST_STATUS=$?
  set -e
}

write_fixture() {
  local name="$1"
  local statement="$2"
  local root="${TMP_ROOT}/${name}/iris-client-go"
  mkdir -p "${root}/internal/client"
  {
    printf 'package client\n\n'
    printf 'func (e *HTTPError) LogValue() any {\n'
    printf '\treturn []any{\n'
    printf '\t\t%s\n' "${statement}"
    printf '\t}\n'
    printf '}\n'
  } >"${root}/internal/client/errors.go"
  printf '%s\n' "${root}"
}

safe_target="$(write_fixture safe 'slog.String("Body", redactSensitiveTokens(e.Body)),')"
run_gate "${safe_target}"
if [[ "${LAST_STATUS}" -ne 0 ]]; then
  printf '[FAIL] exact audited wrapper was rejected\n%s\n' "${LAST_OUTPUT}" >&2
  exit 1
fi
printf '[PASS] exact audited wrapper is accepted\n'

malicious_target="$(write_fixture malicious 'slog.String("password", password), slog.String("Body", redactSensitiveTokens(e.Body)),')"
run_gate "${malicious_target}"
if [[ "${LAST_STATUS}" -eq 0 ]]; then
  printf '[FAIL] allowed wrapper hid a second unsafe field\n%s\n' "${LAST_OUTPUT}" >&2
  exit 1
fi
if [[ "${LAST_OUTPUT}" != *'suspicious sensitive log:'* ]]; then
  printf '[FAIL] malicious fixture failed for an unexpected reason\n%s\n' "${LAST_OUTPUT}" >&2
  exit 1
fi
printf '[PASS] allowed wrapper cannot hide a second unsafe field\n'

raw_target="$(write_fixture raw 'slog.String("Body", e.Body),')"
run_gate "${raw_target}"
if [[ "${LAST_STATUS}" -eq 0 || "${LAST_OUTPUT}" != *'suspicious sensitive log:'* ]]; then
  printf '[FAIL] raw body fixture was not rejected\n%s\n' "${LAST_OUTPUT}" >&2
  exit 1
fi
printf '[PASS] raw sensitive field is rejected\n'
