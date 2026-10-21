#!/usr/bin/env bash
# local-ci.sh가 사용하는 Go 도구 설치·최소 지원 버전 검증 헬퍼.

GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.9.0}"
GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.15.0}"
NILAWAY_VERSION="${NILAWAY_VERSION:-v0.0.0-20260918162853-acb8859b9031}"

go_bin_tool() {
    local tool="$1" minimum="${2:-}" probe="${3:-}" pattern=""
    local gobin gopath bin output invalid_bin=""
    local required_major required_minor required_patch
    local -a candidates=()

    case "${tool}" in
        golangci-lint)
            minimum="${minimum:-${GOLANGCI_LINT_VERSION}}"
            probe="${probe:-version}"
            pattern='version[[:space:]]+([0-9]+)\.([0-9]+)\.([0-9]+)($|[[:space:]])'
            ;;
        govulncheck)
            minimum="${minimum:-${GOVULNCHECK_VERSION}}"
            probe="${probe:--version}"
            pattern='govulncheck@v([0-9]+)\.([0-9]+)\.([0-9]+)($|[[:space:]])'
            ;;
    esac
    if [[ -n "${pattern}" ]]; then
        [[ "${minimum}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
            echo "invalid minimum version for ${tool}: ${minimum}" >&2
            return 1
        }
        IFS=. read -r required_major required_minor required_patch <<<"${minimum#v}"
    fi

    gobin="$(go env GOBIN)" || return
    gopath="$(go env GOPATH)" || return
    [[ -z "${gobin}" ]] || candidates+=("${gobin}/${tool}")
    [[ -z "${gopath}" ]] || candidates+=("${gopath}/bin/${tool}")
    bin="$(command -v "${tool}" || true)"
    [[ -z "${bin}" ]] || candidates+=("${bin}")

    for bin in "${candidates[@]}"; do
        [[ -x "${bin}" ]] || continue
        if [[ -z "${pattern}" ]]; then
            printf '%s\n' "${bin}"
            return
        fi
        if ! output="$("${bin}" "${probe}" 2>/dev/null)" || [[ ! "${output}" =~ ${pattern} ]]; then
            invalid_bin="${bin}"
            continue
        fi
        if (( 10#${BASH_REMATCH[1]} > 10#${required_major}
            || (10#${BASH_REMATCH[1]} == 10#${required_major} && 10#${BASH_REMATCH[2]} > 10#${required_minor})
            || (10#${BASH_REMATCH[1]} == 10#${required_major} && 10#${BASH_REMATCH[2]} == 10#${required_minor}
                && 10#${BASH_REMATCH[3]} >= 10#${required_patch}) )); then
            printf '%s\n' "${bin}"
            return
        fi
    done
    if [[ -n "${invalid_bin}" ]]; then
        echo "cannot verify ${tool} version: ${invalid_bin}" >&2
        return 1
    fi
}

ensure_pinned_go_tool() {
    local tool="$1"
    local module="$2"
    local version="$3"
    local bin

    bin="$(go_bin_tool "${tool}" "${version}")" || return
    if [[ -z "${bin}" ]]; then
        echo "[GO TOOLING] Installing ${tool}@${version}" >&2
        go install "${module}@${version}" || return
        bin="$(go_bin_tool "${tool}" "${version}")" || return
    fi
    [[ -n "${bin}" ]] || {
        echo "${tool} >= ${version} is required" >&2
        return 1
    }
    printf '%s\n' "${bin}"
}

ensure_govulncheck() {
    ensure_pinned_go_tool govulncheck "golang.org/x/vuln/cmd/govulncheck" "${GOVULNCHECK_VERSION}"
}

ensure_golangci_lint() {
    ensure_pinned_go_tool golangci-lint "github.com/golangci/golangci-lint/v2/cmd/golangci-lint" "${GOLANGCI_LINT_VERSION}"
}

ensure_nilaway() {
    local tooling_dir
    tooling_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" || return 1
    NILAWAY_VERSION="${NILAWAY_VERSION}" bash "${tooling_dir}/nilaway-models/build.sh"
}
