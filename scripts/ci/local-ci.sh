#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
. "${SCRIPT_DIR}/python-runtime.sh"
repo_python_init
export GOTOOLCHAIN="${GOTOOLCHAIN-auto}"
source "${SCRIPT_DIR}/go-workspace-modules.sh"
source "${SCRIPT_DIR}/go-tooling.sh"
source "${SCRIPT_DIR}/nilaway-inputs.sh"
source "${SCRIPT_DIR}/local-ci-nilaway.sh"
cd "${ROOT_DIR}"

GO_MODULES=("${GO_WORKSPACE_MODULES[@]}")
source "${SCRIPT_DIR}/local-ci-files.sh"
source "${SCRIPT_DIR}/go-work-sync-drift.sh"
# local-ci-packages.sh가 이 배열을 입력으로 사용한다.
# shellcheck disable=SC2034
mapfile -t ROOT_GO_PACKAGES < <(root_go_package_patterns)
# shellcheck disable=SC2034
mapfile -t WORKSPACE_GO_PACKAGES < <(go_workspace_package_patterns)
GO_PACKAGES=()
source "${SCRIPT_DIR}/local-ci-packages.sh"
source "${SCRIPT_DIR}/local-ci-integration.sh"

LOCAL_CI_GO_SCOPE="${LOCAL_CI_GO_SCOPE:-all}"
RUN_RACE_TESTS="${RUN_RACE_TESTS:-true}"
RUN_NILAWAY="${RUN_NILAWAY:-true}"
STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}"
RUN_INTEGRATION_TESTS="${RUN_INTEGRATION_TESTS:-false}"


run_step() {
    local name="$1"
    shift

    echo "[LOCAL CI] ${name}"
    "$@"
    echo
}

check_go_mod_tidy() {
    local module

    for module in . "${GO_MODULES[@]}"; do
        run_step "go mod tidy -diff: ${module}" bash -c "cd '$module' && GOWORK=off go mod tidy -diff"
    done
}

check_staticcheck() {
    if [[ "${STRICT_STATICCHECK}" != "true" ]]; then
        echo "[LOCAL CI] Skip staticcheck: STRICT_STATICCHECK=${STRICT_STATICCHECK}"
        echo
        return 0
    fi

    if ! has_go_packages; then
        echo "[LOCAL CI] Skip staticcheck: no Go packages in scope"
        echo
        return 0
    fi

    local staticcheck_bin
    staticcheck_bin="$(ensure_staticcheck)"

    run_step "staticcheck" "${staticcheck_bin}" "${GO_PACKAGES[@]}"
}

go_mod_readonly() {
    GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" "$@"
}

check_canonical_module_builds() {
    local module

    for module in . "${GO_MODULES[@]}"; do
        run_step "Canonical vet (GOWORK=off): ${module}" \
            bash -c "cd '${module}' && GOWORK=off go vet ./..."
    done
}

run_go_package_step() {
    local name="$1"
    shift

    if ! has_go_packages; then
        echo "[LOCAL CI] Skip ${name}: no Go packages in scope"
        echo
        return 0
    fi

    run_step "${name}" "$@" "${GO_PACKAGES[@]}"
}

owned_go_package_patterns() {
    local package_pattern
    for package_pattern in "${GO_PACKAGES[@]}"; do
        case "${package_pattern}" in
            ./../shared-go/...|../shared-go/...|./../iris-client-go/...|../iris-client-go/...)
                continue
                ;;
        esac
        printf '%s\n' "${package_pattern}"
    done
}

check_golangci_lint() {
    local packages=()
    mapfile -t packages < <(owned_go_package_patterns)
    if (( ${#packages[@]} == 0 )); then
        echo "[LOCAL CI] Skip golangci-lint: no owned Go packages in scope"
        echo
        return 0
    fi

    local golangci_lint_bin
    golangci_lint_bin="$(ensure_golangci_lint)"

    run_step "golangci-lint" "${golangci_lint_bin}" run -c .golangci.yml "${packages[@]}"
}


if [[ "${1:-}" == "--integration-tests-only" ]]; then
    if (( $# != 1 )); then
        echo "usage: $0 [--integration-tests-only]" >&2
        exit 2
    fi
    check_integration_tests
    exit 0
fi
if (( $# != 0 )); then
    echo "usage: $0 [--integration-tests-only]" >&2
    exit 2
fi

# 배포·운영 스크립트 테스트는 scripts/ci/pre-push-gate.sh 가 입력 변경 시에만 실행한다.
configure_go_packages
echo "[LOCAL CI] Go package scope: ${LOCAL_CI_GO_SCOPE} (${#GO_PACKAGES[@]} packages)"
if has_go_packages; then
    printf '[LOCAL CI]   %s\n' "${GO_PACKAGES[@]}"
else
    echo "[LOCAL CI]   no Go packages selected"
fi
echo

run_step "Architecture gates" ./scripts/architecture/ci-boundary-gate.sh
run_step "Sensitive log scan" ./scripts/refactor/grep-sensitive-logs.sh
run_step "Go toolchain" go env GOVERSION
run_step "go work sync drift" verify_go_work_sync_drift "${ROOT_DIR}"
# gofmt·go fix modernizer drift는 golangci-lint의 formatters와 modernize 린터가 소유한다.
check_go_mod_tidy
check_canonical_module_builds
check_integration_tag_compilation
check_staticcheck
check_golangci_lint
check_nilaway
run_go_package_step "Go build" go_mod_readonly go build
run_step "PGO default gate" ./scripts/ci/check-pgo-default.sh
run_step "collector YouTube.js dependencies" bash scripts/ci/public-pr-collector-helper-install.sh
run_step "collector production default JSON tests" bash ./scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector test-prod
run_step "collector production build" bash ./scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector build-prod
run_step "X Spaces helper tests" bash scripts/ci/public-pr-x-spaces-helper-gate.sh
run_step "production Go workspace gate" ./scripts/ci/check-production-go-workspace.sh
run_step "AP rsync manifest gate" ./scripts/deploy/check-ap-rsync-manifest.sh
run_step "PostgreSQL capacity gate" ./scripts/ci/check-postgres-capacity.sh
run_step "YouTube plane performance budget" ./scripts/perf/check-youtube-plane-budget.sh
if [[ "${RUN_RACE_TESTS}" == "true" ]]; then
    RACE_TEST_PARALLEL="${RACE_TEST_PARALLEL:-$(( ($(nproc) + 2) / 3 ))}"
    # 산술 컨텍스트는 변수 내용을 재귀 평가하므로, 검증 없이 (( ))에 넣으면 호출 env 가
    # 제어하는 RACE_TEST_PARALLEL 로 코드가 실행될 수 있다(82cbfe75). 정수만 허용.
    if [[ ! "${RACE_TEST_PARALLEL}" =~ ^[0-9]+$ ]]; then
        echo "[LOCAL CI] invalid RACE_TEST_PARALLEL=${RACE_TEST_PARALLEL}; expected a non-negative integer" >&2
        exit 1
    fi
    (( 10#${RACE_TEST_PARALLEL} < 2 )) && RACE_TEST_PARALLEL=2
    run_go_package_step "Go race test (testcontainer boot fan-out limited via -p ${RACE_TEST_PARALLEL})" \
        go_mod_readonly go test -race -p "${RACE_TEST_PARALLEL}" -count=1
    run_go_package_step "Go non-race-only tests" go_mod_readonly bash scripts/ci/go-test-nonrace.sh
else
    run_go_package_step "Go test" go_mod_readonly go test -count=1
fi

check_integration_tests

# 의존성 hygiene(govulncheck)은 scripts/ci/pre-push-gate.sh 가 소유한다.

echo "[LOCAL CI] Passed"
