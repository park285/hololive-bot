#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

failures=0

record_fail() {
  echo "[FAIL] $*" >&2
  failures=$((failures + 1))
}

pass() {
  echo "[PASS] $*"
}

setup_fixture() {
  local workdir="$1"

  mkdir -p "${workdir}/scripts/architecture/lib"
  cp "${ROOT_DIR}/scripts/architecture/check-tracked-local-artifacts.sh" "${workdir}/scripts/architecture/check-tracked-local-artifacts.sh"
  cp "${ROOT_DIR}/scripts/architecture/lib/git_guard.sh" "${workdir}/scripts/architecture/lib/git_guard.sh"
  chmod +x "${workdir}/scripts/architecture/check-tracked-local-artifacts.sh"

  git -C "${workdir}" init -q
  git -C "${workdir}" config user.email "codex@example.invalid"
  git -C "${workdir}" config user.name "Codex"

  printf 'secret\n' >"${workdir}/.env.osaka"
  git -C "${workdir}" add .env.osaka scripts/architecture/check-tracked-local-artifacts.sh scripts/architecture/lib/git_guard.sh
  git -C "${workdir}" commit -q -m "fixture"
}

expect_gate_failure_for_env_osaka() {
  local label="$1"
  local workdir="$2"
  local out_file="${TMP_DIR}/${label}.out"
  local err_file="${TMP_DIR}/${label}.err"

  if "${workdir}/scripts/architecture/check-tracked-local-artifacts.sh" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected tracked artifact gate failure: ${label}"
    return
  fi

  if ! grep -Fq "FAIL: tracked local artifacts detected" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected failure banner: ${label}"
  fi
  if ! grep -Fq ".env.osaka" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected .env.osaka in failure output: ${label}"
  fi
  pass "${label}"
}

missing_worktree="${TMP_DIR}/missing-working-tree"
setup_fixture "${missing_worktree}"
rm "${missing_worktree}/.env.osaka"
expect_gate_failure_for_env_osaka "missing working-tree forbidden artifact fails" "${missing_worktree}"

staged_delete="${TMP_DIR}/staged-delete"
setup_fixture "${staged_delete}"
git -C "${staged_delete}" rm -q .env.osaka
expect_gate_failure_for_env_osaka "staged forbidden artifact deletion fails" "${staged_delete}"

staged_rename="${TMP_DIR}/staged-rename"
setup_fixture "${staged_rename}"
git -C "${staged_rename}" mv .env.osaka allowed.txt
expect_gate_failure_for_env_osaka "staged forbidden artifact rename fails" "${staged_rename}"

if (( failures > 0 )); then
  echo "[FAIL] tracked local artifact tests failed: ${failures}" >&2
  exit 1
fi

echo "ok: tracked local artifact tests passed"
