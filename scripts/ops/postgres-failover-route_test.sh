#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CONTROLLER="${SCRIPT_DIR}/postgres-failover-route-ssh.sh"
HELPER="${SCRIPT_DIR}/postgres-route-tailscale.sh"
TMP_DIR="$(mktemp -d /tmp/postgres-route-test.XXXXXX)"
failures=0
if [[ "$(/usr/bin/id -u)" == 0 ]]; then
  CONTROLLER_TEST_MODE=0
else
  CONTROLLER_TEST_MODE=1
fi

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

new_case() {
  local root="$1"
  mkdir -p "${root}/bin" "${root}/state"
  chmod 0700 "${root}" "${root}/state"
  printf 'route-key\n' >"${root}/route-key"
  printf '100.100.1.5 ssh-ed25519 AAAA\n' >"${root}/known-hosts"
  printf 'password-file\n' >"${root}/pgpass"
  printf 'ca\n' >"${root}/ca.pem"
  chmod 0600 "${root}/route-key" "${root}/pgpass"
  chmod 0644 "${root}/known-hosts" "${root}/ca.pem"
  cat >"${root}/bin/ssh" <<'FAKE_SSH'
#!/usr/bin/env bash
set -u
printf '%s\n' "$*" >>"${FAKE_SSH_LOG:?}"
printf '%s\n' "${FAKE_SSH_ACK:-ROUTED|100.100.1.5:5434|fence-token-1234}"
FAKE_SSH
  cat >"${root}/bin/tailscale" <<'FAKE_TAILSCALE'
#!/usr/bin/env bash
set -u
printf '%s\n' "$*" >>"${FAKE_TAILSCALE_LOG:?}"
  if [[ "${FAKE_TAILSCALE_FAIL:-0}" == 1 && "${1:-}" == serve && ( "${2:-}" == --service=* || ( "${2:-}" == --yes && "${3:-}" == --service=* ) ) ]]; then
    exit 1
  fi
  if [[ "${1:-}" == serve && "${2:-}" == --yes ]]; then
    printf '%s\n' "${5:?}" >"${FAKE_TAILSCALE_CONFIG:?}"
    printf '1\n' >"${FAKE_TAILSCALE_ADVERTISED:?}"
    exit 0
  fi
  case "${1:-} ${2:-}" in
  "serve --service="*)
    printf '%s\n' "${4:?}" >"${FAKE_TAILSCALE_CONFIG:?}"
    ;;
  "serve get-config")
    target=""
    advertised=true
    [[ -r "${FAKE_TAILSCALE_CONFIG:?}" ]] && target="$(<"${FAKE_TAILSCALE_CONFIG}")"
    [[ -r "${FAKE_TAILSCALE_ADVERTISED:?}" && "$(<"${FAKE_TAILSCALE_ADVERTISED}")" == 0 ]] && advertised=false
    [[ "${FAKE_TAILSCALE_CONFIG_MISMATCH:-0}" == 0 ]] || target='tcp://100.100.1.7:5434'
    [[ "${FAKE_TAILSCALE_FORCE_UNADVERTISED:-0}" == 0 ]] || advertised=false
    printf '{"version":"0.0.1","services":{"svc:hololive-postgres":{"endpoints":{"tcp:5433":"%s"},"advertised":%s}}}\n' "${target}" "${advertised}"
    ;;
  *)
    exit 2
    ;;
esac
FAKE_TAILSCALE
  cat >"${root}/bin/ip" <<'FAKE_IP'
#!/usr/bin/env bash
set -u
printf '2: tailscale0    inet %s/32 scope global tailscale0\n' "${FAKE_LOCAL_IP:-100.100.1.5}"
FAKE_IP
  cat >"${root}/bin/psql" <<'FAKE_PSQL'
#!/usr/bin/env bash
set -u
printf 'PGSSLMODE=%s PGPASSFILE=%s PGSSLROOTCERT=%s args=%s\n' "${PGSSLMODE:-}" "${PGPASSFILE:-}" "${PGSSLROOTCERT:-}" "$*" >>"${FAKE_PSQL_LOG:?}"
if [[ "${FAKE_PSQL_FAIL:-0}" == 1 ]]; then
  exit 1
fi
if [[ "${FAKE_PSQL_FAIL_SERVICE:-0}" == 1 && "$*" == *"-h hololive-postgres.example.ts.net"* ]]; then
  exit 1
fi
if [[ "${FAKE_PSQL_FAIL_SERVICE_ONCE:-0}" == 1 && "$*" == *"-h hololive-postgres.example.ts.net"* && ! -e "${FAKE_PSQL_FAIL_MARKER:?}" ]]; then
  : >"${FAKE_PSQL_FAIL_MARKER}"
  exit 1
fi
printf '%s\n' "${FAKE_PSQL_OUTPUT:-f|off}"
FAKE_PSQL
  cat >"${root}/bin/sync" <<'FAKE_SYNC'
#!/usr/bin/env bash
set -u
if [[ "${FAKE_SYNC_FAIL_STATE_ONCE:-0}" == 1 && "${2:-}" == "${FAKE_SYNC_STATE_FILE:?}" && ! -e "${FAKE_SYNC_FAIL_MARKER:?}" ]]; then
  : >"${FAKE_SYNC_FAIL_MARKER}"
  exit 1
fi
exec /usr/bin/sync "$@"
FAKE_SYNC
  chmod 0755 "${root}/bin/ssh" "${root}/bin/tailscale" "${root}/bin/ip" "${root}/bin/psql" "${root}/bin/sync"
  : >"${root}/ssh.log"
  : >"${root}/tailscale.log"
  : >"${root}/psql.log"
  : >"${root}/tailscale-config"
  printf '1\n' >"${root}/tailscale-advertised"
}

write_config() {
  local root="$1"
  local config="${root}/route.conf"
  cat >"${config}" <<EOF_CONFIG
POSTGRES_FAILOVER_TAILSCALE_SERVICE=svc:hololive-postgres
POSTGRES_FAILOVER_ROUTE_SERVICE_PORT=5433
POSTGRES_FAILOVER_ROUTE_SERVICE_DNS=hololive-postgres.example.ts.net
POSTGRES_FAILOVER_ROUTE_PGPASS_FILE=${root}/pgpass
POSTGRES_FAILOVER_ROUTE_CA_FILE=${root}/ca.pem
POSTGRES_FAILOVER_ROUTE_TAILSCALE_PATH=${root}/bin/tailscale
POSTGRES_FAILOVER_ROUTE_PSQL_PATH=${root}/bin/psql
POSTGRES_FAILOVER_ROUTE_IP_PATH=${root}/bin/ip
POSTGRES_FAILOVER_ROUTE_STATE_FILE=${root}/state/route.state
POSTGRES_FAILOVER_ROUTE_JQ_PATH=/usr/bin/jq
POSTGRES_FAILOVER_ROUTE_SYNC_PATH=${root}/bin/sync
EOF_CONFIG
  chmod 0600 "${config}"
  printf '%s\n' "${config}"
}

run_helper() {
  local root="$1" config="$2"
  shift 2
  env \
    FAKE_TAILSCALE_LOG="${root}/tailscale.log" \
    FAKE_TAILSCALE_CONFIG="${root}/tailscale-config" \
    FAKE_TAILSCALE_ADVERTISED="${root}/tailscale-advertised" \
    FAKE_PSQL_LOG="${root}/psql.log" \
    FAKE_PSQL_FAIL_MARKER="${root}/psql-fail-marker" \
    FAKE_SYNC_STATE_FILE="${root}/state/route.state" \
    FAKE_SYNC_FAIL_MARKER="${root}/sync-fail-marker" \
    /usr/bin/bash "${HELPER}" --config "${config}" 100.100.1.8 5433 100.100.1.5 5434 fence-token-1234 svc:hololive-postgres "$@"
}

controller_accepts_exact_ack() {
  local root="${TMP_DIR}/controller-ack"
  new_case "${root}"
  if ! output="$(
    PATH="${root}/bin:${PATH}" \
    FAKE_SSH_LOG="${root}/ssh.log" \
    POSTGRES_FAILOVER_OLD_PRIMARY_HOST=100.100.1.8 \
    POSTGRES_FAILOVER_OLD_PRIMARY_PORT=5433 \
    POSTGRES_FAILOVER_NEW_PRIMARY_HOST=100.100.1.5 \
    POSTGRES_FAILOVER_NEW_PRIMARY_PORT=5434 \
    POSTGRES_FAILOVER_FENCE_TOKEN=fence-token-1234 \
    POSTGRES_FAILOVER_TAILSCALE_SERVICE=svc:hololive-postgres \
    POSTGRES_FAILOVER_ROUTE_SSH_TARGET=hololive-pg-route@100.100.1.5 \
    POSTGRES_FAILOVER_ROUTE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    POSTGRES_FAILOVER_ROUTE_SSH_IDENTITY_FILE="${root}/route-key" \
    POSTGRES_FAILOVER_ROUTE_SSH_KNOWN_HOSTS_FILE="${root}/known-hosts" \
    POSTGRES_FAILOVER_ROUTE_SSH_HOST_KEY_ALIAS=100.100.1.5 \
    POSTGRES_FAILOVER_ROUTE_SSH_CONNECT_TIMEOUT_SEC=5 \
    POSTGRES_FAILOVER_ROUTE_REMOTE_SCRIPT=/usr/local/libexec/hololive-postgres-failover/postgres-route-tailscale.sh \
    POSTGRES_FAILOVER_ROUTE_CONFIG_FILE=/etc/hololive-postgres-failover/route.env \
    /usr/bin/bash "${CONTROLLER}"
  )"; then
    fail "controller rejected exact route acknowledgement"
    return
  fi
  [[ "${output}" == ROUTED\|100.100.1.5:5434\|fence-token-1234 ]] || { fail "controller returned a transformed acknowledgement"; return; }
  grep -Fq -- '/usr/bin/sudo -n /usr/bin/env bash /usr/local/libexec/hololive-postgres-failover/postgres-route-tailscale.sh --config /etc/hololive-postgres-failover/route.env 100.100.1.8 5433 100.100.1.5 5434 fence-token-1234 svc:hololive-postgres' "${root}/ssh.log" || { fail "controller did not invoke the fixed remote helper boundary"; return; }
  pass "controller accepts and returns the exact helper acknowledgement"
}

controller_rejects_invalid_ack_and_input() {
  local root="${TMP_DIR}/controller-invalid"
  new_case "${root}"
  if FAKE_SSH_ACK='ROUTED|100.100.1.5:5434|wrong-token' \
    PATH="${root}/bin:${PATH}" FAKE_SSH_LOG="${root}/ssh.log" \
    POSTGRES_FAILOVER_OLD_PRIMARY_HOST=100.100.1.8 POSTGRES_FAILOVER_OLD_PRIMARY_PORT=5433 \
    POSTGRES_FAILOVER_NEW_PRIMARY_HOST=100.100.1.5 POSTGRES_FAILOVER_NEW_PRIMARY_PORT=5434 \
    POSTGRES_FAILOVER_FENCE_TOKEN=fence-token-1234 POSTGRES_FAILOVER_TAILSCALE_SERVICE=svc:hololive-postgres POSTGRES_FAILOVER_ROUTE_SSH_TARGET=hololive-pg-route@100.100.1.5 POSTGRES_FAILOVER_ROUTE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    POSTGRES_FAILOVER_ROUTE_SSH_IDENTITY_FILE="${root}/route-key" POSTGRES_FAILOVER_ROUTE_SSH_KNOWN_HOSTS_FILE="${root}/known-hosts" \
    POSTGRES_FAILOVER_ROUTE_REMOTE_SCRIPT=/usr/local/libexec/hololive-postgres-failover/postgres-route-tailscale.sh \
    POSTGRES_FAILOVER_ROUTE_CONFIG_FILE=/etc/hololive-postgres-failover/route.env \
    /usr/bin/bash "${CONTROLLER}" >/dev/null 2>&1; then
    fail "controller accepted an invalid helper acknowledgement"
    return
  fi
  if PATH="${root}/bin:${PATH}" FAKE_SSH_LOG="${root}/ssh.log" \
    POSTGRES_FAILOVER_OLD_PRIMARY_HOST=100.100.1.8 POSTGRES_FAILOVER_OLD_PRIMARY_PORT=bad \
    POSTGRES_FAILOVER_NEW_PRIMARY_HOST=100.100.1.5 POSTGRES_FAILOVER_NEW_PRIMARY_PORT=5434 \
    POSTGRES_FAILOVER_FENCE_TOKEN=fence-token-1234 POSTGRES_FAILOVER_TAILSCALE_SERVICE=svc:hololive-postgres POSTGRES_FAILOVER_ROUTE_SSH_TARGET=hololive-pg-route@100.100.1.5 POSTGRES_FAILOVER_ROUTE_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    POSTGRES_FAILOVER_ROUTE_SSH_IDENTITY_FILE="${root}/route-key" POSTGRES_FAILOVER_ROUTE_SSH_KNOWN_HOSTS_FILE="${root}/known-hosts" \
    /usr/bin/bash "${CONTROLLER}" >/dev/null 2>&1; then
    fail "controller accepted invalid route input"
    return
  fi
  pass "controller rejects invalid acknowledgement and route input"
}

helper_rejects_config_ownership_mode_and_symlink() {
  local root="${TMP_DIR}/helper-config" config target non_root_uid non_root_gid
  new_case "${root}"
  config="$(write_config "${root}")"
  non_root_uid="$(awk -F: '$3 >= 1000 && $3 != 65534 {print $3; exit}' /etc/passwd)"
  if [[ -n "${non_root_uid}" ]]; then
    non_root_gid="$(awk -F: -v uid="${non_root_uid}" '$3 == uid {print $4; exit}' /etc/passwd)"
    chown "${non_root_uid}:${non_root_gid}" "${config}"
    if run_helper "${root}" "${config}" >/dev/null 2>&1; then
      fail "helper accepted a non-root-owned route config"
      return
    fi
    chown 0:0 "${config}"
  fi
  chmod 0664 "${config}"
  if run_helper "${root}" "${config}" >/dev/null 2>&1; then
    fail "helper accepted a group/world-writable route config"
    return
  fi
  chmod 0600 "${config}"
  target="${root}/config-target"
  cp -- "${config}" "${target}"
  rm -f -- "${config}"
  ln -s -- "${target}" "${config}"
  if run_helper "${root}" "${config}" >/dev/null 2>&1; then
    fail "helper accepted a symlinked route config"
    return
  fi
  pass "helper rejects unsafe route config ownership/mode paths"
}

helper_rejects_wrong_local_host() {
  local root="${TMP_DIR}/helper-host" config
  new_case "${root}"
  config="$(write_config "${root}")"
  if FAKE_LOCAL_IP=100.100.1.6 run_helper "${root}" "${config}" >/dev/null 2>&1; then
    fail "helper accepted a non-local Tailscale target"
    return
  fi
  [[ ! -s "${root}/tailscale.log" ]] || { fail "helper configured Tailscale before validating local ownership"; return; }
  pass "helper rejects a target that is not a local Tailscale IP"
}

helper_rejects_tailscale_failure() {
  local root="${TMP_DIR}/helper-tailscale" config
  new_case "${root}"
  config="$(write_config "${root}")"
  if FAKE_TAILSCALE_FAIL=1 run_helper "${root}" "${config}" >/dev/null 2>&1; then
    fail "helper accepted a failed Tailscale configuration"
    return
  fi
  [[ ! -e "${root}/state/route.state" ]] || { fail "failed Tailscale configuration wrote durable route state"; return; }
  pass "helper fails closed on Tailscale configuration failure"
}

helper_rejects_read_only_probe() {
  local root="${TMP_DIR}/helper-read-only" config
  new_case "${root}"
  config="$(write_config "${root}")"
  if FAKE_PSQL_OUTPUT='f|on' run_helper "${root}" "${config}" >/dev/null 2>&1; then
    fail "helper accepted a read-only service probe"
    return
  fi
  [[ ! -e "${root}/state/route.state" ]] || { fail "read-only probe wrote durable route state"; return; }
  [[ ! -s "${root}/tailscale-config" ]] || { fail "helper changed the service before probing the candidate"; return; }
  pass "helper requires the exact f|off PostgreSQL probe result"
}

helper_rolls_back_failed_service_probe() {
  local root="${TMP_DIR}/helper-rollback" config
  new_case "${root}"
  config="$(write_config "${root}")"
  if FAKE_PSQL_FAIL_SERVICE_ONCE=1 run_helper "${root}" "${config}" >/dev/null 2>&1; then
    fail "helper accepted a failed post-switch service probe"
    return
  fi
  [[ "$(<"${root}/tailscale-config")" == "tcp://100.100.1.8:5433" ]] || { fail "failed service probe did not restore the old route"; return; }
  [[ ! -e "${root}/state/route.state" ]] || { fail "failed service probe wrote durable route state"; return; }
  [[ "$(grep -Fc -- '-h hololive-postgres.example.ts.net' "${root}/psql.log")" == 2 ]] || { fail "rollback route did not receive a writable verification probe"; return; }
  pass "helper restores the prior route after post-switch validation failure"
}

helper_restores_state_after_publish_sync_failure() {
  local root="${TMP_DIR}/helper-state-rollback" config
  new_case "${root}"
  config="$(write_config "${root}")"
  printf 'prior-state\n' >"${root}/state/route.state"
  chmod 0600 "${root}/state/route.state"

  if FAKE_SYNC_FAIL_STATE_ONCE=1 run_helper "${root}" "${config}" >/dev/null 2>&1; then
    fail "helper accepted a failed durable state publish sync"
    return
  fi

  [[ "$(<"${root}/tailscale-config")" == "tcp://100.100.1.8:5433" ]] || { fail "state publish failure did not restore the old route"; return; }
  [[ "$(<"${root}/state/route.state")" == "prior-state" ]] || { fail "state publish failure did not restore the prior durable state"; return; }
  [[ "$(grep -Fc -- 'serve get-config --all' "${root}/tailscale.log")" -ge 3 ]] || { fail "rollback route was not re-read after restoration"; return; }
  [[ "$(grep -Fc -- '-h hololive-postgres.example.ts.net' "${root}/psql.log")" == 2 ]] || { fail "rollback route was not verified writable"; return; }
  pass "helper restores and verifies route plus durable state after publish sync failure"
}

helper_rejects_route_config_mismatch() {
  local root config mode
  for mode in mismatch unadvertised; do
    root="${TMP_DIR}/helper-config-${mode}"
    new_case "${root}"
    config="$(write_config "${root}")"
    if [[ "${mode}" == mismatch ]]; then
      if FAKE_TAILSCALE_CONFIG_MISMATCH=1 run_helper "${root}" "${config}" >/dev/null 2>&1; then
        fail "helper accepted a mismatched Tailscale endpoint"
        return
      fi
    elif FAKE_TAILSCALE_FORCE_UNADVERTISED=1 run_helper "${root}" "${config}" >/dev/null 2>&1; then
      fail "helper accepted an unadvertised Tailscale service"
      return
    fi
    [[ ! -e "${root}/state/route.state" ]] || { fail "invalid Tailscale config wrote durable route state"; return; }
  done
  pass "helper rejects mismatched and unadvertised Tailscale route config"
}

helper_is_idempotent_for_same_token() {
  local root="${TMP_DIR}/helper-idempotent" config first second configure_count
  new_case "${root}"
  config="$(write_config "${root}")"
  first="$(run_helper "${root}" "${config}")" || { fail "first route application failed"; return; }
  printf '0\n' >"${root}/tailscale-advertised"
  second="$(run_helper "${root}" "${config}")" || { fail "idempotent route application failed"; return; }
  [[ "${first}" == ROUTED\|100.100.1.5:5434\|fence-token-1234 && "${second}" == "${first}" ]] || { fail "idempotent route acknowledgement changed"; return; }
  configure_count="$(grep -Fc -- '--yes --service=svc:hololive-postgres --tcp=5433 tcp://100.100.1.5:5434' "${root}/tailscale.log")"
  [[ "${configure_count}" == 2 ]] || { fail "same-token retry did not reapply the Tailscale route"; return; }
  grep -Fq -- 'PGSSLMODE=verify-full' "${root}/psql.log" || { fail "PostgreSQL probe did not use verify-full"; return; }
  grep -Fq -- "PGPASSFILE=${root}/pgpass" "${root}/psql.log" || { fail "PostgreSQL probe did not use pgpass"; return; }
  grep -Fq -- "PGSSLROOTCERT=${root}/ca.pem" "${root}/psql.log" || { fail "PostgreSQL probe did not use the CA file"; return; }
  pass "helper is idempotent for the same fence token and verifies TLS probe inputs"
}

controller_accepts_exact_ack
controller_rejects_invalid_ack_and_input

if [[ "$(/usr/bin/id -u)" != 0 ]]; then
  printf 'ok: route helper tests requiring root skipped\n'
  exit 0
fi

helper_rejects_config_ownership_mode_and_symlink
helper_rejects_wrong_local_host
helper_rejects_tailscale_failure
helper_rejects_read_only_probe
helper_rolls_back_failed_service_probe
helper_restores_state_after_publish_sync_failure
helper_rejects_route_config_mismatch
helper_is_idempotent_for_same_token

if (( failures > 0 )); then
  printf '[FAIL] route tests failed: %s\n' "${failures}" >&2
  exit 1
fi
printf 'ok: route tests passed\n'
