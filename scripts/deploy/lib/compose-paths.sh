#!/usr/bin/env bash

compose_file_resolve_path() {
    local file="$1"
    if [[ ! -r "${file}" && -r "${ROOT_DIR}/deploy/compose/${file}" ]]; then
        printf '%s\n' "deploy/compose/${file}"
        return
    fi
    printf '%s\n' "${file}"
}

resolve_required_workspace_path() {
    local explicit_value="$1"
    local sibling_path="$2"
    local embedded_path="$3"
    local label="$4"
    local candidate="${explicit_value}"

    if [[ -z "${candidate}" ]]; then
        if [[ -d "${sibling_path}" ]]; then
            candidate="${sibling_path}"
        elif [[ -d "${embedded_path}" ]]; then
            candidate="${embedded_path}"
        fi
    fi
    if [[ ! -d "${candidate}" ]]; then
        echo "[ERROR] Active ${label} workspace not found" >&2
        return 1
    fi

    (cd "${candidate}" && pwd)
}

resolve_optional_workspace_path() {
    local explicit_value="$1"
    local sibling_path="$2"
    local embedded_path="$3"
    local label="$4"

    if [[ -n "${explicit_value}" ]]; then
        if [[ ! -d "${explicit_value}" ]]; then
            echo "[ERROR] Explicit ${label} workspace not found: ${explicit_value}" >&2
            return 1
        fi
        (cd "${explicit_value}" && pwd)
        return
    fi

    if [[ -d "${sibling_path}" ]]; then
        (cd "${sibling_path}" && pwd)
        return
    fi
    if [[ -d "${embedded_path}" ]]; then
        (cd "${embedded_path}" && pwd)
        return
    fi

    # Producer-only AP hosts do not need this build context. Keep the conventional
    # absolute candidate so Compose can render; an API image build will fail before
    # any runtime is stopped if the context is genuinely required and absent.
    printf '%s\n' "${sibling_path}"
}
