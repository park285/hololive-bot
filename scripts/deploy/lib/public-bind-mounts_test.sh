#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/public-bind-mounts.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

mkdir -p "${tmpdir}/deploy/nginx"
config="${tmpdir}/deploy/nginx/admin-dashboard-ingress.conf"
printf 'events {}\n' >"${config}"
chmod 0600 "${config}"

prepare_admin_dashboard_ingress_bind_mount "${tmpdir}"
[[ "$(stat -c '%a' "${config}")" == "644" ]]
echo "[PASS] public ingress bind source is normalized to 0644"

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
