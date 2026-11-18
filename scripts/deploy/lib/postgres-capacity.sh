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

postgres_capacity_parse_policy_target() {
    if [[ $# -ne 3 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_parse_policy_target <policy-file> <target-env-file> <role-capacities>" >&2
        return 2
    fi
    local policy_file="$1"
    local target_env_file="$2"
    local -n parsed_role_capacities="$3"
    local raw line owner service_name _env_key source_key instances default_value role maximum
    local pipes capacity reserve
    local server_limit="" server_limit_rows=0 used=0
    local superuser_reserved="" superuser_reserved_rows=0
    # exporter·야간 작업 할당 뒤에도 일반 제어 슬롯 셋을 남긴다.
    local min_reserve=3
    local -A seen_owners=() role_limits=() role_used=()
    parsed_role_capacities=()

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
            # 앞자리 0은 bash 산술에서 8진수로 해석되므로 허용하지 않는다.
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
        if [[ "${owner}" == pgbouncer-auth ]] \
            && [[ -n "${service_name}${_env_key}${source_key}" || "${instances}" != 1 \
                || "${default_value}" != 1 || "${role}" != hololive_pgbouncer_auth ]]; then
            echo "[pg-capacity] pgbouncer-auth must reserve one fixed authentication connection" >&2
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
    done <"${policy_file}"

    if (( server_limit_rows != 1 )); then
        echo "[pg-capacity] policy must pin exactly one @server-limit" >&2
        return 1
    fi
    if (( superuser_reserved_rows != 1 )); then
        echo "[pg-capacity] policy must pin exactly one @superuser-reserved" >&2
        return 1
    fi
    if [[ "${seen_owners[pgbouncer-auth]:-}" != 1 \
        || "${role_limits[hololive_pgbouncer_auth]:-}" != 1 \
        || "${role_used[hololive_pgbouncer_auth]:-}" != 1 ]]; then
        echo "[pg-capacity] policy must reserve and limit one PgBouncer authentication connection" >&2
        return 1
    fi

    # superuser 예약 슬롯은 비슈퍼유저 앱 역할이 쓸 수 없으므로 reserve에서 뺀다(PG 거부 조건과 같은 기준).
    reserve=$((server_limit - superuser_reserved - used))
    if (( reserve < min_reserve )); then
        echo "[pg-capacity] connection budget exhausted: max=${server_limit} superuser_reserved=${superuser_reserved} allocated=${used} reserve=${reserve}, want reserve >= ${min_reserve}" >&2
        return 1
    fi
    for role in "${!role_used[@]}"; do
        parsed_role_capacities["${role}"]="${role_used[${role}]}"
    done
    echo "[pg-capacity] source=target-env:${target_env_file} max=${server_limit} superuser_reserved=${superuser_reserved} allocated=${used} reserve=${reserve}; runtime, pooler auth, migrator, exporter and nightly allocations included"
}

postgres_capacity_assert_policy_target() {
    if [[ $# -ne 2 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_assert_policy_target <policy-file> <target-env-file>" >&2
        return 2
    fi
    local -A capacities=()
    postgres_capacity_parse_policy_target "$1" "$2" capacities
}

postgres_capacity_write_pgbouncer_config() (
    if [[ $# -ne 3 ]]; then
        echo "[pg-capacity] internal usage: postgres_capacity_write_pgbouncer_config <policy-file> <target-env-file> <output-file>" >&2
        return 2
    fi
    local output_file="$3" temporary_file role database_capacity=0
    local -A capacities=()
    local -a pooled_roles=(
        hololive_runtime hololive_alarm_worker
        hololive_collector_a hololive_collector_b hololive_collector_c hololive_collector_d
        hololive_pgbouncer_auth
    )
    postgres_capacity_parse_policy_target "$1" "$2" capacities || return
    for role in "${pooled_roles[@]}"; do
        if [[ -z "${capacities[${role}]:-}" ]]; then
            echo "[pg-capacity] missing pooled role allocation: ${role}" >&2
            return 1
        fi
        ((database_capacity += capacities[${role}]))
    done

    temporary_file="$(mktemp -- "${output_file}.tmp.XXXXXX")" || return
    trap 'rm -f -- "${temporary_file}"' EXIT
    {
        cat <<'INI'
[databases]
hololive = host=holo-postgres port=5432 dbname=hololive

[pgbouncer]
listen_addr = 0.0.0.0
listen_port = 6432
unix_socket_dir = /tmp
unix_socket_mode = 0700
auth_type = hba
auth_hba_file = /etc/pgbouncer/pg_hba.conf
auth_file = /etc/pgbouncer/stats-users.txt
auth_user = hololive_pgbouncer_auth
auth_query = SELECT username, password FROM hololive_pooler_auth.lookup($1)
stats_users = pgbouncer
pool_mode = transaction
max_client_conn = 100
default_pool_size = 1
min_pool_size = 0
reserve_pool_size = 0
INI
        printf 'max_db_connections = %s\n' "${database_capacity}"
        cat <<'INI'
track_extra_parameters = statement_timeout
max_prepared_statements = 200
query_wait_timeout = 5
server_idle_timeout = 600
client_tls_sslmode = require
client_tls_protocols = tlsv1.2,tlsv1.3
client_tls_cert_file = /run/hololive-bot/pgbouncer/server.crt
client_tls_key_file = /run/hololive-bot/pgbouncer/server.key
server_tls_sslmode = verify-full
server_tls_protocols = tlsv1.2,tlsv1.3
server_tls_ca_file = /run/hololive-bot/certs/postgres-ca.pem
server_tls_cert_file = /run/hololive-bot/pgbouncer/backend-client.crt
server_tls_key_file = /run/hololive-bot/pgbouncer/backend-client.key

[users]
INI
        for role in "${pooled_roles[@]}"; do
            printf '%s = pool_size=%s max_user_connections=%s\n' \
                "${role}" "${capacities[${role}]}" "${capacities[${role}]}"
        done
    } >"${temporary_file}" || return
    chmod 0644 "${temporary_file}" || return
    mv -fT -- "${temporary_file}" "${output_file}"
)

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
    . "$(dirname "${BASH_SOURCE[0]}")/compose-env.sh"
    . "$(dirname "${BASH_SOURCE[0]}")/postgres-endpoint.sh"
    postgres_capacity_assert_policy_target "$policy" "$compose" >&2 || return
    # pooler 자체의 별도 Quadlet/systemd 원천도 config 게시 전에 차단한다.
    postgres_quadlet_environment "$units/holo-pgbouncer.container" "$compose" \
        "$units/holo-pgbouncer.container" >/dev/null || return
    while IFS='|' read -r owner service key source instances maximum role; do
        [[ -n "$source" && "$owner" != \#* && "$owner" != @* ]] || continue
        unit="$units/$service.container"
        [[ "$service" != "$replacement_service" ]] || unit="$replacement_unit"
        values="$(postgres_quadlet_environment "$unit" "$compose" "$units/$service.container" "$key")" || return
        printf '%s=%s\n' "$source" "${values#*=}"
    done < "$policy"
}

postgres_capacity_write_quadlet_config() (
    local repo="$1" compose="$2" units="$3" output="$4" temporary
    temporary="$(mktemp)" || return
    trap 'rm -f -- "$temporary"' EXIT
    postgres_capacity_effective_env "$repo/scripts/ci/postgres-capacity-policy.tsv" "$compose" "$units" \
        "${5:-}" "${6:-}" > "$temporary" || return
    postgres_capacity_write_pgbouncer_config "$repo/scripts/ci/postgres-capacity-policy.tsv" \
        "$temporary" "$output"
)

postgres_capacity_assert_live() (
    local repo="$1" compose="$2" units="$3" temporary expected observed
    temporary="$(mktemp -d)" || return
    trap 'rm -rf -- "$temporary"' EXIT
    postgres_capacity_write_quadlet_config "$repo" "$compose" "$units" "$temporary/pgbouncer.ini" \
        "${4:-}" "${5:-}" || return
    expected="$(sha256sum "$temporary/pgbouncer.ini")" || return
    # 원자 교체된 호스트 경로가 아니라 실행 중 container가 붙잡은 bind inode를 검사한다.
    observed="$(podman exec holo-pgbouncer sha256sum /etc/pgbouncer/pgbouncer.ini)" || {
        echo '[pg-capacity] live pooler configuration inspection failed; activation refused' >&2
        return 1
    }
    [[ "$observed" =~ ^[0-9a-f]{64}[[:space:]]+/etc/pgbouncer/pgbouncer.ini$ &&
        "${observed%% *}" == "${expected%% *}" ]] || {
        echo '[pg-capacity] live pooler configuration differs from effective application capacity; activation refused' >&2
        return 1
    }
)

postgres_pooler_assert_writer_sql() {
    local role result
    case "$1" in
        hololive-api) role=hololive_runtime ;;
        hololive-alarm-worker) role=hololive_alarm_worker ;;
        *) echo '[pg-pooler] unknown central writer' >&2; return 1 ;;
    esac
    # 최초 auth_query는 migration이 만든다. 두 writer는 그 뒤 실제 SCRAM·TLS SQL을 확인할 때까지 정지 상태다.
    # 비밀번호는 이미 입력을 가진 PG 컨테이너 안에서만 libpq에 전달한다.
    result="$(podman exec -i holo-postgres timeout -k 2 38 sh -ec '
        unset PGSERVICE PGSERVICEFILE PGHOSTADDR PGOPTIONS
        export PGHOST=holo-pgbouncer PGPORT=6432 PGDATABASE=hololive PGUSER="$1"
        export PGSSLMODE=verify-full PGSSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem
        export PGGSSENCMODE=disable PGREQUIREAUTH=scram-sha-256 PGCONNECT_TIMEOUT=5
        export PGAPPNAME=hololive-pooler-cutover-check
        case "$PGUSER" in
            hololive_runtime) PGPASSWORD="${HOLOLIVE_DB_PASSWORD:?}" ;;
            hololive_alarm_worker) PGPASSWORD="${HOLOLIVE_ALARM_WORKER_PASSWORD:?}" ;;
        esac
        export PGPASSWORD
        exec psql -XqwAt -v ON_ERROR_STOP=1 -v role="$PGUSER"
    ' sh "$role" <<'SQL'
BEGIN READ ONLY;
SET LOCAL default_transaction_read_only = on;
SET LOCAL statement_timeout = '30s';
SET LOCAL lock_timeout = '3s';
SELECT current_database() = 'hololive' AND current_user = :'role' AND session_user = :'role'
   AND current_setting('default_transaction_read_only') = 'on' AND current_setting('transaction_read_only') = 'on'
   AND current_setting('statement_timeout') = '30s' AND current_setting('lock_timeout') = '3s'
   AND inet_client_addr() = inet '172.23.255.254'
   AND EXISTS (SELECT 1 FROM pg_catalog.pg_stat_ssl WHERE pid = pg_backend_pid() AND ssl AND version IN ('TLSv1.2','TLSv1.3'));
ROLLBACK;
SQL
    )" && [[ "$result" == t ]] || {
        echo "[pg-pooler] pooled SQL verification failed for $role; activation refused" >&2
        return 1
    }
    printf '[pg-pooler] pooled read-only SQL verified for %s\n' "$role"
}

postgres_capacity_assert_service() (
    local repo="$1" compose="$2" units="$3" service="$4" unit="$5" endpoint temporary
    . "$(dirname "${BASH_SOURCE[0]}")/compose-env.sh"
    . "$(dirname "${BASH_SOURCE[0]}")/postgres-endpoint.sh"
    endpoint="$(postgres_endpoint_quadlet "$unit" "$compose" "$units/$service.container")" || return
    case "$endpoint" in
        'holo-pgbouncer|6432')
            postgres_capacity_assert_live "$repo" "$compose" "$units" "$service" "$unit"
            ;;
        'holo-postgres|5432')
            temporary="$(mktemp -d)" || return
            trap 'rm -rf -- "$temporary"' EXIT
            postgres_capacity_write_quadlet_config "$repo" "$compose" "$units" "$temporary/pgbouncer.ini" "$service" "$unit"
            ;;
        *)
            echo '[pg-capacity] unreviewed application PostgreSQL endpoint; activation refused' >&2
            return 1
            ;;
    esac
)
