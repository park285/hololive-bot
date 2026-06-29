#!/usr/bin/env bash

HOLOLIVE_APP_UID="${HOLOLIVE_APP_UID:-1000}"
HOLOLIVE_APP_GID="${HOLOLIVE_APP_GID:-1000}"

declare -gA CUTOVER_RESTART_BASELINE=()
declare -gA CUTOVER_CONTAINER_ID=()

compose_health_resolve_container() {
    local service="$1"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" \
        ps -q "${service}" 2>/dev/null | head -1
}

wait_for_service_health() {
    local service="$1"
    local timeout="${HEALTH_GATE_TIMEOUT:-120}"
    local interval=3
    local elapsed=0
    local container=""

    container="$(compose_health_resolve_container "${service}")"
    if [[ -z "${container}" ]]; then
        echo "[HEALTH] no container resolved for ${service}" >&2
        return 1
    fi

    local baseline_restarts=0
    if [[ -n "${CUTOVER_CONTAINER_ID[${service}]:-}" \
       && "${CUTOVER_CONTAINER_ID[${service}]}" == "${container}" ]]; then
        baseline_restarts="${CUTOVER_RESTART_BASELINE[${service}]:-0}"
    fi
    : "${baseline_restarts:=0}"

    local has_health=""
    has_health="$("${CONTAINER_CLI}" inspect -f '{{if .State.Health}}yes{{end}}' "${container}" 2>/dev/null || true)"

    local status="" health="" restarts=""
    while (( elapsed < timeout )); do
        status="$("${CONTAINER_CLI}" inspect -f '{{.State.Status}}' "${container}" 2>/dev/null || echo unknown)"
        restarts="$("${CONTAINER_CLI}" inspect -f '{{.RestartCount}}' "${container}" 2>/dev/null || echo 0)"
        if [[ "${has_health}" == yes ]]; then
            health="$("${CONTAINER_CLI}" inspect -f '{{.State.Health.Status}}' "${container}" 2>/dev/null || echo unknown)"
        else
            health="n/a"
        fi
        echo "[HEALTH] ${service}: status=${status} health=${health} restarts=${restarts} (${elapsed}s/${timeout}s)"

        if [[ "${status}" == restarting || "${status}" == exited || "${status}" == dead ]] || (( restarts > baseline_restarts )); then
            echo "[HEALTH] ${service} unstable (status=${status} restarts=${restarts} baseline=${baseline_restarts})" >&2
            return 1
        fi
        if [[ "${has_health}" == yes ]]; then
            [[ "${health}" == healthy ]] && return 0
            if [[ "${health}" == unhealthy ]]; then
                echo "[HEALTH] ${service} reported unhealthy" >&2
                return 1
            fi
        elif [[ "${status}" == running ]]; then
            sleep "${interval}"; elapsed=$((elapsed + interval))
            status="$("${CONTAINER_CLI}" inspect -f '{{.State.Status}}' "${container}" 2>/dev/null || echo unknown)"
            restarts="$("${CONTAINER_CLI}" inspect -f '{{.RestartCount}}' "${container}" 2>/dev/null || echo 0)"
            if [[ "${status}" == running ]] && (( restarts == baseline_restarts )); then
                return 0
            fi
            echo "[HEALTH] ${service} did not stay running (status=${status} restarts=${restarts})" >&2
            return 1
        fi
        sleep "${interval}"; elapsed=$((elapsed + interval))
    done

    echo "[HEALTH] ${service} did not become healthy within ${timeout}s" >&2
    return 1
}

dump_failure_diagnostics() {
    local service="$1"
    {
        echo "=== [FAIL] diagnostics: ${service} ==="
        "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" ps "${service}" || true
        local container
        container="$(compose_health_resolve_container "${service}")"
        if [[ -n "${container}" ]]; then
            "${CONTAINER_CLI}" inspect \
                -f 'status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}n/a{{end}} restarts={{.RestartCount}} exitcode={{.State.ExitCode}}' \
                "${container}" || true
        fi
        "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" logs --tail=300 "${service}" || true
    } >&2
}

assert_host_dir_writable_by_app() {
    local dir="$1"
    local uid="${HOLOLIVE_APP_UID}"
    local gid="${HOLOLIVE_APP_GID}"

    if [[ ! -d "${dir}" ]]; then
        echo "[PREFLIGHT] bind-mount dir missing: ${dir}" >&2
        return 1
    fi

    local owner group mode
    owner="$(stat -c '%u' "${dir}")"
    group="$(stat -c '%g' "${dir}")"
    mode="$(stat -c '%a' "${dir}")"

    if [[ "${owner}" == "${uid}" ]] && (( 0${mode} & 0200 )); then return 0; fi
    if [[ "${group}" == "${gid}" ]] && (( 0${mode} & 0020 )); then return 0; fi
    if (( 0${mode} & 0002 )); then return 0; fi

    echo "[PREFLIGHT] ${dir} (owner=${owner} group=${group} mode=${mode}) not writable by app uid=${uid} gid=${gid}" >&2
    echo "[PREFLIGHT] app services run as ${uid}:${gid}; chown ${uid}:${gid} '${dir}' or grant write" >&2
    return 1
}

assert_app_bind_mounts_writable() {
    local rc=0
    local dir=""
    for dir in "$@"; do
        assert_host_dir_writable_by_app "${dir}" || rc=1
    done
    return "${rc}"
}

HOLOLIVE_APP_WRITABLE_BIND_DIRS=("logs" "data")

cutover_bind_mount_preflight() {
    local root="$1"
    local -a dirs=()
    local name=""
    for name in "${HOLOLIVE_APP_WRITABLE_BIND_DIRS[@]}"; do
        dirs+=("${root}/${name}")
    done
    assert_app_bind_mounts_writable "${dirs[@]}"
}

cutover_service_uses_app_writable_bind_mount() {
    local service="$1"

    case "${service}" in
        hololive-api|hololive-alarm-worker|youtube-producer|youtube-producer-c|admin-dashboard)
            return 0
            ;;
    esac
    return 1
}

cutover_health_gate() {
    local rc=0
    local svc=""
    for svc in "$@"; do
        echo "[HEALTH-GATE] ${svc}"
        if wait_for_service_health "${svc}"; then
            echo "[OK] ${svc} passed health gate"
        else
            dump_failure_diagnostics "${svc}"
            echo "[ERROR] ${svc} failed health gate" >&2
            rc=1
        fi
    done
    return "${rc}"
}

cutover_capture_restart_baseline() {
    local svc="" container="" restarts=""
    for svc in "$@"; do
        container="$(compose_health_resolve_container "${svc}")"
        if [[ -n "${container}" ]]; then
            restarts="$("${CONTAINER_CLI}" inspect -f '{{.RestartCount}}' "${container}" 2>/dev/null || echo 0)"
            CUTOVER_RESTART_BASELINE["${svc}"]="${restarts:-0}"
            CUTOVER_CONTAINER_ID["${svc}"]="${container}"
        else
            CUTOVER_RESTART_BASELINE["${svc}"]=0
            CUTOVER_CONTAINER_ID["${svc}"]=""
        fi
    done
}
