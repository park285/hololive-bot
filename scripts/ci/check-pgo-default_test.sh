#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="${SCRIPT_DIR}/check-pgo-default.sh"
DEFAULT_POLICY="${SCRIPT_DIR}/pgo-off-policy.tsv"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

LAST_STATUS=0
LAST_OUTPUT=""
PASSED=0

write_fixture() {
  local dir="$1"
  mkdir -p "${dir}/service/cmd/app"
  cat >"${dir}/service/go.mod" <<'EOF'
module example.com/pgo-policy-fixture

go 1.26.5
EOF
  cat >"${dir}/service/cmd/app/main.go" <<'EOF'
package main

func main() {}
EOF
  cat >"${dir}/policy.tsv" <<EOF
off|${dir}/service|./cmd/app|app
EOF
  cat >"${dir}/compose.yml" <<'EOF'
services:
  app:
    build:
      context: .
      dockerfile: service/Dockerfile
EOF
  cat >"${dir}/service/Dockerfile" <<'EOF'
FROM scratch
RUN go build -pgo=off ./cmd/app
EOF
}

run_gate() {
  local dir="$1"
  set +e
  LAST_OUTPUT="$(bash "${GATE}" --policy "${dir}/policy.tsv" --compose "${dir}/compose.yml" 2>&1)"
  LAST_STATUS=$?
  set -e
}

assert_success() {
  local name="$1"
  if [[ "${LAST_STATUS}" -ne 0 ]]; then
    printf 'not ok - %s\n%s\n' "${name}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_failure() {
  local name="$1"
  if [[ "${LAST_STATUS}" -eq 0 ]]; then
    printf 'not ok - %s unexpectedly succeeded\n%s\n' "${name}" "${LAST_OUTPUT}" >&2
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

case_off_policy_passes() {
  local dir="${TMP_ROOT}/healthy"
  write_fixture "${dir}"
  run_gate "${dir}"
  assert_success "off-only policy accepts profile-free build"
  assert_contains "off-only policy reports no PGO stamp" "without a -pgo build stamp"
}

case_artifacts_rejected() {
  local suffix
  for suffix in "" ".meta.json" ".hotpaths"; do
    local dir="${TMP_ROOT}/artifact-${suffix//[^a-z]/x}"
    write_fixture "${dir}"
    printf 'forbidden\n' >"${dir}/service/cmd/app/default.pgo${suffix}"
    run_gate "${dir}"
    assert_failure "off-only policy rejects default.pgo${suffix}"
    assert_contains "artifact rejection names default.pgo${suffix}" "default.pgo${suffix}"
  done
}

case_on_row_rejected() {
  local dir="${TMP_ROOT}/on-row"
  write_fixture "${dir}"
  sed -i 's/^off|/on|/' "${dir}/policy.tsv"
  run_gate "${dir}"
  assert_failure "default-on policy row is forbidden"
  assert_contains "default-on rejection is explicit" "default-on policy is forbidden"
}

case_env_override_rejected() {
  local dir="${TMP_ROOT}/env-override"
  write_fixture "${dir}"
  sed -i "/dockerfile:/a\\      args:\\n        GO_PGO_FILE: \${APP_GO_PGO_FILE:-}" "${dir}/compose.yml"
  run_gate "${dir}"
  assert_failure "Compose PGO environment override is rejected"
  assert_contains "override rejection removes build arg" "must not expose GO_PGO_FILE"
}

case_nonempty_literal_rejected() {
  local dir="${TMP_ROOT}/nonempty"
  write_fixture "${dir}"
  sed -i '/dockerfile:/a\      args:\n        GO_PGO_FILE: cmd/app/default.pgo' "${dir}/compose.yml"
  run_gate "${dir}"
  assert_failure "Compose non-empty PGO path is rejected"
  assert_contains "non-empty rejection removes build arg" "must not expose GO_PGO_FILE"
}

case_dockerfile_arg_rejected() {
  local dir="${TMP_ROOT}/dockerfile-arg"
  write_fixture "${dir}"
  sed -i '2iARG GO_PGO_FILE=' "${dir}/service/Dockerfile"
  run_gate "${dir}"
  assert_failure "Dockerfile PGO build arg is rejected"
  assert_contains "Dockerfile PGO build arg rejection is explicit" "Dockerfile exposes GO_PGO_FILE"
}

case_dockerfile_requires_explicit_off() {
  local dir="${TMP_ROOT}/dockerfile-auto"
  write_fixture "${dir}"
  sed -i 's/go build -pgo=off/go build/' "${dir}/service/Dockerfile"
  run_gate "${dir}"
  assert_failure "Dockerfile implicit auto PGO is rejected"
  assert_contains "Dockerfile requires explicit off" "Go build must use -pgo=off"
}

case_artifact_scan_failure_rejected() {
  local dir="${TMP_ROOT}/find-failure"
  write_fixture "${dir}"
  mkdir -p "${dir}/bin"
  cat >"${dir}/bin/find" <<'EOF'
#!/usr/bin/env bash
exit 73
EOF
  chmod +x "${dir}/bin/find"
  PATH="${dir}/bin:${PATH}" run_gate "${dir}"
  assert_failure "artifact scan failure is rejected"
  assert_contains "artifact scan failure is explicit" "failed to scan for default PGO artifacts"
}

case_unmanaged_service_rejected() {
  local dir="${TMP_ROOT}/unmanaged-service"
  write_fixture "${dir}"
  cat >>"${dir}/compose.yml" <<'EOF'
  extra:
    build:
      context: .
      args:
        GO_PGO_FILE: forbidden
EOF
  run_gate "${dir}"
  assert_failure "unmanaged Compose PGO service is rejected"
  assert_contains "unmanaged service rejection names service" "unmanaged Compose PGO service: extra"
}

case_default_policy_has_exact_rows() {
  local actual
  local expected
  actual="$(awk -F'|' 'NF && $1 !~ /^#/ { print }' "${DEFAULT_POLICY}" | sort)"
  expected="$(printf '%s\n' \
    'off|hololive/hololive-alarm-worker|./cmd/alarm-worker|hololive-alarm-worker' \
    'off|hololive/hololive-api|./cmd/hololive-api|hololive-api,hololive-db-migrate' | sort)"
  if [[ "${actual}" != "${expected}" ]]; then
    printf 'not ok - production off-only policy rows differ\nexpected:\n%s\nactual:\n%s\n' \
      "${expected}" "${actual}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - production off-only policy has exact service rows\n'
}

case_off_policy_passes
case_artifacts_rejected
case_on_row_rejected
case_env_override_rejected
case_nonempty_literal_rejected
case_dockerfile_arg_rejected
case_dockerfile_requires_explicit_off
case_artifact_scan_failure_rejected
case_unmanaged_service_rejected
case_default_policy_has_exact_rows

printf 'ok - %s off-only PGO default policy checks passed\n' "${PASSED}"
