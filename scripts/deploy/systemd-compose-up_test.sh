#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
UNIT="${ROOT_DIR}/scripts/systemd/hololive-compose.service"
SYSTEMD_DIR="${ROOT_DIR}/scripts/systemd"

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

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

FIREWALL_UNIT="${SYSTEMD_DIR}/admin-dashboard-ingress-firewall.service"
FIREWALL_RULES="${SYSTEMD_DIR}/admin-dashboard-ingress.nft"
LIVE_COMPAT="${ROOT_DIR}/deploy/compose/docker-compose.live-compat.yml"
if [[ ! -f "${FIREWALL_UNIT}" || ! -f "${FIREWALL_RULES}" ]]; then
  record_fail "public ingress firewall unit and nft rules must be tracked"
elif ! grep -Fq 'Requires=docker.service admin-dashboard-ingress-firewall.service' "${UNIT}"; then
  record_fail "hololive-compose must fail closed when the ingress firewall cannot start"
elif ! grep -Fq 'ip saddr 100.100.1.5' "${FIREWALL_RULES}"; then
  record_fail "ingress firewall must restrict the Tailscale source set"
elif ! grep -A20 '^  admin-dashboard-ingress:' "${LIVE_COMPAT}" | grep -Fq 'network_mode: host'; then
  record_fail "ingress firewall input hook requires the ingress container to remain host-networked"
elif ! grep -Fq 'type filter hook input' "${FIREWALL_RULES}"; then
  record_fail "host-networked ingress must be filtered on the host input hook before HTTP parsing"
else
  pass "host-networked public ingress is filtered on input and required before compose"
fi

if (( failures > 0 )); then
  echo "systemd compose wrapper checks failed: ${failures}" >&2
  exit 1
fi

echo "systemd compose wrapper checks passed"
