#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
# root 배포에서도 BuildKit의 optional Git refresh가 checkout index 소유권을 바꾸지 않게 한다.
export GIT_OPTIONAL_LOCKS=0
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
. "${ROOT_DIR}/scripts/deploy/lib/compose-paths.sh"
. "${ROOT_DIR}/scripts/deploy/lib/ap-compose-version.sh"
. "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh"
. "${ROOT_DIR}/scripts/deploy/lib/health-gate.sh"
. "${ROOT_DIR}/scripts/deploy/lib/kapu-alarm-worker-fence.sh"
. "${ROOT_DIR}/scripts/deploy/lib/postgres-capacity.sh"
. "${ROOT_DIR}/scripts/deploy/lib/public-bind-mounts.sh"

compose_export_release_versions "${ROOT_DIR}"

compose_args=()
compose_files=()
compose_invokes_up=false
compose_requires_capacity=false
compose_up_build=false
compose_up_after_separator=false
compose_scale_overrides=()
compose_command_seen=false
compose_command_index=-1
previous=""
previous_option=""
for arg in "$@"; do
    if [[ "${previous}" == "-f" || "${previous}" == "--file" ]]; then
        resolved_file="$(compose_file_resolve_path "${arg}")"
        compose_files+=("${resolved_file}")
        compose_args+=("${previous}" "${resolved_file}")
        previous=""
        continue
    fi
    if [[ "${previous}" == "global-option-value" ]]; then
        compose_args+=("${arg}")
        previous=""
        previous_option=""
        continue
    fi
    if [[ "${previous}" == "up-scale-value" ]]; then
        compose_scale_overrides+=("${arg}")
        compose_args+=("${arg}")
        previous=""
        previous_option=""
        continue
    fi

    case "${arg}" in
        --env-file|--env-file=*)
            echo "[ERROR] Use COMPOSE_ENV_FILE with this wrapper; do not pass --env-file directly" >&2
            exit 1
            ;;
    esac

    if [[ "${compose_command_seen}" == false ]]; then
        case "${arg}" in
            -f|--file)
                previous="${arg}"
                continue
                ;;
            -f=*|--file=*)
                resolved_file="$(compose_file_resolve_path "${arg#*=}")"
                compose_files+=("${resolved_file}")
                compose_args+=("${arg%%=*}=${resolved_file}")
                continue
                ;;
            --ansi|--parallel|--profile|--progress|--project-directory|-p|--project-name)
                compose_args+=("${arg}")
                previous="global-option-value"
                previous_option="${arg}"
                continue
                ;;
            --ansi=*|--parallel=*|--profile=*|--progress=*|--project-directory=*|--project-name=*)
                ;;
            -*)
                ;;
            *)
                compose_command_seen=true
                compose_command_index="${#compose_args[@]}"
                ;;
        esac
    fi

    case "${arg}" in
        up)
            compose_invokes_up=true
            compose_requires_capacity=true
            ;;
        start|restart|run)
            compose_requires_capacity=true
            ;;
        --build)
            if [[ "${compose_invokes_up}" == true ]]; then
                compose_up_build=true
                continue
            fi
            ;;
        --)
            if [[ "${compose_invokes_up}" == true ]]; then
                compose_up_after_separator=true
            fi
            ;;
        --scale)
            if [[ "${compose_invokes_up}" == true && "${compose_up_after_separator}" == false ]]; then
                compose_args+=("${arg}")
                previous="up-scale-value"
                previous_option="${arg}"
                continue
            fi
            ;;
        --scale=*)
            if [[ "${compose_invokes_up}" == true && "${compose_up_after_separator}" == false ]]; then
                compose_scale_overrides+=("${arg#*=}")
            fi
            ;;
    esac

    compose_args+=("${arg}")
done

if [[ -n "${previous}" ]]; then
    echo "[ERROR] Missing value for ${previous_option:-${previous}}" >&2
    exit 1
fi

if [[ ${#compose_files[@]} -eq 0 ]]; then
    compose_files=(deploy/compose/docker-compose.prod.yml)
    compose_args=(-f deploy/compose/docker-compose.prod.yml "${compose_args[@]}")
    if (( compose_command_index >= 0 )); then
        compose_command_index=$((compose_command_index + 2))
    fi
fi

if ! assert_kapu_alarm_worker_start_allowed "$(hostname -s)" "${compose_command_index}" "${compose_args[@]}"; then
    exit 1
fi

SHARED_GO_WORKSPACE_PATH="$(resolve_required_workspace_path \
    "${SHARED_GO_WORKSPACE_PATH:-}" \
    "${ROOT_DIR}/../shared-go" \
    "${ROOT_DIR}/shared-go" \
    "shared-go")"
export SHARED_GO_WORKSPACE_PATH

CONTAINER_CLI="${CONTAINER_CLI:-docker}"
case "${CONTAINER_CLI}" in
    docker|podman) ;;
    *)
        echo "[ERROR] Unsupported CONTAINER_CLI: ${CONTAINER_CLI}" >&2
        echo "        Allowed values: docker, podman" >&2
        exit 1
        ;;
esac

if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
    echo "[ERROR] Container CLI not found: ${CONTAINER_CLI}" >&2
    exit 1
fi

COMPOSE_CMD=("${CONTAINER_CLI}" compose)
if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=(podman-compose)
elif ! "${CONTAINER_CLI}" compose version >/dev/null 2>&1; then
    echo "[ERROR] '${CONTAINER_CLI} compose' is unavailable" >&2
    exit 1
fi

if ! COMPOSE_ENV_FILE="$(compose_env_resolve_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE

compose_env_validate_file_format "${COMPOSE_ENV_FILE}"
compose_env_assert_shell_matches_all_file_keys "${COMPOSE_ENV_FILE}"

collector_disable_value="${HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR:-}"
if compose_env_key_exists_in_file "${COMPOSE_ENV_FILE}" "HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR"; then
    collector_disable_value="$(compose_env_read_value_from_file "${COMPOSE_ENV_FILE}" "HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR")"
fi
case "${collector_disable_value}" in
    ""|0) ;;
    1)
        collector_disable_overlay="deploy/compose/docker-compose.youtube-collector-disabled.yml"
        collector_disable_present=false
        for file in "${compose_files[@]}"; do
            if [[ "${file##*/}" == "${collector_disable_overlay##*/}" ]]; then
                collector_disable_present=true
                break
            fi
        done
        if [[ "${collector_disable_present}" == false ]]; then
            compose_files+=("${collector_disable_overlay}")
            if (( compose_command_index >= 0 )); then
                compose_args=(
                    "${compose_args[@]:0:compose_command_index}"
                    -f "${collector_disable_overlay}"
                    "${compose_args[@]:compose_command_index}"
                )
            else
                compose_args+=(-f "${collector_disable_overlay}")
            fi
        fi
        ;;
    *)
        echo "[ERROR] HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR must be 0 or 1" >&2
        exit 1
        ;;
esac

compose_env_assert_no_shell_shadow_for_compose_files "${COMPOSE_ENV_FILE}" "${compose_files[@]}"
compose_env_assert_admin_dashboard_loopback_bind "${COMPOSE_ENV_FILE}"

if [[ "${compose_requires_capacity}" == true ]]; then
    postgres_capacity_assert_target "${ROOT_DIR}" "${COMPOSE_ENV_FILE}" "${compose_scale_overrides[@]}"
fi

if [[ "${compose_invokes_up}" == true ]]; then
    compose_env_assert_live_compat_for_host_networked_postgres "${compose_files[@]}"

    up_index=-1
    for index in "${!compose_args[@]}"; do
        if [[ "${compose_args[$index]}" == "up" ]]; then
            up_index="${index}"
            break
        fi
    done
    if (( up_index < 0 )); then
        echo "[ERROR] Internal error: compose up index was not found" >&2
        exit 1
    fi

    compose_prefix=("${compose_args[@]:0:up_index}")
    up_service_targets=()
    option_requires_value=false
    after_separator=false
    for ((index = up_index + 1; index < ${#compose_args[@]}; index++)); do
        token="${compose_args[$index]}"
        if [[ "${option_requires_value}" == true ]]; then
            option_requires_value=false
            continue
        fi
        if [[ "${after_separator}" == true ]]; then
            up_service_targets+=("${token}")
            continue
        fi
        case "${token}" in
            --)
                after_separator=true
                ;;
            --scale|--wait-timeout|--timeout|-t|--exit-code-from|--pull|--attach|--no-attach)
                option_requires_value=true
                ;;
            --scale=*|--wait-timeout=*|--timeout=*|--exit-code-from=*|--pull=*|--attach=*|--no-attach=*)
                ;;
            -*)
                ;;
            *)
                up_service_targets+=("${token}")
                ;;
        esac
    done

    bind_preflight_required=false
    public_ingress_preflight_required=false
    removed_runtime_cleanup_required=false
    gate_targets=()
    if [[ ${#up_service_targets[@]} -eq 0 ]]; then
        bind_preflight_required=true
        removed_runtime_cleanup_required=true
        collector_disabled=false
        for file in "${compose_files[@]}"; do
            if [[ "${file##*/}" == "docker-compose.youtube-collector-disabled.yml" ]]; then
                collector_disabled=true
                break
            fi
        done
        gate_targets=(hololive-api hololive-alarm-worker admin-dashboard)
        if [[ "${collector_disabled}" == false ]]; then
            gate_targets+=(youtube-collector)
        fi
        for file in "${compose_files[@]}"; do
            if [[ "${file##*/}" == "docker-compose.live-compat.yml" ]]; then
                public_ingress_preflight_required=true
                gate_targets+=(admin-dashboard-ingress)
                break
            fi
        done
    else
        for service in "${up_service_targets[@]}"; do
            if cutover_service_uses_app_writable_bind_mount "${service}"; then
                bind_preflight_required=true
                gate_targets+=("${service}")
            fi
            if [[ "${service}" == "hololive-api" ]]; then
                removed_runtime_cleanup_required=true
            fi
            if [[ "${service}" == "admin-dashboard-ingress" ]]; then
                public_ingress_preflight_required=true
                gate_targets+=("${service}")
            fi
        done
    fi

    if [[ "${public_ingress_preflight_required}" == true ]]; then
        echo "[PREFLIGHT] Preparing public ingress bind source mode"
        if ! prepare_admin_dashboard_ingress_bind_mount "${ROOT_DIR}"; then
            echo "[ERROR] public ingress bind source preflight failed before cutover" >&2
            exit 1
        fi
    fi

    echo "[PREFLIGHT] Rendering Compose before start"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_prefix[@]}" config --quiet

    if [[ "${compose_up_build}" == true ]]; then
        echo "[PREFLIGHT] Building images before start"
        if [[ ${#up_service_targets[@]} -gt 0 ]]; then
            "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" \
                "${compose_prefix[@]}" build --with-dependencies "${up_service_targets[@]}"
        else
            "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_prefix[@]}" build
        fi
    fi

    if [[ "${bind_preflight_required}" == true || "${removed_runtime_cleanup_required}" == true || ${#gate_targets[@]} -gt 0 ]]; then
        COMPOSE_FILE_ARGS=("${compose_prefix[@]}")
        if [[ "${bind_preflight_required}" == true ]]; then
            echo "[PREFLIGHT] Verifying host bind-mount write access for app uid ${HOLOLIVE_APP_UID}:${HOLOLIVE_APP_GID}"
            if ! cutover_bind_mount_preflight "${ROOT_DIR}"; then
                echo "[ERROR] host bind-mount preflight failed before cutover; aborting (no containers changed)" >&2
                exit 1
            fi
        fi
        if [[ "${removed_runtime_cleanup_required}" == true ]]; then
            removed_runtime_cleanup_before_cutover
        fi
        if [[ ${#gate_targets[@]} -gt 0 ]]; then
            cutover_capture_restart_baseline "${gate_targets[@]}"
        fi
    fi

    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"

    if [[ ${#gate_targets[@]} -gt 0 ]] && ! cutover_health_gate "${gate_targets[@]}"; then
        echo "[ERROR] health gate failed after cutover up" >&2
        exit 1
    fi
    exit 0
fi

exec "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"
