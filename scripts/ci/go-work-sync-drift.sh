#!/usr/bin/env bash

verify_go_work_sync_drift() (
    set -euo pipefail

    local root_dir="$1"
    local temp_root
    local temp_repo
    local file
    local candidate
    local drift=false
    local module
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
    go work sync
    cd "${root_dir}"

    for file in "${sync_files[@]}"; do
        candidate="${temp_repo}/${file}"
        if [[ ! -e "${file}" && ! -L "${file}" && ! -e "${candidate}" && ! -L "${candidate}" ]]; then
            continue
        fi
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
        echo "go work sync changed workspace or module metadata; review the drift against canonical module versions" >&2
        exit 1
    fi
)
