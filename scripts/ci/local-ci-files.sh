#!/usr/bin/env bash

is_go_scope_excluded_file() {
    local file="$1"

    case "${file}" in
        */node_modules/*|node_modules/*|*/target/*|target/*|.tmp/*|*/.tmp/*)
            return 0
            ;;
        # benchgate는 check_benchgate가 GOWORK=off로 따로 게이트하므로 루트 Go 스코프에서 제외(삭제 금지).
        scripts/perf/benchgate/*)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

is_workspace_module_file() {
    local file="$1"
    local module

    for module in "${GO_MODULES[@]}"; do
        if [[ "${file}" == "${module}/"* ]]; then
            return 0
        fi
    done

    return 1
}

root_go_package_patterns() {
    local file
    local dir
    local has_root_package=false
    local package_patterns=()

    while IFS= read -r file; do
        is_go_scope_excluded_file "${file}" && continue
        is_workspace_module_file "${file}" && continue
        [[ -f "${file}" ]] || continue

        dir="$(dirname "${file}")"
        if [[ "${dir}" == "." ]]; then
            has_root_package=true
        else
            package_patterns+=("./${dir}")
        fi
    done < <(git ls-files --cached --others --exclude-standard '*.go')

    if [[ "${has_root_package}" == "true" ]]; then
        printf './\n'
    fi
    printf '%s\n' "${package_patterns[@]}" | awk 'NF && !seen[$0]++'
}

go_source_files() {
    local file
    git ls-files --cached --others --exclude-standard '*.go' | while IFS= read -r file; do
        is_go_scope_excluded_file "${file}" && continue
        [[ -f "${file}" ]] && printf '%s\n' "${file}"
    done
}

workspace_metadata_files() {
    git ls-files --cached --others --exclude-standard \
        go.work go.work.sum \
        'go.mod' 'go.sum' \
        '*/go.mod' '*/go.sum'
}

snapshot_files() {
    local file
    while IFS= read -r file; do
        if [[ -f "${file}" ]]; then
            sha256sum "${file}"
        else
            printf 'missing  %s\n' "${file}"
        fi
    done | sort
}
