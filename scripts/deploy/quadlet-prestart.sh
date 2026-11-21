#!/usr/bin/env bash
set -euo pipefail

self_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/deploy/lib/admin-bind.sh
. "${self_dir}/lib/admin-bind.sh"
# shellcheck source=scripts/deploy/lib/public-bind-mounts.sh
. "${self_dir}/lib/public-bind-mounts.sh"
# shellcheck source=scripts/deploy/lib/postgres-capacity.sh
. "${self_dir}/lib/postgres-capacity.sh"

# shellcheck source=scripts/deploy/lib/compose-env.sh
. "${self_dir}/lib/compose-env.sh"

die() { echo "[quadlet-prestart] $*" >&2; exit 1; }

if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    deploy_root=/opt/hololive-bot/compose/current
    env_dir=/run/hololive-bot/quadlet-env
    compose_env=/etc/stack-secrets/hololive-bot/compose.env
    run_dir=/run/hololive-bot
    secrets_dir=/etc/stack-secrets/hololive-bot
    installed_firewall=/etc/nftables.d/admin-dashboard-ingress.nft
    quadlet_dir=/etc/containers/systemd/hololive-bot
else
    test_root="${HOLOLIVE_QUADLET_TEST_ROOT:?non-root execution is only for tests and requires HOLOLIVE_QUADLET_TEST_ROOT}"
    deploy_root="${test_root}/deploy"
    env_dir="${test_root}/run/quadlet-env"
    compose_env="${test_root}/compose.env"
    run_dir="${test_root}/run"
    secrets_dir="${test_root}/secrets"
    installed_firewall="${test_root}/installed.nft"
    quadlet_dir="${test_root}/units"
fi
source_root="$deploy_root"
compose_root="${deploy_root%/*}"
if (( EUID != 0 )); then compose_root="$test_root/opt/hololive-bot/compose"; fi
if [[ "$self_dir" == "$compose_root/app-sources/"* ]]; then
    source_root="${self_dir%/scripts/deploy}"
    sha="${source_root##*/}"
    [[ "$sha" =~ ^[0-9a-f]{40}$ && "$self_dir" == "$compose_root/app-sources/$sha/scripts/deploy" ]] &&
        cmp -s "$source_root/.source-revision" <(printf '%s\n' "$sha") || die "invalid pinned app prestart"
fi

render_env=false
app_version_override=
target="${1:-}"
if [[ "$target" == --render-env ]]; then
    [[ "$#" -eq 3 || "$#" -eq 5 ]] \
        || die "usage: --render-env <unit-name> <private-output-directory> [--app-version <version>]"
    render_env=true
    target="$2"
    case "$target" in
        hololive-api|hololive-alarm-worker|hololive-collector-c) ;;
        *) die "unsupported render-env unit" ;;
    esac
    if [[ "$#" -eq 5 ]]; then
        [[ "$4" == --app-version && "$target" == hololive-api ]] \
            || die "--app-version is only supported for hololive-api"
        [[ "$5" =~ ^[0-9A-Za-z.+-]+$ ]] || die "retained APP_VERSION is not safe for a Quadlet drop-in"
        app_version_override="$5"
    fi
    [[ -d "$3" && ! -L "$3" && -O "$3" ]] \
        || die "render-env output must be a caller-owned private directory"
    [[ "$(stat -c '%a' -- "$3")" == 700 ]] \
        || die "render-env output directory mode must be 0700"
    env_dir="$(cd -- "$3" && pwd -P)"
    case "$env_dir/" in
        /run/hololive-bot/*|"$run_dir/"*) die "render-env output must not use the runtime directory" ;;
    esac
    if ! (shopt -s nullglob dotglob; entries=("$env_dir/"*); [[ "${#entries[@]}" -eq 0 ]]); then
        die "render-env output directory must be empty"
    fi
fi

validate_container_env_file() {
    local file="$1"
    [[ -f "$file" && ! -L "$file" && -s "$file" ]] || die "secret file is missing or empty: ${file}"
    awk '
        /^[[:space:]]*(#|$)/ { next }
        {
            line = $0
            sub(/\r$/, "", line)
            eq = index(line, "=")
            if (eq == 0) { print "[quadlet-prestart] container env line must be KEY=VALUE: line " NR > "/dev/stderr"; bad = 1; next }
            key = substr(line, 1, eq - 1)
            value = substr(line, eq + 1)
            if (key !~ /^[A-Za-z_][A-Za-z0-9_]*$/) { print "[quadlet-prestart] invalid env key at line " NR > "/dev/stderr"; bad = 1; next }
            if (seen[key]++) { print "[quadlet-prestart] duplicate env key: " key > "/dev/stderr"; bad = 1; next }        }
        END { exit bad }
    ' "$file" || die "container env file is invalid: ${file}"
}

require_values() {
    local name
    for name in "$@"; do
        [[ -n "${!name:-}" ]] || die "${name} is required in ${compose_env}"
    done
}

wait_for_tailnet_address() {
    local name="$1" address="${!1:-}" attempt family=-4
    case "$address" in
        100.*) ;;
        fd7a:115c:a1e0:*) family=-6 ;;
        *) return 0 ;;
    esac
    for ((attempt = 0; attempt < 90; attempt++)); do
        if ip "$family" -o addr show dev tailscale0 2>/dev/null | grep -qF " ${address}/"; then
            return 0
        fi
        sleep 1
    done
    die "tailscale0 does not carry ${name}=${address} after 90s"
}

require_secret_file() {
    local path="$1"
    [[ -f "$path" && -s "$path" ]] || die "secret file is missing or empty: ${path}"
}

write_secret_env() {
    local container="$1" target tmp spec key source required value
    shift
    if [[ "$render_env" == false ]]; then
        install -d -m 0700 -- "$env_dir"
    fi
    target="${env_dir}/${container}.env"
    tmp="$(mktemp "${env_dir}/.${container}.env.XXXXXX")"
    chmod 0600 -- "$tmp"
    for spec in "$@"; do
        key="${spec%%=*}"
        source="${spec#*=}"
        required=false
        if [[ "$source" == *! ]]; then
            required=true
            source="${source%!}"
        fi
        value="${!source:-}"
        if [[ "$required" == true && -z "$value" ]] || [[ "$value" == *$'\n'* ]]; then
            rm -f -- "$tmp"
            die "${source} must be a non-empty single line in ${compose_env}"
        fi
        printf '%s=%s\n' "$key" "$value" >> "$tmp"
    done
    if [[ "$render_env" == true ]]; then
        if ! ln -T -- "$tmp" "$target"; then
            rm -f -- "$tmp"
            die "render-env output already exists or cannot be published"
        fi
        rm -f -- "$tmp"
    else
        mv -f -- "$tmp" "$target"
    fi
}

api_release_version() {
    local file="${source_root}/hololive/hololive-api/VERSION"
    local -a lines=()
    [[ -f "$file" && ! -L "$file" ]] || die "hololive-api VERSION is missing: ${file}"
    mapfile -t lines < "$file"
    [[ "${#lines[@]}" -eq 1 && "${lines[0]}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] \
        || die "hololive-api VERSION must be one strict SemVer line"
    printf '%s\n' "${lines[0]}"
}

case "$target" in
    hololive-api|hololive-alarm-worker|hololive-collector-c)
        compose_env_validate_file_format "$compose_env" || die "compose env file is invalid: ${compose_env}"
        if [[ "$render_env" != true ]]; then
            postgres_capacity_assert_service "$source_root" "$compose_env" "$quadlet_dir" "$target" "$quadlet_dir/$target.container"
        fi
        ;;
esac
case "$target" in
    collector-enabled)
        case "${HOLOLIVE_DISABLE_COLLECTOR:-}" in
            ""|0) exit 0 ;;
            1) echo "[quadlet-prestart] HOLOLIVE_DISABLE_COLLECTOR=1; collector c stays stopped" >&2; exit 1 ;;
            *) echo "[quadlet-prestart] HOLOLIVE_DISABLE_COLLECTOR must be 0 or 1" >&2; exit 255 ;;
        esac
        ;;
    holo-postgres)
        compose_env_validate_file_format "$compose_env" || die "compose env file is invalid: ${compose_env}"
        require_values POSTGRES_PORT_BIND_IP
        require_secret_file "${secrets_dir}/postgres-tls/server.crt"
        require_secret_file "${secrets_dir}/postgres-tls/server.key"
        require_secret_file "${secrets_dir}/certs/postgres-ca.pem"
        wait_for_tailnet_address POSTGRES_PORT_BIND_IP
        admin_postgres_bind_assert "$deploy_root" "$POSTGRES_PORT_BIND_IP"
        admin_network_assert_firewall "${deploy_root}/scripts/systemd/admin-dashboard-ingress.nft" "$installed_firewall"
        write_secret_env holo-postgres \
            'POSTGRES_PASSWORD=DB_PASSWORD!' \
            'POSTGRES_ADMIN_PASSWORD=DB_PASSWORD!' \
            'HOLOLIVE_DB_PASSWORD=HOLOLIVE_DB_PASSWORD!' \
            'HOLOLIVE_ALARM_WORKER_PASSWORD=HOLOLIVE_ALARM_WORKER_PASSWORD!' \
            'HOLOLIVE_MIGRATOR_PASSWORD=HOLOLIVE_MIGRATOR_PASSWORD' \
            'HOLOLIVE_COLLECTOR_A_PASSWORD=HOLOLIVE_COLLECTOR_A_PASSWORD!' \
            'HOLOLIVE_COLLECTOR_B_PASSWORD=HOLOLIVE_COLLECTOR_B_PASSWORD!' \
            'HOLOLIVE_COLLECTOR_C_PASSWORD=HOLOLIVE_COLLECTOR_C_PASSWORD!' \
            'HOLOLIVE_COLLECTOR_D_PASSWORD=HOLOLIVE_COLLECTOR_D_PASSWORD!' \
            'HOLOLIVE_REPLICATOR_PASSWORD=HOLOLIVE_REPLICATOR_PASSWORD'
        ;;
    holo-pgbouncer)
        compose_env_validate_file_format "$compose_env" || die "compose env file is invalid: ${compose_env}"
        require_values POSTGRES_PORT_BIND_IP
        require_secret_file "${secrets_dir}/certs/postgres-ca.pem"
        for tls_file in server.crt server.key backend-client.crt backend-client.key; do
            require_secret_file "${secrets_dir}/pgbouncer/${tls_file}"
        done
        wait_for_tailnet_address POSTGRES_PORT_BIND_IP
        admin_postgres_bind_assert "$deploy_root" "$POSTGRES_PORT_BIND_IP"
        admin_network_assert_firewall "${deploy_root}/scripts/systemd/admin-dashboard-ingress.nft" "$installed_firewall"
        install -d -m 0755 -- "${run_dir}/pgbouncer"
        postgres_capacity_write_quadlet_config "$deploy_root" "$compose_env" "$quadlet_dir" \
            "${run_dir}/pgbouncer/pgbouncer.ini"
        ;;
    valkey-cache)
        compose_env_validate_file_format "$compose_env" || die "compose env file is invalid: ${compose_env}"
        require_values VALKEY_BIND_IP
        wait_for_tailnet_address VALKEY_BIND_IP
        ;;
    hololive-db-migrate)
        compose_env_validate_file_format "$compose_env" || die "compose env file is invalid: ${compose_env}"
        case "${2:-}" in
            pool-auth) write_secret_env hololive-db-migrate 'POSTGRES_ADMIN_PASSWORD=DB_PASSWORD!' ;;
            schema) write_secret_env hololive-db-migrate 'PGPASSWORD=HOLOLIVE_MIGRATOR_PASSWORD!' ;;
            *) die "hololive-db-migrate requires pool-auth or schema" ;;
        esac
        ;;
    hololive-alarm-worker)
        require_values LIVE_LOGS_PATH VALKEY_SENTINEL_ADDRS
        validate_container_env_file "${secrets_dir}/alarm-worker.env"
        if [[ "$render_env" == false ]]; then
            wait_for_tailnet_address HOLOLIVE_METRICS_PORT_BIND_IP
        fi
        write_secret_env hololive-alarm-worker \
            'POSTGRES_PASSWORD=HOLOLIVE_ALARM_WORKER_PASSWORD!' \
            'HOLODEX_API_KEY=HOLODEX_API_KEY'
        ;;
    hololive-api)
        require_values LIVE_LOGS_PATH VALKEY_SENTINEL_ADDRS \
            HOLOLIVE_API_PORT_BIND_IP HOLOLIVE_ADMIN_API_PORT_BIND_IP HOLOLIVE_H3_SERVER_NAME
        validate_container_env_file "${secrets_dir}/bot.env"
        if [[ "$render_env" == false ]]; then
            wait_for_tailnet_address HOLOLIVE_API_PORT_BIND_IP
            wait_for_tailnet_address HOLOLIVE_ADMIN_API_PORT_BIND_IP
            wait_for_tailnet_address HOLOLIVE_METRICS_PORT_BIND_IP
            admin_bind_assert_host "$HOLOLIVE_ADMIN_API_PORT_BIND_IP"
            admin_network_assert_firewall "${source_root}/scripts/systemd/admin-dashboard-ingress.nft" "$installed_firewall"
        fi
        if [[ -n "$app_version_override" ]]; then
            APP_VERSION_VALUE="$app_version_override"
        else
            APP_VERSION_VALUE="$(api_release_version)"
        fi
        export APP_VERSION_VALUE
        write_secret_env hololive-api \
            'APP_VERSION=APP_VERSION_VALUE!' \
            'POSTGRES_PASSWORD=HOLOLIVE_DB_PASSWORD!' \
            'HOLODEX_API_KEY=HOLODEX_API_KEY'
        ;;
    hololive-collector-c)
        require_values LIVE_LOGS_PATH
        validate_container_env_file "${secrets_dir}/collector.env"
        if [[ "$render_env" == false ]]; then
            wait_for_tailnet_address HOLOLIVE_METRICS_PORT_BIND_IP
        fi
        write_secret_env hololive-collector-c \
            'POSTGRES_PASSWORD=HOLOLIVE_COLLECTOR_C_PASSWORD!' \
            'HOLODEX_API_KEY=HOLODEX_API_KEY'
        ;;
    admin-dashboard-ingress)
        compose_env_validate_file_format "$compose_env" || die "compose env file is invalid: ${compose_env}"
        require_values HOLOLIVE_BOT_PORT_BIND_IP
        admin_network_assert_firewall "${deploy_root}/scripts/systemd/admin-dashboard-ingress.nft" "$installed_firewall"
        wait_for_tailnet_address HOLOLIVE_BOT_PORT_BIND_IP
        install -d -m 0755 -- "$run_dir"
        HOLOLIVE_INGRESS_CONF="${run_dir}/admin-dashboard-ingress.conf" \
            prepare_admin_dashboard_ingress_bind_mount "$deploy_root"
        ;;
    *)
        die "unsupported container: ${target:-<none>}"
        ;;
esac
