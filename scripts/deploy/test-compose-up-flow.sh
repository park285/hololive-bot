#!/usr/bin/env bash
set -euo pipefail

# compose.sh up 경로의 단계 순서와 대상별 preflight를 고정한다. 퇴역 runtime 부재 단언(removed-runtimes.sh)과 그 검사는
# DEC-20260926-hololive-retired-rollback-tooling에 따라 stack-audit 2026-09-26 T19(holo-removed-runtimes-cleanup-guard)에서
# 지웠고, 이 파일은 옛 test-removed-runtimes.sh에서 퇴역 runtime과 무관한 up 경로 검사만 옮겼다.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

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
  compose)
    if [[ "${2:-}" == "version" ]]; then
      exit 0
    fi
    _has_ps=false
    _has_q=false
    _last=""
    for _a in "$@"; do
      [[ "${_a}" == ps ]] && _has_ps=true
      [[ "${_a}" == -q ]] && _has_q=true
      _last="${_a}"
    done
    if [[ "${_has_ps}" == true && "${_has_q}" == true ]]; then
      printf 'mock-%s\n' "${_last}"
    fi
    exit 0
    ;;
  inspect)
    _fmt=""
    _prev=""
    for _a in "$@"; do
      [[ "${_prev}" == -f ]] && _fmt="${_a}"
      _prev="${_a}"
    done
    case "${_fmt}" in
      *Health.Status*) printf 'healthy\n' ;;
      *.State.Health*) printf 'yes\n' ;;
      *.State.Status*) printf 'running\n' ;;
      *RestartCount*) printf '0\n' ;;
    esac
    ;;
esac
MOCK
chmod +x "${tmpdir}/docker"

export PATH="${tmpdir}:${PATH}"
export CONTAINER_CLI=docker
export MOCK_DOCKER_LOG="${tmpdir}/docker.log"
export HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1

env_file="${tmpdir}/env"
compose_file="${tmpdir}/docker-compose.yml"
mkdir -p "${tmpdir}/shared-go" "${tmpdir}/iris-client-go"
cat >"${env_file}" <<'EOF'
TEST_VALUE=ok
HOLOLIVE_BOT_PORT_BIND_IP=127.0.0.1
EOF
cat >"${compose_file}" <<'EOF'
services:
  hololive-api:
    image: example-api
  hololive-alarm-worker:
    image: example-worker
  youtube-collector:
    image: example-collector
EOF
mkdir -p "${ROOT_DIR}/logs" "${ROOT_DIR}/data"

: >"${MOCK_DOCKER_LOG}"
COMPOSE_ENV_FILE="${env_file}" \
SHARED_GO_WORKSPACE_PATH="${tmpdir}/shared-go" \
IRIS_CLIENT_GO_WORKSPACE_PATH="${tmpdir}/iris-client-go" \
HOLOLIVE_APP_UID="$(id -u)" \
HOLOLIVE_APP_GID="$(id -g)" \
    "${ROOT_DIR}/scripts/deploy/compose.sh" -f "${compose_file}" up -d --build hololive-api

config_line="$(grep -nE '^compose --env-file .* config --quiet$' "${MOCK_DOCKER_LOG}" | cut -d: -f1 | head -n1)"
build_line="$(grep -nE '^compose --env-file .* build --with-dependencies hololive-api$' "${MOCK_DOCKER_LOG}" | cut -d: -f1 | head -n1)"
up_line="$(grep -nE '^compose --env-file .* up -d hololive-api$' "${MOCK_DOCKER_LOG}" | cut -d: -f1 | head -n1)"

[[ -n "${config_line}" && -n "${build_line}" && -n "${up_line}" ]] \
    || fail "unified API start did not execute render, dependency build and up phases"
(( config_line < build_line && build_line < up_line )) \
    || fail "unified API start order must be render -> build -> up"
if grep -Eq '^(stop|rm -f) ' "${MOCK_DOCKER_LOG}"; then
    fail "unified API start must not stop or remove containers"
fi
if grep -Eq '^compose --env-file .* up .*--build' "${MOCK_DOCKER_LOG}"; then
    fail "final up must not rebuild after the dependency build"
fi
pass "unified API start renders, builds dependencies and starts last without a rebuild"

: >"${MOCK_DOCKER_LOG}"
unset IRIS_CLIENT_GO_WORKSPACE_PATH
COMPOSE_ENV_FILE="${env_file}" \
SHARED_GO_WORKSPACE_PATH="${tmpdir}/shared-go" \
HOLOLIVE_APP_UID="$(id -u)" \
HOLOLIVE_APP_GID="$(id -g)" \
    "${ROOT_DIR}/scripts/deploy/compose.sh" -f "${compose_file}" up -d --build youtube-collector >"${tmpdir}/collector.out" 2>&1

grep -Eq '^compose --env-file .* build --with-dependencies youtube-collector$' "${MOCK_DOCKER_LOG}" \
    || fail "collector-only start did not preserve targeted dependency build"
grep -Fq "[PREFLIGHT] Verifying host bind-mount write access" "${tmpdir}/collector.out" \
    || fail "collector-only start did not run writable bind-mount preflight"
pass "collector-only AP start does not require iris-client-go and runs the bind-mount preflight"

: >"${MOCK_DOCKER_LOG}"
COMPOSE_ENV_FILE="${env_file}" \
SHARED_GO_WORKSPACE_PATH="${tmpdir}/shared-go" \
HOLOLIVE_APP_UID="$(id -u)" \
HOLOLIVE_APP_GID="$(id -g)" \
    "${ROOT_DIR}/scripts/deploy/compose.sh" -f "${compose_file}" up -d hololive-alarm-worker >"${tmpdir}/alarm-worker.out" 2>&1
grep -Fq "[PREFLIGHT] Verifying host bind-mount write access" "${tmpdir}/alarm-worker.out" \
    || fail "alarm-worker-only start did not run writable bind-mount preflight"
pass "alarm-worker-only start runs the bind-mount preflight"
