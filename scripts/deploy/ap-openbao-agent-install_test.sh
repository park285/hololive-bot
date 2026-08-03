#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
readonly INSTALLER="$SCRIPT_DIR/ap-openbao-agent-install.sh"
TEST_ROOT="$(mktemp -d)"
readonly TEST_ROOT
trap 'rm -rf -- "$TEST_ROOT"' EXIT

require_pattern() {
  local pattern="$1" label="$2"
  grep -Eq -- "$pattern" "$INSTALLER" || {
    printf 'missing AP installer contract: %s\n' "$label" >&2
    exit 1
  }
}

reject_pattern() {
  local pattern="$1" label="$2"
  if grep -Eq -- "$pattern" "$INSTALLER"; then
    printf 'forbidden AP installer contract: %s\n' "$label" >&2
    exit 1
  fi
}

assert_freshness_contract() {
  local installer="$1"
  grep -Eq 'previous_start_monotonic=.*ExecMainStartTimestampMonotonic' "$installer" || return 1
  grep -Eq 'previous_cert_generation=.*stat -c.*%Y:%i.*hololive-h3\.crt' "$installer" || return 1
  grep -Eq 'systemctl restart openbao-agent-hololive-bot\.service' "$installer" || return 1
  grep -Eq 'current_start_monotonic=.*ExecMainStartTimestampMonotonic' "$installer" || return 1
  grep -Eq 'current_cert_generation=.*stat -c.*%Y:%i.*hololive-h3\.crt' "$installer" || return 1
  grep -Eq 'current_cert_mtime >= apply_started_epoch' "$installer" || return 1
  grep -Eq 'candidate_cert_generation.*previous_cert_generation' "$installer" || return 1
  grep -Eq 'candidate_cert_mtime.*apply_started_epoch' "$installer" || return 1
  grep -Eq "systemctl restart \"\\\$consumer_unit\"" "$installer" || return 1
  grep -Eq "up -d --no-deps --force-recreate \"\\\$consumer_service\"" "$installer" || return 1
  grep -Fq 'container environment matches rendered Compose config' "$installer" || return 1
  grep -Eq 'State\.Health\.Status.*healthy' "$installer" || return 1
  grep -Eq 'CHANGE_STARTED_AT=.*ap-completion-check\.sh' "$installer" || return 1
  ! grep -Eq "docker restart \"\\\$consumer_container\"" "$installer" || return 1
}

assert_architecture_contract() {
  local installer="$1"
  grep -Eq 'remote_arch=.*AP_SSH.*uname -m' "$installer" || return 1
  grep -Fq '736b8ecf354fda6b2af62e4ae064f12fe6c52d7db8425b9c6de22f286a5485ec' "$installer" || return 1
  grep -Fq '994a10a7d42f750345ec815540747022cfc6b5a2dd707f535a6ea45d6eac2bfd' "$installer" || return 1
  grep -Fq 'artifacts/openbao/2.6.1/bao-linux-arm64' "$installer" || return 1
  grep -Eq 'actual_bao_sha256.*expected_bao_sha256' "$installer" || return 1
}

require_pattern 'useradd --system --user-group --no-create-home' 'dedicated system user'
require_pattern 'install -d -m 0711 -o root -g root /etc/openbao-agent' 'traversable credential directory'
require_pattern 'install -m 0400 -o openbao-agent-hololive-bot-ap -g openbao-agent-hololive-bot-ap .*hololive-bot-ap\.role_id' 'dedicated role ownership'
require_pattern 'install -m 0400 -o openbao-agent-hololive-bot-ap -g openbao-agent-hololive-bot-ap .*hololive-bot-ap\.secret_id' 'dedicated secret ownership'
require_pattern 'install -d -m 0750 -o openbao-agent-hololive-bot-ap -g opc /run/hololive-bot /run/hololive-bot/certs' 'Agent-owned certificate directory'
require_pattern 'REUSE_REMOTE_OPENBAO_CREDENTIALS.*:-false' 'explicit remote credential reuse gate'
require_pattern "test -s \"/etc/openbao-agent/\\\$credential\"" 'remote credential non-empty check'
require_pattern "stat -c '%a %U %G'.*/etc/openbao-agent/\\\$credential" 'remote credential metadata check'
require_pattern 'trap cleanup_remote EXIT' 'remote payload cleanup'
reject_pattern 'install -m 0600 -o root -g root .*hololive-bot-ap\.secret_id' 'root-owned secret'
assert_freshness_contract "$INSTALLER" || {
  printf 'missing AP installer contract: restart and fresh render generation\n' >&2
  exit 1
}
assert_architecture_contract "$INSTALLER" || {
  printf 'missing AP installer contract: target architecture and approved binary hash\n' >&2
  exit 1
}

cp -- "$INSTALLER" "$TEST_ROOT/active-reapply-mutant.sh"
sed -i '/systemctl restart openbao-agent-hololive-bot\.service/d' "$TEST_ROOT/active-reapply-mutant.sh"
if assert_freshness_contract "$TEST_ROOT/active-reapply-mutant.sh"; then
  printf 'active-unit reapply without restart unexpectedly passed\n' >&2
  exit 1
fi

cp -- "$INSTALLER" "$TEST_ROOT/readiness-mutant.sh"
sed -i '/CHANGE_STARTED_AT=.*ap-completion-check\.sh/d' "$TEST_ROOT/readiness-mutant.sh"
if assert_freshness_contract "$TEST_ROOT/readiness-mutant.sh"; then
  printf 'installer without canonical completion gate unexpectedly passed\n' >&2
  exit 1
fi

cp -- "$INSTALLER" "$TEST_ROOT/architecture-mutant.sh"
sed -i '/remote_arch=.*uname -m/d' "$TEST_ROOT/architecture-mutant.sh"
if assert_architecture_contract "$TEST_ROOT/architecture-mutant.sh"; then
  printf 'installer without remote architecture detection unexpectedly passed\n' >&2
  exit 1
fi

printf 'AP OpenBao Agent installer contract passed\n'
