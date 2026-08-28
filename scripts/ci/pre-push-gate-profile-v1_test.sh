#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/ci/python-runtime.sh"
repo_python_init
GATE="${ROOT_DIR}/scripts/ci/pre-push-gate.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

# 실제 hook이 주입한 ref range 대신 fixture가 만든 Git history만 사용한다.
unset BASE_SHA HEAD_SHA FULL_PRE_PUSH PRE_PUSH_MODE LOCAL_CI_GO_SCOPE
unset RUN_RACE_TESTS RUN_DEPENDENCY_HYGIENE RUN_ADMIN_TOUCH_GUARDRAIL
unset PRE_PUSH_LOCAL_REF PRE_PUSH_LOCAL_SHA PRE_PUSH_REMOTE_REF PRE_PUSH_REMOTE_SHA
unset PRE_PUSH_PEELED_TAG_TARGET PRE_PUSH_UPDATE_KIND PRE_PUSH_GATE_MODE

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

fixture="${TMP_DIR}/fixture"
mkdir -p "${fixture}/scripts/ci" "${fixture}/fake-bin"
cp "${GATE}" "${fixture}/scripts/ci/pre-push-gate.sh"
cp "${ROOT_DIR}/scripts/ci/python-runner.sh" "${ROOT_DIR}/scripts/ci/python-runtime.sh" \
  "${fixture}/scripts/ci/"
cp "${ROOT_DIR}/.python-version" "${fixture}/.python-version"

cat >"${fixture}/fake-bin/bash" <<'SH'
#!/bin/bash
printf 'bash %s\n' "$*" >>"${GATE_TEST_LOG}"
SH
cat >"${fixture}/fake-bin/git" <<'SH'
#!/bin/bash
case "$*" in
  "rev-parse --verify origin/main") exit 0 ;;
  "diff --name-only origin/main...HEAD") printf 'hololive/hololive-api/main.go\n' ;;
  *) exit 1 ;;
esac
SH
cat >"${fixture}/scripts/ci/local-ci.sh" <<'SH'
#!/bin/bash
printf 'local-ci scope=%s base=%s dependency=%s\n' \
  "${LOCAL_CI_GO_SCOPE}" "${BASE_REF}" "${RUN_DEPENDENCY_HYGIENE}" >>"${GATE_TEST_LOG}"
SH
chmod +x "${fixture}/fake-bin/bash" "${fixture}/fake-bin/git" \
  "${fixture}/scripts/ci/local-ci.sh"

run_phase() {
  local name="$1"
  shift
  : >"${TMP_DIR}/${name}.log"
  GATE_TEST_LOG="${TMP_DIR}/${name}.log" PRE_PUSH_GATE_SCOPED=1 \
    CI_PYTHON_BIN="${CI_PYTHON_BIN}" CI_PYTHON_RUNTIME_ROOT="${fixture}" \
    PATH="${fixture}/fake-bin:${PATH}" /bin/bash "${fixture}/scripts/ci/pre-push-gate.sh" "$@" \
    >"${TMP_DIR}/${name}.out" 2>"${TMP_DIR}/${name}.err"
}

run_phase reusable --phase=reusable
grep -Fq 'scripts/check-release-version.sh' "${TMP_DIR}/reusable.log" || fail "reusable misses release policy"
grep -Fq 'scripts/ci/check-workflow-secrets.sh' "${TMP_DIR}/reusable.log" || fail "reusable misses workflow policy"
if grep -Fq 'test-go-workspace-modules.sh' "${TMP_DIR}/reusable.log"; then
  fail "reusable consumes sibling workspace"
fi

run_phase freshness --phase=freshness
[[ ! -s "${TMP_DIR}/freshness.log" ]] || fail "freshness must be an explicit no-op"

run_phase ambient --phase=ambient
ambient_first="$(head -n 1 "${TMP_DIR}/ambient.log")"
[[ "${ambient_first}" == 'bash scripts/ci/test-go-workspace-modules.sh' ]] || \
  fail "ambient must start with the sibling workspace check"
grep -Fq 'local-ci scope=changed base=origin/main dependency=true' "${TMP_DIR}/ambient.log" || \
  fail "ambient must always run local-ci with existing fast-route defaults"
if grep -Fq 'scripts/ci/public-pr-collector-helper-gate.sh' "${TMP_DIR}/ambient.log"; then
  fail "fast mode must not run the collector helper gate for an unrelated path"
fi

: >"${TMP_DIR}/full.log"
GATE_TEST_LOG="${TMP_DIR}/full.log" PRE_PUSH_GATE_SCOPED=1 PRE_PUSH_MODE=full \
  CI_PYTHON_BIN="${CI_PYTHON_BIN}" CI_PYTHON_RUNTIME_ROOT="${fixture}" \
  PATH="${fixture}/fake-bin:${PATH}" /bin/bash "${fixture}/scripts/ci/pre-push-gate.sh" --phase=ambient \
  >"${TMP_DIR}/full.out" 2>"${TMP_DIR}/full.err"
grep -Fq 'scripts/ci/public-pr-collector-helper-gate.sh' "${TMP_DIR}/full.log" || \
  fail "full mode must run the collector helper gate for an unrelated path"

run_phase default
cat "${TMP_DIR}/reusable.log" "${TMP_DIR}/ambient.log" >"${TMP_DIR}/expected.log"
cmp -s "${TMP_DIR}/expected.log" "${TMP_DIR}/default.log" || fail "default order is not reusable -> freshness -> ambient"

: >"${TMP_DIR}/invalid-mode.log"
if GATE_TEST_LOG="${TMP_DIR}/invalid-mode.log" PRE_PUSH_GATE_SCOPED=1 PRE_PUSH_MODE=invalid \
  CI_PYTHON_BIN="${CI_PYTHON_BIN}" CI_PYTHON_RUNTIME_ROOT="${fixture}" \
  PATH="${fixture}/fake-bin:${PATH}" /bin/bash "${fixture}/scripts/ci/pre-push-gate.sh" \
  >"${TMP_DIR}/invalid-mode.out" 2>"${TMP_DIR}/invalid-mode.err"; then
  fail "invalid mode was accepted"
fi
grep -Fq 'scripts/check-release-version.sh' "${TMP_DIR}/invalid-mode.log" || \
  fail "default path must run release policy before route validation"
if grep -Fq 'scripts/ci/check-workflow-secrets.sh' "${TMP_DIR}/invalid-mode.log"; then
  fail "workflow checks ran before route validation failed"
fi

if PRE_PUSH_GATE_SCOPED=1 CI_PYTHON_BIN="${CI_PYTHON_BIN}" CI_PYTHON_RUNTIME_ROOT="${fixture}" \
  PATH="${fixture}/fake-bin:${PATH}" \
  /bin/bash "${fixture}/scripts/ci/pre-push-gate.sh" reusable >/dev/null 2>&1; then
  fail "positional phase alias must fail"
fi
if PRE_PUSH_GATE_SCOPED=1 CI_PYTHON_BIN="${CI_PYTHON_BIN}" CI_PYTHON_RUNTIME_ROOT="${fixture}" \
  PATH="${fixture}/fake-bin:${PATH}" \
  /bin/bash "${fixture}/scripts/ci/pre-push-gate.sh" --phase reusable >/dev/null 2>&1; then
  fail "split phase argument must fail"
fi

fingerprint_fixture="${TMP_DIR}/fingerprint"
mkdir -p "${fingerprint_fixture}/scripts/ci" "${fingerprint_fixture}/fake-bin"
cp "${GATE}" "${fingerprint_fixture}/scripts/ci/pre-push-gate.sh"
cp "${ROOT_DIR}/scripts/ci/pre-push-gate-profile-v1.json" "${fingerprint_fixture}/scripts/ci/"
cp "${ROOT_DIR}/scripts/ci/go-tooling.sh" "${fingerprint_fixture}/scripts/ci/"
cp "${ROOT_DIR}/scripts/ci/local-ci.sh" "${fingerprint_fixture}/scripts/ci/"
cp "${ROOT_DIR}/scripts/ci/python-runner.sh" "${ROOT_DIR}/scripts/ci/python-runtime.sh" \
  "${fingerprint_fixture}/scripts/ci/"
cp "${ROOT_DIR}/.python-version" "${fingerprint_fixture}/.python-version"
cat >"${fingerprint_fixture}/fake-bin/go" <<'SH'
#!/bin/bash
case "${1:-}" in
  version) echo 'go version go1.27.0 linux/amd64' ;;
  env) printf 'go1.27.0\nlinux\namd64\ngo1.27.0+auto\n' ;;
  *) exit 1 ;;
esac
SH
cat >"${fingerprint_fixture}/fake-bin/systemd-run" <<'SH'
#!/bin/bash
[[ "${1:-}" == "--version" ]] || exit 1
echo 'systemd 257 (257.7-1)'
SH
chmod +x "${fingerprint_fixture}/fake-bin/go" "${fingerprint_fixture}/fake-bin/systemd-run"
git -C "${fingerprint_fixture}" init -q
git -C "${fingerprint_fixture}" config user.name gate-test
git -C "${fingerprint_fixture}" config user.email gate-test@example.invalid
git -C "${fingerprint_fixture}" add .python-version scripts/ci fake-bin
git -C "${fingerprint_fixture}" commit -qm fixture

CI_PYTHON_BIN="${CI_PYTHON_BIN}" CI_PYTHON_RUNTIME_ROOT="${fingerprint_fixture}" \
  PATH="${fingerprint_fixture}/fake-bin:${PATH}" \
  /bin/bash "${fingerprint_fixture}/scripts/ci/pre-push-gate.sh" --phase=fingerprint \
  >"${TMP_DIR}/fingerprint.json"
"${CI_PYTHON_BIN}" - "${TMP_DIR}/fingerprint.json" <<'PY'
import json
import re
import sys

path = sys.argv[1]
raw = open(path, encoding="utf-8").read()
data = json.loads(raw)
expected_keys = [
    "schema_version",
    "profile_id",
    "effective_base_sha",
    "route_fingerprint",
    "toolchain_fingerprint",
    "profile_inputs_fingerprint",
]
if list(data) != expected_keys:
    raise SystemExit(f"fingerprint keys/order mismatch: {list(data)}")
if data["schema_version"] != 1 or data["profile_id"] != "hololive-bot-v1":
    raise SystemExit("fingerprint identity mismatch")
if data["effective_base_sha"] != "":
    raise SystemExit("unset BASE_SHA must serialize as an empty string")
for key in expected_keys[3:]:
    if not re.fullmatch(r"[0-9a-f]{64}", data[key]):
        raise SystemExit(f"invalid {key}")
canonical = json.dumps(data, separators=(",", ":"), ensure_ascii=True) + "\n"
if raw != canonical:
    raise SystemExit("fingerprint JSON is not canonical")
PY

printf 'dirty\n' >"${fingerprint_fixture}/untracked"
if CI_PYTHON_BIN="${CI_PYTHON_BIN}" CI_PYTHON_RUNTIME_ROOT="${fingerprint_fixture}" \
  PATH="${fingerprint_fixture}/fake-bin:${PATH}" \
  /bin/bash "${fingerprint_fixture}/scripts/ci/pre-push-gate.sh" --phase=fingerprint >/dev/null 2>&1; then
  fail "dirty repository fingerprint must fail closed"
fi

echo "[PASS] pre-push gate phase and fingerprint contract"
