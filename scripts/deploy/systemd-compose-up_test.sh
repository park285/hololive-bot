#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
UNIT="${ROOT_DIR}/scripts/systemd/hololive-compose.service"
WRAPPER="${ROOT_DIR}/scripts/deploy/systemd-compose-up.sh"
SYSTEMD_DIR="${ROOT_DIR}/scripts/systemd"

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

if grep -q 'HOLOLIVE_EXEC_TREE_ENFORCE' "${WRAPPER}"; then
  record_fail "root exec-tree ownership enforcement must be fail-closed, not opt-in (4d57f81c/03e6dca8)"
else
  pass "root exec-tree ownership enforcement has no opt-in bypass"
fi

if grep -Eq '^WorkingDirectory=/home/' "${UNIT}"; then
  record_fail "root systemd unit must not use a mutable home-tree WorkingDirectory (ee1c9a5b)"
else
  pass "root systemd unit avoids mutable home-tree WorkingDirectory"
fi

if ! grep -Eq '^Exec(Start|Stop)=/usr/local/sbin/hololive-compose-' "${UNIT}"; then
  record_fail "root systemd unit must enter through root-owned /usr/local/sbin wrappers"
else
  pass "root systemd unit enters through /usr/local/sbin wrappers"
fi

while IFS= read -r service; do
  user="$(awk -F= '/^User=/{print $2; exit}' "${service}")"
  if [[ -n "${user}" && "${user}" != "root" ]]; then
    continue
  fi
  if grep -Eq '^(WorkingDirectory|ExecStart|ExecStop)=/(home|root/work)' "${service}"; then
    record_fail "$(basename "${service}") root unit references mutable home/root-work paths"
  fi
done < <(find "${SYSTEMD_DIR}" -maxdepth 1 -type f -name '*.service' | sort)

if (( failures == 0 )); then
  pass "root systemd units avoid mutable home/root-work paths"
fi

if (( failures > 0 )); then
  echo "systemd compose wrapper checks failed: ${failures}" >&2
  exit 1
fi

echo "systemd compose wrapper checks passed"
