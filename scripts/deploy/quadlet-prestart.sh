#!/usr/bin/env bash
# 중앙 Quadlet container의 ExecStartPre/ExecCondition이다. 설치 도구가 root 소유
# /usr/local/libexec/hololive-bot/quadlet-prestart로 설치하며 lib/는 같은 디렉터리 아래에 둔다.
#
# systemd가 [Service] EnvironmentFile=compose.env로 읽은 값을 그대로 받는다. ExecStart 인자는
# 다른 사용자도 /proc에서 읽을 수 있으므로 비밀번호·API key는 여기서 root 전용 env 파일로 옮기고
# container가 --env-file로만 받게 한다. compose의 ${VAR:?}처럼 반드시 있어야 하는 값도 여기서 확인한다.
set -euo pipefail

self_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/deploy/lib/admin-bind.sh
. "${self_dir}/lib/admin-bind.sh"
# shellcheck source=scripts/deploy/lib/public-bind-mounts.sh
. "${self_dir}/lib/public-bind-mounts.sh"

die() { echo "[quadlet-prestart] $*" >&2; exit 1; }

# 운영(root)에서는 경로를 고정한다. 테스트만 비root로 임시 root 아래 경로를 쓴다.
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    deploy_root=/opt/hololive-bot/compose/current
    env_dir=/run/hololive-bot/quadlet-env
    compose_env=/etc/stack-secrets/hololive-bot/compose.env
    run_dir=/run/hololive-bot
    secrets_dir=/etc/stack-secrets/hololive-bot
    installed_firewall=/etc/nftables.d/admin-dashboard-ingress.nft
else
    test_root="${HOLOLIVE_QUADLET_TEST_ROOT:?non-root execution is only for tests and requires HOLOLIVE_QUADLET_TEST_ROOT}"
    deploy_root="${test_root}/deploy"
    env_dir="${test_root}/run/quadlet-env"
    compose_env="${test_root}/compose.env"
    run_dir="${test_root}/run"
    secrets_dir="${test_root}/secrets"
    installed_firewall="${test_root}/installed.nft"
fi

# compose .env와 systemd EnvironmentFile의 해석이 갈리는 문법을 거부한다. 같은 파일을 두 문법이
# 다르게 읽으면 Docker 시절과 다른 값으로 조용히 기동하기 때문이다(값은 출력하지 않는다).
validate_env_file_syntax() {
    local file="$1"
    [[ -f "$file" && ! -L "$file" && -s "$file" ]] || die "compose env must be a non-empty regular file: ${file}"
    awk '
        /^[[:space:]]*(#|$)/ { next }
        {
            line = $0
            sub(/\r$/, "", line)
            eq = index(line, "=")
            if (eq == 0) { print "[quadlet-prestart] compose env line must be KEY=VALUE: line " NR > "/dev/stderr"; bad = 1; next }
            key = substr(line, 1, eq - 1)
            value = substr(line, eq + 1)
            if (key !~ /^[A-Za-z_][A-Za-z0-9_]*$/) { print "[quadlet-prestart] invalid env key at line " NR > "/dev/stderr"; bad = 1; next }
            if (seen[key]++) { print "[quadlet-prestart] duplicate env key: " key > "/dev/stderr"; bad = 1; next }
            # unit은 [Service] Environment= 기본값 위에 이 파일을 덮는다. KEY= 빈 줄은 compose의 ${KEY:-기본값}과 달리
            # 기본값을 빈 값으로 바꾸므로(예: metrics bind 주소) 줄을 지워 기본값을 쓰게 한다.
            # TODO(kapu): hololive-seoul 수용 뒤 이 줄과 아래 따옴표·$ 검사(compose와 해석 차이)를 삭제한다. 전환 회귀 방지용(빅뱅 계획 F절)
            if (value == "" || value == "\"\"" || value == "\x27\x27") { print "[quadlet-prestart] empty env value; delete the line to use the unit default: " key > "/dev/stderr"; bad = 1; next }
            if (value ~ /^[[:space:]]|[[:space:]]$/) { print "[quadlet-prestart] env value has surrounding whitespace: " key > "/dev/stderr"; bad = 1; next }
            if (value ~ /^\x27[^\x27\\]*\x27$/) next
            if (value ~ /^"[^"\\$`]*"$/) next
            if (value ~ /["\x27\\$`]/ || value ~ / #/) { print "[quadlet-prestart] env value needs compose/systemd-neutral quoting: " key > "/dev/stderr"; bad = 1 }
        }
        END { exit bad }
    ' "$file" || die "compose env syntax is not shared by compose and systemd: ${file}"
}

# bot.env 같은 서비스별 비밀 파일은 [Container] EnvironmentFile=(podman --env-file)로 읽힌다. Podman은 compose env_file과
# 달리 따옴표를 벗기지 않고 ${VAR}도 보간하지 않으며 '='가 없는 줄은 호스트 env를 끌어온다. 그 차이가 값에 그대로
# 섞이지 않도록 두 해석이 같은 줄만 받는다. 메시지에는 key 이름만 남긴다.
# TODO(kapu): hololive-seoul 수용 뒤 따옴표·$·backtick 검사만 삭제한다('=' 없는 줄·중복 key 검사는 유지). 전환 회귀 방지용(빅뱅 계획 F절)
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
            if (seen[key]++) { print "[quadlet-prestart] duplicate env key: " key > "/dev/stderr"; bad = 1; next }
            if (value ~ /^["\x27]|[$`]/) { print "[quadlet-prestart] container env value must be unquoted with no $ or backtick: " key > "/dev/stderr"; bad = 1 }
        }
        END { exit bad }
    ' "$file" || die "container env syntax differs between compose env_file and podman --env-file: ${file}"
}

require_values() {
    local name
    for name in "$@"; do
        [[ -n "${!name:-}" ]] || die "${name} is required in ${compose_env}"
    done
}

# Podman은 publish 주소를 container 기동 때 확인하므로 tailnet 주소가 붙을 때까지 기다린다.
# tailscale ip --assert 같은 검사보다 먼저 불러야 부팅 직후 netmap이 늦을 때 기동이 실패하지 않는다.
wait_for_tailnet_address() {
    local name="$1" address="${!1:-}" attempt family=-4
    # tailnet 주소만 기다린다. loopback·wildcard 같은 값의 허용 여부는 뒤의 bind 검사가 판정한다.
    case "$address" in
        100.*) ;;
        # admin bind는 Tailscale IPv6(fd7a:115c:a1e0::/48)도 허용한다.
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

# 형식: CONTAINER_KEY=HOST_KEY, 끝의 !는 compose의 ${HOST_KEY:?}처럼 비어 있으면 실패한다.
write_secret_env() {
    local container="$1" target tmp spec key source required value
    shift
    install -d -m 0700 -- "$env_dir"
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
    mv -f -- "$tmp" "$target"
}

# 이미지 label이 아니라 배포 트리의 VERSION이 APP_VERSION이다(compose.sh가 HOLO_API_VERSION으로 넘기던 값).
api_release_version() {
    local file="${deploy_root}/hololive/hololive-api/VERSION"
    local -a lines=()
    [[ -f "$file" && ! -L "$file" ]] || die "hololive-api VERSION is missing: ${file}"
    mapfile -t lines < "$file"
    [[ "${#lines[@]}" -eq 1 && "${lines[0]}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] \
        || die "hololive-api VERSION must be one strict SemVer line"
    printf '%s\n' "${lines[0]}"
}

target="${1:-}"
case "$target" in
    collector-enabled)
        # ExecCondition: 1이면 건너뛰고(unit 실패 아님), 잘못된 값은 255로 실패시킨다.
        case "${HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR:-}" in
            ""|0) exit 0 ;;
            1) echo "[quadlet-prestart] HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR=1; collector c stays stopped" >&2; exit 1 ;;
            *) echo "[quadlet-prestart] HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR must be 0 or 1" >&2; exit 255 ;;
        esac
        ;;
    holo-postgres)
        validate_env_file_syntax "$compose_env"
        require_values POSTGRES_PORT_BIND_IP
        require_secret_file "${secrets_dir}/postgres-tls/server.crt"
        require_secret_file "${secrets_dir}/postgres-tls/server.key"
        wait_for_tailnet_address POSTGRES_PORT_BIND_IP
        admin_postgres_bind_assert "$deploy_root" "$POSTGRES_PORT_BIND_IP"
        admin_network_assert_firewall "${deploy_root}/scripts/systemd/admin-dashboard-ingress.nft" "$installed_firewall"
        write_secret_env holo-postgres \
            'POSTGRES_PASSWORD=DB_PASSWORD!' \
            'POSTGRES_ADMIN_PASSWORD=DB_PASSWORD!' \
            'HOLOLIVE_DB_PASSWORD=HOLOLIVE_DB_PASSWORD!' \
            'HOLOLIVE_MIGRATOR_PASSWORD=HOLOLIVE_MIGRATOR_PASSWORD' \
            'HOLOLIVE_SCRAPER_PASSWORD=HOLOLIVE_SCRAPER_PASSWORD!' \
            'HOLOLIVE_REPLICATOR_PASSWORD=HOLOLIVE_REPLICATOR_PASSWORD'
        ;;
    valkey-cache)
        validate_env_file_syntax "$compose_env"
        require_values VALKEY_BIND_IP
        wait_for_tailnet_address VALKEY_BIND_IP
        ;;
    hololive-db-migrate)
        validate_env_file_syntax "$compose_env"
        write_secret_env hololive-db-migrate \
            'PGPASSWORD=HOLOLIVE_MIGRATOR_PASSWORD' \
            'POSTGRES_ADMIN_PASSWORD=DB_PASSWORD!' \
            'HOLOLIVE_SCRAPER_PASSWORD=HOLOLIVE_SCRAPER_PASSWORD!'
        ;;
    hololive-alarm-worker)
        validate_env_file_syntax "$compose_env"
        require_values LIVE_LOGS_PATH VALKEY_SENTINEL_ADDRS
        validate_container_env_file "${secrets_dir}/alarm-worker.env"
        wait_for_tailnet_address HOLOLIVE_METRICS_PORT_BIND_IP
        write_secret_env hololive-alarm-worker \
            'POSTGRES_PASSWORD=HOLOLIVE_DB_PASSWORD!' \
            'HOLODEX_API_KEY=HOLODEX_API_KEY'
        ;;
    hololive-api)
        validate_env_file_syntax "$compose_env"
        require_values LIVE_LOGS_PATH VALKEY_SENTINEL_ADDRS HOLOLIVE_OTLP_GRPC_ENDPOINT \
            HOLOLIVE_API_PORT_BIND_IP HOLOLIVE_ADMIN_API_PORT_BIND_IP
        validate_container_env_file "${secrets_dir}/bot.env"
        wait_for_tailnet_address HOLOLIVE_API_PORT_BIND_IP
        wait_for_tailnet_address HOLOLIVE_ADMIN_API_PORT_BIND_IP
        wait_for_tailnet_address HOLOLIVE_METRICS_PORT_BIND_IP
        admin_bind_assert_host "$HOLOLIVE_ADMIN_API_PORT_BIND_IP"
        admin_network_assert_firewall "${deploy_root}/scripts/systemd/admin-dashboard-ingress.nft" "$installed_firewall"
        APP_VERSION_VALUE="$(api_release_version)"
        export APP_VERSION_VALUE
        write_secret_env hololive-api \
            'APP_VERSION=APP_VERSION_VALUE!' \
            'POSTGRES_PASSWORD=HOLOLIVE_DB_PASSWORD!' \
            'CLIPROXY_API_KEY=CLIPROXY_API_KEY' \
            'GEMINI_API_KEY=GEMINI_API_KEY' \
            'HOLODEX_API_KEY=HOLODEX_API_KEY'
        ;;
    hololive-youtube-collector-c)
        validate_env_file_syntax "$compose_env"
        require_values LIVE_LOGS_PATH
        validate_container_env_file "${secrets_dir}/youtube-collector.env"
        wait_for_tailnet_address HOLOLIVE_METRICS_PORT_BIND_IP
        write_secret_env hololive-youtube-collector-c \
            'POSTGRES_PASSWORD=HOLOLIVE_SCRAPER_PASSWORD!' \
            'HOLODEX_API_KEY=HOLODEX_API_KEY'
        ;;
    admin-dashboard-ingress)
        validate_env_file_syntax "$compose_env"
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
