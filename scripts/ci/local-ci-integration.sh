#!/usr/bin/env bash

INTEGRATION_TEST_PACKAGES=(
    ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch
    ./hololive/hololive-collector/internal/runtime/joblease
)
INTEGRATION_TAG_PACKAGES=(
    ./hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox
    ./hololive/hololive-api/internal/planes/admin/internal/service/dispatchops
)
INTEGRATION_POSTGRES_IMAGE="${INTEGRATION_POSTGRES_IMAGE:-docker.io/library/postgres:18.7-alpine@sha256:b6972e1115a4ff8eba5d7f7d19184926d1e16d7d1136ae1e1a7ebf286da0f015}"
INTEGRATION_VALKEY_IMAGE="${INTEGRATION_VALKEY_IMAGE:-docker.io/valkey/valkey:9.1.3-alpine3.24@sha256:bf09d58e96d6b4524a15396b65b242bbe18beebbcc15a04d8ee75ddac62fae0b}"
INTEGRATION_TEST_DB_CONTAINER=""
INTEGRATION_TEST_VALKEY_CONTAINER=""

check_integration_tag_compilation() {
    run_step "Integration-tag vet" \
        go_mod_readonly go vet -tags=integration "${INTEGRATION_TAG_PACKAGES[@]}"
}

cleanup_integration_test_database() {
    if [[ -z "${INTEGRATION_TEST_DB_CONTAINER}" ]]; then
        return 0
    fi

    local container="${INTEGRATION_TEST_DB_CONTAINER}"
    INTEGRATION_TEST_DB_CONTAINER=""
    if ! podman rm --force --volumes "${container}" >/dev/null; then
        echo "failed to remove integration test PostgreSQL container ${container}" >&2
        return 1
    fi
}

cleanup_integration_test_valkey() {
    if [[ -z "${INTEGRATION_TEST_VALKEY_CONTAINER}" ]]; then
        return 0
    fi

    local container="${INTEGRATION_TEST_VALKEY_CONTAINER}"
    INTEGRATION_TEST_VALKEY_CONTAINER=""
    if ! podman rm --force --volumes "${container}" >/dev/null; then
        echo "failed to remove integration test Valkey container ${container}" >&2
        return 1
    fi
}

cleanup_integration_test_services() {
    local failed=0
    cleanup_integration_test_valkey || failed=1
    cleanup_integration_test_database || failed=1
    return "${failed}"
}

provision_integration_test_database() {
    local platform
    local postgres_user="local_ci"
    local postgres_password="local_ci"
    local postgres_database="local_ci"
    local ready=false
    local published_address
    local published_port
    local owner_token

    platform="$(podman info --format '{{.Host.OS}}/{{.Host.Arch}}')"
    INTEGRATION_TEST_DB_CONTAINER="$(podman run --detach --rm \
        --platform "${platform}" \
        --label io.iris.observability.logs=exclude \
        --publish 127.0.0.1::5432 \
        --env POSTGRES_USER="${postgres_user}" \
        --env POSTGRES_PASSWORD="${postgres_password}" \
        --env POSTGRES_DB="${postgres_database}" \
        --env POSTGRES_INITDB_ARGS="--locale-provider=builtin --builtin-locale=C.UTF-8 --encoding=UTF8 --data-checksums" \
        --tmpfs /var/lib/postgresql \
        "${INTEGRATION_POSTGRES_IMAGE}" \
        -c fsync=off -c synchronous_commit=off -c full_page_writes=off)"
    trap cleanup_integration_test_services EXIT

    for _ in {1..60}; do
        if podman exec "${INTEGRATION_TEST_DB_CONTAINER}" \
            pg_isready --host 127.0.0.1 --username "${postgres_user}" --dbname "${postgres_database}" >/dev/null 2>&1; then
            ready=true
            break
        fi
        if [[ "$(podman inspect --format '{{.State.Running}}' "${INTEGRATION_TEST_DB_CONTAINER}" 2>/dev/null || true)" != "true" ]]; then
            break
        fi
        sleep 1
    done
    if [[ "${ready}" != "true" ]]; then
        echo "integration test PostgreSQL did not become ready" >&2
        return 1
    fi

    published_address="$(podman port "${INTEGRATION_TEST_DB_CONTAINER}" 5432/tcp)"
    published_port="${published_address##*:}"
    if [[ ! "${published_port}" =~ ^[0-9]+$ ]]; then
        echo "invalid integration test PostgreSQL port: ${published_address}" >&2
        return 1
    fi

    owner_token="local-ci-${INTEGRATION_TEST_DB_CONTAINER}"
    podman exec "${INTEGRATION_TEST_DB_CONTAINER}" \
        psql --set ON_ERROR_STOP=1 --host 127.0.0.1 --username "${postgres_user}" --dbname "${postgres_database}" \
        --command "CREATE TABLE ci_ephemeral_sentinel (token TEXT NOT NULL); INSERT INTO ci_ephemeral_sentinel(token) VALUES ('${owner_token}');" \
        >/dev/null

    export TEST_DATABASE_URL="postgresql://${postgres_user}:${postgres_password}@127.0.0.1:${published_port}/${postgres_database}?sslmode=disable"
    export TEST_DATABASE_OWNER_TOKEN="${owner_token}"
    export ALLOW_EXTERNAL_TEST_DB=false
}

provision_integration_test_valkey() {
    local platform
    local ready=false
    local published_address
    local published_port

    platform="$(podman info --format '{{.Host.OS}}/{{.Host.Arch}}')"
    INTEGRATION_TEST_VALKEY_CONTAINER="$(podman run --detach --rm \
        --platform "${platform}" \
        --label io.iris.observability.logs=exclude \
        --publish 127.0.0.1::6379 \
        "${INTEGRATION_VALKEY_IMAGE}")"
    trap cleanup_integration_test_services EXIT

    for _ in {1..60}; do
        if podman exec "${INTEGRATION_TEST_VALKEY_CONTAINER}" valkey-cli ping 2>/dev/null | grep -qx PONG; then
            ready=true
            break
        fi
        if [[ "$(podman inspect --format '{{.State.Running}}' "${INTEGRATION_TEST_VALKEY_CONTAINER}" 2>/dev/null || true)" != "true" ]]; then
            break
        fi
        sleep 1
    done
    if [[ "${ready}" != "true" ]]; then
        echo "integration test Valkey did not become ready" >&2
        return 1
    fi

    published_address="$(podman port "${INTEGRATION_TEST_VALKEY_CONTAINER}" 6379/tcp)"
    published_port="${published_address##*:}"
    if [[ ! "${published_port}" =~ ^[0-9]+$ ]]; then
        echo "invalid integration test Valkey port: ${published_address}" >&2
        return 1
    fi

    ready=false
    for _ in {1..60}; do
        if (exec 3<>"/dev/tcp/127.0.0.1/${published_port}") 2>/dev/null; then
            ready=true
            break
        fi
        sleep 1
    done
    if [[ "${ready}" != "true" ]]; then
        echo "integration test Valkey published port did not become ready" >&2
        return 1
    fi

    export TEST_VALKEY_ADDR="127.0.0.1:${published_port}"
}

check_integration_tests() {
    if [[ "${RUN_INTEGRATION_TESTS}" == "true" ]]; then
        if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
            run_step "Provision integration test PostgreSQL" provision_integration_test_database
        fi
        if [[ -z "${TEST_VALKEY_ADDR:-}" ]]; then
            run_step "Provision integration test Valkey" provision_integration_test_valkey
        fi
        run_step "Integration-tag tests" \
            go_mod_readonly env INTEGRATION_TEST=true go test -count=1 -tags=integration "${INTEGRATION_TAG_PACKAGES[@]}"
        run_step "INTEGRATION_TEST group" \
            go_mod_readonly env INTEGRATION_TEST=true go test -count=1 "${INTEGRATION_TEST_PACKAGES[@]}"
        if [[ -n "${INTEGRATION_TEST_DB_CONTAINER}${INTEGRATION_TEST_VALKEY_CONTAINER}" ]]; then
            run_step "Remove integration test services" cleanup_integration_test_services
            trap - EXIT
        fi
        return 0
    fi

    if [[ -z "${INTEGRATION_TEST_DB_CONTAINER}" && -n "${TEST_DATABASE_URL:-}" ]]; then
        run_step "Alarm dispatch PostgreSQL integration test" \
            go_mod_readonly go test -count=1 -tags=integration ./hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox
        return 0
    fi

    echo "[LOCAL CI] Skip integration tests: set RUN_INTEGRATION_TESTS=true to run"
    echo
}
