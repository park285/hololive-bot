#!/usr/bin/env bash

POSTGRES_CAPACITY_APPLICATION_ROLES=(
    hololive_runtime hololive_alarm_worker
    hololive_collector_a hololive_collector_b hololive_collector_c hololive_collector_d
)

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

postgres_capacity_parse_policy_target() {
    if [[ $# -ne 2 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_parse_policy_target <policy-file> <target-env-file>" >&2
        return 2
    fi
    local policy_file="$1"
    local target_env_file="$2"
    local raw line owner service_name _env_key source_key instances default_value role maximum
    local pipes capacity reserve
    local server_limit="" server_limit_rows=0 used=0
    local superuser_reserved="" superuser_reserved_rows=0
    local min_reserve=3
    local -A seen_owners=() role_limits=() role_used=() role_reserves=()

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
            [[ "${server_limit}" =~ ^[1-9][0-9]{0,8}$ ]] || {
                echo "[pg-capacity] policy has an invalid @server-limit" >&2
                return 1
            }
            continue
        fi
        if [[ "${line}" == @superuser-reserved\|* ]]; then
            ((superuser_reserved_rows += 1))
            superuser_reserved="${line#*|}"
            [[ "${superuser_reserved}" =~ ^(0|[1-9][0-9]{0,8})$ ]] || {
                echo "[pg-capacity] policy has an invalid @superuser-reserved" >&2
                return 1
            }
            continue
        fi
        if [[ "${line}" == @role-limit\|* ]]; then
            IFS='|' read -r _env_key role maximum <<<"${line}"
            if [[ ! "$role" =~ ^[a-z_][a-z0-9_]*$ || ! "$maximum" =~ ^[1-9][0-9]{0,8}$ ]] || [[ -v "role_limits[$role]" ]]; then
                echo '[pg-capacity] invalid or duplicate role limit' >&2
                return 1
            fi
            role_limits["$role"]="$maximum"
            continue
        fi
        if [[ "${line}" == @probe-reserve\|* ]]; then
            IFS='|' read -r _env_key role maximum <<<"${line}"
            if [[ ! -v "role_limits[$role]" || ! "$maximum" =~ ^[1-9][0-9]{0,8}$ ]] || [[ -v "role_reserves[$role]" ]]; then
                echo '[pg-capacity] probe reserve needs one positive value for a limited role' >&2
                return 1
            fi
            role_reserves["$role"]="$maximum"
            continue
        fi

        pipes="${line//[^|]/}"
        if (( ${#pipes} != 6 )); then
            echo "[pg-capacity] malformed policy row: ${line}" >&2
            return 1
        fi
        IFS='|' read -r owner service_name _env_key source_key instances default_value role <<<"${line}"
        if [[ ! "${owner}" =~ ^[a-z][a-z0-9-]*$ ]]; then
            echo "[pg-capacity] invalid allocation owner" >&2
            return 1
        fi
        if [[ -v "seen_owners[${owner}]" ]]; then
            echo "[pg-capacity] duplicate owner: ${owner}" >&2
            return 1
        fi
        seen_owners["${owner}"]=1
        if [[ ! "${instances}" =~ ^[1-9][0-9]{0,8}$ || ! "${default_value}" =~ ^[1-9][0-9]{0,8}$ ]]; then
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
                if [[ ! "${capacity}" =~ ^[1-9][0-9]{0,8}$ ]]; then
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
        if [[ ! "$role" =~ ^[a-z_][a-z0-9_]*$ ]] || [[ ! -v "role_limits[$role]" ]]; then
            echo '[pg-capacity] missing role limit' >&2
            return 1
        fi
        role_used["$role"]=$(( ${role_used[$role]:-0} + instances * capacity ))
        if (( role_used[$role] > role_limits[$role] )); then
            echo "[pg-capacity] aggregate pools exceed role limit: $role" >&2
            return 1
        fi
        if (( role_used[$role] + ${role_reserves[$role]:-0} > role_limits[$role] )); then
            echo "[pg-capacity] aggregate pools leave no reserved probe connection: $role" >&2
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
    for role in "${POSTGRES_CAPACITY_APPLICATION_ROLES[@]}"; do
        if [[ -z "${role_used[$role]:-}" ]]; then
            echo "[pg-capacity] missing application role allocation: ${role}" >&2
            return 1
        fi
    done

    reserve=$((server_limit - superuser_reserved - used))
    if (( reserve < min_reserve )); then
        echo "[pg-capacity] connection budget exhausted: max=${server_limit} superuser_reserved=${superuser_reserved} allocated=${used} reserve=${reserve}, want reserve >= ${min_reserve}" >&2
        return 1
    fi
    echo "[pg-capacity] source=target-env:${target_env_file} max=${server_limit} superuser_reserved=${superuser_reserved} allocated=${used} reserve=${reserve}; runtime, migrator, exporter and nightly allocations included"
}

postgres_capacity_assert_policy_target() {
    if [[ $# -ne 2 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_assert_policy_target <policy-file> <target-env-file>" >&2
        return 2
    fi
    postgres_capacity_parse_policy_target "$1" "$2"
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

postgres_capacity_effective_env() {
    local policy="$1" compose="$2" units="$3" replacement_service="${4:-}" replacement_unit="${5:-}"
    local owner service key source instances maximum role unit values
    postgres_capacity_assert_policy_target "$policy" "$compose" >&2 || return
    while IFS='|' read -r owner service key source instances maximum role; do
        [[ -n "$source" && "$owner" != \#* && "$owner" != @* ]] || continue
        unit="$units/$service.container"
        [[ "$service" != "$replacement_service" ]] || unit="$replacement_unit"
        values="$(postgres_quadlet_environment "$unit" "$compose" "$units/$service.container" "$key")" || return
        printf '%s=%s\n' "$source" "${values#*=}"
    done < "$policy"
}

postgres_writer_assert_sql() {
    local role result sql
    case "$1" in
        hololive-api) role=hololive_runtime ;;
        hololive-alarm-worker) role=hololive_alarm_worker ;;
        *) echo '[pg-writer] unknown central writer' >&2; return 1 ;;
    esac
    sql="$(cat <<'SQL'
BEGIN READ ONLY;
SET LOCAL default_transaction_read_only = on;
SET LOCAL statement_timeout = '30s';
SET LOCAL lock_timeout = '3s';
SELECT current_database() = 'hololive' AND current_user = :'role' AND session_user = :'role'
   AND current_setting('default_transaction_read_only') = 'on' AND current_setting('transaction_read_only') = 'on'
   AND current_setting('statement_timeout') = '30s' AND current_setting('lock_timeout') = '3s'
   AND inet_client_addr() << inet '172.16.0.0/12'
   AND EXISTS (SELECT 1 FROM pg_catalog.pg_stat_ssl WHERE pid = pg_backend_pid() AND ssl AND version = 'TLSv1.3');
ROLLBACK;
SQL
)"
    result="$(printf '%s\n' "$sql" | podman exec -i holo-postgres timeout -k 2 38 sh -ec '
        unset PGSERVICE PGSERVICEFILE PGHOSTADDR PGOPTIONS
        export PGHOST=holo-postgres PGPORT=5432 PGDATABASE=hololive PGUSER="$1"
        export PGSSLMODE=verify-full PGSSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem
        export PGGSSENCMODE=disable PGREQUIREAUTH=scram-sha-256 PGCONNECT_TIMEOUT=5
        export PGAPPNAME=hololive-writer-cutover-check
        case "$PGUSER" in
            hololive_runtime) PGPASSWORD="${HOLOLIVE_DB_PASSWORD:?}" ;;
            hololive_alarm_worker) PGPASSWORD="${HOLOLIVE_ALARM_WORKER_PASSWORD:?}" ;;
        esac
        export PGPASSWORD
        exec psql -XqwAt -v ON_ERROR_STOP=1 -v role="$PGUSER"
    ' sh "$role")" && [[ "$result" == t ]] || {
        echo "[pg-writer] direct TLS SQL verification failed for $role; activation refused" >&2
        return 1
    }
    printf '[pg-writer] direct TLS read-only SQL verified for %s\n' "$role"
}

postgres_capacity_assert_installed() (
    local policy="$1/scripts/ci/postgres-capacity-policy.tsv" compose="$2" units="$3" temporary
    . "$(dirname "${BASH_SOURCE[0]}")/compose-env.sh"
    . "$(dirname "${BASH_SOURCE[0]}")/postgres-endpoint.sh"
    temporary="$(mktemp)" || return
    trap 'rm -f -- "$temporary"' EXIT
    postgres_capacity_effective_env "$policy" "$compose" "$units" "${4:-}" "${5:-}" >"$temporary" || return
    postgres_capacity_assert_policy_target "$policy" "$temporary"
)

postgres_capacity_assert_service() (
    local repo="$1" compose="$2" units="$3" service="$4" unit="$5" endpoint
    . "$(dirname "${BASH_SOURCE[0]}")/compose-env.sh"
    . "$(dirname "${BASH_SOURCE[0]}")/postgres-endpoint.sh"
    endpoint="$(postgres_endpoint_quadlet "$unit" "$compose" "$units/$service.container")" || return
    case "$endpoint" in
        'holo-postgres|5432')
            postgres_capacity_assert_installed "$repo" "$compose" "$units" "$service" "$unit"
            ;;
        *)
            echo '[pg-capacity] unreviewed application PostgreSQL endpoint; activation refused' >&2
            return 1
            ;;
    esac
)
