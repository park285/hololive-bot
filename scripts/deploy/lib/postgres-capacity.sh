#!/usr/bin/env bash

postgres_capacity_assert_target() {
    if [[ $# -lt 2 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_assert_target <repo-root> <compose-env-file> [service=replicas ...]" >&2
        return 2
    fi
    local repo_root="$1"
    local compose_env_file="$2"
    local scale
    local -a scale_args=()
    shift 2
    for scale in "$@"; do
        scale_args+=("--scale=${scale}")
    done

    echo "[PREFLIGHT] Checking target PostgreSQL connection capacity"
    "${repo_root}/scripts/ci/check-postgres-capacity.sh" \
        "${repo_root}/deploy/compose/docker-compose.prod.yml" \
        "${repo_root}/scripts/ci/postgres-capacity-policy.tsv" \
        "${compose_env_file}" \
        --target-env-only \
        "${scale_args[@]}"
}
