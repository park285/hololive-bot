#!/usr/bin/env bash

GO_WORKSPACE_MODULES=(
    ../shared-go
    admin-dashboard/backend
    hololive/hololive-shared
    hololive/hololive-api
    hololive/hololive-kakao-bot-go
    hololive/hololive-alarm-worker
    hololive/hololive-youtube-producer
)

go_workspace_package_patterns() {
    local module
    for module in "${GO_WORKSPACE_MODULES[@]}"; do
        printf './%s/...\n' "${module}"
    done
}

go_workspace_module_dirs() {
    local root_dir="$1"
    local shared_go_dir="$2"
    local module

    printf '%s\n' "${root_dir}"
    for module in "${GO_WORKSPACE_MODULES[@]}"; do
        if [[ "${module}" == "../shared-go" ]]; then
            printf '%s\n' "${shared_go_dir}"
        else
            printf '%s/%s\n' "${root_dir}" "${module}"
        fi
    done
}

go_workspace_non_admin_package_patterns() {
    local module
    for module in "${GO_WORKSPACE_MODULES[@]}"; do
        if [[ "${module}" == "hololive/hololive-admin-api" ]]; then
            continue
        fi
        printf './%s/...\n' "${module}"
    done
}

go_workspace_runtime_log_scan_targets() {
    local module
    for module in "${GO_WORKSPACE_MODULES[@]}"; do
        if [[ "${module}" == "hololive/hololive-admin-api" ]]; then
            continue
        fi
        printf '%s\n' "${module}"
    done
}
