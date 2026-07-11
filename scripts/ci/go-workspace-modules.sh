#!/usr/bin/env bash

GO_WORKSPACE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

load_go_workspace_modules() {
    local root_dir="${1:-${GO_WORKSPACE_ROOT}}"
    local workspace_json

    if ! workspace_json="$(cd "${root_dir}" && go work edit -json)"; then
        echo "failed to read ${root_dir}/go.work" >&2
        return 1
    fi

    WORKSPACE_JSON="${workspace_json}" python3 - "${root_dir}" <<'PY'
import json
import os
import sys
from pathlib import Path

root = Path(sys.argv[1]).resolve()
boundary = root.parent
document = json.loads(os.environ["WORKSPACE_JSON"])
modules: list[str] = []
seen: set[str] = set()

for entry in document.get("Use", []):
    raw = entry.get("DiskPath")
    if not isinstance(raw, str) or not raw:
        raise SystemExit("go.work contains an invalid module path")
    normalized = raw[2:] if raw.startswith("./") else raw
    if normalized in {"", "."}:
        continue
    resolved = (root / normalized).resolve()
    try:
        resolved.relative_to(boundary)
    except ValueError as exc:
        raise SystemExit(f"go.work module escapes workspace boundary: {raw}") from exc
    if resolved == boundary or not (resolved / "go.mod").is_file():
        raise SystemExit(f"go.work module is missing go.mod: {raw}")
    if normalized in seen:
        raise SystemExit(f"go.work contains a duplicate module path: {normalized}")
    seen.add(normalized)
    modules.append(normalized)

if not modules:
    raise SystemExit("go.work must contain at least one non-root module")

print("\n".join(modules))
PY
}

workspace_modules="$(load_go_workspace_modules)" || {
    return 1 2>/dev/null || exit 1
}
mapfile -t GO_WORKSPACE_MODULES < <(printf '%s\n' "${workspace_modules}")
unset workspace_modules

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
        printf './%s/...\n' "${module}"
    done
}

go_workspace_runtime_log_scan_targets() {
    local module
    for module in "${GO_WORKSPACE_MODULES[@]}"; do
        printf '%s\n' "${module}"
    done
}
