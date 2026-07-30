#!/usr/bin/env bash

verify_go_work_sync_drift() (
    set -euo pipefail

    local root_dir="$1"
    local post_sync_hook="${2:-}"
    local temp_root
    local temp_repo
    local file
    local candidate
    local drift=false
    local sync_files=()

    mapfile -t sync_files < <(workspace_metadata_files)
    temp_root="$(mktemp -d)"
    temp_repo="${temp_root}/hololive-bot"
    trap 'rm -rf "${temp_root}"' EXIT

    for file in "${sync_files[@]}"; do
        candidate="${temp_repo}/${file}"
        mkdir -p "$(dirname "${candidate}")"
        if [[ -f "${file}" ]]; then
            cp -p "${file}" "${candidate}"
        fi
    done

    cd "${temp_repo}"
    GOWORK="${temp_repo}/go.work" go work sync
    if [[ -n "${post_sync_hook}" ]]; then
        "${post_sync_hook}"
    fi
    cd "${root_dir}"

    for file in "${sync_files[@]}"; do
        candidate="${temp_repo}/${file}"
        if cmp -s "${file}" "${candidate}"; then
            continue
        fi

        drift=true
        if [[ -f "${file}" && -f "${candidate}" ]]; then
            diff -u --label "${file}" --label "${file} (go work sync)" "${file}" "${candidate}" >&2 || true
        elif [[ -f "${candidate}" ]]; then
            echo "go work sync would create ${file}" >&2
        else
            echo "go work sync would remove ${file}" >&2
        fi
    done

    if [[ "${drift}" == "true" ]]; then
        echo "go work sync changed workspace or module metadata; commit the sync result" >&2
        exit 1
    fi
)
