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
    if [[ $# -ne 2 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_assert_policy_target <policy-file> <target-env-file>" >&2
        return 2
    fi
    local policy_file="$1"
    local target_env_file="$2"
    local raw line owner service_name _env_key source_key instances default_value role maximum
    local pipes capacity reserve
    local server_limit="" server_limit_rows=0 used=0
    local superuser_reserved="" superuser_reserved_rows=0
    # exporter·야간 작업 할당 뒤에도 일반 제어 슬롯 셋을 남긴다.
    local min_reserve=3
    local -A seen_owners=() role_limits=() role_used=()

    [[ -f "${policy_file}" && -r "${policy_file}" ]] || {
        echo "[pg-capacity] policy file is not readable: ${policy_file}" >&2
        return 1
    }
    [[ -f "${target_env_file}" && -r "${target_env_file}" ]] || {
        echo "[pg-capacity] target env file is not readable: ${target_env_file}" >&2
        return 1
    }

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
        if [[ "${line}" == @superuser-reserved\|* ]]; then
            ((superuser_reserved_rows += 1))
            superuser_reserved="${line#*|}"
            # 앞자리 0은 bash 산술에서 8진수로 해석되므로 허용하지 않는다.
            [[ "${superuser_reserved}" =~ ^(0|[1-9][0-9]*)$ ]] || {
                echo "[pg-capacity] policy has an invalid @superuser-reserved" >&2
                return 1
            }
            continue
        fi
        if [[ "${line}" == @role-limit\|* ]]; then
            IFS='|' read -r _env_key role maximum <<<"${line}"
            if [[ ! "$role" =~ ^[a-z_][a-z0-9_]*$ || ! "$maximum" =~ ^[1-9][0-9]*$ ]] || [[ -v "role_limits[$role]" ]]; then
                echo '[pg-capacity] invalid or duplicate role limit' >&2
                return 1
            fi
            role_limits["$role"]="$maximum"
            continue
        fi

        pipes="${line//[^|]/}"
        if (( ${#pipes} != 6 )); then
            echo "[pg-capacity] malformed policy row: ${line}" >&2
            return 1
        fi
        IFS='|' read -r owner service_name _env_key source_key instances default_value role <<<"${line}"
        if [[ -v "seen_owners[${owner}]" ]]; then
            echo "[pg-capacity] duplicate owner: ${owner}" >&2
            return 1
        fi
        seen_owners["${owner}"]=1
        if [[ ! "${instances}" =~ ^[1-9][0-9]*$ || ! "${default_value}" =~ ^[1-9][0-9]*$ ]]; then
            echo "[pg-capacity] non-positive capacity row: ${line}" >&2
            return 1
        fi

        if [[ -z "${service_name}" && ( -n "${_env_key}" || -n "${source_key}" ) ]]; then
            echo "[pg-capacity] external allocation must be fixed: ${owner}" >&2
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
                if (( instances > 1 )) && [[ "${capacity}" != "${default_value}" ]]; then
                    echo "[pg-capacity] ${source_key} is shared by multiple independently rendered instances; change the reviewed policy default and roll it out uniformly" >&2
                    return 1
                fi
            fi
        fi
        ((used += instances * capacity))
        if [[ -z "$role" ]] || [[ ! -v "role_limits[$role]" ]]; then
            echo '[pg-capacity] missing role limit' >&2
            return 1
        fi
        role_used["$role"]=$(( ${role_used[$role]:-0} + instances * capacity ))
        if (( role_used[$role] > role_limits[$role] )); then
            echo "[pg-capacity] aggregate pools exceed role limit: $role" >&2
            return 1
        fi
    done <"${policy_file}"

    if (( server_limit_rows != 1 )); then
        echo "[pg-capacity] policy must pin exactly one @server-limit" >&2
        return 1
    fi
    if (( superuser_reserved_rows != 1 )); then
        echo "[pg-capacity] policy must pin exactly one @superuser-reserved" >&2
        return 1
    fi

    # superuser 예약 슬롯은 비슈퍼유저 앱 역할이 쓸 수 없으므로 reserve에서 뺀다(PG 거부 조건과 같은 기준).
    reserve=$((server_limit - superuser_reserved - used))
    if (( reserve < min_reserve )); then
        echo "[pg-capacity] connection budget exhausted: max=${server_limit} superuser_reserved=${superuser_reserved} allocated=${used} reserve=${reserve}, want reserve >= ${min_reserve}" >&2
        return 1
    fi
    echo "[pg-capacity] source=target-env:${target_env_file} max=${server_limit} superuser_reserved=${superuser_reserved} allocated=${used} reserve=${reserve}; runtime, migrator, exporter and nightly allocations included"
}

postgres_capacity_assert_target() {
    if [[ $# -ne 2 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_assert_target <repo-root> <compose-env-file>" >&2
        return 2
    fi
    local repo_root="$1"
    local compose_env_file="$2"

    echo "[PREFLIGHT] Checking target PostgreSQL connection capacity"
    postgres_capacity_assert_policy_target \
        "${repo_root}/scripts/ci/postgres-capacity-policy.tsv" \
        "${compose_env_file}"
}
