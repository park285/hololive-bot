#!/usr/bin/env bash

prepare_offline_sync() {
    local module=github.com/park285/iris-client-go/adapters/valkeydedup
    local dir=../iris-client-go/adapters/valkeydedup
    local api=hololive/hololive-api
    local cache version work hash manifest
    if [[ "$(go env -json=false -changed=false GOPROXY)" != off ]]; then
        return 0
    fi
    export GONOPROXY=none
    cache="$(go env -json=false -changed=false GOMODCACHE)"
    manifest="$(GOWORK=off go mod edit -print=false -json "${api}/go.mod")"
    version="$(jq -r --arg m "${module}" '.Require[]? | select(.Path == $m) | .Version' <<<"${manifest}")"
    work="$(go work edit -print=false -json)"
    if [[ "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ && ! -f "${cache}/cache/download/${module}/@v/${version}.mod" ]] &&
        printf '%s\n' "${work}" | jq -e --arg m "${module}" --arg d "${dir}" --arg v "${version}" '
            any(.Use[]?; .DiskPath == $d) and
            any(.Replace[]?; .Old.Path == $m and .Old.Version == $v and .New.Path == $d and .New.Version == null)
        ' >/dev/null; then
        if ! printf '%s\n' "${manifest}" | jq -e --arg m "${module}" 'all(.Replace[]?; .Old.Path != $m)' >/dev/null; then
            echo "unexpected ${module} replacement in ${api}/go.mod" >&2
            return 1
        fi
        GOWORK=off go mod edit -print=false -json "${dir}/go.mod" | jq -e --arg m "${module}" '.Module.Path == $m' >/dev/null || return 1
        hash="$(sha256sum < "${dir}/go.mod")" || return 1
        hash="$(printf '%s  go.mod\n' "${hash%% *}" | openssl dgst -sha256 -binary | base64)" || return 1
        if ! awk -v m="${module}" -v v="${version}/go.mod" -v h="h1:${hash}" '
            $1 == m && $2 == v { count++; if ($3 != h || NF != 3) wrong=1 }
            END { exit !(count == 1 && !wrong) }
        ' "${api}/go.sum"; then
            echo "missing or mismatched reviewed ${module} ${version}/go.mod checksum" >&2
            return 1
        fi
        GOWORK=off go mod edit -print=false -json=false "-replace=${module}@${version}=$(realpath "${dir}")" "${api}/go.mod" || return 1
        cp "${api}/go.mod" "$1"
    fi
}

verify_go_work_sync_drift() (
    set -euo pipefail

    local root_dir="$1"
    local temp_root
    local temp_repo
    local file module baseline
    local candidate
    local drift=false
    local sync_files=()

    cd "${root_dir}"

    mapfile -t sync_files < <(workspace_metadata_files)
    temp_root="$(mktemp -d)"
    temp_repo="${temp_root}/hololive-bot"
    trap 'rm -rf "${temp_root}"' EXIT

    for module in "${GO_MODULES[@]}"; do
        if [[ "${module}" == /* || "$(realpath -m "${temp_repo}/${module}")/" != "$(realpath "${temp_root}")/"* ]]; then
            echo "workspace module outside scratch workspace: ${module}" >&2
            exit 1
        fi
    done
    for file in "${sync_files[@]}"; do
        candidate="${temp_repo}/${file}"
        mkdir -p "$(dirname "${candidate}")"
        if [[ -f "${file}" ]]; then
            cp -p "${file}" "${candidate}"
        fi
    done

    cd "${temp_repo}"
    export GOWORK="${temp_repo}/go.work"
    prepare_offline_sync "${temp_root}/api-before-sync.mod"
    go work sync
    cd "${root_dir}"

    for file in "${sync_files[@]}"; do
        candidate="${temp_repo}/${file}"
        if [[ ! -e "${file}" && ! -L "${file}" && ! -e "${candidate}" && ! -L "${candidate}" ]]; then
            continue
        fi
        baseline="${file}"
        if [[ "${file}" == hololive/hololive-api/go.mod && -f "${temp_root}/api-before-sync.mod" ]]; then
            baseline="${temp_root}/api-before-sync.mod"
        fi
        if cmp -s "${baseline}" "${candidate}"; then
            continue
        fi

        drift=true
        if [[ -f "${file}" && -f "${candidate}" ]]; then
            diff -u --label "${file}" --label "${file} (go work sync)" "${baseline}" "${candidate}" >&2 || true
        elif [[ -f "${candidate}" ]]; then
            echo "go work sync would create ${file}" >&2
        else
            echo "go work sync would remove ${file}" >&2
        fi
    done

    if [[ "${drift}" == "true" ]]; then
        echo "go work sync changed workspace or module metadata; review the drift against canonical module versions" >&2
        exit 1
    fi
)
