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
unset RUN_RACE_TESTS RUN_ADMIN_TOUCH_GUARDRAIL GATE_TEST_CHANGED_FILES
unset PRE_PUSH_LOCAL_REF PRE_PUSH_LOCAL_SHA PRE_PUSH_REMOTE_REF PRE_PUSH_REMOTE_SHA
unset PRE_PUSH_PEELED_TAG_TARGET PRE_PUSH_UPDATE_KIND PRE_PUSH_GATE_MODE

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

# 게이트가 선언한 자기 테스트 입력은 전부 저장소에 있어야 한다. 오타나 rename 은 조건부 실행을
# 조용히 무력화하므로(입력 부재 → 항상 실행 + 경고) 여기서 실제 저장소 기준으로 잡는다.
declared_self_tests="$(awk '
  /\\$/ { sub(/[[:space:]]*\\$/, " "); buf = buf $0; next }
  { print buf $0; buf = "" }
' "${GATE}" | sed -nE 's/^[[:space:]]*run_self_test[[:space:]]+//p')"
[[ -n "${declared_self_tests}" ]] || fail "gate declares no run_self_test calls"
while IFS= read -r declaration; do
  # shellcheck disable=SC2086
  for input in ${declaration}; do
    [[ -e "${ROOT_DIR}/${input}" ]] || fail "run_self_test input missing in repository: ${input}"
  done
done <<<"${declared_self_tests}"
if grep -nE '^[[:space:]]*(bash|"\$\{CI_PYTHON_BIN\}")[[:space:]]+[^[:space:]]*_test\.(sh|py)' "${GATE}"; then
  fail "self-tests must run only through run_self_test"
fi
if grep -nE '_test\.(sh|py)|/test-[^[:space:]]*\.sh' "${ROOT_DIR}/scripts/ci/local-ci.sh" | grep -v '^[0-9]*:#'; then
  fail "local-ci.sh must not run self-tests; they belong to the gate reusable phase"
fi

fixture="${TMP_DIR}/fixture"
mkdir -p "${fixture}/scripts/ci" "${fixture}/scripts/logs" "${fixture}/scripts/runtime" \
  "${fixture}/fake-bin" "${fixture}/mod"
cp "${GATE}" "${fixture}/scripts/ci/pre-push-gate.sh"
cp "${ROOT_DIR}/scripts/ci/python-runner.sh" "${ROOT_DIR}/scripts/ci/python-runtime.sh" \
  "${ROOT_DIR}/scripts/ci/go-tooling.sh" "${ROOT_DIR}/scripts/ci/go-workspace-modules.sh" \
  "${fixture}/scripts/ci/"
cp "${ROOT_DIR}/.python-version" "${fixture}/.python-version"
# 조건부 실행 검증용: 이 두 자기 테스트만 입력이 fixture 에 존재한다. 나머지는 입력 부재로
# fail-closed 실행된다.
touch "${fixture}/scripts/logs/daily-rollup-logs.sh" "${fixture}/scripts/logs/daily-rollup-logs_test.sh" \
  "${fixture}/scripts/runtime/set-iris-base-url.sh" "${fixture}/scripts/runtime/set-iris-base-url_test.sh"

cat >"${fixture}/fake-bin/bash" <<'SH'
#!/bin/bash
printf 'bash %s\n' "$*" >>"${GATE_TEST_LOG}"
SH
cat >"${fixture}/fake-bin/git" <<'SH'
#!/bin/bash
case "$*" in
  "rev-parse --verify origin/main") exit 0 ;;
  "diff --name-only origin/main...HEAD") printf '%s\n' "${GATE_TEST_CHANGED_FILES:-hololive/hololive-api/main.go}" ;;
  "diff --name-only fixture-base..fixture-head") printf '%s\n' "${GATE_TEST_CHANGED_FILES:-}" ;;
  *) exit 1 ;;
esac
SH
cat >"${fixture}/fake-bin/python" <<'SH'
#!/bin/bash
if [[ "$*" == *'import platform; print(platform.python_version())'* ]]; then
  echo '3.14.7'
  exit 0
fi
if [[ -n "${WORKSPACE_JSON:-}" ]]; then
  echo 'mod'
  exit 0
fi
printf 'python %s\n' "$*" >>"${GATE_TEST_LOG}"
SH
cat >"${fixture}/fake-bin/go" <<'SH'
#!/bin/bash
case "${1:-}" in
  work) echo '{"Use":[{"DiskPath":"./mod"}]}' ;;
  env) echo '' ;;
  list) printf 'go %s (pwd=%s)\n' "$*" "$(basename "${PWD}")" >>"${GATE_TEST_LOG}" ;;
  *) exit 1 ;;
esac
SH
cat >"${fixture}/fake-bin/govulncheck" <<'SH'
#!/bin/bash
if [[ "${1:-}" == "-version" ]]; then
  echo 'govulncheck@v1.7.0'
  exit 0
fi
printf 'govulncheck %s (pwd=%s)\n' "$*" "$(basename "${PWD}")" >>"${GATE_TEST_LOG}"
SH
cat >"${fixture}/scripts/ci/local-ci.sh" <<'SH'
#!/bin/bash
printf 'local-ci scope=%s base=%s race=%s\n' \
  "${LOCAL_CI_GO_SCOPE}" "${BASE_REF}" "${RUN_RACE_TESTS}" >>"${GATE_TEST_LOG}"
SH
chmod +x "${fixture}/fake-bin/"* "${fixture}/scripts/ci/local-ci.sh"

# run_phase <name> <phase-arg|""> [VAR=value ...]
run_phase() {
  local name="$1" phase_arg="$2"
  shift 2
  local -a args=()
  if [[ -n "${phase_arg}" ]]; then
    args=("${phase_arg}")
  fi
  : >"${TMP_DIR}/${name}.log"
  env "$@" GATE_TEST_LOG="${TMP_DIR}/${name}.log" PRE_PUSH_GATE_SCOPED=1 \
    CI_PYTHON_BIN="${fixture}/fake-bin/python" CI_PYTHON_RUNTIME_ROOT="${fixture}" \
    PATH="${fixture}/fake-bin:${PATH}" /bin/bash "${fixture}/scripts/ci/pre-push-gate.sh" "${args[@]}" \
    >"${TMP_DIR}/${name}.out" 2>"${TMP_DIR}/${name}.err"
}
range=(BASE_SHA=fixture-base HEAD_SHA=fixture-head)
code_change=(GATE_TEST_CHANGED_FILES=hololive/hololive-api/main.go)

# reusable: fast 모드, 정확한 범위, 코드 변경.
run_phase reusable --phase=reusable "${range[@]}" "${code_change[@]}"
grep -Fq 'bash scripts/check-release-version.sh' "${TMP_DIR}/reusable.log" || fail "reusable misses release policy"
grep -Fq 'bash scripts/ci/check-workflow-secrets.sh' "${TMP_DIR}/reusable.log" || fail "reusable misses workflow policy"
grep -Fq 'python scripts/ci/check-workflow-ci-owner.py' "${TMP_DIR}/reusable.log" || fail "reusable misses workflow owner policy"
grep -Fq 'bash scripts/ci/shell-syntax-sweep.sh' "${TMP_DIR}/reusable.log" || fail "reusable misses shell syntax sweep"
grep -Fq 'local-ci scope=changed base=fixture-base race=true' "${TMP_DIR}/reusable.log" || \
  fail "reusable must run local-ci with fast-route defaults"
grep -Fq 'self-test skipped (inputs unchanged): scripts/logs/daily-rollup-logs_test.sh' "${TMP_DIR}/reusable.out" || \
  fail "unchanged self-test was not skipped"
grep -Fq 'self-test skipped (inputs unchanged): scripts/runtime/set-iris-base-url_test.sh' "${TMP_DIR}/reusable.out" || \
  fail "unchanged self-test was not skipped"
if grep -Fq 'daily-rollup-logs_test.sh' "${TMP_DIR}/reusable.log"; then
  fail "unchanged self-test ran"
fi
grep -Fq 'bash scripts/ci/race-parallel-guard_test.sh' "${TMP_DIR}/reusable.log" || \
  fail "self-test with missing declared inputs must run (fail-closed)"
grep -Fq 'self-test input missing' "${TMP_DIR}/reusable.err" || fail "missing self-test input was not reported"
if grep -Fq 'test-go-workspace-modules.sh' "${TMP_DIR}/reusable.log"; then
  fail "reusable consumes sibling workspace"
fi
if grep -Fq 'govulncheck' "${TMP_DIR}/reusable.log"; then
  fail "reusable ran dependency hygiene"
fi
if grep -Fxq 'bash scripts/deploy/check-ap-rsync-manifest.sh' "${TMP_DIR}/reusable.log"; then
  fail "AP rsync manifest gate is owned by local-ci and must not run twice"
fi
if grep -Fq 'scripts/ci/public-pr-collector-helper-gate.sh' "${TMP_DIR}/reusable.log"; then
  fail "fast mode must not run the collector helper gate for an unrelated path"
fi

# 검사기가 바뀌면 그 자기 테스트만 실행된다.
run_phase reusable-changed --phase=reusable "${range[@]}" GATE_TEST_CHANGED_FILES=scripts/logs/daily-rollup-logs.sh
grep -Fq 'bash scripts/logs/daily-rollup-logs_test.sh' "${TMP_DIR}/reusable-changed.log" || \
  fail "changed checker did not trigger its self-test"
grep -Fq 'self-test skipped (inputs unchanged): scripts/runtime/set-iris-base-url_test.sh' "${TMP_DIR}/reusable-changed.out" || \
  fail "unrelated self-test was not skipped"

# full 모드는 skip 조건을 전부 해제한다.
run_phase reusable-full --phase=reusable "${range[@]}" "${code_change[@]}" PRE_PUSH_MODE=full
grep -Fq 'bash scripts/logs/daily-rollup-logs_test.sh' "${TMP_DIR}/reusable-full.log" || fail "full mode skipped a self-test"
grep -Fq 'bash scripts/runtime/set-iris-base-url_test.sh' "${TMP_DIR}/reusable-full.log" || fail "full mode skipped a self-test"
grep -Fq 'local-ci scope=all base=fixture-base race=true' "${TMP_DIR}/reusable-full.log" || fail "full mode must widen local-ci scope"
grep -Fq 'scripts/ci/public-pr-collector-helper-gate.sh' "${TMP_DIR}/reusable-full.log" || \
  fail "full mode must run the collector helper gate for an unrelated path"

# 정확한 범위가 없으면(새 branch·수동 실행) 자기 테스트는 전량 실행된다.
run_phase reusable-norange --phase=reusable
grep -Fq 'bash scripts/logs/daily-rollup-logs_test.sh' "${TMP_DIR}/reusable-norange.log" || \
  fail "missing push range must fail closed to running self-tests"
grep -Fq 'local-ci scope=changed base=origin/main race=true' "${TMP_DIR}/reusable-norange.log" || \
  fail "missing push range must keep the origin/main routing fallback"

# docs-only 판별식: docs/·*.md 만 변경, docs/ 아래 코드 없음, fast 모드, 정확한 범위.
run_phase reusable-docs --phase=reusable "${range[@]}" GATE_TEST_CHANGED_FILES=$'docs/ops/a.md\nREADME.md'
grep -Fq 'docs-only change detected; skipping reusable Go gate' "${TMP_DIR}/reusable-docs.out" || \
  fail "docs-only push did not skip the reusable Go gate"
grep -Fq 'bash scripts/check-release-version.sh' "${TMP_DIR}/reusable-docs.log" || \
  fail "release policy must run before the docs-only skip"
if grep -Fq 'local-ci' "${TMP_DIR}/reusable-docs.log"; then
  fail "docs-only push ran local-ci"
fi
run_phase reusable-docs-code --phase=reusable "${range[@]}" GATE_TEST_CHANGED_FILES=docs/ops/run.sh
grep -Fq 'local-ci scope=changed' "${TMP_DIR}/reusable-docs-code.log" || fail "docs/ code change was treated as docs-only"
run_phase reusable-docs-full --phase=reusable "${range[@]}" GATE_TEST_CHANGED_FILES=docs/ops/a.md PRE_PUSH_MODE=full
grep -Fq 'local-ci scope=all' "${TMP_DIR}/reusable-docs-full.log" || fail "full mode honored the docs-only skip"
run_phase reusable-docs-norange --phase=reusable GATE_TEST_CHANGED_FILES=docs/ops/a.md
grep -Fq 'local-ci scope=changed' "${TMP_DIR}/reusable-docs-norange.log" || \
  fail "docs-only skip must require an exact push range"

# freshness: route 의 모든 go.work 모듈에 go list -m -u 와 govulncheck.
run_phase freshness --phase=freshness "${range[@]}" "${code_change[@]}"
grep -Fq 'go list -m -u -mod=readonly all (pwd=fixture)' "${TMP_DIR}/freshness.log" || fail "freshness misses go list -m -u for root"
grep -Fq 'govulncheck ./... (pwd=fixture)' "${TMP_DIR}/freshness.log" || fail "freshness misses govulncheck for root"
grep -Fq 'govulncheck ./... (pwd=mod)' "${TMP_DIR}/freshness.log" || fail "freshness misses govulncheck for a go.work module"
if grep -Fq 'local-ci' "${TMP_DIR}/freshness.log" || grep -Fq '_test' "${TMP_DIR}/freshness.log"; then
  fail "freshness ran a commit-determined check"
fi
run_phase freshness-docs --phase=freshness "${range[@]}" GATE_TEST_CHANGED_FILES=docs/ops/a.md
grep -Fq 'docs-only change detected; skipping freshness Go gate' "${TMP_DIR}/freshness-docs.out" || \
  fail "docs-only push did not skip freshness"
[[ ! -s "${TMP_DIR}/freshness-docs.log" ]] || fail "docs-only freshness ran a scan"

# ambient: 형제 workspace 검사만, receipt 없이.
run_phase ambient --phase=ambient "${range[@]}" "${code_change[@]}"
[[ "$(head -n 1 "${TMP_DIR}/ambient.log")" == 'bash scripts/ci/test-go-workspace-modules.sh' ]] || \
  fail "ambient must start with the sibling workspace check"
[[ "$(wc -l <"${TMP_DIR}/ambient.log")" == 1 ]] || fail "ambient ran more than the sibling workspace check"
run_phase ambient-docs --phase=ambient "${range[@]}" GATE_TEST_CHANGED_FILES=docs/ops/a.md
grep -Fq 'docs-only change detected; skipping ambient Go gate' "${TMP_DIR}/ambient-docs.out" || \
  fail "docs-only push did not skip ambient"
[[ ! -s "${TMP_DIR}/ambient-docs.log" ]] || fail "docs-only ambient ran a check"

# legacy 무인자 실행은 같은 함수를 reusable → freshness → ambient 순서로 호출한다.
run_phase default "" "${range[@]}" "${code_change[@]}"
cat "${TMP_DIR}/reusable.log" "${TMP_DIR}/freshness.log" "${TMP_DIR}/ambient.log" >"${TMP_DIR}/expected.log"
cmp -s "${TMP_DIR}/expected.log" "${TMP_DIR}/default.log" || fail "default order is not reusable -> freshness -> ambient"

if run_phase invalid-mode "" PRE_PUSH_MODE=invalid; then
  fail "invalid mode was accepted"
fi
grep -Fq 'bash scripts/check-release-version.sh' "${TMP_DIR}/invalid-mode.log" || \
  fail "default path must run release policy before route validation"
if grep -Fq 'scripts/ci/check-workflow-secrets.sh' "${TMP_DIR}/invalid-mode.log"; then
  fail "workflow checks ran before route validation failed"
fi

if run_phase positional reusable; then
  fail "positional phase alias must fail"
fi
if PRE_PUSH_GATE_SCOPED=1 CI_PYTHON_BIN="${fixture}/fake-bin/python" CI_PYTHON_RUNTIME_ROOT="${fixture}" \
  PATH="${fixture}/fake-bin:${PATH}" \
  /bin/bash "${fixture}/scripts/ci/pre-push-gate.sh" --phase reusable >/dev/null 2>&1; then
  fail "split phase argument must fail"
fi

fingerprint_fixture="${TMP_DIR}/fingerprint"
sibling_fixture="${TMP_DIR}/sib"
mkdir -p "${fingerprint_fixture}/scripts/ci" "${fingerprint_fixture}/fake-bin" "${sibling_fixture}"
cp "${GATE}" "${fingerprint_fixture}/scripts/ci/pre-push-gate.sh"
cp "${ROOT_DIR}/scripts/ci/pre-push-gate-profile-v1.json" "${fingerprint_fixture}/scripts/ci/"
cp "${ROOT_DIR}/scripts/ci/go-tooling.sh" "${fingerprint_fixture}/scripts/ci/"
cp "${ROOT_DIR}/scripts/ci/local-ci.sh" "${fingerprint_fixture}/scripts/ci/"
cp "${ROOT_DIR}/scripts/ci/python-runner.sh" "${ROOT_DIR}/scripts/ci/python-runtime.sh" \
  "${fingerprint_fixture}/scripts/ci/"
cp "${ROOT_DIR}/.python-version" "${fingerprint_fixture}/.python-version"
# go.work 의 ../ 모듈(형제 checkout)은 toolchain identity 에 들어간다.
cat >"${fingerprint_fixture}/fake-bin/go" <<'SH'
#!/bin/bash
case "${1:-}" in
  version) echo 'go version go1.27.0 linux/amd64' ;;
  env) printf 'go1.27.0\nlinux\namd64\ngo1.27.0+auto\n' ;;
  work) echo '{"Use":[{"DiskPath":"../sib"},{"DiskPath":"./"}]}' ;;
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
git -C "${sibling_fixture}" init -q
git -C "${sibling_fixture}" config user.name gate-test
git -C "${sibling_fixture}" config user.email gate-test@example.invalid
printf 'package sib\n' >"${sibling_fixture}/sib.go"
git -C "${sibling_fixture}" add sib.go
git -C "${sibling_fixture}" commit -qm sibling

run_fingerprint() {
  CI_PYTHON_BIN="${CI_PYTHON_BIN}" CI_PYTHON_RUNTIME_ROOT="${fingerprint_fixture}" \
    PATH="${fingerprint_fixture}/fake-bin:${PATH}" \
    /bin/bash "${fingerprint_fixture}/scripts/ci/pre-push-gate.sh" --phase=fingerprint
}

run_fingerprint >"${TMP_DIR}/fingerprint.json"
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

# 형제 checkout 의 working tree 변경은 toolchain identity 를 바꿔 reusable receipt 를 무효화한다.
printf 'package sib // edited\n' >"${sibling_fixture}/sib.go"
run_fingerprint >"${TMP_DIR}/fingerprint-sibling-dirty.json"
"${CI_PYTHON_BIN}" - "${TMP_DIR}/fingerprint.json" "${TMP_DIR}/fingerprint-sibling-dirty.json" <<'PY'
import json
import sys

clean = json.load(open(sys.argv[1], encoding="utf-8"))
dirty = json.load(open(sys.argv[2], encoding="utf-8"))
if clean["toolchain_fingerprint"] == dirty["toolchain_fingerprint"]:
    raise SystemExit("sibling checkout change did not invalidate the toolchain identity")
if clean["route_fingerprint"] != dirty["route_fingerprint"]:
    raise SystemExit("sibling checkout change must not alter the route fingerprint")
PY
mv "${sibling_fixture}/.git" "${sibling_fixture}/.git-detached"
if run_fingerprint >/dev/null 2>&1; then
  fail "sibling that is not a git checkout must fail the fingerprint closed"
fi

printf 'dirty\n' >"${fingerprint_fixture}/untracked"
if run_fingerprint >/dev/null 2>&1; then
  fail "dirty repository fingerprint must fail closed"
fi

echo "[PASS] pre-push gate phase and fingerprint contract"
