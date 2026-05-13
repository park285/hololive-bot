#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

GO_PACKAGES=(
    ./shared-go/...
    ./hololive/hololive-shared/...
    ./hololive/hololive-admin-api/...
    ./hololive/hololive-alarm-worker/...
    ./hololive/hololive-dispatcher-go/...
    ./hololive/hololive-kakao-bot-go/...
    ./hololive/hololive-llm-sched/...
    ./hololive/hololive-stream-ingester/...
)

GO_MODULES=(
    shared-go
    hololive/hololive-shared
    hololive/hololive-admin-api
    hololive/hololive-alarm-worker
    hololive/hololive-dispatcher-go
    hololive/hololive-kakao-bot-go
    hololive/hololive-llm-sched
    hololive/hololive-stream-ingester
)

RUN_DEPENDENCY_HYGIENE="${RUN_DEPENDENCY_HYGIENE:-true}"
RUN_RACE_TESTS="${RUN_RACE_TESTS:-false}"
STRICT_STATICCHECK="${STRICT_STATICCHECK:-true}"

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

check_go_work_sync() {
    local before
    local after
    local tracked_sync_files=()

    mapfile -t tracked_sync_files < <(git ls-files go.work go.work.sum 'go.mod' 'go.sum' '*/go.mod' '*/go.sum')

    before="$(git status --porcelain=v1 -- "${tracked_sync_files[@]}")"
    go work sync
    after="$(git status --porcelain=v1 -- "${tracked_sync_files[@]}")"
    if [[ "${before}" != "${after}" ]]; then
        echo "go work sync changed workspace or module metadata; commit the sync result" >&2
        git diff -- "${tracked_sync_files[@]}" >&2
        exit 1
    fi
}

check_gofmt() {
    local files
    files="$(git ls-files '*.go' | xargs -r gofmt -l)"
    if [[ -n "${files}" ]]; then
        echo "gofmt required for:" >&2
        echo "${files}" >&2
        exit 1
    fi
}

check_go_fix() {
    local tmp_dir
    tmp_dir="$(mktemp -d)"

    mkdir -p "${tmp_dir}/repo"
    tar \
        --exclude=.git \
        --exclude=.worktrees \
        --exclude=.tasklists \
        --exclude=.runlogs \
        --exclude=.codex \
        --exclude=.claude \
        --exclude=.serena \
        --exclude=.gemini \
        -C "${ROOT_DIR}" -cf - . | tar -C "${tmp_dir}/repo" -xf -

    (cd "${tmp_dir}/repo" && go fix "${GO_PACKAGES[@]}")

    local changed=()
    local file
    while IFS= read -r file; do
        if [[ -f "${tmp_dir}/repo/${file}" ]] && ! cmp -s "${ROOT_DIR}/${file}" "${tmp_dir}/repo/${file}"; then
            changed+=("${file}")
        fi
    done < <(git ls-files '*.go')

    if (( ${#changed[@]} > 0 )); then
        echo "go fix would update modern Go compatibility rewrites:" >&2
        printf ' - %s\n' "${changed[@]}" >&2
        echo "Run go fix on the listed packages/files and commit the result." >&2
        rm -rf "${tmp_dir}"
        exit 1
    fi

    rm -rf "${tmp_dir}"
}

check_go_mod_tidy() {
    local module
    for module in "${GO_MODULES[@]}"; do
        run_step "go mod tidy -diff: ${module}" bash -c "cd '$module' && go mod tidy -diff"
    done
}

check_staticcheck() {
    if [[ "${STRICT_STATICCHECK}" != "true" ]]; then
        echo "[LOCAL CI] Skip staticcheck: STRICT_STATICCHECK=${STRICT_STATICCHECK}"
        echo
        return 0
    fi

    local staticcheck_bin
    if ! staticcheck_bin="$(go_bin_tool staticcheck)"; then
        echo "[LOCAL CI] Installing staticcheck"
        go install honnef.co/go/tools/cmd/staticcheck@latest
        staticcheck_bin="$(go_bin_tool staticcheck)"
        echo
    fi

    run_step "staticcheck" "${staticcheck_bin}" "${GO_PACKAGES[@]}"
}

go_mod_readonly() {
    GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" "$@"
}

run_step "Architecture gates" ./scripts/architecture/ci-boundary-gate.sh
run_step "Go toolchain" check_go_toolchain
run_step "go work sync drift" check_go_work_sync
run_step "gofmt" check_gofmt
run_step "go fix drift" check_go_fix
check_go_mod_tidy
run_step "Go vet" go_mod_readonly go vet "${GO_PACKAGES[@]}"
check_staticcheck
run_step "Go build" go_mod_readonly go build "${GO_PACKAGES[@]}"
run_step "Go test" go_mod_readonly go test -count=1 "${GO_PACKAGES[@]}"

if [[ "${RUN_RACE_TESTS}" == "true" ]]; then
    run_step "Go race test" go_mod_readonly go test -race -count=1 "${GO_PACKAGES[@]}"
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
    govulncheck_bin=""
    if ! govulncheck_bin="$(go_bin_tool govulncheck)"; then
        echo "[LOCAL CI] Installing govulncheck"
        go install golang.org/x/vuln/cmd/govulncheck@latest
        govulncheck_bin="$(go_bin_tool govulncheck)"
        echo
    fi

    for module in "${GO_MODULES[@]}"; do
        run_step "Dependency hygiene: ${module}" \
            bash -c "cd '$module' && GOWORK=off go list -m -u -mod=readonly all >/dev/null && GOWORK='${ROOT_DIR}/go.work' '${govulncheck_bin}' ./..."
    done
else
    echo "[LOCAL CI] Skip dependency hygiene: RUN_DEPENDENCY_HYGIENE=${RUN_DEPENDENCY_HYGIENE}"
    echo
fi

echo "[LOCAL CI] Passed"
