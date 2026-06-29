#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d /tmp/compose-env-test.XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

# fake docker: `docker inspect holo-postgres` 의 --format 에 HostIp 가 있으면 published-port
# 조회로 보고 FAKE_PG_PUBLISHED_IPS 를, 아니면 network-mode 조회로 보고 FAKE_PG_NETWORK 를 반환한다.
# 둘 다 비어 있으면 컨테이너 미존재로 간주해 non-zero로 종료한다.
FAKEBIN="${TMP_DIR}/bin"
mkdir -p "${FAKEBIN}"
cat >"${FAKEBIN}/docker" <<'EOF'
#!/usr/bin/env bash
set -u
if [ "${1:-}" = "inspect" ]; then
  case "$*" in
    *HostIp*)
      [ -n "${FAKE_PG_PUBLISHED_IPS:-}" ] && printf '%s\n' "${FAKE_PG_PUBLISHED_IPS}"
      [ -n "${FAKE_PG_NETWORK:-}" ] || exit 1
      exit 0
      ;;
    *)
      if [ -n "${FAKE_PG_NETWORK:-}" ]; then
        printf '%s\n' "${FAKE_PG_NETWORK}"
        exit 0
      fi
      exit 1
      ;;
  esac
fi
exit 0
EOF
chmod +x "${FAKEBIN}/docker"
export PATH="${FAKEBIN}:${PATH}"

# shellcheck source=/dev/null
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"

PROD="deploy/compose/docker-compose.prod.yml"
LIVE_COMPAT="deploy/compose/docker-compose.live-compat.yml"
MAIN_AP_LIVE_COMPAT="deploy/compose/docker-compose.main-ap.live-compat.yml"

run_guard() {
  # exit 1 을 부모로 전파하지 않도록 subshell 로 격리한다.
  ( compose_env_assert_live_compat_for_host_networked_postgres "$@" ) 2>/dev/null
}

if FAKE_PG_NETWORK=host run_guard "${PROD}"; then
  record_fail "host-networked postgres without live-compat should be rejected"
else
  pass "host-networked postgres without live-compat rejected"
fi

if FAKE_PG_NETWORK=host run_guard "${PROD}" "${LIVE_COMPAT}"; then
  pass "host-networked postgres with live-compat allowed"
else
  record_fail "host-networked postgres with live-compat should be allowed"
fi

if FAKE_PG_NETWORK=host run_guard "${PROD}" "${MAIN_AP_LIVE_COMPAT}"; then
  record_fail "main-ap.live-compat alone (no host-postgres overlay) should be rejected"
else
  pass "main-ap.live-compat alone rejected"
fi

if FAKE_PG_NETWORK=hololive-bot_hololive-net run_guard "${PROD}"; then
  pass "bridge-networked postgres allowed without overlay"
else
  record_fail "bridge-networked postgres should be allowed"
fi

if FAKE_PG_NETWORK= run_guard "${PROD}"; then
  pass "no running postgres allowed"
else
  record_fail "no running postgres should be allowed"
fi

if FAKE_PG_NETWORK=host ALLOW_POSTGRES_TOPOLOGY_CHANGE=true run_guard "${PROD}"; then
  pass "explicit ALLOW_POSTGRES_TOPOLOGY_CHANGE opt-out allowed"
else
  record_fail "ALLOW_POSTGRES_TOPOLOGY_CHANGE=true should allow"
fi

if FAKE_PG_NETWORK=hololive-bot_hololive-net FAKE_PG_PUBLISHED_IPS=100.100.1.3 run_guard "${PROD}"; then
  record_fail "bridge postgres published on routable IP without live-compat should be rejected"
else
  pass "bridge postgres published on routable IP without live-compat rejected"
fi

if FAKE_PG_NETWORK=hololive-bot_hololive-net FAKE_PG_PUBLISHED_IPS=100.100.1.3 run_guard "${PROD}" "${LIVE_COMPAT}"; then
  pass "bridge postgres published on routable IP with live-compat allowed"
else
  record_fail "bridge postgres published on routable IP with live-compat should be allowed"
fi

if FAKE_PG_NETWORK=hololive-bot_hololive-net FAKE_PG_PUBLISHED_IPS=127.0.0.1 run_guard "${PROD}"; then
  pass "bridge postgres published on loopback only allowed without overlay"
else
  record_fail "bridge postgres published on loopback only should be allowed"
fi

if FAKE_PG_NETWORK=hololive-bot_hololive-net FAKE_PG_PUBLISHED_IPS=100.100.1.3 \
    ALLOW_POSTGRES_TOPOLOGY_CHANGE=true run_guard "${PROD}"; then
  pass "routable-published opt-out via ALLOW_POSTGRES_TOPOLOGY_CHANGE allowed"
else
  record_fail "routable-published ALLOW_POSTGRES_TOPOLOGY_CHANGE=true should allow"
fi

run_dashboard_guard() {
  ( compose_env_assert_admin_dashboard_loopback_bind "$@" ) 2>/dev/null
}

ENV_LOOPBACK="${TMP_DIR}/dashboard-loopback.env"
printf 'ADMIN_DASHBOARD_PORT_BIND_IP=127.0.0.1\n' >"${ENV_LOOPBACK}"
ENV_IPV6_LOOPBACK="${TMP_DIR}/dashboard-ipv6-loopback.env"
printf 'ADMIN_DASHBOARD_PORT_BIND_IP=::1\n' >"${ENV_IPV6_LOOPBACK}"
ENV_ROUTABLE="${TMP_DIR}/dashboard-routable.env"
printf 'ADMIN_DASHBOARD_PORT_BIND_IP=100.100.1.3\n' >"${ENV_ROUTABLE}"
ENV_WILDCARD="${TMP_DIR}/dashboard-wildcard.env"
printf 'ADMIN_DASHBOARD_PORT_BIND_IP=0.0.0.0\n' >"${ENV_WILDCARD}"
ENV_UNSET="${TMP_DIR}/dashboard-unset.env"
printf 'CACHE_PASSWORD=stub\n' >"${ENV_UNSET}"

unset ADMIN_DASHBOARD_PORT_BIND_IP

if run_dashboard_guard "${ENV_LOOPBACK}"; then
  pass "admin-dashboard loopback bind (127.0.0.1) allowed"
else
  record_fail "admin-dashboard loopback bind should be allowed"
fi

if run_dashboard_guard "${ENV_IPV6_LOOPBACK}"; then
  pass "admin-dashboard IPv6 loopback bind (::1) allowed"
else
  record_fail "admin-dashboard IPv6 loopback bind should be allowed"
fi

if run_dashboard_guard "${ENV_UNSET}"; then
  pass "admin-dashboard default (unset) bind allowed"
else
  record_fail "admin-dashboard default bind should be allowed"
fi

if run_dashboard_guard "${ENV_ROUTABLE}"; then
  record_fail "admin-dashboard routable bind (100.100.1.3) should be rejected"
else
  pass "admin-dashboard routable bind rejected"
fi

if run_dashboard_guard "${ENV_WILDCARD}"; then
  record_fail "admin-dashboard wildcard bind (0.0.0.0) should be rejected"
else
  pass "admin-dashboard wildcard bind rejected"
fi

if ADMIN_DASHBOARD_PORT_BIND_IP=100.100.1.3 run_dashboard_guard "${ENV_LOOPBACK}"; then
  record_fail "admin-dashboard shell-env routable override should be rejected"
else
  pass "admin-dashboard shell-env routable override rejected"
fi

if [ "${failures}" -ne 0 ]; then
  echo "compose-env guard: ${failures} failure(s)" >&2
  exit 1
fi
echo "compose-env guard: all checks passed"
