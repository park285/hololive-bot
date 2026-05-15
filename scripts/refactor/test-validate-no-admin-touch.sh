#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
scripts=(
  "${repo_root}/scripts/refactor/validate-no-admin-touch.sh"
  "${repo_root}/docs/history/plan-kits/hololive-bot-integrated-refactor-v3/scripts/refactor/validate-no-admin-touch.sh"
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
  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-rename-out"
  setup_repo "${workdir}"
  mkdir -p "${workdir}/hololive/hololive-admin-api" "${workdir}/other"
  printf 'x\n' >"${workdir}/hololive/hololive-admin-api/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  git -C "${workdir}" mv hololive/hololive-admin-api/a.txt other/a.txt
  commit_all "${workdir}" "rename admin out"
  expect_fail "$(basename "${script}")-rename-out" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-staged-rename-out"
  setup_repo "${workdir}"
  mkdir -p "${workdir}/hololive/hololive-admin-api" "${workdir}/other"
  printf 'x\n' >"${workdir}/hololive/hololive-admin-api/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  git -C "${workdir}" mv hololive/hololive-admin-api/a.txt other/a.txt
  expect_fail "$(basename "${script}")-staged-rename-out" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-unstaged-rename-out"
  setup_repo "${workdir}"
  mkdir -p "${workdir}/hololive/hololive-admin-api" "${workdir}/other"
  printf 'x\n' >"${workdir}/hololive/hololive-admin-api/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  mv "${workdir}/hololive/hololive-admin-api/a.txt" "${workdir}/other/a.txt"
  expect_fail "$(basename "${script}")-unstaged-rename-out" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-rename-in"
  setup_repo "${workdir}"
  mkdir -p "${workdir}/other" "${workdir}/hololive/hololive-admin-api"
  printf 'x\n' >"${workdir}/other/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  git -C "${workdir}" mv other/a.txt hololive/hololive-admin-api/a.txt
  commit_all "${workdir}" "rename admin in"
  expect_fail "$(basename "${script}")-rename-in" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-clean"
  setup_repo "${workdir}"
  mkdir -p "${workdir}/other"
  printf 'x\n' >"${workdir}/other/a.txt"
  commit_all "${workdir}" "base"
  base_ref="$(git -C "${workdir}" rev-parse HEAD)"
  printf 'y\n' >>"${workdir}/other/a.txt"
  commit_all "${workdir}" "non admin"
  expect_pass "$(basename "${script}")-clean" run_guardrail "${workdir}" "${script}" "${base_ref}" HEAD

  workdir="${tmpdir}/$(basename "$(dirname "$(dirname "$(dirname "${script}")")")")-missing-base"
  setup_repo "${workdir}"
  mkdir -p "${workdir}/other"
  printf 'x\n' >"${workdir}/other/a.txt"
  commit_all "${workdir}" "base"
  expect_fail "$(basename "${script}")-missing-base" run_guardrail "${workdir}" "${script}" refs/heads/missing HEAD
done

echo "ok: validate-no-admin-touch tests passed"
