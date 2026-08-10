#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/ci/local-ci-files.sh"
source "${ROOT_DIR}/scripts/ci/local-ci-gofix.sh"

tmpdir="$(mktemp -d)"
check_pid=""
cleanup() {
    if [[ -n "${check_pid}" ]] && kill -0 "${check_pid}" 2>/dev/null; then
        kill "${check_pid}" 2>/dev/null || true
        wait "${check_pid}" 2>/dev/null || true
    fi
    rm -rf "${tmpdir}"
}
trap cleanup EXIT

stack="${tmpdir}/stack"
fixture="${stack}/hololive-bot"
shared="${stack}/shared-go"
iris="${stack}/iris-client-go"
fakebin="${tmpdir}/bin"
ready="${tmpdir}/go-fix.ready"
proceed="${tmpdir}/go-fix.proceed"
stamp="${tmpdir}/go-fix.stamp"
mkdir -p "${fixture}" "${shared}" "${iris}" "${fakebin}"
printf 'package fixture\n' >"${fixture}/target.go"
printf 'module example.com/fixture\n\ngo 1.26.5\n' >"${fixture}/go.mod"
printf 'go 1.26.5\nuse (\n\t.\n\t../shared-go\n\t../iris-client-go\n)\n' >"${fixture}/go.work"
printf 'package shared\n' >"${shared}/shared.go"
printf 'module example.com/shared\n\ngo 1.26.5\n' >"${shared}/go.mod"
printf 'package iris\n' >"${iris}/iris.go"
printf 'module example.com/iris\n\ngo 1.26.5\n' >"${iris}/go.mod"

for repo in "${shared}" "${iris}"; do
    git -C "${repo}" init --quiet
    git -C "${repo}" add .
    git -C "${repo}" -c user.name=fixture -c user.email=fixture@example.invalid commit --quiet -m fixture
done

cat >"${fakebin}/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == 'fix' ]]
: >"${FAKE_GO_READY}"
while [[ ! -e "${FAKE_GO_PROCEED}" ]]; do
    sleep 0.01
done
if [[ "${FAKE_GO_REWRITE:-false}" == 'true' ]]; then
    printf '// go-fix-rewrite\n' >>"${FAKE_GO_REWRITE_PATH}"
fi
EOF
chmod +x "${fakebin}/go"

ROOT_DIR="${fixture}"
LOCAL_CI_TMPDIR="${tmpdir}/work"
GO_PACKAGES=(./)
has_go_packages() {
    return 0
}
go_source_files() {
    printf 'target.go\n'
}
go_fix_memo_key() {
    sha256sum "${ROOT_DIR}/target.go" "${ROOT_DIR}/../shared-go/shared.go" \
        "${ROOT_DIR}/../iris-client-go/iris.go" | sha256sum | awk '{print $1}'
}
go_fix_memo_stamp_file() {
    printf '%s\n' "${stamp}"
}

wait_for_file() {
    local file="$1"
    local attempt
    for ((attempt = 0; attempt < 200; attempt++)); do
        [[ -e "${file}" ]] && return 0
        sleep 0.01
    done
    return 1
}

PATH="${fakebin}:${PATH}" \
    FAKE_GO_READY="${ready}" \
    FAKE_GO_PROCEED="${proceed}" \
    FAKE_GO_REWRITE=false \
    FAKE_GO_REWRITE_PATH=target.go \
    check_go_fix >"${tmpdir}/concurrent.out" 2>&1 &
check_pid=$!
wait_for_file "${ready}"
printf '// concurrent live edit\n' >>"${fixture}/target.go"
touch "${proceed}"
if ! wait "${check_pid}"; then
    cat "${tmpdir}/concurrent.out" >&2
    exit 1
fi
check_pid=""
if grep -Fq 'go fix would update modern Go compatibility rewrites:' "${tmpdir}/concurrent.out"; then
    cat "${tmpdir}/concurrent.out" >&2
    exit 1
fi
[[ ! -e "${stamp}" ]]
echo '[PASS] concurrent live edit is not reported as go fix drift or memoized'

rm -f "${ready}" "${proceed}"
PATH="${fakebin}:${PATH}" \
    FAKE_GO_READY="${ready}" \
    FAKE_GO_PROCEED="${proceed}" \
    FAKE_GO_REWRITE=true \
    FAKE_GO_REWRITE_PATH=target.go \
    check_go_fix >"${tmpdir}/rewrite.out" 2>&1 &
check_pid=$!
wait_for_file "${ready}"
touch "${proceed}"
set +e
wait "${check_pid}"
status=$?
set -e
check_pid=""
[[ "${status}" -ne 0 ]]
grep -Fq ' - target.go' "${tmpdir}/rewrite.out"
echo '[PASS] go fix rewrites remain blocking'

rm -f "${ready}" "${proceed}" "${stamp}"
PATH="${fakebin}:${PATH}" \
    FAKE_GO_READY="${ready}" \
    FAKE_GO_PROCEED="${proceed}" \
    FAKE_GO_REWRITE=false \
    FAKE_GO_REWRITE_PATH=../shared-go/shared.go \
    check_go_fix >"${tmpdir}/sibling-concurrent.out" 2>&1 &
check_pid=$!
wait_for_file "${ready}"
printf '// concurrent sibling edit\n' >>"${shared}/shared.go"
touch "${proceed}"
if ! wait "${check_pid}"; then
    cat "${tmpdir}/sibling-concurrent.out" >&2
    exit 1
fi
check_pid=""
if grep -Fq 'go fix would update modern Go compatibility rewrites:' "${tmpdir}/sibling-concurrent.out"; then
    cat "${tmpdir}/sibling-concurrent.out" >&2
    exit 1
fi
[[ ! -e "${stamp}" ]]
echo '[PASS] concurrent sibling edit is not reported or memoized'

rm -f "${ready}" "${proceed}"
PATH="${fakebin}:${PATH}" \
    FAKE_GO_READY="${ready}" \
    FAKE_GO_PROCEED="${proceed}" \
    FAKE_GO_REWRITE=true \
    FAKE_GO_REWRITE_PATH=../shared-go/shared.go \
    check_go_fix >"${tmpdir}/shared-rewrite.out" 2>&1 &
check_pid=$!
wait_for_file "${ready}"
touch "${proceed}"
set +e
wait "${check_pid}"
status=$?
set -e
check_pid=""
[[ "${status}" -ne 0 ]]
grep -Fq ' - ../shared-go/shared.go' "${tmpdir}/shared-rewrite.out"
echo '[PASS] shared-go go fix rewrites remain blocking'

rm -f "${ready}" "${proceed}"
PATH="${fakebin}:${PATH}" \
    FAKE_GO_READY="${ready}" \
    FAKE_GO_PROCEED="${proceed}" \
    FAKE_GO_REWRITE=true \
    FAKE_GO_REWRITE_PATH=../iris-client-go/iris.go \
    check_go_fix >"${tmpdir}/iris-rewrite.out" 2>&1 &
check_pid=$!
wait_for_file "${ready}"
touch "${proceed}"
set +e
wait "${check_pid}"
status=$?
set -e
check_pid=""
[[ "${status}" -ne 0 ]]
grep -Fq ' - ../iris-client-go/iris.go' "${tmpdir}/iris-rewrite.out"
echo '[PASS] iris-client-go go fix rewrites remain blocking'
