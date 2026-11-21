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
    local file
    local candidate
    local drift=false
    local module baseline
    local sync_files=(go.work go.work.sum)
    local python_runner
    python_runner="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/python-runner.sh"
    cd "${root_dir}"
    temp_root="$(mktemp -d)"
    temp_root="$(cd "${temp_root}" && pwd -P)"
    temp_repo="${temp_root}/hololive-bot"
    trap 'rm -rf "${temp_root}"' EXIT

    mkdir -p "${temp_repo}"
    cp -p go.work "${temp_repo}/"
    if [[ -f go.work.sum ]]; then cp -p go.work.sum "${temp_repo}/"; fi

    "${python_runner}" -- - "${temp_repo}" "${GO_WORKSPACE_MODULES[@]}" <<'PY' || return 1
import os
import shutil
import subprocess
import sys
from pathlib import Path

mirror = Path(sys.argv[1])
for entry in sys.argv[2:]:
    module = Path(entry)
    src, dst = module.resolve(strict=True), (mirror / module).resolve()
    if module.is_absolute() or not dst.is_relative_to(mirror.parent) or dst == mirror.parent:
        sys.exit(f"workspace module outside isolated sync root: {module}")
    dst.mkdir(parents=True, exist_ok=True)
    paths = subprocess.check_output(["git", "ls-files", "-zco", "--exclude-standard"], cwd=src)
    for item in paths.split(b"\0"):
        if not item:
            continue
        path = Path(os.fsdecode(item))
        origin, copy = src / path, dst / path
        go_input = path.suffix == ".go" or path.name in {"go.mod", "go.sum"}
        if not go_input and not (origin.is_symlink() and origin.is_dir()):
            continue
        if not origin.exists() and not origin.is_symlink():
            continue
        target = origin.resolve(strict=True)
        if not target.is_relative_to(src):
            sys.exit(f"workspace source link outside module: {origin}")
        copy.parent.mkdir(parents=True, exist_ok=True)
        if origin.is_symlink() and target.is_dir():
            if not copy.is_symlink():
                copy.symlink_to(os.path.relpath(dst / target.relative_to(src), copy.parent))
        elif target.is_file():
            shutil.copyfile(target, copy)
PY
    for module in "${GO_OWNED_MODULES[@]}"; do
        sync_files+=("${module}/go.mod" "${module}/go.sum")
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
