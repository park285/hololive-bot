#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

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
printf 'services: {}\n' >"${repo}/deploy/compose/docker-compose.prod.yml"
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
compose_env_assert_admin_dashboard_loopback_bind() { :; }
compose_env_assert_live_compat_for_host_networked_postgres() { :; }
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
cat >"${repo}/scripts/deploy/lib/removed-runtimes.sh" <<'EOF'
#!/usr/bin/env bash
removed_runtime_cleanup_before_cutover() { :; }
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

cat >"${fakebin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'revision=%s %s\n' "${REVISION:-unset}" "$*" >>"${FAKE_DOCKER_LOG}"
if [[ "${1:-}" == compose && "${2:-}" == version ]]; then
    exit 0
fi
if [[ "${1:-}" == compose ]]; then
    if [[ " $* " == *" run --rm --no-deps hololive-api --check-config "* ]]; then
        exit "${FAKE_CONFIG_CHECK_STATUS:-0}"
    fi
    if [[ " $* " == *" ps -q "* ]]; then
        printf '%b' "${FAKE_COMPOSE_IDS:-container-one\n}"
    fi
    exit 0
fi
if [[ "${1:-}" == image ]]; then
    printf '%s\n' "${FAKE_IMAGE_REVISION_LABEL:-${FAKE_REVISION_LABEL:-}}"
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
built_line="$(grep -n ' image inspect ' "${docker_log}" | tail -n1 | cut -d: -f1)"
up_line="$(grep -nE ' compose .* up -d ' "${docker_log}" | tail -n1 | cut -d: -f1)"
live_line="$(grep -n ' container inspect ' "${docker_log}" | tail -n1 | cut -d: -f1)"
[[ "${built_line}" -lt "${up_line}" && "${up_line}" -lt "${live_line}" ]] \
    || fail "central revision verification ordering must be build-image, up, live-container"

: >"${docker_log}"
if env "${common_env[@]}" \
    FAKE_IMAGE_REVISION_LABEL="${revision}" \
    FAKE_CONTAINER_REVISION_LABEL=0000000000000000000000000000000000000000 \
    bash "${repo}/scripts/deploy/compose-redeploy-service.sh" hololive-api >/dev/null 2>&1; then
    fail "central redeploy must reject a post-cutover live revision mismatch"
fi
grep -Eq ' compose .* up -d ' "${docker_log}" \
    || fail "post-cutover mismatch fixture must reach up"
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
built_line="$(grep -n ' image inspect ' "${docker_log}" | tail -n1 | cut -d: -f1)"
config_line="$(grep -n ' compose .* run --rm --no-deps hololive-api --check-config' "${docker_log}" | tail -n1 | cut -d: -f1)"
up_line="$(grep -nE ' compose .* up -d ' "${docker_log}" | tail -n1 | cut -d: -f1)"
live_line="$(grep -n ' container inspect ' "${docker_log}" | head -n1 | cut -d: -f1)"
[[ "${built_line}" -lt "${config_line}" && "${config_line}" -lt "${up_line}" && "${up_line}" -lt "${live_line}" ]] \
    || fail "build-all ordering must be build-image, config-check, up, live-container"

echo "all production revision entrypoint fixtures passed"
