#!/usr/bin/env bash
# Docker compose AP(iris-seoul)용 wrapper다. 중앙(hololive-seoul)은 Podman Quadlet unit으로 운영하므로
# 이 스크립트를 쓰지 않는다. compose AP(AP_RUNTIME_MODE=compose)는 Docker다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
# root 배포에서도 BuildKit의 optional Git refresh가 checkout index 소유권을 바꾸지 않게 한다.
export GIT_OPTIONAL_LOCKS=0
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
. "${ROOT_DIR}/scripts/deploy/lib/compose-paths.sh"
. "${ROOT_DIR}/scripts/deploy/lib/ap-compose-version.sh"
. "${ROOT_DIR}/scripts/deploy/lib/health-gate.sh"
. "${ROOT_DIR}/scripts/deploy/lib/postgres-capacity.sh"

compose_export_release_versions "${ROOT_DIR}"

compose_args=()
compose_files=()
compose_invokes_up=false
compose_requires_capacity=false
compose_up_build=false
compose_up_no_start=false
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
        --no-start)
            if [[ "${compose_invokes_up}" == true ]]; then
                compose_up_no_start=true
            fi
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
    echo "[ERROR] Pass the AP compose file with -f (for example deploy/compose/docker-compose.seoul.yml)" >&2
    exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
    echo "[ERROR] 'docker compose' is unavailable" >&2
    exit 1
fi
COMPOSE_CMD=(docker compose)
# health-gate.sh가 container inspect에 쓰는 CLI다. 이 wrapper의 대상은 Docker AP뿐이다.
CONTAINER_CLI=docker

if ! COMPOSE_ENV_FILE="$(compose_env_resolve_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE

compose_env_validate_file_format "${COMPOSE_ENV_FILE}"
compose_env_assert_shell_matches_all_file_keys "${COMPOSE_ENV_FILE}"

# 릴리스 pair overlay는 재부팅에도 유지한다. 배포 사전 검사는 명시한 후보 overlay만 사용한다.
profile_overlays=()
case "${WORKER_PROFILE_OVERRIDE_FILE:-}" in
    none) ;;
    "")
        for profile_receipt in "${ROOT_DIR}"/artifacts/worker-profile/active/*.pair; do
            [[ -e "$profile_receipt" || -L "$profile_receipt" ]] || continue
            profile_overlay="${profile_receipt%.pair}.yml"
            if [[ ! -f "$profile_receipt" || -L "$profile_receipt" || ! -f "$profile_overlay" || -L "$profile_overlay" ]]; then
                echo "[ERROR] incomplete worker profile activation; retain release artifacts" >&2
                exit 1
            fi
        done
        for profile_overlay in "${ROOT_DIR}"/artifacts/worker-profile/active/*.yml; do
            [[ -e "$profile_overlay" || -L "$profile_overlay" ]] || continue
            if [[ ! -f "$profile_overlay" || -L "$profile_overlay" || ! -f "${profile_overlay%.yml}.pair" ]]; then
                echo "[ERROR] incomplete worker profile activation; retain release artifacts" >&2
                exit 1
            fi
            profile_overlays+=("$profile_overlay")
        done
        ;;
    *) profile_overlays+=("$WORKER_PROFILE_OVERRIDE_FILE") ;;
esac
for profile_overlay in "${profile_overlays[@]}"; do
    compose_files+=("$profile_overlay")
    if (( compose_command_index >= 0 )); then
        compose_args=("${compose_args[@]:0:compose_command_index}" -f "$profile_overlay" "${compose_args[@]:compose_command_index}")
        compose_command_index=$((compose_command_index + 2))
    else
        compose_args+=(-f "$profile_overlay")
    fi
done
compose_env_assert_no_shell_shadow_for_compose_files "${COMPOSE_ENV_FILE}" "${compose_files[@]}"

if [[ "${compose_requires_capacity}" == true ]]; then
    postgres_capacity_assert_target "${ROOT_DIR}" "${COMPOSE_ENV_FILE}" "${compose_scale_overrides[@]}"
fi

if [[ "${compose_invokes_up}" == true ]]; then
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

    # AP 배포는 항상 대상 service를 지정한다. 전체 up은 health gate 대상을 알 수 없어 거부한다.
    if [[ ${#up_service_targets[@]} -eq 0 ]]; then
        echo "[ERROR] AP compose up must name its services" >&2
        exit 1
    fi
    bind_preflight_required=false
    gate_targets=()
    for service in "${up_service_targets[@]}"; do
        if cutover_service_uses_app_writable_bind_mount "${service}"; then
            bind_preflight_required=true
            gate_targets+=("${service}")
        fi
    done

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

    if [[ "${bind_preflight_required}" == true || ${#gate_targets[@]} -gt 0 ]]; then
        COMPOSE_FILE_ARGS=("${compose_prefix[@]}")
        if [[ "${bind_preflight_required}" == true ]]; then
            echo "[PREFLIGHT] Verifying host bind-mount write access for app uid ${HOLOLIVE_APP_UID}:${HOLOLIVE_APP_GID}"
            if ! cutover_bind_mount_preflight "${ROOT_DIR}"; then
                echo "[ERROR] host bind-mount preflight failed before cutover; aborting (no containers changed)" >&2
                exit 1
            fi
        fi
        if [[ ${#gate_targets[@]} -gt 0 ]]; then
            cutover_capture_restart_baseline "${gate_targets[@]}"
        fi
    fi

    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"

    if [[ "${compose_up_no_start}" == false && ${#gate_targets[@]} -gt 0 ]] && ! cutover_health_gate "${gate_targets[@]}"; then
        echo "[ERROR] health gate failed after cutover up" >&2
        exit 1
    fi
    exit 0
fi

exec "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"
