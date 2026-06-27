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
    exit "${MOCK_DOCKER_COMPOSE_EXIT:-0}"
    ;;
  ps)
    filter="${*: -1}"
    name="${filter#name=^}"
    name="${name%\$}"
    for present in ${MOCK_DOCKER_PRESENT_NAMES:-}; do
      if [[ "${present}" == "${name}" ]]; then
        printf '%s\n' "mock-${name}"
        break
      fi
    done
    ;;
  stop)
    exit "${MOCK_DOCKER_STOP_EXIT:-0}"
    ;;
  rm)
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

: >"${MOCK_DOCKER_LOG}"
MOCK_DOCKER_PRESENT_NAMES="" removed_runtime_cleanup_before_cutover
if grep -Eq '^(stop|rm -f) ' "${MOCK_DOCKER_LOG}"; then
    fail "cleanup issued stop/rm when retired containers are absent"
fi
pass "absent retired runtime cleanup is a no-op"

: >"${MOCK_DOCKER_LOG}"
retired_names="$(removed_runtime_container_names | tr '\n' ' ')"
MOCK_DOCKER_PRESENT_NAMES="${retired_names}" MOCK_DOCKER_STOP_EXIT=1 removed_runtime_cleanup_before_cutover
while IFS= read -r name; do
    grep -Fqx "ps -aq --filter name=^${name}$" "${MOCK_DOCKER_LOG}" \
        || fail "cleanup did not query exact retired container name: ${name}"
    grep -Fqx "stop ${name}" "${MOCK_DOCKER_LOG}" \
        || fail "cleanup did not stop retired container: ${name}"
    grep -Fqx "rm -f ${name}" "${MOCK_DOCKER_LOG}" \
        || fail "cleanup did not remove retired container: ${name}"
done < <(removed_runtime_container_names)
pass "all retired runtime containers are stopped and removed"

env_file="${tmpdir}/env"
compose_file="${tmpdir}/docker-compose.yml"
mkdir -p "${tmpdir}/shared-go" "${tmpdir}/iris-client-go"
cat >"${env_file}" <<'EOF'
TEST_VALUE=ok
EOF
cat >"${compose_file}" <<'EOF'
services:
  hololive-api:
    image: example-api
  hololive-alarm-worker:
    image: example-worker
  youtube-producer:
    image: example-producer
EOF

: >"${MOCK_DOCKER_LOG}"
MOCK_DOCKER_PRESENT_NAMES="hololive-kakao-bot-go hololive-admin-api hololive-llm-scheduler hololive-dispatcher-go" \
COMPOSE_ENV_FILE="${env_file}" \
SHARED_GO_WORKSPACE_PATH="${tmpdir}/shared-go" \
IRIS_CLIENT_GO_WORKSPACE_PATH="${tmpdir}/iris-client-go" \
    "${ROOT_DIR}/scripts/deploy/compose.sh" -f "${compose_file}" up -d --build hololive-api

config_line="$(grep -nE '^compose --env-file .* config --quiet$' "${MOCK_DOCKER_LOG}" | cut -d: -f1 | head -n1)"
build_line="$(grep -nE '^compose --env-file .* build --with-dependencies hololive-api$' "${MOCK_DOCKER_LOG}" | cut -d: -f1 | head -n1)"
cleanup_line="$(grep -n '^stop hololive-kakao-bot-go$' "${MOCK_DOCKER_LOG}" | cut -d: -f1 | head -n1)"
up_line="$(grep -nE '^compose --env-file .* up -d hololive-api$' "${MOCK_DOCKER_LOG}" | cut -d: -f1 | head -n1)"

[[ -n "${config_line}" && -n "${build_line}" && -n "${cleanup_line}" && -n "${up_line}" ]] \
    || fail "unified API cutover did not execute render, dependency build, cleanup and up phases"
(( config_line < build_line && build_line < cleanup_line && cleanup_line < up_line )) \
    || fail "unified API cutover order must be render -> build -> cleanup -> up"
if grep -Eq '^compose --env-file .* up .*--build' "${MOCK_DOCKER_LOG}"; then
    fail "final up must not rebuild after retired runtimes have been stopped"
fi
pass "unified API cutover builds dependencies before cleanup and starts last"

: >"${MOCK_DOCKER_LOG}"
unset IRIS_CLIENT_GO_WORKSPACE_PATH
MOCK_DOCKER_PRESENT_NAMES="${retired_names}" \
COMPOSE_ENV_FILE="${env_file}" \
SHARED_GO_WORKSPACE_PATH="${tmpdir}/shared-go" \
    "${ROOT_DIR}/scripts/deploy/compose.sh" -f "${compose_file}" up -d --build youtube-producer

grep -Eq '^compose --env-file .* build --with-dependencies youtube-producer$' "${MOCK_DOCKER_LOG}" \
    || fail "producer-only start did not preserve targeted dependency build"
if grep -Eq '^(stop|rm -f) ' "${MOCK_DOCKER_LOG}"; then
    fail "producer-only start must not stop retired central runtimes"
fi
pass "producer-only AP start neither requires iris-client-go nor triggers central cutover cleanup"

: >"${MOCK_DOCKER_LOG}"
MOCK_DOCKER_PRESENT_NAMES="${retired_names}" \
COMPOSE_ENV_FILE="${env_file}" \
SHARED_GO_WORKSPACE_PATH="${tmpdir}/shared-go" \
    "${ROOT_DIR}/scripts/deploy/compose.sh" -f "${compose_file}" up -d hololive-alarm-worker
if grep -Eq '^(stop|rm -f) ' "${MOCK_DOCKER_LOG}"; then
    fail "alarm-worker-only start must not stop retired API-plane runtimes"
fi
pass "alarm-worker-only start does not trigger API-plane cutover cleanup"
