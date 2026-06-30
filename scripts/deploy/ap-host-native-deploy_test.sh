#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="${ROOT_DIR}/scripts/deploy/ap-host-native-deploy.sh"

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

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all ap-host-native H3 bind checks passed (8c2e3ef9)"
