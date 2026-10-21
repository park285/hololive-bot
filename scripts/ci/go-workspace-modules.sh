#!/usr/bin/env bash

GO_WORKSPACE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

load_go_workspace_modules() {
    local root_dir="${1:-${GO_WORKSPACE_ROOT}}"
    local workspace_json modules

    if ! workspace_json="$(cd "${root_dir}" && go work edit -json)"; then
        echo "failed to read ${root_dir}/go.work" >&2
        return 1
    fi

    # go.work의 중복 경로와 go.mod가 없는 경로는 go 명령이 거부한다. 여기서는 root를 뺀 모듈 경로만 고른다.
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
# 형제 저장소 모듈(../)은 각 저장소 게이트가 검증한다. 이 저장소의 검사는 소유 모듈만 반복한다.
GO_OWNED_MODULES=()
for workspace_module in "${GO_WORKSPACE_MODULES[@]}"; do
    [[ "${workspace_module}" == ../* ]] || GO_OWNED_MODULES+=("${workspace_module}")
done
unset workspace_module

go_workspace_package_patterns() {
    local module
    for module in "${GO_OWNED_MODULES[@]}"; do
        printf './%s/...\n' "${module}"
    done
}

go_workspace_runtime_log_scan_targets() {
    printf '%s\n' "${GO_WORKSPACE_MODULES[@]}"
}
