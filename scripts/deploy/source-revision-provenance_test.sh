#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/source-revision.sh"
literal_dollar='$'

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

expect_failure() {
    local label="$1"
    shift
    if "$@" >/dev/null 2>&1; then
        fail "${label}"
    fi
}

fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
git -C "${fixture}" init -q
printf 'clean\n' >"${fixture}/tracked"
git -C "${fixture}" add tracked
git -C "${fixture}" -c user.name=fixture -c user.email=fixture@example.invalid commit -qm initial
expected_revision="$(git -C "${fixture}" rev-parse HEAD)"
[[ "$(deploy_source_revision "${fixture}")" == "${expected_revision}" ]] \
    || fail "clean checkout must resolve its full HEAD"

printf 'dirty\n' >"${fixture}/tracked"
expect_failure "tracked changes must fail closed" deploy_source_revision "${fixture}"
printf 'clean\n' >"${fixture}/tracked"
printf 'untracked\n' >"${fixture}/untracked"
expect_failure "untracked changes must fail closed" deploy_source_revision "${fixture}"

fakebin="${fixture}/fakebin"
mkdir -p "${fakebin}"
cat >"${fakebin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "${FAKE_REVISION_LABEL:-}"
EOF
cat >"${fakebin}/compose" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%b' "${FAKE_COMPOSE_IDS:-}"
EOF
chmod +x "${fakebin}/docker" "${fakebin}/compose"

verify_object_fixture() {
    local label="$1"
    local expected="$2"
    FAKE_REVISION_LABEL="${label}" \
        deploy_verify_object_revision "${fakebin}/docker" image image:fixture "${expected}"
}

single_identifier_fixture() {
    local identifiers="$1"
    FAKE_COMPOSE_IDS="${identifiers}" \
        deploy_require_single_identifier fixture "${fakebin}/compose"
}

verify_object_fixture "${expected_revision}" "${expected_revision}" \
    || fail "exact image revision must pass"
expect_failure "unknown expected revision must fail" \
    verify_object_fixture unknown unknown
expect_failure "missing image revision must fail" \
    verify_object_fixture "" "${expected_revision}"
expect_failure "mismatched image revision must fail" \
    verify_object_fixture 0000000000000000000000000000000000000000 "${expected_revision}"
[[ "$(single_identifier_fixture one)" == one ]] \
    || fail "single compose mapping must resolve its identifier"
expect_failure "missing compose mapping must fail" \
    single_identifier_fixture ""
expect_failure "multiple compose mappings must fail" \
    single_identifier_fixture 'one\ntwo\n'

cat >"${fixture}/compose.yml" <<'EOF'
services:
  valkey-cache:
    image: ${FIXTURE_SOURCE_IMAGE:-fixture-cache:default}
  docker-proxy:
    image: fixture-proxy:default
EOF
cat >"${fixture}/compose override.yml" <<'EOF'
services:
  docker-proxy:
    image: ${FIXTURE_PROXY_IMAGE:?fixture proxy image required}
EOF
printf 'FIXTURE_SOURCE_IMAGE=fixture-cache:env-override\nFIXTURE_PROXY_IMAGE=fixture-proxy:file-override\n' \
    >"${fixture}/compose images.env"
compose_cmd=(docker compose --project-name revision-fixture \
    --env-file "${fixture}/compose images.env" -f "${fixture}/compose.yml")
[[ "$(deploy_service_image_ref valkey-cache "${compose_cmd[@]}")" == fixture-cache:env-override ]] \
    || fail "image resolution must use the caller's Compose env file"
[[ "$(deploy_service_image_ref docker-proxy "${compose_cmd[@]}" \
    -f "${fixture}/compose override.yml")" == fixture-proxy:file-override ]] \
    || fail "image resolution must use every caller-supplied Compose file"
expect_failure "unknown Compose service must fail" \
    deploy_service_image_ref unknown-service "${compose_cmd[@]}"
FAKE_COMPOSE_IDS='' expect_failure "empty Compose image output must fail" \
    deploy_service_image_ref valkey-cache "${fakebin}/compose"
FAKE_COMPOSE_IDS='one\ntwo\n' expect_failure "multiple Compose images must fail" \
    deploy_service_image_ref valkey-cache "${fakebin}/compose"

dockerfiles=(
    hololive/hololive-api/Dockerfile
    hololive/hololive-alarm-worker/Dockerfile
    hololive/hololive-youtube-collector/Dockerfile
)
for dockerfile in "${dockerfiles[@]}"; do
    path="${ROOT_DIR}/${dockerfile}"
    [[ "$(grep -c '^ARG REVISION=unknown$' "${path}")" -eq 2 ]] \
        || fail "${dockerfile} must expose an explicit local-development revision fallback"
    grep -Fq "if [ \"${literal_dollar}{REVISION}\" != \"unknown\" ]; then test \"${literal_dollar}{#REVISION}\" -eq 40" "${path}" \
        || fail "${dockerfile} must reject abbreviated production revisions"
    grep -Fq "case \"${literal_dollar}{REVISION}\" in *[!0-9a-f]*) exit 1;; esac" "${path}" \
        || fail "${dockerfile} must reject non-hex production revisions"
    grep -Fq "org.opencontainers.image.revision=\"${literal_dollar}{REVISION}\"" "${path}" \
        || fail "${dockerfile} must publish the OCI revision label"
done

compose_file="${ROOT_DIR}/deploy/compose/docker-compose.prod.yml"
for service in hololive-db-migrate hololive-alarm-worker hololive-api youtube-collector; do
    block="$(awk -v service="${service}" '
        $0 == "services:" { service_section = 1; next }
        service_section && $0 == "  " service ":" { found = 1; next }
        found && /^  [a-zA-Z0-9_-]+:$/ { exit }
        found { print }
    ' "${compose_file}")"
    grep -Fq "REVISION: ${literal_dollar}{REVISION:-unknown}" <<<"${block}" \
        || fail "${service} build must receive REVISION"
done

echo "all source revision provenance checks passed"
