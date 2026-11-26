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

source "${SCRIPT_DIR}/go-work-sync-drift.sh"
GO_PACKAGES=()
SCRIPTTEST_PACKAGES=()
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

check_canonical_modules() {
    local module package
    for module in "${GO_OWNED_MODULES[@]}"; do
        (
            cd "${module}"
            export GOWORK=off
            run_step "go mod tidy -diff: ${module}" go mod tidy -diff
            for package in "${GO_PACKAGES[@]}"; do
                if [[ "${package}" == "./${module}/..." ]]; then
                    run_step "Canonical vet (GOWORK=off): ${module}" go vet ./...
                    break
                fi
            done
        )
    done
}

go_mod_readonly() {
    GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" "$@"
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

run_go_tests() {
    local -a parallel=()

    [[ -z "${RACE_TEST_PARALLEL:-}" ]] || parallel=(-p "${RACE_TEST_PARALLEL}")
    if [[ "${RUN_RACE_TESTS}" == "true" ]]; then
        run_go_package_step "Go race test" go_mod_readonly go test -race "${parallel[@]}" -count=1
        run_go_package_step "Go non-race-only tests" go_mod_readonly bash scripts/ci/go-test-nonrace.sh
    else
        run_go_package_step "Go test" go_mod_readonly go test "${parallel[@]}" -count=1
    fi
}

run_database_steps() (
    if has_go_packages; then
        if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
            run_step "Provision test PostgreSQL" provision_integration_test_database
        fi
        if [[ "${collector_in_scope}" == true ]]; then
            run_step "collector production default JSON tests" bash ./scripts/ci/public-pr-go-gate.sh hololive/hololive-collector test-prod
        fi
        run_go_tests
    fi
    check_integration_tests
)

run_local_ci() {
    local run_collector_js="$1" collector_in_scope=false package
    configure_go_packages
    echo "[LOCAL CI] Go package scope: ${LOCAL_CI_GO_SCOPE} (${#GO_PACKAGES[@]} package patterns)"
    if has_go_packages; then
        printf '[LOCAL CI]   %s\n' "${GO_PACKAGES[@]}"
        for package in "${GO_PACKAGES[@]}"; do
            if [[ "${package}" == ./hololive/hololive-collector/... ]]; then
                collector_in_scope=true
                break
            fi
        done
        if [[ -n "${RACE_TEST_PARALLEL:-}" && ! "${RACE_TEST_PARALLEL}" =~ ^[1-9][0-9]*$ ]]; then
            echo "[LOCAL CI] invalid RACE_TEST_PARALLEL=${RACE_TEST_PARALLEL}; expected a positive integer" >&2
            return 1
        fi
    fi

    run_step "Architecture gates" ./scripts/architecture/ci-boundary-gate.sh
    run_step "Sensitive log scan" ./scripts/refactor/grep-sensitive-logs.sh
    run_step "Go toolchain" go env GOVERSION
    run_step "go work sync drift" verify_go_work_sync_drift "${ROOT_DIR}"
    check_canonical_modules
    check_integration_tag_compilation
    check_golangci_lint
    check_nilaway
    run_go_package_step "Go build" go_mod_readonly go build
    if [[ "${collector_in_scope}" == true || "${run_collector_js}" == true ]]; then
        run_step "collector YouTube.js dependencies" bash scripts/ci/public-pr-collector-helper-install.sh
    fi
    run_step "collector production build" bash scripts/ci/public-pr-go-gate.sh hololive/hololive-collector build-prod
    run_step "X Spaces helper tests" bash scripts/ci/public-pr-x-spaces-helper-gate.sh
    run_step "production Go workspace gate" ./scripts/ci/check-production-go-workspace.sh
    run_step "PostgreSQL capacity gate" ./scripts/ci/check-postgres-capacity.sh
    if (( ${#SCRIPTTEST_PACKAGES[@]} )); then
        run_step "Deploy/runtime script tests" bash scripts/ci/run-script-tests.sh "${SCRIPTTEST_PACKAGES[@]}"
    fi
    run_database_steps
    if [[ "${run_collector_js}" == true ]]; then
        run_step "YouTube.js typecheck" npm --prefix hololive/hololive-collector/youtubejs run typecheck
        run_step "YouTube.js tests" npm --prefix hololive/hololive-collector/youtubejs test
        run_step "Event Grade helper tests" npm --prefix hololive/hololive-collector/eventgrade test
    fi
    echo "[LOCAL CI] Passed"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    case "$#:${1:-}" in
        1:--integration-tests-only) check_integration_tests; exit 0 ;;
        0:) ;;
        *) echo "usage: $0 [--integration-tests-only]" >&2; exit 2 ;;
    esac
    if [[ "${LOCAL_CI_GO_SCOPE}" == changed ]]; then
        head_ref="${HEAD_REF:-HEAD}"
        require_checkout_head "${head_ref}"
        if base="$(git merge-base "${BASE_REF:-origin/main}" "${head_ref}")"; then
            load_changed_files "${base}..${head_ref}"
        else
            echo "[LOCAL CI] No merge base; checking all owned Go modules" >&2
            LOCAL_CI_GO_SCOPE=all
        fi
    fi
    run_local_ci false
fi
