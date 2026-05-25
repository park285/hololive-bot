#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
if [[ -d "${ROOT_DIR}/shared-go" ]]; then SHARED_GO_DIR="${ROOT_DIR}/shared-go"; else SHARED_GO_DIR="${ROOT_DIR}/../shared-go"; fi

mapfile -t generic_dirs < <(
    find "${ROOT_DIR}/hololive" "${SHARED_GO_DIR}" \
        -type d \( -name core -o -name servicecore \) \
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
