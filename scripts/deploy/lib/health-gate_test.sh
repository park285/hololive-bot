#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/health-gate.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

check_app_service() {
    local service="$1"
    if cutover_service_uses_app_writable_bind_mount "${service}"; then
        pass "${service} uses writable app bind mounts"
    else
        record_fail "${service} should require writable app bind mount preflight"
    fi
}

for service in hololive-api hololive-alarm-worker youtube-producer youtube-producer-c admin-dashboard; do
    check_app_service "${service}"
done

if cutover_service_uses_app_writable_bind_mount holo-postgres; then
    record_fail "holo-postgres should not require app bind mount preflight"
else
    pass "holo-postgres does not use writable app bind mounts"
fi

mkdir "${tmpdir}/logs" "${tmpdir}/data"
HOLOLIVE_APP_UID="$(id -u)"
HOLOLIVE_APP_GID="$(id -g)"
if cutover_bind_mount_preflight "${tmpdir}"; then
    pass "current user-owned writable bind dirs pass"
else
    record_fail "current user-owned writable bind dirs should pass"
fi

chmod 0500 "${tmpdir}/logs"
if HOLOLIVE_APP_UID=65534 HOLOLIVE_APP_GID=65534 cutover_bind_mount_preflight "${tmpdir}" >/dev/null 2>"${tmpdir}/err"; then
    record_fail "unwritable bind dir should fail"
else
    if grep -Fq "not writable by app uid=65534 gid=65534" "${tmpdir}/err"; then
        pass "unwritable bind dir fails with uid/gid diagnostic"
    else
        record_fail "unwritable bind dir failure did not include uid/gid diagnostic"
    fi
fi

mkdir "${tmpdir}/fakebin"
cat >"${tmpdir}/fakebin/container-cli" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
    *'{{if .State.Health}}yes{{end}}'*) printf 'yes\n' ;;
    *'{{.State.Status}}'*) printf 'running\n' ;;
    *'{{.RestartCount}}'*) printf '0\n' ;;
    *'{{.State.Health.Status}}'*) printf 'healthy\n' ;;
    *) exit 2 ;;
esac
EOF
chmod +x "${tmpdir}/fakebin/container-cli"

if (
    resolve_count_file="${tmpdir}/resolve-count"
    printf '0\n' >"${resolve_count_file}"
    compose_health_resolve_container() {
        local count
        count="$(<"${resolve_count_file}")"
        count=$((count + 1))
        printf '%s\n' "${count}" >"${resolve_count_file}"
        if (( count > 1 )); then
            printf 'replacement-container\n'
        fi
    }
    sleep() { :; }
    CONTAINER_CLI="${tmpdir}/fakebin/container-cli"
    HEALTH_GATE_TIMEOUT=6
    wait_for_service_health hololive-api
) >"${tmpdir}/replacement.out" 2>"${tmpdir}/replacement.err"; then
    pass "health gate waits through a transient Compose replacement gap"
else
    record_fail "health gate should wait for the replacement container to resolve"
fi

if (
    compose_health_resolve_container() { :; }
    sleep() { :; }
    HEALTH_GATE_TIMEOUT=6
    wait_for_service_health hololive-api
) >"${tmpdir}/missing.out" 2>"${tmpdir}/missing.err"; then
    record_fail "health gate should reject a service whose container never resolves"
elif grep -Fq "no container resolved for hololive-api within 6s" "${tmpdir}/missing.err"; then
    pass "health gate remains fail-closed when no container resolves"
else
    record_fail "missing container failure did not include the bounded timeout diagnostic"
fi

if (( failures > 0 )); then
    echo "health-gate tests failed: ${failures}" >&2
    exit 1
fi

echo "health-gate tests passed"
