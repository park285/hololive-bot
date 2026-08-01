#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="${ROOT_DIR}/scripts/deploy/ap-host-native-deploy.sh"
ROLLBACK="${ROOT_DIR}/scripts/deploy/ap-host-native-rollback.sh"

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

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

if grep -Fq 'SETTINGS_DIR=/var/lib/hololive-bot/youtube-producer/settings' "${DEPLOY}"; then
  pass "ap-host-native settings dir uses persistent varlib path"
else
  record_fail "ap-host-native settings dir must not default to read-only release data"
fi

if grep -Fq 'ReadWritePaths=/var/lib/hololive-bot' "${DEPLOY}" &&
   grep -Fq 'install -d -m 0750 -o hololive -g opc /var/lib/hololive-bot/youtube-producer/settings' "${DEPLOY}"; then
  pass "ap-host-native settings dir is writable for hololive"
else
  record_fail "ap-host-native settings dir must be created and writable under systemd hardening"
fi

if grep -Fq 'rollback_contract_dir="$old_target/rollback-contract"' "${DEPLOY}" &&
   grep -Fq '"$host_env" "$rollback_contract_dir/youtube-producer-host.env"' "${DEPLOY}" &&
   grep -Fq '"$unit_file" "$rollback_contract_dir/hololive-youtube-producer@.service"' "${DEPLOY}"; then
  pass "ap-host-native deploy preserves the installed host env and systemd unit with the previous release"
else
  record_fail "ap-host-native deploy must preserve the installed host env and systemd unit"
fi

capture_line="$(grep -nF '"$host_env" "$rollback_contract_dir/youtube-producer-host.env"' "${DEPLOY}" | head -1 | cut -d: -f1)"
install_line="$(grep -nF '"$payload/youtube-producer-host.env" "$host_env"' "${DEPLOY}" | head -1 | cut -d: -f1)"
if [[ -n "${capture_line}" && -n "${install_line}" ]] && (( capture_line < install_line )); then
  pass "ap-host-native deploy captures the old contract before installing the new contract"
else
  record_fail "ap-host-native deploy must capture the old contract before overwriting it"
fi

tmp="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp}"
}
trap cleanup EXIT
mkdir -p "${tmp}/bin" "${tmp}/success" "${tmp}/failure"
touch "${tmp}/KR.key"

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

if grep -Fq 'date -u +%Y-%m-%dT%H:%M:%SZ' <<<"${payload}"; then
  printf '2026-08-01T03:04:05Z\n'
fi
if [[ "${AP_NATIVE_ROLLBACK_FAIL_COMPLETION:-false}" == "true" ]] &&
   grep -Fq "active-active completion check passed" <<<"${payload}"; then
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

restore_payload="${tmp}/success/call-2.stdin"
completion_cmd="${tmp}/success/call-4.cmd"
if [[ -r "${restore_payload}" ]] &&
   bash -n "${restore_payload}" &&
   grep -Fq 'previous host-native release is unavailable; refusing partial rollback' "${restore_payload}" &&
   grep -Fq 'previous host-native rollback contract is incomplete; refusing partial rollback' "${restore_payload}" &&
   grep -Fq '"$rollback_contract_dir/youtube-producer-host.env" "$host_env"' "${restore_payload}" &&
   grep -Fq '"$rollback_contract_dir/hololive-youtube-producer@.service" "$unit_file"' "${restore_payload}" &&
   grep -Fq 'systemctl daemon-reload' "${restore_payload}" &&
   grep -Fq 'systemctl restart "$unit"' "${restore_payload}"; then
  pass "ap-host-native rollback restores binary-adjacent contract files before restarting"
else
  record_fail "ap-host-native rollback must restore the host env and systemd unit before restarting"
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
