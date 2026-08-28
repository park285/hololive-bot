#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SHARED_GO_DIR="${SHARED_GO_WORKSPACE_PATH:-}"
if [[ -z "${SHARED_GO_DIR}" && -d "${ROOT_DIR}/shared-go" ]]; then SHARED_GO_DIR="${ROOT_DIR}/shared-go"; fi
if [[ -z "${SHARED_GO_DIR}" && -d "${ROOT_DIR}/../shared-go" ]]; then SHARED_GO_DIR="${ROOT_DIR}/../shared-go"; fi
[[ -d "${SHARED_GO_DIR}" ]] || { echo "error: active shared-go dir not found" >&2; exit 1; }
SHARED_GO_DIR="$(cd "${SHARED_GO_DIR}" && pwd)"

mapfile -t generic_dirs < <(
    find "${ROOT_DIR}/hololive" "${SHARED_GO_DIR}" \
        \( -name node_modules -o -name vendor -o -name dist -o -name .git \) -prune -o \
        -type d \( -name core -o -name servicecore \) -print \
        | sed "s#^${ROOT_DIR}/##" \
        | sort
)

mapfile -t generic_packages < <(
    rg -n '^\s*package (core|core_test|servicecore|servicecore_test)$' \
        "${ROOT_DIR}/hololive" "${SHARED_GO_DIR}" \
        --glob '*.go' \
        | sed "s#^${ROOT_DIR}/##" \
        | sort
)

mapfile -t generic_import_aliases < <(
    rg -n 'import\s+core\s+"' \
        "${ROOT_DIR}/hololive" "${SHARED_GO_DIR}" \
        --glob '*.go' \
        | sed "s#^${ROOT_DIR}/##" \
        | sort
)

if ((${#generic_dirs[@]} > 0)); then
    echo "generic Go implementation directories remain:"
    printf '  - %s\n' "${generic_dirs[@]}"
    exit 1
fi

if ((${#generic_packages[@]} > 0)); then
    echo "generic Go package names remain:"
    printf '  - %s\n' "${generic_packages[@]}"
    exit 1
fi

if ((${#generic_import_aliases[@]} > 0)); then
    echo "generic Go import aliases remain:"
    printf '  - %s\n' "${generic_import_aliases[@]}"
    exit 1
fi

echo "generic Go internal package name check passed"
