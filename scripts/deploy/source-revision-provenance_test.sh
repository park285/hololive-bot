#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/source-revision.sh"

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
cat >"${fakebin}/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "${FAKE_REVISION_LABEL:-}"
EOF
chmod +x "${fakebin}/podman"

verify_object_fixture() {
    local label="$1"
    local expected="$2"
    FAKE_REVISION_LABEL="${label}" \
        deploy_verify_object_revision "${fakebin}/podman" image image:fixture "${expected}"
}

verify_object_fixture "${expected_revision}" "${expected_revision}" \
    || fail "exact image revision must pass"
expect_failure "unknown expected revision must fail" \
    verify_object_fixture unknown unknown
expect_failure "missing image revision must fail" \
    verify_object_fixture "" "${expected_revision}"
expect_failure "mismatched image revision must fail" \
    verify_object_fixture 0000000000000000000000000000000000000000 "${expected_revision}"

echo "all source revision provenance checks passed"
