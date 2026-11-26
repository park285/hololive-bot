#!/usr/bin/env bash

GO_WORKSPACE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

load_go_workspace_modules() {
    local root_dir="${1:-${GO_WORKSPACE_ROOT}}"
    local workspace_json modules

    if ! workspace_json="$(cd "${root_dir}" && GOWORK="${PWD}/go.work" go work edit -print=false -json)"; then
        echo "failed to read ${root_dir}/go.work" >&2
        return 1
    fi

    modules="$(jq -r '.Use[].DiskPath | sub("^\\./"; "") | select(. != "" and . != ".")' <<<"${workspace_json}")" || return 1
    if [[ -z "${modules}" ]]; then
        echo "go.work must contain at least one non-root module" >&2
        return 1
    fi

    printf '%s\n' "${modules}"
}

workspace_modules="$(load_go_workspace_modules)" || {
    return 1 2>/dev/null || exit 1
}
mapfile -t GO_WORKSPACE_MODULES < <(printf '%s\n' "${workspace_modules}")
unset workspace_modules
GO_OWNED_MODULES=()
GO_PRODUCT_MODULES=()
for workspace_module in "${GO_WORKSPACE_MODULES[@]}"; do
    [[ "${workspace_module}" != ../* ]] || continue
    GO_OWNED_MODULES+=("${workspace_module}")
    [[ "${workspace_module}" == scripts/scripttest ]] || GO_PRODUCT_MODULES+=("${workspace_module}")
done
unset workspace_module

go_workspace_package_patterns() {
    local module
    for module in "${GO_PRODUCT_MODULES[@]}"; do
        printf './%s/...\n' "${module}"
    done
}

go_workspace_runtime_log_scan_targets() {
    printf '%s\n' "${GO_WORKSPACE_MODULES[@]}"
}
