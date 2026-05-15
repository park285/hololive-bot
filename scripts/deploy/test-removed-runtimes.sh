#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

pass() {
    echo "[PASS] $*"
}

cat >"${tmpdir}/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail

echo "$*" >>"${MOCK_DOCKER_LOG}"
case "$1" in
  ps)
    if [[ "${MOCK_DOCKER_HAS_DISPATCHER:-0}" == "1" ]]; then
      printf '%s\n' "mock-container-id"
    fi
    ;;
  stop)
    exit "${MOCK_DOCKER_STOP_EXIT:-0}"
    ;;
  rm)
    ;;
esac
MOCK
chmod +x "${tmpdir}/docker"

export PATH="${tmpdir}:${PATH}"
export CONTAINER_CLI=docker
export MOCK_DOCKER_LOG="${tmpdir}/docker.log"

MOCK_DOCKER_HAS_DISPATCHER=0 removed_runtime_cleanup_standalone_dispatcher
if rg -q 'stop|rm -f' "${MOCK_DOCKER_LOG}" 2>/dev/null; then
    fail "cleanup issued stop/rm when dispatcher container is absent"
fi
pass "absent dispatcher cleanup is no-op"

: >"${MOCK_DOCKER_LOG}"
MOCK_DOCKER_HAS_DISPATCHER=1 MOCK_DOCKER_STOP_EXIT=1 removed_runtime_cleanup_standalone_dispatcher

if ! rg -q 'ps -aq --filter name=\^hololive-dispatcher-go\$' "${MOCK_DOCKER_LOG}"; then
    fail "cleanup did not query exact dispatcher container name"
fi
if ! rg -q '^stop hololive-dispatcher-go$' "${MOCK_DOCKER_LOG}"; then
    fail "cleanup did not stop dispatcher container"
fi
if ! rg -q '^rm -f hololive-dispatcher-go$' "${MOCK_DOCKER_LOG}"; then
    fail "cleanup did not remove dispatcher container"
fi
pass "present dispatcher cleanup stops and removes container"
