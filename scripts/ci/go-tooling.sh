#!/usr/bin/env bash
# local-ci.sh 와 admin-dashboard-go-ci.sh 가 source 해서 쓰는 Go 툴 설치·핀 검증 헬퍼.

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

ensure_pinned_go_tool() {
    local tool="$1"
    local module="$2"
    local version="$3"
    local version_marker="$4"

    local bin
    bin="$(go_bin_tool "${tool}" || true)"
    if [[ -z "${bin}" ]] || [[ "$("${bin}" -version 2>/dev/null || true)" != *"${version_marker}"* ]]; then
        echo "[GO TOOLING] Installing ${tool}@${version}" >&2
        go install "${module}@${version}"
        bin="$(go_tool_install_path "${tool}")"
        echo >&2
    fi

    local version_output
    version_output="$("${bin}" -version 2>/dev/null || true)"
    if [[ "${version_output}" != *"${version_marker}"* ]]; then
        echo "expected ${tool} ${version}, got: ${version_output}" >&2
        exit 1
    fi

    printf '%s\n' "${bin}"
}

ensure_staticcheck() {
    ensure_pinned_go_tool staticcheck "honnef.co/go/tools/cmd/staticcheck" \
        "${STATICCHECK_VERSION}" "staticcheck ${STATICCHECK_VERSION}"
}

ensure_govulncheck() {
    ensure_pinned_go_tool govulncheck "golang.org/x/vuln/cmd/govulncheck" \
        "${GOVULNCHECK_VERSION}" "govulncheck@${GOVULNCHECK_VERSION}"
}
