#!/usr/bin/env bash
# local-ci.sh가 사용하는 Go 도구 설치·최소 지원 버전 검증 헬퍼.

STATICCHECK_VERSION="${STATICCHECK_VERSION:-2026.2.1}"
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.8.0}"
GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.14.0}"
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
    if [[ "${tool}" == govulncheck || "${tool}" == golangci-lint ]]; then
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
        return
    fi
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

# PATH/GOBIN의 무패치 staticcheck는 generic method fact를 다른 method에 붙여 거짓 SA1019를 낸다.
# staticcheck-facts profile이 고정 x/tools objectpath 패치와 source/export 경계 fixture를 검증한다.
ensure_staticcheck() {
    local tooling_dir
    tooling_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" || return 1
    STATICCHECK_VERSION="${STATICCHECK_VERSION}" bash "${tooling_dir}/staticcheck-facts/build.sh"
}

ensure_govulncheck() {
    ensure_pinned_go_tool govulncheck "golang.org/x/vuln/cmd/govulncheck" \
        "${GOVULNCHECK_VERSION}" "govulncheck@${GOVULNCHECK_VERSION}"
}

ensure_golangci_lint() {
    ensure_pinned_go_tool golangci-lint "github.com/golangci/golangci-lint/v2/cmd/golangci-lint" \
        "${GOLANGCI_LINT_VERSION}" "version ${GOLANGCI_LINT_VERSION#v}"
}

ensure_nilaway() {
    local tooling_dir
    tooling_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" || return 1
    NILAWAY_VERSION="${NILAWAY_VERSION}" bash "${tooling_dir}/nilaway-models/build.sh"
}
