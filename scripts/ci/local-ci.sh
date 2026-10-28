#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
export GOTOOLCHAIN="${GOTOOLCHAIN-auto}"
source "${SCRIPT_DIR}/go-workspace-modules.sh"
source "${SCRIPT_DIR}/go-tooling.sh"
source "${SCRIPT_DIR}/nilaway-inputs.sh"
source "${SCRIPT_DIR}/local-ci-nilaway.sh"
cd "${ROOT_DIR}"

# local-ci-files.sh·local-ci-packages.sh가 형제 모듈을 포함한 이 배열로 경로를 판별한다.
# shellcheck disable=SC2034
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

    for module in "${GO_OWNED_MODULES[@]}"; do
        run_step "go mod tidy -diff: ${module}" bash -c "cd '$module' && GOWORK=off go mod tidy -diff"
    done
}

go_mod_readonly() {
    GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" "$@"
}

check_canonical_module_builds() {
    local module

    for module in "${GO_OWNED_MODULES[@]}"; do
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

check_golangci_lint() {
    if ! has_go_packages; then
        echo "[LOCAL CI] Skip golangci-lint: no Go packages in scope"
        echo
        return 0
    fi

    local golangci_lint_bin
    golangci_lint_bin="$(ensure_golangci_lint)"

    run_step "golangci-lint" "${golangci_lint_bin}" run -c .golangci.yml "${GO_PACKAGES[@]}"
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
check_golangci_lint
check_nilaway
run_go_package_step "Go build" go_mod_readonly go build
run_step "collector YouTube.js dependencies" bash scripts/ci/public-pr-collector-helper-install.sh
# production JSON 테스트는 race 단계가 덮는 collector 테스트를 CGO_ENABLED=0으로 다시 돌려 환경 때문에 건너뛴
# 테스트가 없는지 확인한다. Go 범위에 collector 모듈이 없으면 결과가 바뀌지 않으므로 생략한다.
if [[ " ${GO_PACKAGES[*]} " == *" ./hololive/hololive-youtube-collector/... "* ]]; then
    run_step "collector production default JSON tests" bash ./scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector test-prod
else
    echo "[LOCAL CI] Skip collector production default JSON tests: collector module is outside the Go scope"
    echo
fi
run_step "collector production build" bash ./scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector build-prod
run_step "X Spaces helper tests" bash scripts/ci/public-pr-x-spaces-helper-gate.sh
run_step "production Go workspace gate" ./scripts/ci/check-production-go-workspace.sh
run_step "AP rsync manifest gate" ./scripts/deploy/check-ap-rsync-manifest.sh
run_step "PostgreSQL capacity gate" ./scripts/ci/check-postgres-capacity.sh
run_step "YouTube plane performance budget" ./scripts/perf/check-youtube-plane-budget.sh
# hololive-dbtest는 테스트마다 템플릿을 복제한 데이터베이스로 격리하므로 패키지들이 PostgreSQL 하나를 공유한다.
# 패키지마다 testcontainers PostgreSQL을 띄우면 기동 비용과 남는 컨테이너 때문에 -p를 낮춰야 했다.
# subshell에서 실행해 TEST_DATABASE_URL이 뒤의 통합 테스트 선택으로 새지 않고, 종료 시 컨테이너를 지운다.
run_go_tests() {
    local -a parallel=()

    if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
        provision_integration_test_database
    fi
    [[ -z "${RACE_TEST_PARALLEL:-}" ]] || parallel=(-p "${RACE_TEST_PARALLEL}")
    if [[ "${RUN_RACE_TESTS}" == "true" ]]; then
        run_go_package_step "Go race test" go_mod_readonly go test -race "${parallel[@]}" -count=1
        run_go_package_step "Go non-race-only tests" go_mod_readonly bash scripts/ci/go-test-nonrace.sh
    else
        run_go_package_step "Go test" go_mod_readonly go test "${parallel[@]}" -count=1
    fi
}

if has_go_packages; then
    # 산술 컨텍스트에 넣지 않더라도 go test 인자로 전달되므로 호출 env 값은 양의 정수만 허용한다(82cbfe75).
    if [[ -n "${RACE_TEST_PARALLEL:-}" && ! "${RACE_TEST_PARALLEL}" =~ ^[1-9][0-9]*$ ]]; then
        echo "[LOCAL CI] invalid RACE_TEST_PARALLEL=${RACE_TEST_PARALLEL}; expected a positive integer" >&2
        exit 1
    fi
    (run_go_tests)
fi

check_integration_tests

# 의존성 hygiene(govulncheck)은 scripts/ci/pre-push-gate.sh 가 소유한다.

echo "[LOCAL CI] Passed"
