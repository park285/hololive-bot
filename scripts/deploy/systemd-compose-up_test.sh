#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
UNIT="${ROOT_DIR}/scripts/systemd/hololive-compose.service"
WRAPPER="${ROOT_DIR}/scripts/deploy/systemd-compose-up.sh"
DOWN_WRAPPER="${ROOT_DIR}/scripts/deploy/systemd-compose-down.sh"
SYSTEMD_DIR="${ROOT_DIR}/scripts/systemd"

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

if grep -q 'HOLOLIVE_EXEC_TREE_ENFORCE' "${WRAPPER}"; then
  record_fail "root exec-tree ownership enforcement must be fail-closed, not opt-in (4d57f81c/03e6dca8)"
else
  pass "root exec-tree ownership enforcement has no opt-in bypass"
fi

if [[ ! -f "${DOWN_WRAPPER}" ]]; then
  record_fail "root systemd ExecStop wrapper source is missing"
elif grep -Eq '/(home|root/work)' "${DOWN_WRAPPER}"; then
  record_fail "root systemd ExecStop wrapper source must not reference mutable home/root-work paths"
elif ! grep -q 'verify-exec-tree-ownership.sh' "${DOWN_WRAPPER}"; then
  record_fail "root systemd ExecStop wrapper must enforce root-owned deploy tree"
else
  pass "root systemd ExecStop wrapper source is root-exec-tree guarded"
fi

for wrapper in "${WRAPPER}" "${DOWN_WRAPPER}"; do
  wrapper_name="$(basename "${wrapper}")"
  if ! grep -q 'root verifier is writable by a non-root user' "${wrapper}"; then
    record_fail "${wrapper_name} must self-check the verifier's own ownership before running it as root (4d57f81c)"
  else
    pass "${wrapper_name} self-checks the verifier before root execution"
  fi
done

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

SYNC="${ROOT_DIR}/scripts/deploy/sync-opt-current.sh"
if [[ ! -f "${SYNC}" ]]; then
  record_fail "sync-opt-current.sh source is missing"
elif grep -nE '^[[:space:]]*install[[:space:]].*\$REPO_ROOT.*(systemd-compose-(up|down)\.sh|hololive-compose\.service\.d)' "${SYNC}" >&2; then
  record_fail "sync-opt-current.sh must install root systemd material from \$STAGING (git archive HEAD), not \$REPO_ROOT — working-tree untracked .conf injection is a root drop-in LPE"
else
  pass "sync-opt-current.sh installs root systemd material from the git archive snapshot, not the working tree"
fi

if (( failures > 0 )); then
  echo "systemd compose wrapper checks failed: ${failures}" >&2
  exit 1
fi

echo "systemd compose wrapper checks passed"
