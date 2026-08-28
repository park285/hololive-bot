#!/usr/bin/env bash

postgres_capacity_trim() {
    local value="$1"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    printf '%s' "${value}"
}

postgres_capacity_read_override() {
    local target_env_file="$1"
    local wanted_key="$2"
    local raw line key value

    POSTGRES_CAPACITY_OVERRIDE_FOUND=false
    POSTGRES_CAPACITY_OVERRIDE_VALUE=""
    while IFS= read -r raw || [[ -n "${raw}" ]]; do
        line="$(postgres_capacity_trim "${raw}")"
        [[ -n "${line}" && "${line}" != \#* ]] || continue
        if [[ "${line}" == export[[:space:]]* ]]; then
            line="$(postgres_capacity_trim "${line#export}")"
        fi
        [[ "${line}" == *=* ]] || continue
        key="$(postgres_capacity_trim "${line%%=*}")"
        [[ "${key}" == "${wanted_key}" ]] || continue
        value="$(postgres_capacity_trim "${line#*=}")"
        if (( ${#value} >= 2 )) \
            && [[ "${value:0:1}" == '"' && "${value: -1}" == '"' \
                || "${value:0:1}" == "'" && "${value: -1}" == "'" ]]; then
            value="${value:1:${#value}-2}"
        fi
        POSTGRES_CAPACITY_OVERRIDE_FOUND=true
        POSTGRES_CAPACITY_OVERRIDE_VALUE="${value}"
    done <"${target_env_file}"
}

postgres_capacity_assert_policy_target() {
    if [[ $# -lt 2 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_assert_policy_target <policy-file> <target-env-file> [service=replicas ...]" >&2
        return 2
    fi
    local policy_file="$1"
    local target_env_file="$2"
    local scale raw line owner service_name _env_key source_key instances default_value
    local pipes effective_instances capacity reserve
    local server_limit="" server_limit_rows=0 used=0 owner_inventory=""
    local expected_owner_inventory=$'bot\nadmin-api\nllm-scheduler\nyoutube-plane\nalarm-worker\nyoutube-collector\ndb-migrate\n'
    local -A scale_overrides=() scaled_services_seen=() seen_owners=()
    shift 2

    [[ -f "${policy_file}" && -r "${policy_file}" ]] || {
        echo "[pg-capacity] policy file is not readable: ${policy_file}" >&2
        return 1
    }
    [[ -f "${target_env_file}" && -r "${target_env_file}" ]] || {
        echo "[pg-capacity] target env file is not readable: ${target_env_file}" >&2
        return 1
    }

    for scale in "$@"; do
        if [[ ! "${scale}" =~ ^([A-Za-z0-9][A-Za-z0-9_.-]*)=([0-9]+)$ ]]; then
            echo "[pg-capacity] malformed scale override: --scale=${scale}" >&2
            return 1
        fi
        service_name="${BASH_REMATCH[1]}"
        capacity="${BASH_REMATCH[2]}"
        if [[ -v "scale_overrides[${service_name}]" ]]; then
            if [[ "${scale_overrides[${service_name}]}" != "${capacity}" ]]; then
                echo "[pg-capacity] conflicting scale overrides for service: ${service_name}" >&2
            else
                echo "[pg-capacity] duplicate scale override for service: ${service_name}" >&2
            fi
            return 1
        fi
        scale_overrides["${service_name}"]="${capacity}"
    done

    while IFS= read -r raw || [[ -n "${raw}" ]]; do
        line="$(postgres_capacity_trim "${raw}")"
        [[ -n "${line}" && "${line}" != \#* ]] || continue
        if [[ "${line}" == @server-limit\|* ]]; then
            ((server_limit_rows += 1))
            server_limit="${line#*|}"
            [[ "${server_limit}" =~ ^[1-9][0-9]*$ ]] || {
                echo "[pg-capacity] policy has an invalid @server-limit" >&2
                return 1
            }
            continue
        fi

        pipes="${line//[^|]/}"
        if (( ${#pipes} != 5 )); then
            echo "[pg-capacity] malformed policy row: ${line}" >&2
            return 1
        fi
        IFS='|' read -r owner service_name _env_key source_key instances default_value <<<"${line}"
        if [[ -v "seen_owners[${owner}]" ]]; then
            echo "[pg-capacity] duplicate owner: ${owner}" >&2
            return 1
        fi
        seen_owners["${owner}"]=1
        owner_inventory+="${owner}"$'\n'
        if [[ ! "${instances}" =~ ^[1-9][0-9]*$ || ! "${default_value}" =~ ^[1-9][0-9]*$ ]]; then
            echo "[pg-capacity] non-positive capacity row: ${line}" >&2
            return 1
        fi

        if [[ -v "scale_overrides[${service_name}]" ]]; then
            effective_instances=$((instances + scale_overrides[${service_name}] - 1))
            scaled_services_seen["${service_name}"]=1
        else
            effective_instances="${instances}"
        fi
        if (( effective_instances < 0 )); then
            echo "[pg-capacity] invalid effective instance count for service: ${service_name}" >&2
            return 1
        fi

        capacity="${default_value}"
        if [[ -n "${source_key}" ]]; then
            postgres_capacity_read_override "${target_env_file}" "${source_key}"
            if [[ "${POSTGRES_CAPACITY_OVERRIDE_FOUND}" == true ]]; then
                capacity="${POSTGRES_CAPACITY_OVERRIDE_VALUE}"
                if [[ ! "${capacity}" =~ ^[1-9][0-9]*$ ]]; then
                    echo "[pg-capacity] ${source_key} must be a positive integer" >&2
                    return 1
                fi
                if (( effective_instances > 1 )) && [[ "${capacity}" != "${default_value}" ]]; then
                    echo "[pg-capacity] ${source_key} is shared by multiple independently rendered instances; change the reviewed policy default and roll it out uniformly" >&2
                    return 1
                fi
            fi
        fi
        ((used += effective_instances * capacity))
    done <"${policy_file}"

    if (( server_limit_rows != 1 )); then
        echo "[pg-capacity] policy must pin exactly one @server-limit" >&2
        return 1
    fi
    if [[ "${owner_inventory}" != "${expected_owner_inventory}" ]]; then
        echo "[pg-capacity] owner inventory mismatch: policy owner set or order changed" >&2
        return 1
    fi
    for service_name in "${!scale_overrides[@]}"; do
        if [[ ! -v "scaled_services_seen[${service_name}]" ]]; then
            echo "[pg-capacity] scale override references service absent from the reviewed capacity policy: ${service_name}" >&2
            return 1
        fi
    done

    reserve=$((server_limit - used))
    if (( reserve < 5 )); then
        echo "[pg-capacity] connection budget exhausted: max=${server_limit} allocated=${used} reserve=${reserve}, want reserve >= 5" >&2
        return 1
    fi
    echo "[pg-capacity] source=target-env:${target_env_file} max=${server_limit} allocated=${used} reserve=${reserve}; all central and four AP pools are inventoried"
}

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
    postgres_capacity_assert_policy_target \
        "${repo_root}/scripts/ci/postgres-capacity-policy.tsv" \
        "${compose_env_file}" \
        "${scale_args[@]#--scale=}"
}
