#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/ci/go-work-sync-drift.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

fixture="${tmpdir}/stack/hololive-bot"
sibling="${tmpdir}/stack/iris-client-go"
fakebin="${tmpdir}/bin"
mkdir -p "${fixture}/admin-dashboard/backend" "${sibling}" "${fakebin}"

printf 'go 1.26.5\nuse (\n\t.\n\t./admin-dashboard/backend\n\t../iris-client-go\n)\n' >"${fixture}/go.work"
printf 'module example.com/root\n\ngo 1.26.5\n' >"${fixture}/go.mod"
: >"${fixture}/go.sum"
: >"${fixture}/go.work.sum"
printf 'module example.com/admin\n\ngo 1.26.5\n' >"${fixture}/admin-dashboard/backend/go.mod"
: >"${fixture}/admin-dashboard/backend/go.sum"
printf 'module example.com/client\n\ngo 1.26.5\n' >"${sibling}/go.mod"
printf 'baseline\n' >"${sibling}/go.sum"

workspace_metadata_files() {
    printf '%s\n' \
        go.work \
        go.work.sum \
        go.mod \
        go.sum \
        admin-dashboard/backend/go.mod \
        admin-dashboard/backend/go.sum \
        ../iris-client-go/go.mod \
        ../iris-client-go/go.sum
}

cat >"${fakebin}/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "work sync" ]]
[[ "${GOWORK}" == "${PWD}/go.work" ]]
if [[ "${FAKE_GO_MUTATE:-false}" == "true" ]]; then
    printf 'workspace-only\n' >>../iris-client-go/go.sum
fi
EOF
chmod +x "${fakebin}/go"

before="$(sha256sum "${sibling}/go.sum")"
(
    cd "${fixture}"
    PATH="${fakebin}:${PATH}" verify_go_work_sync_drift "${fixture}"
)
[[ "$(sha256sum "${sibling}/go.sum")" == "${before}" ]]
echo "[PASS] clean workspace sync check leaves sibling metadata unchanged"

if (
    cd "${fixture}"
    PATH="${fakebin}:${PATH}" FAKE_GO_MUTATE=true verify_go_work_sync_drift "${fixture}"
) >"${tmpdir}/drift.out" 2>&1; then
    echo "expected workspace drift failure" >&2
    exit 1
fi
grep -Fq '../iris-client-go/go.sum (go work sync)' "${tmpdir}/drift.out"
[[ "$(sha256sum "${sibling}/go.sum")" == "${before}" ]]
echo "[PASS] workspace drift is reported without mutating sibling metadata"

if grep -Fq 'run_step "go work sync" go work sync' "${ROOT_DIR}/scripts/ci/admin-dashboard-go-ci.sh"; then
    echo "admin dashboard gate must not sync the real workspace" >&2
    exit 1
fi
grep -Fq 'verify_go_work_sync_drift' "${ROOT_DIR}/scripts/ci/admin-dashboard-go-ci.sh"
echo "[PASS] standalone admin gate uses the read-only workspace drift check"
