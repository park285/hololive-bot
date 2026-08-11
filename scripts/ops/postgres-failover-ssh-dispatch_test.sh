#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
DISPATCH="${SCRIPT_DIR}/postgres-failover-ssh-dispatch.sh"
SSHD_CONFIG="${SCRIPT_DIR}/hololive-postgres-failover.sshd.conf"
TMP_DIR="$(mktemp -d /tmp/postgres-failover-ssh-dispatch-test.XXXXXX)"
failures=0

cleanup() {
  rm -rf -- "${TMP_DIR}"
}
trap cleanup EXIT

pass() {
  printf '[PASS] %s\n' "$1"
}

fail() {
  printf '[FAIL] %s\n' "$1" >&2
  failures=$((failures + 1))
}

rejects_arbitrary_and_cross_role_commands() {
  local command valid_fence startup_dir="${TMP_DIR}/startup" startup_marker="${TMP_DIR}/startup-ran"
  mkdir -p "${startup_dir}"
  printf ': >%s\n' "${startup_marker}" >"${startup_dir}/.bashrc"
  printf ': >%s\n' "${startup_marker}" >"${startup_dir}/env"
  for command in 'id' 'bash -c whoami' '/usr/bin/sudo -n /usr/bin/env bash /usr/local/libexec/hololive-postgres-failover/postgres-route-tailscale.sh --config /etc/hololive-postgres-failover/route.env 100.100.1.8 5433 100.100.1.5 5434 fence-token-1234 svc:hololive-postgres'; do
    if HOME="${startup_dir}" BASH_ENV="${startup_dir}/env" ENV="${startup_dir}/env" \
      SSH_ORIGINAL_COMMAND="${command}" /bin/dash -c "${DISPATCH} fence" >/dev/null 2>&1; then
      fail "fence dispatcher accepted unauthorized command: ${command}"
      return
    fi
  done
  if [[ -e "${startup_marker}" ]]; then
    fail "login shell or dispatcher executed a user-controlled startup file"
    return
  fi
  if SSH_ORIGINAL_COMMAND=$'id\nuname' "${DISPATCH}" route >/dev/null 2>&1; then
    fail "route dispatcher accepted a multiline command"
    return
  fi
  valid_fence='/usr/bin/sudo -n /usr/bin/env bash /usr/local/libexec/hololive-postgres-failover/postgres-primary-fence.sh request-token-1234 100.100.1.8 100.100.1.5 5434 svc:hololive-postgres'
  if SSH_ORIGINAL_COMMAND="${valid_fence}" "${DISPATCH}" fence >/dev/null 2>&1; then
    fail "fence dispatcher accepted the fixed command from the wrong account"
    return
  fi
  pass "server dispatcher rejects arbitrary, cross-role, and multiline commands"
}

sshd_match_blocks_are_command_confined() {
  local fence route test_config="${TMP_DIR}/sshd_config" host_key="${TMP_DIR}/ssh_host_ed25519_key"
  if [[ ! -x /usr/sbin/sshd || ! -x /usr/bin/ssh-keygen ]]; then
    fail "sshd or ssh-keygen is unavailable for command-confinement validation"
    return
  fi
  /usr/bin/ssh-keygen -q -t ed25519 -N '' -f "${host_key}" || {
    fail "temporary sshd host key generation failed"
    return
  }
  {
    printf 'HostKey %s\n' "${host_key}"
    printf 'Include %s\n' "${SSHD_CONFIG}"
  } >"${test_config}"
  fence="$(/usr/sbin/sshd -T -f "${test_config}" -C user=hololive-pg-fence,host=localhost,addr=127.0.0.1)" || {
    fail "fence sshd Match block is invalid"
    return
  }
  route="$(/usr/sbin/sshd -T -f "${test_config}" -C user=hololive-pg-route,host=localhost,addr=127.0.0.1)" || {
    fail "route sshd Match block is invalid"
    return
  }
  if ! grep -Fqx 'authorizedkeysfile /etc/ssh/authorized_keys/hololive-pg-fence' <<<"${fence}" \
    || ! grep -Fqx 'forcecommand /usr/local/libexec/hololive-postgres-failover/postgres-failover-ssh-dispatch.sh fence' <<<"${fence}" \
    || ! grep -Fqx 'disableforwarding yes' <<<"${fence}" \
    || ! grep -Fqx 'permituserenvironment no' <<<"${fence}"; then
    fail "fence account is not fully command-confined"
    return
  fi
  if ! grep -Fqx 'authorizedkeysfile /etc/ssh/authorized_keys/hololive-pg-route' <<<"${route}" \
    || ! grep -Fqx 'forcecommand /usr/local/libexec/hololive-postgres-failover/postgres-failover-ssh-dispatch.sh route' <<<"${route}" \
    || ! grep -Fqx 'disableforwarding yes' <<<"${route}" \
    || ! grep -Fqx 'permituserenvironment no' <<<"${route}"; then
    fail "route account is not fully command-confined"
    return
  fi
  if grep -Eq '^acceptenv (BASH_ENV|ENV|PATH)$' <<<"${fence}" \
    || grep -Eq '^acceptenv (BASH_ENV|ENV|PATH)$' <<<"${route}"; then
    fail "sshd accepts a shell startup environment variable"
    return
  fi
  grep -Fq '/var/empty /bin/dash' "${SCRIPT_DIR}/../systemd/hololive-postgres-fence.sysusers.conf" \
    || { fail "fence account does not use the root-owned empty home and dash"; return; }
  grep -Fq '/var/empty /bin/dash' "${SCRIPT_DIR}/../systemd/hololive-postgres-route.sysusers.conf" \
    || { fail "route account does not use the root-owned empty home and dash"; return; }
  pass "sshd resolves root-owned keys and forced commands for both accounts"
}

rejects_arbitrary_and_cross_role_commands
sshd_match_blocks_are_command_confined

if (( failures > 0 )); then
  printf '%d postgres failover SSH dispatch test(s) failed\n' "${failures}" >&2
  exit 1
fi
