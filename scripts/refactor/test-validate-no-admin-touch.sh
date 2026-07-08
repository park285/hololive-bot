#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
scripts=(
  "${repo_root}/scripts/refactor/validate-no-admin-touch.sh"
)

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

setup_repo() {
  local workdir="$1"
  mkdir -p "${workdir}"
  git -C "${workdir}" init -q
  git -C "${workdir}" config user.email "codex@example.invalid"
  git -C "${workdir}" config user.name "Codex"
}

install_gate_stub() {
  local workdir="$1"
  local exit_code="$2"
  mkdir -p "${workdir}/scripts/ci"
  cat >"${workdir}/scripts/ci/admin-dashboard-go-ci.sh" <<EOF
#!/usr/bin/env bash
touch "${workdir}/.gate-invoked"
exit ${exit_code}
EOF
  chmod +x "${workdir}/scripts/ci/admin-dashboard-go-ci.sh"
}

commit_all() {
  local workdir="$1"
  local message="$2"
  git -C "${workdir}" add -A
  git -C "${workdir}" commit -q -m "${message}"
}

expect_fail() {
  local label="$1"
  shift
  if "$@" >"${tmpdir}/${label}.out" 2>&1; then
    cat "${tmpdir}/${label}.out" >&2
    echo "expected failure: ${label}" >&2
    exit 1
  fi
}

expect_pass() {
  local label="$1"
  shift
  if ! "$@" >"${tmpdir}/${label}.out" 2>&1; then
    cat "${tmpdir}/${label}.out" >&2
    echo "expected pass: ${label}" >&2
    exit 1
  fi
}

expect_gate_invoked() {
  local label="$1"
  local workdir="$2"
  if [[ ! -e "${workdir}/.gate-invoked" ]]; then
    echo "expected admin-dashboard go ci gate invocation: ${label}" >&2
    exit 1
  fi
}

expect_gate_not_invoked() {
  local label="$1"
  local workdir="$2"
  if [[ -e "${workdir}/.gate-invoked" ]]; then
    echo "expected no admin-dashboard go ci gate invocation: ${label}" >&2
    exit 1
  fi
}

run_guardrail() {
  local workdir="$1"
  local script="$2"
  local base_ref="$3"
  local head_ref="$4"
  (
    cd "${workdir}"
    BASE_REF="${base_ref}" HEAD_REF="${head_ref}" "${script}"
  )
}

for script in "${scripts[@]}"; do
  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-admin-plane-rename"
  setup_repo "${workdir}"
  install_gate_stub "${workdir}" 0
  mkdir -p "${workdir}/hololive/hololive-api/internal/planes/admin" "${workdir}/other"
  printf 'x\n' >"${workdir}/hololive/hololive-api/internal/planes/admin/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  git -C "${workdir}" mv hololive/hololive-api/internal/planes/admin/a.txt other/a.txt
  commit_all "${workdir}" "rename admin out"
  expect_pass "$(basename "${script}")-admin-plane-rename" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD
  expect_gate_not_invoked "$(basename "${script}")-admin-plane-rename" "${workdir}"

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-admin-dashboard-staged-change"
  setup_repo "${workdir}"
  install_gate_stub "${workdir}" 0
  mkdir -p "${workdir}/admin-dashboard" "${workdir}/other"
  printf 'x\n' >"${workdir}/admin-dashboard/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  printf 'y\n' >>"${workdir}/admin-dashboard/a.txt"
  git -C "${workdir}" add admin-dashboard/a.txt
  expect_pass "$(basename "${script}")-admin-dashboard-staged-change" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD
  expect_gate_invoked "$(basename "${script}")-admin-dashboard-staged-change" "${workdir}"

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-admin-dashboard-gates-fail"
  setup_repo "${workdir}"
  install_gate_stub "${workdir}" 1
  mkdir -p "${workdir}/admin-dashboard" "${workdir}/other"
  printf 'x\n' >"${workdir}/admin-dashboard/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  printf 'y\n' >>"${workdir}/admin-dashboard/a.txt"
  expect_fail "$(basename "${script}")-admin-dashboard-gates-fail" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD
  expect_gate_invoked "$(basename "${script}")-admin-dashboard-gates-fail" "${workdir}"

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-admin-dashboard-rename-in"
  setup_repo "${workdir}"
  install_gate_stub "${workdir}" 0
  mkdir -p "${workdir}/other" "${workdir}/admin-dashboard"
  printf 'x\n' >"${workdir}/other/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  git -C "${workdir}" mv other/a.txt admin-dashboard/a.txt
  commit_all "${workdir}" "rename dashboard in"
  expect_pass "$(basename "${script}")-admin-dashboard-rename-in" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD
  expect_gate_invoked "$(basename "${script}")-admin-dashboard-rename-in" "${workdir}"

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-admin-dashboard-dockerfile-change"
  setup_repo "${workdir}"
  install_gate_stub "${workdir}" 0
  mkdir -p "${workdir}/admin-dashboard"
  printf 'FROM golang:1.26-alpine AS builder\n' >"${workdir}/admin-dashboard/Dockerfile"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  printf 'FROM golang:1.26.5-alpine AS builder\n' >"${workdir}/admin-dashboard/Dockerfile"
  commit_all "${workdir}" "dashboard dockerfile change"
  expect_pass "$(basename "${script}")-admin-dashboard-dockerfile-change" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD
  expect_gate_invoked "$(basename "${script}")-admin-dashboard-dockerfile-change" "${workdir}"

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-clean"
  setup_repo "${workdir}"
  install_gate_stub "${workdir}" 0
  mkdir -p "${workdir}/other"
  printf 'x\n' >"${workdir}/other/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  printf 'y\n' >>"${workdir}/other/a.txt"
  commit_all "${workdir}" "non admin"
  expect_pass "$(basename "${script}")-clean" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD
  expect_gate_not_invoked "$(basename "${script}")-clean" "${workdir}"

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-missing-base"
  setup_repo "${workdir}"
  install_gate_stub "${workdir}" 0
  mkdir -p "${workdir}/other"
  printf 'x\n' >"${workdir}/other/a.txt"
  commit_all "${workdir}" "base"
  expect_fail "$(basename "${script}")-missing-base" run_guardrail "${workdir}" "${script}" refs/heads/missing HEAD
done

echo "ok: validate-no-admin-touch tests passed"
