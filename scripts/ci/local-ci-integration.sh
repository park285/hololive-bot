#!/usr/bin/env bash

INTEGRATION_TEST_PACKAGES=(
    ./hololive/hololive-api/internal/planes/llm/internal/service/majorevent/summarizer
    ./hololive/hololive-api/internal/planes/llm/internal/service/membernews/summarizer
    ./hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch
    ./hololive/hololive-youtube-producer/internal/runtime/ingestionlease
)
INTEGRATION_TAG_PACKAGES=(
    ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
    ./hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo
)
INTEGRATION_POSTGRES_IMAGE="${INTEGRATION_POSTGRES_IMAGE:-postgres:18-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15}"
INTEGRATION_VALKEY_IMAGE="${INTEGRATION_VALKEY_IMAGE:-valkey/valkey:9.1.0-alpine3.23@sha256:a35428eba9043cc0b79dbe54100f0c92784f2de00ad09b01182bfb1c5c83d1bd}"
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
    if ! docker rm --force "${container}" >/dev/null; then
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
    if ! docker rm --force "${container}" >/dev/null; then
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
    local postgres_user="local_ci"
    local postgres_password="local_ci"
    local postgres_database="local_ci"
    local ready=false
    local published_address
    local published_port
    local owner_token

    INTEGRATION_TEST_DB_CONTAINER="$(docker run --detach --rm \
        --publish 127.0.0.1::5432 \
        --env POSTGRES_USER="${postgres_user}" \
        --env POSTGRES_PASSWORD="${postgres_password}" \
        --env POSTGRES_DB="${postgres_database}" \
        "${INTEGRATION_POSTGRES_IMAGE}")"
    trap cleanup_integration_test_services EXIT

    for _ in {1..60}; do
        if docker exec "${INTEGRATION_TEST_DB_CONTAINER}" \
            pg_isready --host 127.0.0.1 --username "${postgres_user}" --dbname "${postgres_database}" >/dev/null 2>&1; then
            ready=true
            break
        fi
        if [[ "$(docker inspect --format '{{.State.Running}}' "${INTEGRATION_TEST_DB_CONTAINER}" 2>/dev/null || true)" != "true" ]]; then
            break
        fi
        sleep 1
    done
    if [[ "${ready}" != "true" ]]; then
        echo "integration test PostgreSQL did not become ready" >&2
        return 1
    fi

    published_address="$(docker port "${INTEGRATION_TEST_DB_CONTAINER}" 5432/tcp)"
    published_port="${published_address##*:}"
    if [[ ! "${published_port}" =~ ^[0-9]+$ ]]; then
        echo "invalid integration test PostgreSQL port: ${published_address}" >&2
        return 1
    fi

    owner_token="local-ci-${INTEGRATION_TEST_DB_CONTAINER}"
    docker exec "${INTEGRATION_TEST_DB_CONTAINER}" \
        psql --set ON_ERROR_STOP=1 --host 127.0.0.1 --username "${postgres_user}" --dbname "${postgres_database}" \
        --command "CREATE TABLE ci_ephemeral_sentinel (token TEXT NOT NULL); INSERT INTO ci_ephemeral_sentinel(token) VALUES ('${owner_token}');" \
        >/dev/null

    export TEST_DATABASE_URL="postgresql://${postgres_user}:${postgres_password}@127.0.0.1:${published_port}/${postgres_database}?sslmode=disable"
    export TEST_DATABASE_OWNER_TOKEN="${owner_token}"
    export ALLOW_EXTERNAL_TEST_DB=false
}

provision_integration_test_valkey() {
    local ready=false
    local published_address
    local published_port

    INTEGRATION_TEST_VALKEY_CONTAINER="$(docker run --detach --rm \
        --publish 127.0.0.1::6379 \
        "${INTEGRATION_VALKEY_IMAGE}")"
    trap cleanup_integration_test_services EXIT

    for _ in {1..60}; do
        if docker exec "${INTEGRATION_TEST_VALKEY_CONTAINER}" valkey-cli ping 2>/dev/null | grep -qx PONG; then
            ready=true
            break
        fi
        if [[ "$(docker inspect --format '{{.State.Running}}' "${INTEGRATION_TEST_VALKEY_CONTAINER}" 2>/dev/null || true)" != "true" ]]; then
            break
        fi
        sleep 1
    done
    if [[ "${ready}" != "true" ]]; then
        echo "integration test Valkey did not become ready" >&2
        return 1
    fi

    published_address="$(docker port "${INTEGRATION_TEST_VALKEY_CONTAINER}" 6379/tcp)"
    published_port="${published_address##*:}"
    if [[ ! "${published_port}" =~ ^[0-9]+$ ]]; then
        echo "invalid integration test Valkey port: ${published_address}" >&2
        return 1
    fi

    export TEST_VALKEY_ADDR="127.0.0.1:${published_port}"
}

check_integration_tests() {
    if [[ "${RUN_INTEGRATION_TESTS}" == "true" ]]; then
        local provisioned_services=false
        if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
            run_step "Provision integration test PostgreSQL" provision_integration_test_database
            provisioned_services=true
        fi
        if [[ -z "${TEST_VALKEY_ADDR:-}" && -z "${TEST_VALKEY_HOST:-}" ]]; then
            run_step "Provision integration test Valkey" provision_integration_test_valkey
            provisioned_services=true
        fi
        run_step "Integration-tag tests" \
            go_mod_readonly env INTEGRATION_TEST=true go test -count=1 -tags=integration "${INTEGRATION_TAG_PACKAGES[@]}"
        run_step "INTEGRATION_TEST group" \
            go_mod_readonly env INTEGRATION_TEST=true go test -count=1 "${INTEGRATION_TEST_PACKAGES[@]}"
        if [[ "${provisioned_services}" == "true" ]]; then
            run_step "Remove integration test services" cleanup_integration_test_services
            trap - EXIT
        fi
        return 0
    fi

    if [[ -n "${TEST_DATABASE_URL:-}" ]]; then
        run_step "Alarm dispatch PostgreSQL integration test" \
            go_mod_readonly go test -count=1 -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
        return 0
    fi

    echo "[LOCAL CI] Skip integration tests: set RUN_INTEGRATION_TESTS=true to run"
    echo
}
