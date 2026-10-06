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

    # 수집 전용 AP에는 이 빌드 컨텍스트가 없어도 된다. Compose 렌더링에는 정해진 절대 경로를 제공하며,
    # 실제 API 이미지 빌드에 필요하면 런타임을 중지하기 전에 누락으로 실패한다.
    printf '%s\n' "${sibling_path}"
}
