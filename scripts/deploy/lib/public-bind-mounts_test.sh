#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/public-bind-mounts.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

mkdir -p "${tmpdir}/deploy/nginx"
template="${tmpdir}/deploy/nginx/admin-dashboard-ingress.conf.template"
printf 'events {}\nhttp { server { listen @BIND_IP@:30191; allow @BIND_IP@; } }\n' >"${template}"

config="${tmpdir}/rendered/admin-dashboard-ingress.conf"
export HOLOLIVE_INGRESS_CONF="${config}"
export HOLOLIVE_BOT_PORT_BIND_IP="100.100.1.9"
unset COMPOSE_ENV_FILE

prepare_admin_dashboard_ingress_bind_mount "${tmpdir}"
[[ "$(stat -c '%a' "${config}")" == "644" ]]
grep -q 'listen 100.100.1.9:30191;' "${config}"
grep -q 'allow 100.100.1.9;' "${config}"
! grep -q '@BIND_IP@' "${config}"
echo "[PASS] public ingress config is rendered from the template at the configured bind IP"

printf 'stale\n' >"${config}"
export HOLOLIVE_BOT_PORT_BIND_IP="100.100.1.7"
prepare_admin_dashboard_ingress_bind_mount "${tmpdir}"
grep -q 'listen 100.100.1.7:30191;' "${config}"
! grep -q 'stale' "${config}"
echo "[PASS] re-render replaces a stale config instead of appending"

target="${tmpdir}/target"
printf 'unchanged\n' >"${target}"
rm -f "${config}"
ln -s "${target}" "${config}"
if prepare_admin_dashboard_ingress_bind_mount "${tmpdir}" >/dev/null 2>&1; then
    echo "[FAIL] symlinked public ingress config was accepted" >&2
    exit 1
fi
[[ "$(cat "${target}")" == "unchanged" ]]
echo "[PASS] symlinked public ingress config fails closed"

rm -f "${config}"
rm -f "${template}"
ln -s "${target}" "${template}"
if prepare_admin_dashboard_ingress_bind_mount "${tmpdir}" >/dev/null 2>&1; then
    echo "[FAIL] symlinked public ingress template was accepted" >&2
    exit 1
fi
echo "[PASS] symlinked public ingress template fails closed"

rm -f "${template}"
printf 'events {}\nhttp { server { listen @BIND_IP@:30191; } }\n' >"${template}"
unset HOLOLIVE_BOT_PORT_BIND_IP
if prepare_admin_dashboard_ingress_bind_mount "${tmpdir}" >/dev/null 2>&1; then
    echo "[FAIL] missing bind IP was accepted" >&2
    exit 1
fi
echo "[PASS] missing bind IP fails closed"

export HOLOLIVE_BOT_PORT_BIND_IP='127.0.0.1; include /tmp/injected.conf; #'
if prepare_admin_dashboard_ingress_bind_mount "${tmpdir}" >/dev/null 2>&1; then
    echo "[FAIL] config syntax in the bind IP was accepted" >&2
    exit 1
fi
echo "[PASS] non-literal bind IP fails closed before template rendering"

env_file="${tmpdir}/compose.env"
printf 'HOLOLIVE_BOT_PORT_BIND_IP=100.100.1.7\n' >"${env_file}"
unset HOLOLIVE_BOT_PORT_BIND_IP
export COMPOSE_ENV_FILE="${env_file}"
prepare_admin_dashboard_ingress_bind_mount "${tmpdir}"
grep -q 'listen 100.100.1.7:30191;' "${config}"
echo "[PASS] bind IP falls back to COMPOSE_ENV_FILE"

echo "public ingress bind mount checks passed"
