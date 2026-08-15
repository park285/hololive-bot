#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="${ROOT_DIR}/scripts/deploy/ap-host-native-deploy.sh"
REMOTE_APPLY="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-remote-apply.sh"
ROLLBACK="${ROOT_DIR}/scripts/deploy/ap-host-native-rollback.sh"
RELEASE_PATH_LIB="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-release-path.sh"
ROLLBACK_CHECK_LIB="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-rollback-check.sh"

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

# shellcheck source=scripts/deploy/lib/ap-host-native-release-path.sh
. "${RELEASE_PATH_LIB}"
# shellcheck source=scripts/deploy/lib/ap-host-native-rollback-check.sh
. "${ROLLBACK_CHECK_LIB}"

if grep -Eq 'HOLOLIVE_H3_ADDR=:%s' "${DEPLOY}"; then
  record_fail "ap-host-native binds H3 to all interfaces (:port) (8c2e3ef9)"
else
  pass "ap-host-native H3 not bound to all interfaces"
fi

if grep -Eq 'HOLOLIVE_H3_ADDR=127\.0\.0\.1:%s' "${DEPLOY}"; then
  pass "ap-host-native H3 bound to loopback"
else
  record_fail "ap-host-native H3 bind not narrowed to loopback (8c2e3ef9)"
fi

if grep -Fq 'YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true' "${DEPLOY}"; then
  pass "ap-host-native enables the collector runtime"
else
  record_fail "ap-host-native must set YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true"
fi

if grep -Fq 'AP_POSTGRES_HOST="${AP_POSTGRES_HOST:-hololive-postgres.tail742dd8.ts.net}"' "${DEPLOY}" &&
   grep -Fq "printf 'POSTGRES_HOST=%s\\n' \"\$AP_POSTGRES_HOST\"" "${DEPLOY}" &&
   grep -Fq "printf 'CACHE_HOST=%s\\n' \"\${AP_CACHE_HOST:-\$AP_CENTRAL_HOST}\"" "${DEPLOY}"; then
  pass "ap-host-native separates stable PostgreSQL from the central cache endpoint"
else
  record_fail "ap-host-native must use stable PostgreSQL DNS without changing the cache endpoint"
fi

if grep -Fq 'SETTINGS_DIR=/var/lib/hololive-bot/youtube-collector/settings' "${DEPLOY}"; then
  pass "ap-host-native settings dir uses persistent varlib path"
else
  record_fail "ap-host-native settings dir must not default to read-only release data"
fi

if grep -Fq 'ReadWritePaths=/var/lib/hololive-bot' "${DEPLOY}" &&
   grep -Fq 'install -d -m 0750 -o hololive -g opc /var/lib/hololive-bot/youtube-collector/settings' "${REMOTE_APPLY}"; then
  pass "ap-host-native settings dir is writable for hololive"
else
  record_fail "ap-host-native settings dir must be created and writable under systemd hardening"
fi

if grep -q '^ReadWritePaths=.*stack-secrets' "${DEPLOY}"; then
  record_fail "ap-host-native must not grant write access to the static secret directory"
else
  pass "ap-host-native keeps /etc/stack-secrets read-only under ProtectSystem=strict"
fi

if grep -Fq 'rollback_contract_dir="$old_target/rollback-contract"' "${REMOTE_APPLY}" &&
   grep -Fq '"$host_env" "$rollback_contract_dir/youtube-collector-host.env"' "${REMOTE_APPLY}" &&
   grep -Fq '"$unit_file" "$rollback_contract_dir/hololive-youtube-collector@.service"' "${REMOTE_APPLY}"; then
  pass "ap-host-native deploy preserves the installed host env and systemd unit with the previous release"
else
  record_fail "ap-host-native deploy must preserve the installed host env and systemd unit"
fi

if grep -Fq 'write_retired_producer_runtime_state "$service"' "${REMOTE_APPLY}" &&
   grep -Fq 'stop_retired_producer_runtime "$service"' "${REMOTE_APPLY}"; then
  pass "ap-host-native deploy records and stops the prior producer runtime before enabling collector"
else
  record_fail "ap-host-native deploy must record and stop the prior producer runtime"
fi

if grep -Fq 'validate_retired_producer_runtime_state "$producer_state_file" "$service"' "${ROLLBACK}" &&
   grep -Fq 'stop_named_units_and_require_inactive "$unit"' "${ROLLBACK}" &&
   grep -Fq 'restore_retired_producer_runtime "$producer_state_file" "$service"' "${ROLLBACK}"; then
  pass "ap-host-native rollback restores the recorded producer runtime on first cutover"
else
  record_fail "ap-host-native rollback must stop collector then restore the recorded producer runtime"
fi
if grep -Fq 'stop_named_units_and_require_inactive "$unit"' "${REMOTE_APPLY}"; then
  pass "ap-host-native failed cutover stops collector before restoring producer"
else
  record_fail "ap-host-native failed cutover must stop collector before restoring producer"
fi

capture_line="$(grep -nF '"$host_env" "$rollback_contract_dir/youtube-collector-host.env"' "${REMOTE_APPLY}" | head -1 | cut -d: -f1)"
install_line="$(grep -nF '"$payload/youtube-collector-host.env" "$host_env"' "${REMOTE_APPLY}" | head -1 | cut -d: -f1)"
if [[ -n "${capture_line}" && -n "${install_line}" ]] && (( capture_line < install_line )); then
  pass "ap-host-native deploy captures the old contract before installing the new contract"
else
  record_fail "ap-host-native deploy must capture the old contract before overwriting it"
fi

manifest_line="$(grep -nF '> rollback-contract/SHA256SUMS' "${REMOTE_APPLY}" | head -1 | cut -d: -f1)"
previous_line="$(grep -nF 'ln -sfn "$old_target" "$previous_link"' "${REMOTE_APPLY}" | head -1 | cut -d: -f1)"
if [[ -n "${manifest_line}" && -n "${previous_line}" && -n "${install_line}" ]] &&
   (( manifest_line < previous_line && manifest_line < install_line )); then
  pass "ap-host-native deploy seals the complete rollback payload before publishing previous"
else
  record_fail "ap-host-native deploy must seal rollback checksums before publishing previous or installing the new contract"
fi

status_line="$(grep -nF 'CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/logs/ap-host-native-status.sh" "$AP_NAME"' "${DEPLOY}" | tail -1 | cut -d: -f1)"
completion_line="$(grep -nF 'CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"' "${DEPLOY}" | tail -1 | cut -d: -f1)"
if [[ -n "${status_line}" && -n "${completion_line}" ]] && (( status_line < completion_line )); then
  pass "ap-host-native deploy runs the shared completion gate after status inspection"
else
  record_fail "ap-host-native deploy must forward change_started_at to the completion gate after status inspection"
fi

tmp="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp}"
}
trap cleanup EXIT
mkdir -p "${tmp}/bin" "${tmp}/success" "${tmp}/failure"
touch "${tmp}/KR.key"

if RELEASE_ID='../active' \
   ARTIFACT_DIR="${tmp}/traversal-artifact" \
   SSH_KEY="${tmp}/KR.key" \
   "${DEPLOY}" osaka --dry-run >"${tmp}/traversal.out" 2>"${tmp}/traversal.err"; then
  record_fail "ap-host-native deploy must reject RELEASE_ID traversal before building"
elif [[ -e "${tmp}/traversal-artifact" ]]; then
  record_fail "invalid RELEASE_ID must not create the artifact directory"
elif grep -Fq 'RELEASE_ID must be one safe path component' "${tmp}/traversal.err"; then
  pass "ap-host-native deploy rejects RELEASE_ID traversal before artifact creation"
else
  record_fail "invalid RELEASE_ID must report the safe component contract"
fi

release_root="${tmp}/releases"
mkdir -p "${release_root}/active" "${tmp}/outside"
ln -s "${release_root}/active" "${tmp}/current"
ln -s "${release_root}/active" "${release_root}/active-alias"
ln -s "${tmp}/outside" "${release_root}/escape-alias"

if [[ "$(native_release_dir_resolve "${release_root}" safe-release "${tmp}/current")" == "${release_root}/safe-release" ]]; then
  pass "host-native release resolver accepts a contained inactive release"
else
  record_fail "host-native release resolver must accept a contained inactive release"
fi
if native_release_dir_resolve "${release_root}" active-alias "${tmp}/current" >/dev/null 2>&1; then
  record_fail "host-native release resolver must reject a canonical alias of the active release"
else
  pass "host-native release resolver rejects a canonical alias of the active release"
fi
if native_release_dir_resolve "${release_root}" escape-alias "${tmp}/current" >/dev/null 2>&1; then
  record_fail "host-native release resolver must reject a canonical path outside releases root"
else
  pass "host-native release resolver rejects canonical containment escape"
fi

resolve_line="$(grep -nF 'native_release_dir_resolve "$releases_root" "$release_id" "$current_link"' "${REMOTE_APPLY}" | tail -1 | cut -d: -f1)"
delete_line="$(grep -nF 'rm -rf "$release_dir"' "${REMOTE_APPLY}" | tail -1 | cut -d: -f1)"
if [[ -n "${resolve_line}" && -n "${delete_line}" ]] && (( resolve_line < delete_line )); then
  pass "remote canonical release guard runs before release deletion"
else
  record_fail "remote canonical release guard must run before release deletion"
fi

mkdir -p "${tmp}/rollback-bin" "${tmp}/rollback-fixture/bin" \
  "${tmp}/rollback-fixture/internal/domain/data" "${tmp}/rollback-fixture/rollback-contract"
cat > "${tmp}/rollback-bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" != "-n" ]] || shift
exec "$@"
EOF
cat > "${tmp}/rollback-bin/systemd-analyze" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
unit="${2:?unit}"
if grep -Fq INVALID_UNIT "${unit}"; then
  exit 1
fi
EOF
chmod +x "${tmp}/rollback-bin/sudo" "${tmp}/rollback-bin/systemd-analyze"

rollback_fixture="${tmp}/rollback-fixture"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/youtube-collector"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/youtube-collector-wrapper"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/healthcheck"
mkdir -p "${rollback_fixture}/youtubejs/src"
printf 'export {}\n' > "${rollback_fixture}/youtubejs/src/server.mjs"
chmod +x "${rollback_fixture}/bin/youtube-collector" \
  "${rollback_fixture}/bin/youtube-collector-wrapper" \
  "${rollback_fixture}/bin/healthcheck"
printf 'fixture-data\n' > "${rollback_fixture}/internal/domain/data/members.json"
printf 'APP_ENV=production\n' > "${rollback_fixture}/rollback-contract/youtube-collector-host.env"
printf '[Unit]\nDescription=fixture\n' > "${rollback_fixture}/rollback-contract/hololive-youtube-collector@.service"
(
  cd "${rollback_fixture}"
  sha256sum \
    bin/youtube-collector \
    bin/youtube-collector-wrapper \
    bin/healthcheck \
    rollback-contract/youtube-collector-host.env \
    rollback-contract/hololive-youtube-collector@.service \
    internal/domain/data/members.json \
    > rollback-contract/SHA256SUMS
)

if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}"; then
  pass "native rollback validation accepts a complete integrity fixture"
else
  record_fail "native rollback validation must accept a complete integrity fixture"
fi

mv "${rollback_fixture}/bin/healthcheck" "${rollback_fixture}/bin/healthcheck.missing"
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/missing.out" 2>"${tmp}/missing.err"; then
  record_fail "native rollback validation must reject a missing healthcheck"
elif grep -Fq 'previous host-native executable is missing or not executable: bin/healthcheck' "${tmp}/missing.err"; then
  pass "native rollback validation rejects a missing healthcheck"
else
  record_fail "missing healthcheck validation must fail for the executable precondition"
fi
mv "${rollback_fixture}/bin/healthcheck.missing" "${rollback_fixture}/bin/healthcheck"

printf 'corrupt\n' >> "${rollback_fixture}/bin/youtube-collector"
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/corrupt.out" 2>"${tmp}/corrupt.err"; then
  record_fail "native rollback validation must reject a corrupt binary"
elif grep -Fq 'previous host-native rollback payload failed checksum validation' "${tmp}/corrupt.err"; then
  pass "native rollback validation rejects a corrupt binary"
else
  record_fail "corrupt binary validation must fail for the checksum precondition"
fi
sed -i '$d' "${rollback_fixture}/bin/youtube-collector"
chmod +x "${rollback_fixture}/bin/youtube-collector"

printf 'INVALID_UNIT\n' >> "${rollback_fixture}/rollback-contract/hololive-youtube-collector@.service"
(
  cd "${rollback_fixture}"
  sha256sum \
    bin/youtube-collector \
    bin/youtube-collector-wrapper \
    bin/healthcheck \
    rollback-contract/youtube-collector-host.env \
    rollback-contract/hololive-youtube-collector@.service \
    internal/domain/data/members.json \
    > rollback-contract/SHA256SUMS
)
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/invalid-unit.out" 2>"${tmp}/invalid-unit.err"; then
  record_fail "native rollback validation must reject an invalid systemd unit"
elif grep -Fq 'previous host-native systemd unit failed validation' "${tmp}/invalid-unit.err"; then
  pass "native rollback validation rejects an invalid systemd unit"
else
  record_fail "invalid unit validation must fail for the systemd precondition"
fi

if [[ "$(grep -Fc 'native_rollback_validate "$previous_target"' "${ROLLBACK}")" -eq 2 ]]; then
  pass "native rollback dry-run and apply share the payload integrity gate"
else
  record_fail "native rollback dry-run and apply must both invoke payload integrity validation"
fi

cat > "${tmp}/bin/ssh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
capture_dir="${AP_NATIVE_ROLLBACK_CAPTURE:?}"
counter="${capture_dir}/counter"
call=0
[[ ! -r "${counter}" ]] || call="$(<"${counter}")"
call=$((call + 1))
printf '%s\n' "${call}" > "${counter}"
printf '%s\n' "${!#}" > "${capture_dir}/call-${call}.cmd"
payload="$(cat)"
printf '%s\n' "${payload}" > "${capture_dir}/call-${call}.stdin"

if grep -Fq "printf '%s\\n' collector" <<<"${payload}"; then
  printf 'collector\n'
fi
if grep -Fq 'date -u +%Y-%m-%dT%H:%M:%SZ' <<<"${payload}"; then
  printf '2026-08-01T03:04:05Z\n'
fi
if [[ "${AP_NATIVE_ROLLBACK_FAIL_COMPLETION:-false}" == "true" ]] &&
   grep -Fq "collector AP completion check passed" <<<"${payload}"; then
  exit 77
fi
EOF
chmod +x "${tmp}/bin/ssh"

if PATH="${tmp}/bin:${PATH}" \
   SSH_KEY="${tmp}/KR.key" \
   AP_NATIVE_ROLLBACK_CAPTURE="${tmp}/success" \
   I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true \
   "${ROLLBACK}" osaka --apply >"${tmp}/success.out" 2>"${tmp}/success.err"; then
  pass "ap-host-native rollback completes only after the shared completion gate"
else
  cat "${tmp}/success.out"
  cat "${tmp}/success.err" >&2
  record_fail "ap-host-native rollback orchestration must succeed when restore and completion checks pass"
fi

restore_payload="${tmp}/success/call-3.stdin"
completion_cmd="${tmp}/success/call-5.cmd"
if [[ -r "${restore_payload}" ]] &&
   bash -n "${restore_payload}" &&
   grep -Fq 'native_rollback_validate "$previous_target"' "${restore_payload}" &&
   grep -Fq '"$rollback_contract_dir/youtube-collector-host.env" "$host_env"' "${restore_payload}" &&
   grep -Fq '"$rollback_contract_dir/hololive-youtube-collector@.service" "$unit_file"' "${restore_payload}" &&
   grep -Fq 'systemctl daemon-reload' "${restore_payload}" &&
   grep -Fq 'systemctl restart "$unit"' "${restore_payload}"; then
  pass "ap-host-native rollback restores binary-adjacent contract files before restarting"
else
  record_fail "ap-host-native rollback must restore the host env and systemd unit before restarting"
fi

validate_line="$(grep -nF 'native_rollback_validate "$previous_target"' "${restore_payload}" | tail -1 | cut -d: -f1)"
restore_line="$(grep -nF 'install -m 0640 -o root -g root "$rollback_contract_dir/youtube-collector-host.env"' "${restore_payload}" | tail -1 | cut -d: -f1)"
if [[ -n "${validate_line}" && -n "${restore_line}" ]] && (( validate_line < restore_line )); then
  pass "native rollback validates payload integrity and unit before mutation"
else
  record_fail "native rollback validation must run before the first restore mutation"
fi

if [[ -r "${completion_cmd}" ]] && grep -Fq '2026-08-01T03:04:05Z' "${completion_cmd}"; then
  pass "ap-host-native rollback forwards change_started_at to the completion gate"
else
  record_fail "ap-host-native rollback must forward change_started_at to the completion gate"
fi

if PATH="${tmp}/bin:${PATH}" \
   SSH_KEY="${tmp}/KR.key" \
   AP_NATIVE_ROLLBACK_CAPTURE="${tmp}/failure" \
   AP_NATIVE_ROLLBACK_FAIL_COMPLETION=true \
   I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true \
   "${ROLLBACK}" osaka --apply >"${tmp}/failure.out" 2>"${tmp}/failure.err"; then
  record_fail "ap-host-native rollback must fail when the completion gate fails"
else
  pass "ap-host-native rollback propagates completion gate failure"
fi

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all ap-host-native release checks passed"
