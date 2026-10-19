#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REAL_DOCKER="$(command -v docker)"

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
repo="${tmp}/repo"
fakebin="${tmp}/bin"
docker_log="${tmp}/docker.log"
mkdir -p \
    "${repo}/scripts/deploy/lib" \
    "${repo}/deploy/compose" \
    "${repo}/hololive/hololive-api" \
    "${repo}/hololive/hololive-alarm-worker" \
    "${tmp}/shared-go" \
    "${tmp}/iris-client-go" \
    "${fakebin}"

cp "${ROOT_DIR}/build-all.sh" "${repo}/build-all.sh"
cp "${ROOT_DIR}/scripts/deploy/compose-redeploy-service.sh" "${repo}/scripts/deploy/compose-redeploy-service.sh"
cp "${ROOT_DIR}/scripts/deploy/lib/kapu-alarm-worker-fence.sh" "${repo}/scripts/deploy/lib/kapu-alarm-worker-fence.sh"
cp "${ROOT_DIR}/scripts/deploy/lib/ap-compose-version.sh" "${repo}/scripts/deploy/lib/ap-compose-version.sh"
cp "${ROOT_DIR}/scripts/deploy/lib/source-revision.sh" "${repo}/scripts/deploy/lib/source-revision.sh"
cp "${ROOT_DIR}/scripts/deploy/lib/admin-bind.sh" "${repo}/scripts/deploy/lib/admin-bind.sh"
cat >"${repo}/deploy/compose/docker-compose.prod.yml" <<'EOF'
services:
  hololive-api:
    image: hololive-api:prod
    depends_on:
      - hololive-alarm-worker
      - valkey-cache
  hololive-db-migrate:
    image: hololive-api:prod
  hololive-alarm-worker:
    image: hololive-alarm-worker:prod
  youtube-collector:
    image: hololive-youtube-collector:prod
  youtube-po-c:
    image: hololive-youtube-po-sandbox:prod
  holo-postgres:
    image: ${POSTGRES_IMAGE:-hololive-postgres:prod}
  deunhealth:
    image: ${DEUNHEALTH_IMAGE:-hololive-deunhealth:prod}
  valkey-cache:
    image: ${VALKEY_IMAGE:-iris-stack-valkey:9.1.2-zlib1.3.2-r1}
  docker-proxy:
    image: ${DOCKER_SOCKET_PROXY_IMAGE:-iris-stack-socket-proxy:1.13.1-go1.27.2}
EOF
cat >"${repo}/deploy/compose/image-override.yml" <<'EOF'
services:
  hololive-api:
    image: ${FIXTURE_API_IMAGE:-hololive-api:prod}
  hololive-db-migrate:
    image: ${FIXTURE_MIGRATION_IMAGE:-hololive-api:prod}
EOF
cat >"${repo}/deploy/compose/docker-compose.remote-cache.yml" <<'EOF'
services:
  hololive-api:
    image: ${FIXTURE_BUILD_API_IMAGE:-hololive-api:prod}
EOF
printf 'fixture=1\n' >"${repo}/deploy/compose/build-only.env.sample"
printf '1.2.3\n' >"${repo}/hololive/hololive-api/VERSION"
printf '1.2.3\n' >"${repo}/hololive/hololive-alarm-worker/VERSION"
for runtime in hololive-api hololive-alarm-worker; do
    cat >"${repo}/hololive/${runtime}/Makefile" <<'EOF'
bump-patch:
	@printf '1.2.4\n' > VERSION
EOF
done

cat >"${repo}/scripts/deploy/lib/compose-env.sh" <<'EOF'
#!/usr/bin/env bash
compose_env_resolve_file() { printf '%s\n' "${COMPOSE_ENV_FILE}"; }
compose_env_validate_file_format() { :; }
compose_env_assert_shell_matches_all_file_keys() { :; }
compose_env_assert_no_shell_shadow_for_compose_files() { :; }
compose_env_assert_live_compat_for_host_networked_postgres() { :; }
compose_env_key_exists_in_file() { return 1; }
compose_env_read_value_from_file() {
    if [[ "$2" == IRIS_BASE_URL ]]; then
        printf '%s\n' http://fixture.invalid
    fi
}
EOF
cat >"${repo}/scripts/deploy/lib/compose-services.sh" <<'EOF'
#!/usr/bin/env bash
compose_service_resolve_redeploy_target() { printf '%s\n' "$1"; }
compose_service_redeploy_usage_lines() { :; }
compose_service_resolve_build_target() { printf '%s\n' "$1"; }
compose_service_build_targets_text() { :; }
EOF
cat >"${repo}/scripts/deploy/lib/health-gate.sh" <<'EOF'
#!/usr/bin/env bash
cutover_service_uses_app_writable_bind_mount() { return 1; }
cutover_bind_mount_preflight() { return 0; }
cutover_capture_restart_baseline() { :; }
cutover_health_gate() { return 0; }
EOF
cat >"${repo}/scripts/deploy/lib/postgres-capacity.sh" <<'EOF'
#!/usr/bin/env bash
postgres_capacity_assert_target() { :; }
EOF

# 프로필 허용은 독립된 경계이며 이 테스트는 이미지 출처를 검증한다.
cat >"${repo}/scripts/deploy/lib/worker-profile-pair.py" <<'EOF'
import json
import sys
from pathlib import Path

action, service, directory, *rest = sys.argv[1:]
if action in {"candidate", "rollback"}:
    config = json.load(sys.stdin)
    assert service in config["services"]
    pair = Path(directory)
    pair.mkdir(parents=True, exist_ok=True)
    (pair / "compose.yml").write_text("services: {}\n")
elif action not in {"capture", "verify-source", "activate", "verify-live"}:
    raise SystemExit("unsupported profile fixture operation")
EOF
printf 'artifacts/\n' >"${repo}/.gitignore"

cat >"${fakebin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'revision=%s %s\n' "${REVISION:-unset}" "$*" >>"${FAKE_DOCKER_LOG}"
if [[ "${1:-}" == compose && "${2:-}" == version ]]; then
    exit 0
fi
if [[ "${1:-}" == compose ]]; then
    # 이미지 빌드·데몬 접속 없이 실제 Compose CLI로 렌더링한다.
    if [[ " $* " == *" config --format json "* ]]; then
        exec "${FAKE_REAL_DOCKER}" "$@"
    fi
    if [[ " $* " == *" run --rm --no-deps hololive-api --check-config "* ]]; then
        exit "${FAKE_CONFIG_CHECK_STATUS:-0}"
    fi
    if [[ " $* " == *" ps -q "* ]]; then
        printf '%b' "${FAKE_COMPOSE_IDS:-container-one\n}"
    fi
    exit 0
fi
if [[ "$*" == *'{{.Image}}'* ]]; then
    printf '%s\n' sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    exit 0
fi
if [[ "${1:-}" == image ]]; then
    if [[ "${@: -1}" == "${FAKE_BAD_IMAGE_REF:-}" ]]; then
        printf '%s\n' unknown
    else
        printf '%s\n' "${FAKE_IMAGE_REVISION_LABEL:-${FAKE_REVISION_LABEL:-}}"
    fi
    exit 0
fi
if [[ "${1:-}" == container ]]; then
    printf '%s\n' "${FAKE_CONTAINER_REVISION_LABEL:-${FAKE_REVISION_LABEL:-}}"
    exit 0
fi
exit 99
EOF
chmod +x "${fakebin}/docker"

git -C "${repo}" init -q
common_env=(
    PATH="${fakebin}:${PATH}"
    CONTAINER_CLI=docker
    FAKE_DOCKER_LOG="${docker_log}"
    FAKE_REAL_DOCKER="${REAL_DOCKER}"
    COMPOSE_ENV_FILE="${repo}/deploy/compose/build-only.env.sample"
    SHARED_GO_WORKSPACE_PATH="${tmp}/shared-go"
    IRIS_CLIENT_GO_WORKSPACE_PATH="${tmp}/iris-client-go"
)

: >"${docker_log}"
env "${common_env[@]}" bash "${repo}/scripts/deploy/compose-redeploy-service.sh" holo-postgres >/dev/null \
    || fail "infra-only redeploy must tolerate a dirty source checkout"
grep -Eq 'revision=unset compose .* up -d .*holo-postgres' "${docker_log}" \
    || fail "infra-only redeploy must reach up without deriving a source revision"

: >"${docker_log}"
if env "${common_env[@]}" bash "${repo}/scripts/deploy/compose-redeploy-service.sh" hololive-api >/dev/null 2>&1; then
    fail "source-built redeploy must reject a dirty source checkout"
fi
if grep -Eq ' compose .* (build|up)( |$)' "${docker_log}"; then
    fail "dirty source-built redeploy must fail before build or up"
fi

git -C "${repo}" add .
git -C "${repo}" -c user.name=fixture -c user.email=fixture@example.invalid commit -qm fixture
revision="$(git -C "${repo}" rev-parse HEAD)"

api_version_before="$(<"${repo}/hololive/hololive-api/VERSION")"
: >"${docker_log}"
if env "${common_env[@]}" bash "${repo}/build-all.sh" --skip-local-ci >/dev/null 2>&1; then
    fail "default build-all live mode must require --no-bump"
fi
[[ "$(<"${repo}/hololive/hololive-api/VERSION")" == "${api_version_before}" ]] \
    || fail "rejected default live mode must not mutate VERSION"
if grep -Eq ' compose .* (build|up)( |$)' "${docker_log}"; then
    fail "rejected default live mode must stop before build or up"
fi

: >"${docker_log}"
if env "${common_env[@]}" FAKE_REVISION_LABEL=unknown \
    bash "${repo}/scripts/deploy/compose-redeploy-service.sh" hololive-api >/dev/null 2>&1; then
    fail "central redeploy must reject an unknown built image revision"
fi
grep -Eq 'revision=[0-9a-f]{40} image inspect ' "${docker_log}" \
    || fail "central redeploy must inspect the built image"
if grep -Eq ' compose .* up -d ' "${docker_log}"; then
    fail "built image mismatch must fail before central up"
fi

: >"${docker_log}"
env "${common_env[@]}" FAKE_REVISION_LABEL="${revision}" \
    bash "${repo}/scripts/deploy/compose-redeploy-service.sh" hololive-api >/dev/null \
    || fail "central redeploy must accept exact built and live revisions"
built_line="$(grep -n ' image inspect .*hololive-api:prod' "${docker_log}" | tail -n1 | cut -d: -f1)"
up_line="$(grep -nE ' compose .* up -d ' "${docker_log}" | tail -n1 | cut -d: -f1)"
live_line="$(grep -n ' container inspect ' "${docker_log}" | tail -n1 | cut -d: -f1)"
[[ "${built_line}" -lt "${up_line}" && "${up_line}" -lt "${live_line}" ]] \
    || fail "central revision verification ordering must be build-image, up, live-container"

: >"${docker_log}"
if env "${common_env[@]}" \
    FAKE_IMAGE_REVISION_LABEL="${revision}" \
    FAKE_CONTAINER_REVISION_LABEL=0000000000000000000000000000000000000000 \
    bash "${repo}/scripts/deploy/compose-redeploy-service.sh" hololive-api >/dev/null 2>"${tmp}/post-cutover-error"; then
    fail "central redeploy must reject a post-cutover live revision mismatch"
fi
if ! grep -Eq ' compose .* up -d ' "${docker_log}"; then
    cat "${tmp}/post-cutover-error" >&2
    fail "post-cutover mismatch fixture must reach up"
fi
grep -Fq ' container inspect ' "${docker_log}" \
    || fail "post-cutover mismatch must inspect the live container"
if grep -Eq ' compose .* (down|rm|stop|restart) ' "${docker_log}"; then
    fail "post-cutover mismatch must not trigger speculative destructive rollback"
fi

printf 'dirty local build\n' >"${repo}/local-dev-untracked"
: >"${docker_log}"
env "${common_env[@]}" FAKE_REVISION_LABEL=unknown \
    bash "${repo}/build-all.sh" --no-bump --build-only --skip-local-ci >/dev/null \
    || fail "dirty build-only mode must retain the unknown local/dev fallback"
grep -Eq 'revision=unknown compose .* build( |$)' "${docker_log}" \
    || fail "dirty build-only mode must pass the unknown revision"
if grep -Fq ' image inspect ' "${docker_log}"; then
    fail "local/dev build-only mode must not enforce production image provenance"
fi

: >"${docker_log}"
env "${common_env[@]}" FAKE_REVISION_LABEL=unknown \
    bash "${repo}/build-all.sh" --no-bump --build-only --security-scan --skip-local-ci >/dev/null \
    || fail "security scan build-only mode must succeed"
grep -Eq ' compose .* -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.security-scan.yml build( |$)' "${docker_log}" \
    || fail "security scan mode must apply the attestation-free scan overlay"

if env "${common_env[@]}" \
    bash "${repo}/build-all.sh" --no-bump --security-scan --skip-local-ci >/dev/null 2>&1; then
    fail "security scan mode must reject a live build invocation"
fi
if env "${common_env[@]}" \
    bash "${repo}/build-all.sh" --no-bump --build-only --security-scan --skip-local-ci hololive-api >/dev/null 2>&1; then
    fail "security scan mode must reject a partial target build"
fi

: >"${docker_log}"
env "${common_env[@]}" FAKE_REVISION_LABEL=unknown \
    bash "${repo}/build-all.sh" --no-bump --skip-local-ci hololive-api >/dev/null \
    || fail "dirty target build must retain the unknown local/dev fallback"
grep -Eq 'revision=unknown compose .* build hololive-api' "${docker_log}" \
    || fail "dirty target build must pass the unknown revision"
if grep -Eq ' compose .* up ' "${docker_log}"; then
    fail "target build mode must never cut over live services"
fi

: >"${docker_log}"
env "${common_env[@]}" FAKE_REVISION_LABEL=unknown \
    bash "${repo}/build-all.sh" --build-only --skip-local-ci >/dev/null \
    || fail "bump-enabled build-only mode must succeed"
[[ "$(<"${repo}/hololive/hololive-api/VERSION")" == 1.2.4 ]] \
    || fail "bump-enabled build-only mode must intentionally mutate VERSION"
grep -Eq 'revision=unknown compose .* build( |$)' "${docker_log}" \
    || fail "bump-enabled build-only mode must use the unknown revision fallback"

git -C "${repo}" add .
git -C "${repo}" -c user.name=fixture -c user.email=fixture@example.invalid commit -qm local-build-state
revision="$(git -C "${repo}" rev-parse HEAD)"

: >"${docker_log}"
if env "${common_env[@]}" FAKE_REVISION_LABEL=unknown \
    bash "${repo}/build-all.sh" --no-bump --skip-local-ci hololive-api >/dev/null 2>&1; then
    fail "clean target build must reject an unknown built image revision"
fi
grep -Eq "revision=${revision} compose .* build hololive-api" "${docker_log}" \
    || fail "clean target build must inject the full source revision"
grep -Eq "revision=${revision} image inspect .*hololive-api:prod" "${docker_log}" \
    || fail "clean target build must inspect the built image revision"
if grep -Eq ' compose .* up ' "${docker_log}"; then
    fail "clean target build must never cut over live services"
fi

: >"${docker_log}"
env "${common_env[@]}" FAKE_REVISION_LABEL="${revision}" \
    bash "${repo}/build-all.sh" --no-bump --skip-local-ci hololive-api >/dev/null \
    || fail "clean target build must accept an exact built image revision"
grep -Eq "revision=${revision} image inspect .*hololive-api:prod" "${docker_log}" \
    || fail "clean target build must preserve exact revision verification"
if grep -Eq ' compose .* up ' "${docker_log}"; then
    fail "exact target build must never cut over live services"
fi

: >"${docker_log}"
if env "${common_env[@]}" FAKE_REVISION_LABEL=unknown \
    bash "${repo}/build-all.sh" --no-bump --skip-local-ci >/dev/null 2>&1; then
    fail "build-all live mode must reject unknown built image revisions"
fi
if grep -Eq ' compose .* up ' "${docker_log}"; then
    fail "build-all built image mismatch must fail before up"
fi

: >"${docker_log}"
if env "${common_env[@]}" FAKE_REVISION_LABEL="${revision}" FAKE_CONFIG_CHECK_STATUS=1 \
    bash "${repo}/build-all.sh" --no-bump --skip-local-ci >/dev/null 2>&1; then
    fail "build-all live mode must reject an invalid built runtime configuration"
fi
grep -Eq ' compose .* run --rm --no-deps hololive-api --check-config' "${docker_log}" \
    || fail "build-all must run the built runtime config preflight"
if grep -Eq ' compose .* up ' "${docker_log}"; then
    fail "runtime config failure must stop before cutover up"
fi

: >"${docker_log}"
env "${common_env[@]}" FAKE_REVISION_LABEL="${revision}" \
    bash "${repo}/build-all.sh" --no-bump --skip-local-ci >/dev/null \
    || fail "build-all --no-bump full cutover must accept exact built and live revisions"
built_line="$(grep -n ' image inspect .*hololive-api:prod' "${docker_log}" | tail -n1 | cut -d: -f1)"
config_line="$(grep -n ' compose .* run --rm --no-deps hololive-api --check-config' "${docker_log}" | tail -n1 | cut -d: -f1)"
up_line="$(grep -nE ' compose .* up -d ' "${docker_log}" | head -n1 | cut -d: -f1)"
live_line="$(grep -n ' container inspect ' "${docker_log}" | head -n1 | cut -d: -f1)"
[[ "${built_line}" -lt "${config_line}" && "${config_line}" -lt "${up_line}" && "${up_line}" -lt "${live_line}" ]] \
    || fail "build-all ordering must be build-image, config-check, up, live-container"

for image_key in VALKEY_IMAGE DOCKER_SOCKET_PROXY_IMAGE FIXTURE_BUILD_API_IMAGE; do
    image_ref="fixture-${image_key,,}:override"
    printf '%s=%s\n' "${image_key}" "${image_ref}" >"${tmp}/image overrides.env"
    : >"${docker_log}"
    if env "${common_env[@]}" COMPOSE_ENV_FILE="${tmp}/image overrides.env" \
        REMOTE_CACHE_PREFIX=fixture.invalid/cache FAKE_REVISION_LABEL="${revision}" \
        FAKE_BAD_IMAGE_REF="${image_ref}" \
        bash "${repo}/build-all.sh" --no-bump --skip-local-ci --remote-cache >/dev/null 2>&1; then
        fail "build-all must reject a bad ${image_key} override even when default images are valid"
    fi
    grep -Fq " image inspect --format {{index .Config.Labels \"org.opencontainers.image.revision\"}} ${image_ref}" "${docker_log}" \
        || fail "build-all must inspect the env-file image override for ${image_key}"
    if grep -Eq ' compose .* (up|run|stop|down|rm|restart) ' "${docker_log}"; then
        fail "bad ${image_key} override must fail before runtime preflight or cutover"
    fi

    : >"${docker_log}"
    env "${common_env[@]}" COMPOSE_ENV_FILE="${tmp}/image overrides.env" \
        REMOTE_CACHE_PREFIX=fixture.invalid/cache FAKE_REVISION_LABEL="${revision}" \
        bash "${repo}/build-all.sh" --no-bump --skip-local-ci --remote-cache >/dev/null \
        || fail "build-all must accept a valid ${image_key} env-file override"
    grep -Fq " image inspect --format {{index .Config.Labels \"org.opencontainers.image.revision\"}} ${image_ref}" "${docker_log}" \
        || fail "successful build-all must verify ${image_key}'s rendered image"
    grep -Eq ' compose .* up -d ' "${docker_log}" \
        || fail "valid ${image_key} override must permit cutover"
done

for image_key in FIXTURE_API_IMAGE FIXTURE_MIGRATION_IMAGE; do
    image_ref="fixture-${image_key,,}:override"
    printf '%s=%s\n' "${image_key}" "${image_ref}" >"${tmp}/image overrides.env"
    override_env=(COMPOSE_ENV_FILE="${tmp}/image overrides.env" \
        COMPOSE_FILE=deploy/compose/docker-compose.prod.yml:deploy/compose/image-override.yml)
    : >"${docker_log}"
    if env "${common_env[@]}" "${override_env[@]}" FAKE_REVISION_LABEL="${revision}" \
        FAKE_BAD_IMAGE_REF="${image_ref}" \
        bash "${repo}/scripts/deploy/compose-redeploy-service.sh" hololive-api >/dev/null 2>&1; then
        fail "redeploy must reject a bad ${image_key} override even when the default image is valid"
    fi
    if grep -Eq ' compose .* (up|stop|down|rm|restart) ' "${docker_log}" \
        || grep -Eq ' compose .* run .* hololive-db-migrate( |$)' "${docker_log}"; then
        fail "bad ${image_key} override must fail before migration or cutover"
    fi

    : >"${docker_log}"
    env "${common_env[@]}" "${override_env[@]}" FAKE_REVISION_LABEL="${revision}" \
        bash "${repo}/scripts/deploy/compose-redeploy-service.sh" hololive-api >/dev/null \
        || fail "redeploy must accept a valid ${image_key} env-file override"
    grep -Eq ' compose .* run --rm hololive-db-migrate' "${docker_log}" \
        || fail "valid ${image_key} override must permit migration"
    grep -Eq ' compose .* up -d ' "${docker_log}" \
        || fail "valid ${image_key} override must permit cutover"
done

echo "all production revision entrypoint fixtures passed"
