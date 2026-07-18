#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.5+auto}"
source "${SCRIPT_DIR}/go-workspace-modules.sh"
source "${SCRIPT_DIR}/go-tooling.sh"
source "${SCRIPT_DIR}/nilaway-inputs.sh"
cd "${ROOT_DIR}"

GO_MODULES=("${GO_WORKSPACE_MODULES[@]}")
source "${SCRIPT_DIR}/local-ci-files.sh"
# These arrays are the input contract consumed by local-ci-packages.sh.
# shellcheck disable=SC2034
mapfile -t ROOT_GO_PACKAGES < <(root_go_package_patterns)
# shellcheck disable=SC2034
mapfile -t WORKSPACE_GO_PACKAGES < <(go_workspace_package_patterns)
GO_PACKAGES=()
source "${SCRIPT_DIR}/local-ci-packages.sh"
source "${SCRIPT_DIR}/local-ci-gofix.sh"

LOCAL_CI_GO_SCOPE="${LOCAL_CI_GO_SCOPE:-all}"
RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-true}"
RUN_RACE_TESTS="${RUN_RACE_TESTS:-true}"
RUN_NILAWAY="${RUN_NILAWAY:-true}"
STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}"
RUN_ADMIN_TOUCH_GUARDRAIL="${RUN_ADMIN_TOUCH_GUARDRAIL:-true}"
RUN_INTEGRATION_TESTS="${RUN_INTEGRATION_TESTS:-false}"

INTEGRATION_TEST_PACKAGES=(
    ./hololive/hololive-api/internal/planes/llm/internal/service/majorevent/summarizer
    ./hololive/hololive-api/internal/planes/llm/internal/service/membernews/summarizer
    ./hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch
    ./hololive/hololive-youtube-producer/internal/runtime/ingestionlease
)
INTEGRATION_TAG_PACKAGES=(
    ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
    ./hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo
)
INTEGRATION_POSTGRES_IMAGE="${INTEGRATION_POSTGRES_IMAGE:-postgres:18-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15}"
INTEGRATION_VALKEY_IMAGE="${INTEGRATION_VALKEY_IMAGE:-valkey/valkey:9.1.0-alpine3.23@sha256:a35428eba9043cc0b79dbe54100f0c92784f2de00ad09b01182bfb1c5c83d1bd}"
INTEGRATION_TEST_DB_CONTAINER=""
INTEGRATION_TEST_VALKEY_CONTAINER=""

run_step() {
    local name="$1"
    shift

    echo "[LOCAL CI] ${name}"
    "$@"
    echo
}

check_go_toolchain() {
    # 1.26.x patch는 자동 추종한다: minor family만 강제하고 정확한 patch는 고정하지 않는다.
    # 새 patch(예: go1.26.5)가 설치되면 파일 수정 없이 그대로 통과한다.
    local family="${GO_TOOLCHAIN_FAMILY:-go1.26.}"
    local actual
    actual="$(go env GOVERSION)"
    case "${actual}" in
        "${family}"*) ;;
        *)
            echo "expected ${family}x toolchain, got ${actual}" >&2
            exit 1
            ;;
    esac
}

ensure_go_mod_toolchains() {
    # go.mod/go.work의 toolchain 하한을 현재 보안 patch로 고정한다.
    # GOTOOLCHAIN=go1.26.5+auto가 필요한 patch toolchain을 확보한다.
    local module
    local pin="${GO_TOOLCHAIN_PIN:-go1.26.5}"
    local pin_version="${pin#go}"

    # go directive가 핀 이상이면 directive 자체가 하한이고, 그때의 toolchain 라인은
    # 중복이라 GOWORK=off 정본 검사(boundary/export의 go list)가 "go mod tidy 필요"로
    # 거부한다 — 핀이 directive보다 높을 때만 라인을 심어야 두 검사와 공존한다.
    stamp_toolchain_if_below_pin() {
        local mod_dir="$1"
        local go_directive
        go_directive="$(awk '$1 == "go" {print $2; exit}' "${mod_dir}/go.mod")"
        [[ -z "${go_directive}" ]] && return 0
        if [[ "$(printf '%s\n%s\n' "${go_directive}" "${pin_version}" | sort -V | head -1)" == "${go_directive}" \
            && "${go_directive}" != "${pin_version}" ]]; then
            (cd "${mod_dir}" && go mod edit -toolchain="${pin}")
        fi
    }

    local work_go_directive
    work_go_directive="$(awk '$1 == "go" {print $2; exit}' go.work)"
    if [[ -n "${work_go_directive}" && "${work_go_directive}" != "${pin_version}" \
        && "$(printf '%s\n%s\n' "${work_go_directive}" "${pin_version}" | sort -V | head -1)" == "${work_go_directive}" ]]; then
        go work edit -toolchain="${pin}"
    fi
    stamp_toolchain_if_below_pin .
    for module in "${GO_MODULES[@]}"; do
        # sibling repo(../shared-go)의 go.mod는 그 repo 소유 — 스탬프하면 그쪽 정본 검사와 충돌.
        case "${module}" in ../*) continue ;; esac
        stamp_toolchain_if_below_pin "${module}"
    done
}

check_go_work_sync() (
    set -euo pipefail

    local temp_root
    local temp_repo
    local file
    local candidate
    local drift=false
    local sync_files=()

    mapfile -t sync_files < <(workspace_metadata_files)
    temp_root="$(mktemp -d)"
    temp_repo="${temp_root}/hololive-bot"
    trap 'rm -rf "${temp_root}"' EXIT

    for file in "${sync_files[@]}"; do
        candidate="${temp_repo}/${file}"
        mkdir -p "$(dirname "${candidate}")"
        if [[ -f "${file}" ]]; then
            cp -p "${file}" "${candidate}"
        fi
    done

    cd "${temp_repo}"
    go work sync
    ensure_go_mod_toolchains
    cd "${ROOT_DIR}"

    for file in "${sync_files[@]}"; do
        candidate="${temp_repo}/${file}"
        if cmp -s "${file}" "${candidate}"; then
            continue
        fi

        drift=true
        if [[ -f "${file}" && -f "${candidate}" ]]; then
            diff -u --label "${file}" --label "${file} (go work sync)" "${file}" "${candidate}" >&2 || true
        elif [[ -f "${candidate}" ]]; then
            echo "go work sync would create ${file}" >&2
        else
            echo "go work sync would remove ${file}" >&2
        fi
    done

    if [[ "${drift}" == "true" ]]; then
        echo "go work sync changed workspace or module metadata; commit the sync result" >&2
        exit 1
    fi
)

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
        run_step "Canonical build (GOWORK=off): ${module}" \
            bash -c "cd '${module}' && GOWORK=off go build ./..."
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

check_nilaway() {
    if [[ "${RUN_NILAWAY}" != "true" ]]; then
        echo "[LOCAL CI] Skip NilAway: RUN_NILAWAY=${RUN_NILAWAY}"
        echo
        return 0
    fi

    local packages=()
    mapfile -t packages < <(owned_go_package_patterns)
    if (( ${#packages[@]} == 0 )); then
        echo "[LOCAL CI] Skip NilAway: no owned Go packages in scope"
        echo
        return 0
    fi

    local nilaway_bin
    nilaway_bin="$(ensure_nilaway)"

    # NilAway는 패턴당 10~16GB RSS까지 자란다 — 3병렬이 2026-07-04 호스트 global OOM(~40GB 스파이크)을 냈다.
    local nilaway_parallel="${NILAWAY_PARALLEL:-1}"
    local nilaway_gomemlimit="${NILAWAY_GOMEMLIMIT:-10GiB}"
    validate_nilaway_parallel "${nilaway_parallel}" || return 1
    validate_nilaway_gomemlimit "${nilaway_gomemlimit}" || return 1
    local nilaway_tmp_parent="${LOCAL_CI_TMPDIR:-${ROOT_DIR}/.tmp/local-ci}"
    mkdir -p "${nilaway_tmp_parent}"
    local nilaway_tmp
    nilaway_tmp="$(mktemp -d "${nilaway_tmp_parent%/}/nilaway.XXXXXX")"

    local nilaway_fail=0
    local running=0
    local package_pattern
    for package_pattern in "${packages[@]}"; do
        env GOMEMLIMIT="${nilaway_gomemlimit}" GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" \
            "${nilaway_bin}" -pretty-print "${package_pattern}" \
            >"${nilaway_tmp}/$(printf '%s' "${package_pattern}" | tr './' '__').log" 2>&1 &
        running=$(( running + 1 ))
        if (( running >= nilaway_parallel )); then
            wait -n || nilaway_fail=1
            running=$(( running - 1 ))
        fi
    done
    while (( running > 0 )); do
        wait -n || nilaway_fail=1
        running=$(( running - 1 ))
    done

    for package_pattern in "${packages[@]}"; do
        echo "[LOCAL CI] NilAway: ${package_pattern}"
        cat "${nilaway_tmp}/$(printf '%s' "${package_pattern}" | tr './' '__').log"
        echo
    done
    rm -rf "${nilaway_tmp}"

    if (( nilaway_fail != 0 )); then
        echo "NilAway failed or reported issues for at least one package pattern" >&2
        return 1
    fi
}

check_integration_tag_compilation() {
    run_step "Integration-tag vet" \
        go_mod_readonly go vet -tags=integration "${INTEGRATION_TAG_PACKAGES[@]}"
}

cleanup_integration_test_database() {
    if [[ -z "${INTEGRATION_TEST_DB_CONTAINER}" ]]; then
        return 0
    fi

    local container="${INTEGRATION_TEST_DB_CONTAINER}"
    INTEGRATION_TEST_DB_CONTAINER=""
    if ! docker rm --force "${container}" >/dev/null; then
        echo "failed to remove integration test PostgreSQL container ${container}" >&2
        return 1
    fi
}

cleanup_integration_test_valkey() {
    if [[ -z "${INTEGRATION_TEST_VALKEY_CONTAINER}" ]]; then
        return 0
    fi

    local container="${INTEGRATION_TEST_VALKEY_CONTAINER}"
    INTEGRATION_TEST_VALKEY_CONTAINER=""
    if ! docker rm --force "${container}" >/dev/null; then
        echo "failed to remove integration test Valkey container ${container}" >&2
        return 1
    fi
}

cleanup_integration_test_services() {
    local failed=0
    cleanup_integration_test_valkey || failed=1
    cleanup_integration_test_database || failed=1
    return "${failed}"
}

provision_integration_test_database() {
    local postgres_user="local_ci"
    local postgres_password="local_ci"
    local postgres_database="local_ci"
    local ready=false
    local published_address
    local published_port
    local owner_token

    INTEGRATION_TEST_DB_CONTAINER="$(docker run --detach --rm \
        --publish 127.0.0.1::5432 \
        --env POSTGRES_USER="${postgres_user}" \
        --env POSTGRES_PASSWORD="${postgres_password}" \
        --env POSTGRES_DB="${postgres_database}" \
        "${INTEGRATION_POSTGRES_IMAGE}")"
    trap cleanup_integration_test_services EXIT

    for _ in {1..60}; do
        if docker exec "${INTEGRATION_TEST_DB_CONTAINER}" \
            pg_isready --host 127.0.0.1 --username "${postgres_user}" --dbname "${postgres_database}" >/dev/null 2>&1; then
            ready=true
            break
        fi
        if [[ "$(docker inspect --format '{{.State.Running}}' "${INTEGRATION_TEST_DB_CONTAINER}" 2>/dev/null || true)" != "true" ]]; then
            break
        fi
        sleep 1
    done
    if [[ "${ready}" != "true" ]]; then
        echo "integration test PostgreSQL did not become ready" >&2
        return 1
    fi

    published_address="$(docker port "${INTEGRATION_TEST_DB_CONTAINER}" 5432/tcp)"
    published_port="${published_address##*:}"
    if [[ ! "${published_port}" =~ ^[0-9]+$ ]]; then
        echo "invalid integration test PostgreSQL port: ${published_address}" >&2
        return 1
    fi

    owner_token="local-ci-${INTEGRATION_TEST_DB_CONTAINER}"
    docker exec "${INTEGRATION_TEST_DB_CONTAINER}" \
        psql --set ON_ERROR_STOP=1 --host 127.0.0.1 --username "${postgres_user}" --dbname "${postgres_database}" \
        --command "CREATE TABLE ci_ephemeral_sentinel (token TEXT NOT NULL); INSERT INTO ci_ephemeral_sentinel(token) VALUES ('${owner_token}');" \
        >/dev/null

    export TEST_DATABASE_URL="postgresql://${postgres_user}:${postgres_password}@127.0.0.1:${published_port}/${postgres_database}?sslmode=disable"
    export TEST_DATABASE_OWNER_TOKEN="${owner_token}"
    export ALLOW_EXTERNAL_TEST_DB=false
}

provision_integration_test_valkey() {
    local ready=false
    local published_address
    local published_port

    INTEGRATION_TEST_VALKEY_CONTAINER="$(docker run --detach --rm \
        --publish 127.0.0.1::6379 \
        "${INTEGRATION_VALKEY_IMAGE}")"
    trap cleanup_integration_test_services EXIT

    for _ in {1..60}; do
        if docker exec "${INTEGRATION_TEST_VALKEY_CONTAINER}" valkey-cli ping 2>/dev/null | grep -qx PONG; then
            ready=true
            break
        fi
        if [[ "$(docker inspect --format '{{.State.Running}}' "${INTEGRATION_TEST_VALKEY_CONTAINER}" 2>/dev/null || true)" != "true" ]]; then
            break
        fi
        sleep 1
    done
    if [[ "${ready}" != "true" ]]; then
        echo "integration test Valkey did not become ready" >&2
        return 1
    fi

    published_address="$(docker port "${INTEGRATION_TEST_VALKEY_CONTAINER}" 6379/tcp)"
    published_port="${published_address##*:}"
    if [[ ! "${published_port}" =~ ^[0-9]+$ ]]; then
        echo "invalid integration test Valkey port: ${published_address}" >&2
        return 1
    fi

    export TEST_VALKEY_ADDR="127.0.0.1:${published_port}"
}

check_integration_tests() {
    if [[ "${RUN_INTEGRATION_TESTS}" == "true" ]]; then
        local provisioned_services=false
        if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
            run_step "Provision integration test PostgreSQL" provision_integration_test_database
            provisioned_services=true
        fi
        if [[ -z "${TEST_VALKEY_ADDR:-}" && -z "${TEST_VALKEY_HOST:-}" ]]; then
            run_step "Provision integration test Valkey" provision_integration_test_valkey
            provisioned_services=true
        fi
        run_step "Integration-tag tests" \
            go_mod_readonly env INTEGRATION_TEST=true go test -count=1 -tags=integration "${INTEGRATION_TAG_PACKAGES[@]}"
        run_step "INTEGRATION_TEST group" \
            go_mod_readonly env INTEGRATION_TEST=true go test -count=1 "${INTEGRATION_TEST_PACKAGES[@]}"
        if [[ "${provisioned_services}" == "true" ]]; then
            run_step "Remove integration test services" cleanup_integration_test_services
            trap - EXIT
        fi
        return 0
    fi

    if [[ -n "${TEST_DATABASE_URL:-}" ]]; then
        run_step "Alarm dispatch PostgreSQL integration test" \
            go_mod_readonly go test -count=1 -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
        return 0
    fi

    echo "[LOCAL CI] Skip integration tests: set RUN_INTEGRATION_TESTS=true to run"
    echo
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

run_step "local-ci package scope tests" ./scripts/ci/test-local-ci-packages.sh
run_step "NilAway input guard tests" bash ./scripts/ci/nilaway-inputs_test.sh
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
run_step "Sensitive log scan tests" bash ./scripts/refactor/grep-sensitive-logs_test.sh
if [[ "${RUN_ADMIN_TOUCH_GUARDRAIL}" == "true" ]]; then
    run_step "Refactor admin-dashboard guardrail" ./scripts/refactor/validate-no-admin-touch.sh
    run_step "Refactor admin-dashboard guardrail tests" ./scripts/refactor/test-validate-no-admin-touch.sh
else
    echo "[LOCAL CI] Skip refactor admin-dashboard guardrail: RUN_ADMIN_TOUCH_GUARDRAIL=${RUN_ADMIN_TOUCH_GUARDRAIL}"
    echo
fi
run_step "Go toolchain" check_go_toolchain
run_step "go work sync drift" check_go_work_sync
run_step "gofmt" bash "${SCRIPT_DIR}/check-gofmt.sh"
run_step "go fix drift" check_go_fix
check_go_mod_tidy
check_canonical_module_builds
run_go_package_step "Go vet" go_mod_readonly go vet
check_integration_tag_compilation
check_staticcheck
check_golangci_lint
check_nilaway
run_step "benchgate isolated tool gate" check_benchgate
run_go_package_step "Go build" go_mod_readonly go build
run_step "PGO default policy tests" ./scripts/ci/check-pgo-default_test.sh
run_step "PGO default gate" ./scripts/ci/check-pgo-default.sh
run_step "PGO freshness tests" ./scripts/ci/check-pgo-freshness_test.sh
run_step "PGO freshness gate" ./scripts/ci/check-pgo-freshness.sh --strict
run_step "PGO compare tests" bash -c './scripts/perf/pgo/compare_test.sh && ./scripts/perf/pgo/compare_regression_test.sh'
run_step "PGO generator tests" ./scripts/perf/pgo/generate_test.sh
run_go_package_step "Go test" go_mod_readonly go test -count=1

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
else
    echo "[LOCAL CI] Skip race tests: set RUN_RACE_TESTS=true to run go test -race"
    echo
fi

check_integration_tests

if [[ "${RUN_DEPENDENCY_HYGIENE}" == "true" ]]; then
    govulncheck_bin="$(ensure_govulncheck)"

    for module in . "${GO_MODULES[@]}"; do
        run_step "Dependency hygiene: ${module}" \
            bash -c "cd '$module' && GOWORK=off go list -m -u -mod=readonly all >/dev/null && GOWORK=off '${govulncheck_bin}' ./..."
    done
else
    echo "[LOCAL CI] Skip dependency hygiene: RUN_DEPENDENCY_HYGIENE=${RUN_DEPENDENCY_HYGIENE}"
    echo
fi

echo "[LOCAL CI] Passed"
