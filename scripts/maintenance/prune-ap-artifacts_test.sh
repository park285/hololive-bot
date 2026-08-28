#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PRUNE="${ROOT_DIR}/scripts/maintenance/prune-ap-artifacts.sh"

failures=0
record_fail() {
  echo "[FAIL] $*" >&2
  failures=$((failures + 1))
}
pass() { echo "[PASS] $*"; }

TMP_DIR="$(mktemp -d /tmp/prune-ap-artifacts-test.XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT

fixture_root=""
repo=""
setup_fixture() {
  repo="${TMP_DIR}/repo"
  rm -rf "${repo}"
  mkdir -p "${repo}/artifacts/ap-host-native" "${repo}/scripts/deploy/lib" "${repo}/scripts/maintenance"
  cp "${ROOT_DIR}/scripts/deploy/lib/ap-host-native-release-path.sh" "${repo}/scripts/deploy/lib/"
  cp "${PRUNE}" "${repo}/scripts/maintenance/"
  printf 'artifacts/*\n' >"${repo}/.gitignore"
  git -C "${repo}" init -q
  git -C "${repo}" add -A >/dev/null
  git -C "${repo}" -c user.email=t@t -c user.name=t commit -qm init

  fixture_root="${repo}/artifacts/ap-host-native"
  local i=0
  for name in "$@"; do
    mkdir -p "${fixture_root}/${name}"
    printf 'payload\n' >"${fixture_root}/${name}/bin"
    touch -d "2026-01-0$((i + 1))" "${fixture_root}/${name}"
    i=$((i + 1))
  done
}

setup_fixture old1 old2 old3 newest
before="$(find "${fixture_root}" -mindepth 1 -maxdepth 1 | wc -l)"
REPO_ROOT="${repo}" bash "${repo}/scripts/maintenance/prune-ap-artifacts.sh" --keep 1 >/dev/null 2>&1
after="$(find "${fixture_root}" -mindepth 1 -maxdepth 1 | wc -l)"
if [[ "${before}" == "${after}" ]]; then
  pass "dry-run leaves every payload in place"
else
  record_fail "dry-run deleted payloads: ${before} -> ${after}"
fi

setup_fixture old1 old2 old3 newest
REPO_ROOT="${repo}" bash "${repo}/scripts/maintenance/prune-ap-artifacts.sh" --keep 1 --apply >/dev/null 2>&1
remaining="$(find "${fixture_root}" -mindepth 1 -maxdepth 1 -printf '%f\n' | sort | tr '\n' ' ')"
if [[ "${remaining}" == "newest " ]]; then
  pass "--apply keeps exactly the newest payload"
else
  record_fail "--apply kept the wrong set: ${remaining}"
fi

mkdir -p "${fixture_root}/20260828T010000Z" "${fixture_root}/20260828T020000Z"
touch -d '2026-08-28 01:00:00' "${fixture_root}/20260828T010000Z"
touch -d '2026-08-28 02:00:00' "${fixture_root}/20260828T020000Z"
ln -s "${fixture_root}/20260828T020000Z" "${fixture_root}/20260828T000000Z"
touch -h -d '2026-08-28 00:00:00' "${fixture_root}/20260828T000000Z"
REPO_ROOT="${repo}" bash "${repo}/scripts/maintenance/prune-ap-artifacts.sh" --keep 1 --apply >/dev/null 2>&1 || true
if [[ -d "${fixture_root}/20260828T020000Z" && -L "${fixture_root}/20260828T000000Z" ]]; then
  pass "symlink candidate cannot delete its kept payload target"
else
  record_fail "symlink candidate altered the kept payload target"
fi

setup_fixture keepme
mkdir -p "${fixture_root}/.escape"
touch -d "2020-01-01" "${fixture_root}/.escape"
REPO_ROOT="${repo}" bash "${repo}/scripts/maintenance/prune-ap-artifacts.sh" --keep 0 --apply >/dev/null 2>&1 || true
if [[ -d "${fixture_root}/.escape" ]]; then
  pass "unsafe payload name is skipped instead of deleted"
else
  record_fail "unsafe payload name was deleted"
fi

setup_fixture tracked
git -C "${repo}" add -f "artifacts/ap-host-native/tracked" >/dev/null
git -C "${repo}" -c user.email=t@t -c user.name=t commit -qm tracked
REPO_ROOT="${repo}" bash "${repo}/scripts/maintenance/prune-ap-artifacts.sh" --keep 0 --apply >/dev/null 2>&1 || true
if [[ -d "${fixture_root}/tracked" ]]; then
  pass "git-tracked payload is skipped instead of deleted"
else
  record_fail "git-tracked payload was deleted"
fi

if (( failures > 0 )); then
  echo "[FAIL] prune-ap-artifacts tests failed: ${failures}" >&2
  exit 1
fi
echo "ok: prune-ap-artifacts tests passed"
