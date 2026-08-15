#!/usr/bin/env bash

deploy_assert_full_revision() {
    local revision="$1"

    if [[ ! "${revision}" =~ ^[0-9a-f]{40}$ ]]; then
        echo "[ERROR] Deployment source revision is not a full 40-hex commit" >&2
        return 1
    fi
}

deploy_source_revision() {
    local repository_root="$1"
    local worktree_state=""
    local revision=""

    if ! worktree_state="$(git -C "${repository_root}" status --porcelain=v1 --untracked-files=all)"; then
        echo "[ERROR] Failed to inspect deployment source worktree" >&2
        return 1
    fi
    if [[ -n "${worktree_state}" ]]; then
        echo "[ERROR] Deployment source worktree is not clean (tracked or untracked changes present)" >&2
        return 1
    fi
    if ! revision="$(git -C "${repository_root}" rev-parse --verify 'HEAD^{commit}')"; then
        echo "[ERROR] Failed to resolve deployment source revision" >&2
        return 1
    fi
    deploy_assert_full_revision "${revision}" || return 1
    printf '%s\n' "${revision}"
}

deploy_require_single_identifier() {
    local label="$1"
    shift
    local identifier=""

    if ! identifier="$("$@")"; then
        echo "[ERROR] Failed to resolve ${label}" >&2
        return 1
    fi
    if [[ -z "${identifier}" || "${identifier}" == *$'\n'* ]]; then
        echo "[ERROR] Expected exactly one ${label}" >&2
        return 1
    fi
    printf '%s\n' "${identifier}"
}

deploy_service_image_ref() {
    case "$1" in
        hololive-api|hololive-db-migrate) printf '%s\n' hololive-api:prod ;;
        hololive-alarm-worker) printf '%s\n' hololive-alarm-worker:prod ;;
        youtube-collector|youtube-collector-c|youtube-collector-a|youtube-collector-b|youtube-collector-d)
            printf '%s\n' hololive-youtube-collector:prod
            ;;
        admin-dashboard) printf '%s\n' admin-dashboard:prod ;;
        *)
            echo "[ERROR] No source-built image mapping for service: $1" >&2
            return 1
            ;;
    esac
}

deploy_verify_object_revision() {
    local container_cli="$1"
    local object_kind="$2"
    local object_ref="$3"
    local expected_revision="$4"
    local actual_revision=""

    deploy_assert_full_revision "${expected_revision}" || return 1
    case "${object_kind}" in
        image|container) ;;
        *)
            echo "[ERROR] Unsupported revision object kind: ${object_kind}" >&2
            return 1
            ;;
    esac
    if ! actual_revision="$("${container_cli}" "${object_kind}" inspect \
        --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' \
        "${object_ref}")"; then
        echo "[ERROR] Failed to inspect ${object_kind} revision: ${object_ref}" >&2
        return 1
    fi
    if [[ "${actual_revision}" != "${expected_revision}" ]]; then
        echo "[ERROR] ${object_kind} revision mismatch: ${object_ref}" >&2
        return 1
    fi
}
