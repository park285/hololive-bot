#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gate_call='postgres_capacity_assert_target'

for entrypoint in build-all.sh scripts/deploy/compose.sh scripts/deploy/compose-redeploy-service.sh; do
    path="${root}/${entrypoint}"
    grep -Fq 'scripts/deploy/lib/postgres-capacity.sh' "${path}" || {
        echo "${entrypoint} does not source the common PostgreSQL capacity gate" >&2
        exit 1
    }
    gate_line="$(grep -n "${gate_call}" "${path}" | tail -n1 | cut -d: -f1)"
    compose_execution_line="$(grep -n 'config --quiet' "${path}" | tail -n1 | cut -d: -f1)"
    [[ "$(grep -c "${gate_call}" "${path}")" == "1" ]] || {
        echo "${entrypoint} does not invoke the PostgreSQL capacity gate exactly once" >&2
        exit 1
    }
    [[ -n "${gate_line}" && -n "${compose_execution_line}" && "${gate_line}" -lt "${compose_execution_line}" ]] || {
        echo "${entrypoint} can mutate production before the PostgreSQL capacity gate" >&2
        exit 1
    }
done

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
cat >"${tmp}/unsafe.env" <<'ENV'
BOT_POSTGRES_POOL_MAX_CONNS=50
YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=8
ENV
: >"${tmp}/default.env"
mkdir -p "${tmp}/shared-go" "${tmp}/iris-client-go" "${tmp}/bin"
cat >"${tmp}/bin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${MOCK_DOCKER_LOG}"
if [[ "${1:-}" == compose && "${2:-}" == version ]]; then
    exit 0
fi
exit 0
SH
chmod +x "${tmp}/bin/docker"

source "${root}/scripts/deploy/lib/postgres-capacity.sh"
if rg -q 'python|scripts/ci/check-postgres-capacity.sh' \
    "${root}/scripts/deploy/lib/postgres-capacity.sh"; then
    echo "deployment PostgreSQL capacity preflight must not require the CI Python runtime" >&2
    exit 1
fi
if postgres_capacity_assert_target "${root}" "${tmp}/unsafe.env" >"${tmp}/out" 2>&1; then
    echo "common PostgreSQL capacity gate accepted unsafe target overrides" >&2
    exit 1
fi
grep -q 'connection budget exhausted' "${tmp}/out"
if grep -Eq 'BOT_POSTGRES_POOL_MAX_CONNS=|YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=' "${tmp}/out"; then
    echo "common PostgreSQL capacity gate printed target values" >&2
    exit 1
fi

assert_entrypoint_blocked() {
    local label="$1"
    local env_file="$2"
    local expected="$3"
    shift 3
    : >"${tmp}/docker.log"
    if env -u BOT_POSTGRES_POOL_MAX_CONNS -u YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS \
        PATH="${tmp}/bin:${PATH}" CONTAINER_CLI=docker MOCK_DOCKER_LOG="${tmp}/docker.log" \
        HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1 \
        COMPOSE_ENV_FILE="${env_file}" \
        SHARED_GO_WORKSPACE_PATH="${tmp}/shared-go" \
        IRIS_CLIENT_GO_WORKSPACE_PATH="${tmp}/iris-client-go" \
        HOLOLIVE_APP_UID="$(id -u)" HOLOLIVE_APP_GID="$(id -g)" \
        "$@" >"${tmp}/out" 2>&1; then
        echo "${label} accepted unsafe target capacity" >&2
        exit 1
    fi
    grep -q "${expected}" "${tmp}/out"
    if grep -Eq 'compose .* (config|build|run|up)( |$)' "${tmp}/docker.log"; then
        echo "${label} rendered or mutated production before the capacity gate" >&2
        exit 1
    fi
}

assert_entrypoint_blocked "compose wrapper" \
    "${tmp}/unsafe.env" 'connection budget exhausted' \
    "${root}/scripts/deploy/compose.sh" -f deploy/compose/docker-compose.prod.yml up -d hololive-api
for scale_args in \
    'up -d --scale youtube-collector=2 youtube-collector' \
    'up -d youtube-collector --scale=youtube-collector=2'; do
    read -r -a args <<<"${scale_args}"
    assert_entrypoint_blocked "compose wrapper scale ${scale_args}" \
        "${tmp}/default.env" 'connection budget exhausted' \
        "${root}/scripts/deploy/compose.sh" -f deploy/compose/docker-compose.prod.yml "${args[@]}"
    grep -q 'max=60 allocated=63 reserve=-3' "${tmp}/out"
done

for scale_case in \
    'malformed scale override|up --scale youtube-collector youtube-collector' \
    'absent from the reviewed capacity policy|up --scale=unknown-service=2 unknown-service' \
    'duplicate scale override|up --scale=hololive-api=1 --scale hololive-api=1 hololive-api' \
    'conflicting scale overrides|up --scale hololive-api=1 --scale=hololive-api=2 hololive-api'; do
    expected="${scale_case%%|*}"
    scale_args="${scale_case#*|}"
    read -r -a args <<<"${scale_args}"
    assert_entrypoint_blocked "compose wrapper invalid scale ${scale_args}" \
        "${tmp}/default.env" "${expected}" \
        "${root}/scripts/deploy/compose.sh" -f deploy/compose/docker-compose.prod.yml "${args[@]}"
done
assert_entrypoint_blocked "build-all --no-bump" \
    "${tmp}/unsafe.env" 'connection budget exhausted' \
    "${root}/build-all.sh" --no-bump --skip-local-ci
assert_entrypoint_blocked "compose redeploy" \
    "${tmp}/unsafe.env" 'connection budget exhausted' \
    "${root}/scripts/deploy/compose-redeploy-service.sh" hololive-api

echo "ok: every standard production mutation entrypoint owns the common target capacity preflight"
