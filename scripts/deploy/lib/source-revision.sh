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
        *)
            echo "[ERROR] No source-built image mapping for service: $1" >&2
            return 1
            ;;
    esac
}

# 홀로 웹은 다른 저장소의 검증된 image를 소비하므로 Holo commit과 비교하지 않습니다.
deploy_verify_admin_image() {
    local container_cli="$1" image_ref="$2" expected_revision="$3"
    local source="" architecture=""
    deploy_assert_full_revision "$expected_revision" || return 1
    source="$("$container_cli" image inspect --format '{{index .Config.Labels "org.opencontainers.image.source"}}' "$image_ref")" || return 1
    [[ "$source" == https://github.com/park285/iris-admin ]] || {
        echo "[ERROR] admin-dashboard image must be built by iris-admin" >&2
        return 1
    }
    architecture="$("$container_cli" image inspect --format '{{.Architecture}}' "$image_ref")" || return 1
    [[ "$architecture" == arm64 ]] || {
        echo "[ERROR] central admin-dashboard image must target arm64" >&2
        return 1
    }
    deploy_verify_object_revision "$container_cli" image "$image_ref" "$expected_revision"
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

# 교체 후에는 mutable tag 대신 실제 컨테이너가 사용하는 image ID를 다시 검증합니다.
deploy_verify_admin_container() {
    local container_cli="$1" container_id="$2" expected_revision="$3"
    local image_id=""
    image_id="$(deploy_require_single_identifier "admin-dashboard image" \
        "$container_cli" container inspect --format '{{.Image}}' "$container_id")" || return 1
    deploy_verify_admin_image "$container_cli" "$image_id" "$expected_revision" || return 1
    deploy_verify_object_revision "$container_cli" container "$container_id" "$expected_revision"
}
