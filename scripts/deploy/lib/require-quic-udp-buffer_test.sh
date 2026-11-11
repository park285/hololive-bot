#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
ap_udp_lib="${ROOT_DIR}/scripts/deploy/lib/require-quic-udp-buffer.sh"
fixture_root="$(mktemp -d)"
trap 'rm -rf "${fixture_root}"' EXIT

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

pass() {
    echo "[PASS] $*"
}

# persisted 검증은 sysctl --system 적용 의미론(last-wins: sysctl.d lexical 순서 후 sysctl.conf 최종)을 따라야 한다.
mkdir -p "${fixture_root}/etc/sysctl.d"
printf 'net.core.rmem_max=2048\nnet.core.wmem_max=2048\n' > "${fixture_root}/etc/sysctl.d/10-high.conf"
printf 'net.core.rmem_max=512\nnet.core.wmem_max=512\n' > "${fixture_root}/etc/sysctl.d/90-low-override.conf"
if AP_SYSCTL_ROOT="${fixture_root}" bash "${ap_udp_lib}" 1024 fixture-host >/dev/null 2>&1; then
    fail "quic udp lib must fail when a later sysctl.d file overrides persisted buffers below the requirement (last-wins)"
fi
pass "quic udp lib rejects later-file low override (persisted last-wins)"

printf 'net.core.rmem_max=4096\nnet.core.wmem_max=4096\n' > "${fixture_root}/etc/sysctl.d/90-low-override.conf"
AP_SYSCTL_ROOT="${fixture_root}" bash "${ap_udp_lib}" 1024 fixture-host >/dev/null 2>&1 \
    || fail "quic udp lib must pass when the effective persisted value satisfies the requirement"
pass "quic udp lib accepts sufficient effective persisted value"

printf 'net.core.rmem_max=512\nnet.core.wmem_max=512\n' > "${fixture_root}/etc/sysctl.conf"
if AP_SYSCTL_ROOT="${fixture_root}" bash "${ap_udp_lib}" 1024 fixture-host >/dev/null 2>&1; then
    fail "quic udp lib must treat /etc/sysctl.conf as the final persisted assignment (sysctl --system order)"
fi
pass "quic udp lib applies sysctl.conf as final override"
