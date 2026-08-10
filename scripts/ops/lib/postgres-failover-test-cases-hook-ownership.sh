#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

select_non_root_hook_owner() {
  local current_uid owner_uid owner_gid
  current_uid="$(/usr/bin/id -u)"
  if [[ "${current_uid}" != "0" ]]; then
    printf '%s:%s\n' "${current_uid}" "$(/usr/bin/id -g)"
    return 0
  fi
  while IFS=: read -r _ _ owner_uid owner_gid _; do
    if [[ "${owner_uid}" =~ ^[0-9]+$ && "${owner_gid}" =~ ^[0-9]+$ && "${owner_uid}" != "0" ]]; then
      printf '%s:%s\n' "${owner_uid}" "${owner_gid}"
      return 0
    fi
  done < <(/usr/bin/getent passwd 2>/dev/null || true)
  printf '65534:65534\n'
}

make_hook_non_root_owned() {
  local path="$1" owner
  owner="$(select_non_root_hook_owner)"
  if [[ "$(/usr/bin/id -u)" == "0" ]]; then
    /usr/bin/chown "${owner}" -- "${path}" || return 1
  fi
  [[ "$(/usr/bin/stat -c '%u' -- "${path}")" != "0" ]]
}

root_owned_test_file() {
  local candidate real
  for candidate in /usr/bin/bash /bin/true /usr/bin/true /usr/bin/env; do
    real="$(/usr/bin/realpath -e -- "${candidate}" 2>/dev/null || true)"
    if [[ -f "${real}" && "$(/usr/bin/stat -c '%u' -- "${real}")" == "0" ]]; then
      printf '%s\n' "${real}"
      return 0
    fi
  done
  return 1
}

validate_hooks_before_transition() {
  local root="$1" fence_script="$2" route_script="$3"
  if [[ "${CONTROLLER_TEST_MODE}" == "0" ]]; then
    run_controller "${root}" --apply down
    return $?
  fi
  (
    ALLOW_NON_ROOT=0
    CURRENT_UID="$(/usr/bin/id -u)"
    MODE="--apply"
    NOW=150
    STATE_DIR="${root}/state"
    export FENCE_SCRIPT="${fence_script}"
    export ROUTE_SCRIPT="${route_script}"
    REQUIRE_ROUTE_HOOK=1
    source "${EXEC_ROOT}/scripts/ops/lib/postgres-failover-lib.sh"
    source "${EXEC_ROOT}/scripts/ops/lib/postgres-failover-transition-lib.sh"
    validate_apply_hooks
  ) >"${root}/out.log" 2>"${root}/err.log"
}

assert_hook_owner_rejected_before_transition() {
  local root="$1" label="$2" fence_script="$3" route_script="$4"
  if validate_hooks_before_transition "${root}" "${fence_script}" "${route_script}"; then
    fail "${label} unexpectedly passed production hook validation"
    return 1
  fi
  if [[ -s "${root}/hooks.log" ]] || grep -Fq 'pg_promote' "${root}/psql.log"; then
    cat "${root}/hooks.log" "${root}/psql.log" >&2
    fail "${label} reached fencing or promotion"
    return 1
  fi
  grep -Fq 'reason=hook_not_root_owned' "${root}/err.log" || {
    cat "${root}/err.log" >&2
    fail "${label} did not report hook_not_root_owned"
    return 1
  }
  pass "${label} is rejected before fencing or promotion"
}

non_root_fence_hook_blocks_before_fencing() {
  local root; root="$(setup_case non-root-fence-hook)"
  seed_ready_state "${root}"
  make_hook_non_root_owned "${root}/hooks/fence.sh" || { fail "could not construct a non-root-owned fence hook"; return; }
  assert_hook_owner_rejected_before_transition "${root}" "non-root-owned fence hook" \
    "${root}/hooks/fence.sh" "${root}/hooks/route.sh"
}

non_root_route_hook_blocks_before_fencing() {
  local root fence_script
  root="$(setup_case non-root-route-hook)"
  seed_ready_state "${root}"
  make_hook_non_root_owned "${root}/hooks/route.sh" || { fail "could not construct a non-root-owned route hook"; return; }
  fence_script="${root}/hooks/fence.sh"
  [[ "${CONTROLLER_TEST_MODE}" == "0" ]] || fence_script="$(root_owned_test_file)" || { fail "could not find a root-owned test file"; return; }
  assert_hook_owner_rejected_before_transition "${root}" "non-root-owned route hook" \
    "${fence_script}" "${root}/hooks/route.sh"
}

non_root_route_hook_blocks_intent_recovery() {
  local root fence_script
  root="$(setup_case non-root-route-intent)"
  seed_ready_state "${root}"
  cat >"${root}/state/promotion.intent" <<'EOF_INTENT'
role=promotion-intent
created_at=140
old_primary=100.100.1.8:5433
new_primary=100.100.1.5:5434
fence_token=fence-token-1234
last_primary_lsn=0/20
EOF_INTENT
  chmod 0600 "${root}/state/promotion.intent"
  make_hook_non_root_owned "${root}/hooks/route.sh" || { fail "could not construct a non-root-owned intent route hook"; return; }
  fence_script="${root}/hooks/fence.sh"
  [[ "${CONTROLLER_TEST_MODE}" == "0" ]] || fence_script="$(root_owned_test_file)" || { fail "could not find a root-owned test file"; return; }
  assert_hook_owner_rejected_before_transition "${root}" "non-root-owned route hook during intent recovery" \
    "${fence_script}" "${root}/hooks/route.sh"
}

non_root_hook_parent_blocks_in_production() {
  local root owner
  root="$(setup_case non-root-hook-parent-production)"
  seed_ready_state "${root}"
  if [[ "$(/usr/bin/id -u)" == "0" ]]; then
    owner="$(select_non_root_hook_owner)"
    /usr/bin/chown "${owner}" -- "${root}/hooks" || { fail "could not construct a non-root-owned hook parent"; return; }
  fi
  if validate_hooks_before_transition "${root}" "${root}/hooks/fence.sh" "${root}/hooks/route.sh"; then
    fail "non-root-owned hook parent unexpectedly passed production validation"
    return
  fi
  if [[ -s "${root}/hooks.log" ]] || grep -Fq 'pg_promote' "${root}/psql.log"; then
    cat "${root}/hooks.log" "${root}/psql.log" >&2
    fail "non-root-owned hook parent reached fencing or promotion"
    return
  fi
  if ! grep -Eq 'reason=(trusted_path_invalid_owner|hook_not_root_owned)' "${root}/err.log"; then
    cat "${root}/err.log" >&2
    fail "non-root-owned hook parent did not fail closed"
    return
  fi
  pass "non-root-owned hook parent is rejected before fencing or promotion"
}

non_root_production_client_paths_remain_allowed() {
  local root client_dir runtime_dir owner owner_uid owner_gid psql_path
  root="$(setup_case non-root-production-client-paths)"
  client_dir="${root}/client"
  runtime_dir="${root}/runtime"
  mkdir -p "${client_dir}" "${runtime_dir}"
  chmod 0700 "${client_dir}" "${runtime_dir}"
  printf 'test\n' >"${client_dir}/pgpass"
  printf 'test-ca\n' >"${client_dir}/postgres-ca.pem"
  chmod 0600 "${client_dir}/pgpass"
  chmod 0644 "${client_dir}/postgres-ca.pem"
  owner="$(select_non_root_hook_owner)"
  IFS=: read -r owner_uid owner_gid <<<"${owner}"
  if [[ "$(/usr/bin/id -u)" == "0" ]]; then
    /usr/bin/chown "${owner_uid}:${owner_gid}" -- "${client_dir}" "${runtime_dir}" \
      "${client_dir}/pgpass" "${client_dir}/postgres-ca.pem" || {
        fail "could not construct service-owned client/runtime paths"
        return
      }
  fi
  psql_path="$(root_owned_test_file)" || { fail "could not find a root-owned test psql path"; return; }
  if (
    ALLOW_NON_ROOT=0
    CURRENT_UID="${owner_uid}"
    MODE="--apply"
    NOW=150
    PGPASS_FILE="${client_dir}/pgpass"
    CA_FILE="${client_dir}/postgres-ca.pem"
    RUNTIME_DIR="${runtime_dir}"
    PSQL_PATH="${psql_path}"
    source "${EXEC_ROOT}/scripts/ops/lib/postgres-failover-lib.sh"
    validate_client_inputs
    validate_trusted_path_chain "runtime" "${RUNTIME_DIR}"
  ) >"${root}/out.log" 2>"${root}/err.log"; then
    pass "service-owned runtime and client paths remain valid in production mode"
    return
  fi
  cat "${root}/err.log" >&2
  fail "service-owned runtime/client paths were rejected in production mode"
}
