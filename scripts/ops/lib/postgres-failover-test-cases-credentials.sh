#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

systemd_pgpass_is_materialized_privately() {
  local root credential_dir credential_file runtime_dir
  root="$(setup_case systemd-pgpass)"
  runtime_dir="${root}/runtime"
  mkdir -p "${runtime_dir}"
  chmod 0700 "${runtime_dir}"
  if [[ "${CONTROLLER_TEST_MODE}" == "0" ]]; then
    if [[ -z "${SYSTEM_CREDENTIAL_TEST_ROOT}" ]]; then
      SYSTEM_CREDENTIAL_TEST_ROOT="/run/credentials/postgres-failover-test.$$"
      test ! -e "${SYSTEM_CREDENTIAL_TEST_ROOT}" || { fail "system credential fixture already exists"; return; }
      mkdir -p "${SYSTEM_CREDENTIAL_TEST_ROOT}"
      chmod 0700 "${SYSTEM_CREDENTIAL_TEST_ROOT}"
    fi
    credential_dir="${SYSTEM_CREDENTIAL_TEST_ROOT}"
  else
    credential_dir="${root}/run/credentials/postgres-failover.service"
    mkdir -p "${credential_dir}"
    chmod 0700 "${root}/run" "${root}/run/credentials" "${credential_dir}"
  fi
  credential_file="${credential_dir}/pgpass"
  cp "${root}/pgpass" "${credential_file}"
  chmod 0440 "${credential_file}"
  if ! run_controller "${root}" --dry-run up \
    "POSTGRES_FAILOVER_PGPASS_FILE=${credential_file}" \
    "POSTGRES_FAILOVER_RUNTIME_DIR=${runtime_dir}" \
    FAKE_REQUIRE_PRIVATE_PGPASS=1; then
    cat "${root}/err.log" >&2
    fail "systemd pgpass credential was not materialized for libpq"
    return
  fi
  grep -Eq "^mode=600 owner=$(id -u) group=$(id -g) path=${runtime_dir}/pgpass\.[A-Za-z0-9]+$" \
    "${root}/pgpass-metadata.log" || {
      cat "${root}/pgpass-metadata.log" >&2
      fail "libpq did not receive a private runtime passfile"
      return
    }
  if find "${runtime_dir}" -mindepth 1 -print -quit | grep -q .; then
    find "${runtime_dir}" -mindepth 1 -maxdepth 1 -printf '%m %u:%g %f\n' >&2
    fail "ephemeral pgpass remained after controller exit"
    return
  fi
  [[ -r "${credential_file}" ]] || { fail "source credential was unexpectedly removed"; return; }

  if run_controller "${root}" --dry-run up \
    "POSTGRES_FAILOVER_PGPASS_FILE=${credential_file}" \
    "POSTGRES_FAILOVER_RUNTIME_DIR=${runtime_dir}" \
    FAKE_REQUIRE_PRIVATE_PGPASS=1 FAKE_LOCAL_STATUS=invalid; then
    fail "credential cleanup failure probe unexpectedly succeeded"
    return
  fi
  if find "${runtime_dir}" -mindepth 1 -print -quit | grep -q .; then
    fail "ephemeral pgpass remained after controller failure"
    return
  fi
  chmod 0750 "${runtime_dir}"
  if run_controller "${root}" --dry-run up \
    "POSTGRES_FAILOVER_PGPASS_FILE=${credential_file}" \
    "POSTGRES_FAILOVER_RUNTIME_DIR=${runtime_dir}"; then
    fail "controller accepted a group-readable runtime directory"
    return
  fi
  chmod 0700 "${runtime_dir}"
  pass "systemd pgpass is copied to a private runtime file and removed on exit"
}

invalid_pgpass_credential_shapes_fail_closed() {
  local root credential_dir runtime_dir candidate
  root="$(setup_case invalid-pgpass)"
  runtime_dir="${root}/runtime"
  mkdir -p "${runtime_dir}" "${root}/run/credentials/postgres-failover.service/nested"
  chmod 0700 "${runtime_dir}" "${root}/run" "${root}/run/credentials" \
    "${root}/run/credentials/postgres-failover.service" "${root}/run/credentials/postgres-failover.service/nested"

  candidate="${root}/group-readable.pgpass"
  cp "${root}/pgpass" "${candidate}"
  chmod 0440 "${candidate}"
  if run_controller "${root}" --dry-run up \
    "POSTGRES_FAILOVER_PGPASS_FILE=${candidate}" "POSTGRES_FAILOVER_RUNTIME_DIR=${runtime_dir}"; then
    fail "controller accepted a group-readable pgpass outside a credential directory"
    return
  fi

  credential_dir="${root}/run/credentials/postgres-failover.service"
  candidate="${credential_dir}/pgpass"
  cp "${root}/pgpass" "${candidate}"
  chmod 0640 "${candidate}"
  if run_controller "${root}" --dry-run up \
    "POSTGRES_FAILOVER_PGPASS_FILE=${candidate}" "POSTGRES_FAILOVER_RUNTIME_DIR=${runtime_dir}"; then
    fail "controller accepted a writable systemd credential"
    return
  fi

  candidate="${credential_dir}/nested/pgpass"
  cp "${root}/pgpass" "${candidate}"
  chmod 0440 "${candidate}"
  if run_controller "${root}" --dry-run up \
    "POSTGRES_FAILOVER_PGPASS_FILE=${candidate}" "POSTGRES_FAILOVER_RUNTIME_DIR=${runtime_dir}"; then
    fail "controller accepted a nested credential path"
    return
  fi
  if [[ "${CONTROLLER_TEST_MODE}" == "0" ]]; then
    candidate="${SYSTEM_CREDENTIAL_TEST_ROOT}/wrong-group.pgpass"
    cp "${root}/pgpass" "${candidate}"
    chmod 0440 "${candidate}"
    chown 0:1 "${candidate}"
    if run_controller "${root}" --dry-run up \
      "POSTGRES_FAILOVER_PGPASS_FILE=${candidate}" "POSTGRES_FAILOVER_RUNTIME_DIR=${runtime_dir}"; then
      fail "controller accepted a systemd credential outside root group ownership"
      return
    fi
  fi
  pass "invalid pgpass credential modes and paths fail closed"
}
