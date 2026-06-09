#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d /tmp/compose-env-test.XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

# fake docker: `docker inspect holo-postgres ...` 가 FAKE_PG_NETWORK 값을 반환한다.
# 값이 비어 있으면 컨테이너 미존재로 간주해 non-zero로 종료한다.
FAKEBIN="${TMP_DIR}/bin"
mkdir -p "${FAKEBIN}"
cat >"${FAKEBIN}/docker" <<'EOF'
#!/usr/bin/env bash
set -u
if [ "${1:-}" = "inspect" ]; then
  if [ -n "${FAKE_PG_NETWORK:-}" ]; then
    printf '%s\n' "${FAKE_PG_NETWORK}"
    exit 0
  fi
  exit 1
fi
exit 0
EOF
chmod +x "${FAKEBIN}/docker"
export PATH="${FAKEBIN}:${PATH}"

# shellcheck source=/dev/null
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"

PROD="deploy/compose/docker-compose.prod.yml"
LIVE_COMPAT="deploy/compose/docker-compose.live-compat.yml"

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

if [ "${failures}" -ne 0 ]; then
  echo "compose-env guard: ${failures} failure(s)" >&2
  exit 1
fi
echo "compose-env guard: all checks passed"
