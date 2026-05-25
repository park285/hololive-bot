#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/go-workspace-modules.sh"
cd "${ROOT_DIR}"

GO_MODULES=("${GO_WORKSPACE_MODULES[@]}")
source "${SCRIPT_DIR}/local-ci-files.sh"
mapfile -t ROOT_GO_PACKAGES < <(root_go_package_patterns)
mapfile -t WORKSPACE_GO_PACKAGES < <(go_workspace_package_patterns)
GO_PACKAGES=()
source "${SCRIPT_DIR}/local-ci-packages.sh"

LOCAL_CI_GO_SCOPE="${LOCAL_CI_GO_SCOPE:-all}"
RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-true}"
RUN_RACE_TESTS="${RUN_RACE_TESTS:-false}"
STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}"
RUN_ADMIN_TOUCH_GUARDRAIL="${RUN_ADMIN_TOUCH_GUARDRAIL:-true}"
STATICCHECK_VERSION="${STATICCHECK_VERSION:-2026.1}"
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.3.0}"

go_bin_tool() {
    local tool="$1"

    if command -v "${tool}" >/dev/null 2>&1; then
        command -v "${tool}"
        return 0
    fi

    local gobin
    gobin="$(go env GOBIN)"
    if [[ -n "${gobin}" && -x "${gobin}/${tool}" ]]; then
        printf '%s/%s\n' "${gobin}" "${tool}"
        return 0
    fi

    local gopath
    gopath="$(go env GOPATH)"
    if [[ -n "${gopath}" && -x "${gopath}/bin/${tool}" ]]; then
        printf '%s/bin/%s\n' "${gopath}" "${tool}"
        return 0
    fi

    return 1
}

go_tool_install_path() {
    local tool="$1"
    local gobin
    gobin="$(go env GOBIN)"
    if [[ -n "${gobin}" ]]; then
        printf '%s/%s\n' "${gobin}" "${tool}"
        return 0
    fi

    local gopath
    gopath="$(go env GOPATH)"
    if [[ -z "${gopath}" ]]; then
        echo "GOPATH is empty; cannot locate installed Go tool ${tool}" >&2
        exit 1
    fi
    printf '%s/bin/%s\n' "${gopath}" "${tool}"
}

ensure_staticcheck() {
    local bin
    bin="$(go_bin_tool staticcheck || true)"
    if [[ -z "${bin}" ]] || [[ "$("${bin}" -version 2>/dev/null || true)" != *"staticcheck ${STATICCHECK_VERSION}"* ]]; then
        echo "[LOCAL CI] Installing staticcheck@${STATICCHECK_VERSION}" >&2
        go install "honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}"
        bin="$(go_tool_install_path staticcheck)"
        echo >&2
    fi

    local version_output
    version_output="$("${bin}" -version 2>/dev/null || true)"
    if [[ "${version_output}" != *"staticcheck ${STATICCHECK_VERSION}"* ]]; then
        echo "expected staticcheck ${STATICCHECK_VERSION}, got: ${version_output}" >&2
        exit 1
    fi

    printf '%s\n' "${bin}"
}

ensure_govulncheck() {
    local bin
    bin="$(go_bin_tool govulncheck || true)"
    if [[ -z "${bin}" ]] || [[ "$("${bin}" -version 2>/dev/null || true)" != *"govulncheck@${GOVULNCHECK_VERSION}"* ]]; then
        echo "[LOCAL CI] Installing govulncheck@${GOVULNCHECK_VERSION}" >&2
        go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
        bin="$(go_tool_install_path govulncheck)"
        echo >&2
    fi

    local version_output
    version_output="$("${bin}" -version 2>/dev/null || true)"
    if [[ "${version_output}" != *"govulncheck@${GOVULNCHECK_VERSION}"* ]]; then
        echo "expected govulncheck ${GOVULNCHECK_VERSION}, got: ${version_output}" >&2
        exit 1
    fi

    printf '%s\n' "${bin}"
}

run_step() {
    local name="$1"
    shift

    echo "[LOCAL CI] ${name}"
    "$@"
    echo
}

check_go_toolchain() {
    local required="${GO_REQUIRED_VERSION:-go1.26.3}"
    local actual
    actual="$(go env GOVERSION)"
    if [[ "${actual}" != "${required}" ]]; then
        echo "expected ${required}, got ${actual}" >&2
        exit 1
    fi
}

ensure_go_mod_toolchains() {
    local required="${GO_REQUIRED_VERSION:-go1.26.3}"
    local module

    go mod edit -toolchain="${required}"
    for module in "${GO_MODULES[@]}"; do
        (cd "${module}" && go mod edit -toolchain="${required}")
    done
}

check_go_work_sync() {
    local before
    local after
    local sync_files=()

    mapfile -t sync_files < <(workspace_metadata_files)

    before="$(workspace_metadata_files | snapshot_files)"
    go work sync
    ensure_go_mod_toolchains
    after="$(workspace_metadata_files | snapshot_files)"
    if [[ "${before}" != "${after}" ]]; then
        echo "go work sync changed workspace or module metadata; commit the sync result" >&2
        git diff -- "${sync_files[@]}" >&2
        git status --short -- "${sync_files[@]}" >&2
        exit 1
    fi
}

check_gofmt() {
    local go_files=()
    local files
    mapfile -t go_files < <(go_source_files)
    if (( ${#go_files[@]} == 0 )); then
        return 0
    fi

    files="$(gofmt -l "${go_files[@]}")"
    if [[ -n "${files}" ]]; then
        echo "gofmt required for:" >&2
        echo "${files}" >&2
        exit 1
    fi
}

check_go_fix() {
    local tmp_dir
    local tmp_parent
    tmp_parent="${LOCAL_CI_TMPDIR:-${ROOT_DIR}/.tmp/local-ci}"
    local iris_client_go_dir
    iris_client_go_dir="${IRIS_CLIENT_GO_WORKSPACE_PATH:-${ROOT_DIR}/../iris-client-go}"
    local tar_excludes=(
        --exclude=.git
        --exclude=.worktrees
        --exclude=.tasklists
        --exclude=.runlogs
        --exclude=.codex
        --exclude=.claude
        --exclude=.serena
        --exclude=.gemini
        --exclude=.tmp
        --exclude=./admin-dashboard
        --exclude=./admin-dashboard/*
        --exclude=./artifacts
        --exclude=./artifacts/*
        --exclude=./backups
        --exclude=./backups/*
        --exclude=./data
        --exclude=./data/*
        --exclude=./logs
        --exclude=./logs/*
        --exclude=./runtime-config
        --exclude=./runtime-config/*
        --exclude=target
        --exclude='*/target'
        --exclude='*/target/*'
        --exclude=node_modules
        --exclude='*/node_modules'
        --exclude='*/node_modules/*'
        --exclude='*.key'
        --exclude='*.pem'
        --exclude='*.p12'
        --exclude=.env
        --exclude='.env.*'
    )

    if ! has_go_packages; then
        echo "[LOCAL CI] Skip go fix drift: no Go packages in scope"
        return 0
    fi

    mkdir -p "${tmp_parent}"
    find "${tmp_parent}" -mindepth 1 -maxdepth 1 -type d -name 'go-fix.*' -mmin +60 -exec rm -rf {} +
    tmp_dir="$(mktemp -d "${tmp_parent%/}/go-fix.XXXXXX")"

    cleanup_go_fix_tmp() {
        [[ -n "${tmp_dir:-}" ]] && rm -rf "${tmp_dir}"
        trap - RETURN
    }
    trap cleanup_go_fix_tmp RETURN

    mkdir -p "${tmp_dir}/repo"
    if ! tar "${tar_excludes[@]}" -C "${ROOT_DIR}" -cf - . | tar -C "${tmp_dir}/repo" -xf -; then
        return 1
    fi

    local shared_go_dir
    shared_go_dir="${SHARED_GO_WORKSPACE_PATH:-${ROOT_DIR}/../shared-go}"
    if grep -q '../shared-go' "${ROOT_DIR}/go.work"; then
        if [[ ! -d "${shared_go_dir}" ]]; then
            echo "active shared-go workspace not found: ${shared_go_dir}" >&2
            return 1
        fi
        mkdir -p "${tmp_dir}/shared-go"
        if ! tar "${tar_excludes[@]}" -C "${shared_go_dir}" -cf - . | tar -C "${tmp_dir}/shared-go" -xf -; then
            return 1
        fi
    fi

    if grep -q '../iris-client-go' "${ROOT_DIR}/go.work"; then
        if [[ ! -d "${iris_client_go_dir}" ]]; then
            echo "active iris-client-go workspace not found: ${iris_client_go_dir}" >&2
            return 1
        fi
        mkdir -p "${tmp_dir}/iris-client-go"
        if ! tar "${tar_excludes[@]}" -C "${iris_client_go_dir}" -cf - . | tar -C "${tmp_dir}/iris-client-go" -xf -; then
            return 1
        fi
    fi

    if ! (cd "${tmp_dir}/repo" && go fix "${GO_PACKAGES[@]}"); then
        return 1
    fi

    local changed=()
    local file
    while IFS= read -r file; do
        if [[ -f "${tmp_dir}/repo/${file}" ]] && ! cmp -s "${ROOT_DIR}/${file}" "${tmp_dir}/repo/${file}"; then
            changed+=("${file}")
        fi
    done < <(go_source_files)

    if (( ${#changed[@]} > 0 )); then
        echo "go fix would update modern Go compatibility rewrites:" >&2
        printf ' - %s\n' "${changed[@]}" >&2
        echo "Run go fix on the listed packages/files and commit the result." >&2
        return 1
    fi
}

check_go_mod_tidy() {
    local before
    local after
    local module
    local sync_files=()

    mapfile -t sync_files < <(workspace_metadata_files)
    before="$(workspace_metadata_files | snapshot_files)"

    for module in "${GO_MODULES[@]}"; do
        run_step "go mod tidy: ${module}" bash -c "cd '$module' && go mod tidy"
    done

    ensure_go_mod_toolchains
    after="$(workspace_metadata_files | snapshot_files)"
    if [[ "${before}" != "${after}" ]]; then
        echo "go mod tidy changed workspace or module metadata; commit the tidy result" >&2
        git diff -- "${sync_files[@]}" >&2
        git status --short -- "${sync_files[@]}" >&2
        exit 1
    fi
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

run_step "local-ci package scope tests" ./scripts/ci/test-local-ci-packages.sh
configure_go_packages
echo "[LOCAL CI] Go package scope: ${LOCAL_CI_GO_SCOPE} (${#GO_PACKAGES[@]} packages)"
if has_go_packages; then
    printf '[LOCAL CI]   %s\n' "${GO_PACKAGES[@]}"
else
    echo "[LOCAL CI]   no Go packages selected"
fi
echo

run_step "Architecture gates" ./scripts/architecture/ci-boundary-gate.sh
if [[ "${RUN_ADMIN_TOUCH_GUARDRAIL}" == "true" ]]; then
    run_step "Refactor admin-dashboard guardrail" ./scripts/refactor/validate-no-admin-touch.sh
    run_step "Refactor admin-dashboard guardrail tests" ./scripts/refactor/test-validate-no-admin-touch.sh
else
    echo "[LOCAL CI] Skip refactor admin-dashboard guardrail: RUN_ADMIN_TOUCH_GUARDRAIL=${RUN_ADMIN_TOUCH_GUARDRAIL}"
    echo
fi
run_step "Go toolchain" check_go_toolchain
run_step "go work sync drift" check_go_work_sync
run_step "gofmt" check_gofmt
run_step "go fix drift" check_go_fix
check_go_mod_tidy
run_go_package_step "Go vet" go_mod_readonly go vet
check_staticcheck
run_go_package_step "Go build" go_mod_readonly go build
run_go_package_step "Go test" go_mod_readonly go test -count=1

if [[ "${RUN_RACE_TESTS}" == "true" ]]; then
    run_go_package_step "Go race test" go_mod_readonly go test -race -count=1
else
    echo "[LOCAL CI] Skip race tests: set RUN_RACE_TESTS=true to run go test -race"
    echo
fi

if [[ -n "${TEST_DATABASE_URL:-}" ]]; then
    run_step "Alarm dispatch PostgreSQL integration test" \
        go_mod_readonly go test -count=1 -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
else
    echo "[LOCAL CI] Skip PostgreSQL integration test: TEST_DATABASE_URL is not set"
    echo
fi

if [[ "${RUN_DEPENDENCY_HYGIENE}" == "true" ]]; then
    govulncheck_bin="$(ensure_govulncheck)"

    for module in "${GO_MODULES[@]}"; do
        run_step "Dependency hygiene: ${module}" \
            bash -c "cd '$module' && GOWORK=off go list -m -u -mod=readonly all >/dev/null && GOWORK='${ROOT_DIR}/go.work' '${govulncheck_bin}' ./..."
    done
else
    echo "[LOCAL CI] Skip dependency hygiene: RUN_DEPENDENCY_HYGIENE=${RUN_DEPENDENCY_HYGIENE}"
    echo
fi

echo "[LOCAL CI] Passed"
