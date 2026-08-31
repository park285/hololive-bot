#!/usr/bin/env bash

STATE_VERSION=2
STATE_MAX_BYTES=512
STATE_GUARD_FILE="${STATE_FILE}.guard"
STATE_LOCK_FILE="${STATE_FILE}.lock"
STATE_RECEIPT_FILE="${STATE_FILE}.receipt"
RC_PREV=0
PING_FAIL=0
RECOVERY_EPOCH=0
RECOVERY_MUTATIONS=0
COOLDOWN_UNTIL=0
RECOVERY_STATUS=monitoring
RECOVERY_LOCK_TOKEN=""
STATE_VALID=0
STATE_FINGERPRINT=""
STATE_LOCK_TOKEN_MISMATCH=0
HEALTHY_STATE_CHANGED=0

state_artifact_exists() {
  local artifact
  for artifact in "${STATE_FILE}.failed" "${STATE_FILE}.tmp" "${STATE_GUARD_FILE}.tmp" "${STATE_RECEIPT_FILE}.tmp"; do
    [ ! -e "${artifact}" ] && [ ! -L "${artifact}" ] || return 0
  done
  return 1
}

state_file_read_bounded() {
  local path="$1" output_name="$2" fd_meta path_meta mode dev ino size path_mode path_dev path_ino path_size content fd
  [ ! -L "${path}" ] && [ -f "${path}" ] || return 1
  { exec {fd}<"${path}"; } 2>/dev/null || return 1
  fd_meta="$(stat -Lc '%f:%d:%i:%s' -- "/proc/self/fd/${fd}" 2>/dev/null)" || { exec {fd}<&-; return 1; }
  path_meta="$(stat -Lc '%f:%d:%i:%s' -- "${path}" 2>/dev/null)" || { exec {fd}<&-; return 1; }
  IFS=: read -r mode dev ino size <<<"${fd_meta}"
  IFS=: read -r path_mode path_dev path_ino path_size <<<"${path_meta}"
  if [ -L "${path}" ] || (( (0x${mode} & 0xF000) != 0x8000 )) || (( (0x${path_mode} & 0xF000) != 0x8000 )) ||
     [ "${dev}:${ino}:${size}" != "${path_dev}:${path_ino}:${path_size}" ] ||
     [ "${size}" -le 0 ] || [ "${size}" -gt "${STATE_MAX_BYTES}" ]; then
    exec {fd}<&-
    return 1
  fi
  LC_ALL=C IFS= read -r -N 513 content <&"${fd}" || :
  exec {fd}<&-
  [ "${#content}" -le "${STATE_MAX_BYTES}" ] && [[ "${content}" == *$'\n' ]] || return 1
  content="${content%$'\n'}"
  [[ "${content}" != *$'\n'* ]] || return 1
  path_meta="$(stat -Lc '%f:%d:%i:%s' -- "${path}" 2>/dev/null)" || return 1
  [ ! -L "${path}" ] && [ "${path_meta}" = "${fd_meta}" ] || return 1
  printf -v "${output_name}" '%s' "${content}"
}

state_integer_valid() {
  local value="$1" max=9223372036854775807
  [ "${#value}" -lt 19 ] || { [ "${#value}" -eq 19 ] && (( value <= max )); }
}

state_lock_token_valid() {
  [[ "$1" =~ ^[0-9a-f]+:[0-9]+:[0-9]+$ ]]
}

state_values_valid() {
  state_integer_valid "${RC_PREV}" && state_integer_valid "${PING_FAIL}" &&
    state_integer_valid "${RECOVERY_EPOCH}" && state_integer_valid "${COOLDOWN_UNTIL}" || return 1
  case "${RECOVERY_STATUS}" in
    monitoring)
      [ "${RECOVERY_EPOCH}" -eq 0 ] && [ "${RECOVERY_MUTATIONS}" -eq 0 ] &&
        [ "${COOLDOWN_UNTIL}" -eq 0 ] && [ -z "${RECOVERY_LOCK_TOKEN}" ]
      ;;
    recovering)
      [ "${RECOVERY_EPOCH}" -gt 0 ] && [ "${RECOVERY_MUTATIONS}" -eq 1 ] &&
        [ "${COOLDOWN_UNTIL}" -gt 0 ] && state_lock_token_valid "${RECOVERY_LOCK_TOKEN}"
      ;;
    manual_intervention_required)
      [ "${RECOVERY_EPOCH}" -gt 0 ] && [ "${RECOVERY_MUTATIONS}" -eq 2 ] &&
        [ "${COOLDOWN_UNTIL}" -eq 0 ] && state_lock_token_valid "${RECOVERY_LOCK_TOKEN}"
      ;;
    *) return 1 ;;
  esac
}

state_parse() {
  local value="$1" number='(0|[1-9][0-9]*)' token='([0-9a-f]+:[0-9]+:[0-9]+|)' pattern
  pattern="^\{\"version\":${STATE_VERSION},\"restart_count\":${number},\"ping_failures\":${number},\"epoch\":${number},\"mutations\":([0-2]),\"next_eligible_at\":${number},\"status\":\"(monitoring|recovering|manual_intervention_required)\",\"lock_token\":\"${token}\"\}$"
  [[ "${value}" =~ ${pattern} ]] || return 1
  RC_PREV="${BASH_REMATCH[1]}"
  PING_FAIL="${BASH_REMATCH[2]}"
  RECOVERY_EPOCH="${BASH_REMATCH[3]}"
  RECOVERY_MUTATIONS="${BASH_REMATCH[4]}"
  COOLDOWN_UNTIL="${BASH_REMATCH[5]}"
  RECOVERY_STATUS="${BASH_REMATCH[6]}"
  RECOVERY_LOCK_TOKEN="${BASH_REMATCH[7]}"
  state_values_valid
}

state_serialize() {
  printf '{"version":%s,"restart_count":%s,"ping_failures":%s,"epoch":%s,"mutations":%s,"next_eligible_at":%s,"status":"%s","lock_token":"%s"}' \
    "${STATE_VERSION}" "${RC_PREV}" "${PING_FAIL}" "${RECOVERY_EPOCH}" "${RECOVERY_MUTATIONS}" "${COOLDOWN_UNTIL}" "${RECOVERY_STATUS}" "${RECOVERY_LOCK_TOKEN}"
}

state_lock_fd_identity() {
  local output_name="$1" identity mode
  [ -n "${STATE_LOCK_FD:-}" ] || return 1
  identity="$(stat -Lc '%f:%d:%i' -- "/proc/self/fd/${STATE_LOCK_FD}" 2>/dev/null)" || return 1
  mode="${identity%%:*}"
  (( (0x${mode} & 0xF000) == 0x8000 )) || return 1
  printf -v "${output_name}" '%s' "${identity}"
}

state_lock_fd_valid() {
  local fd_identity path_identity
  [ ! -L "${STATE_LOCK_FILE}" ] && [ -f "${STATE_LOCK_FILE}" ] || return 1
  state_lock_fd_identity fd_identity || return 1
  path_identity="$(stat -Lc '%f:%d:%i' -- "${STATE_LOCK_FILE}" 2>/dev/null)" || return 1
  [ "${fd_identity}" = "${path_identity}" ]
}

state_lock_token_owned() {
  local current_token
  state_lock_fd_valid && state_lock_fd_identity current_token &&
    [ -n "${RECOVERY_LOCK_TOKEN}" ] && [ "${RECOVERY_LOCK_TOKEN}" = "${current_token}" ]
}

state_lock_token_mismatch() {
  [ "${STATE_LOCK_TOKEN_MISMATCH}" -eq 1 ]
}

healthy_state_changed() {
  [ "${HEALTHY_STATE_CHANGED}" -eq 1 ]
}

load_state() {
  local state_json guard_json receipt_json
  STATE_VALID=0
  STATE_LOCK_TOKEN_MISMATCH=0
  state_artifact_exists && return 0
  state_file_read_bounded "${STATE_FILE}" state_json || return 0
  state_file_read_bounded "${STATE_GUARD_FILE}" guard_json || return 0
  state_file_read_bounded "${STATE_RECEIPT_FILE}" receipt_json || return 0
  [ "${state_json}" = "${guard_json}" ] && [ "${state_json}" = "${receipt_json}" ] || return 0
  state_parse "${state_json}" || return 0
  STATE_FINGERPRINT="$(stat -Lc '%d:%i:%s:%Y:%Z' -- "${STATE_FILE}" 2>/dev/null)" || return 0
  STATE_VALID=1
  if [ "${MODE}" = --apply ] && [ "${RECOVERY_STATUS}" != monitoring ] && ! state_lock_token_owned; then
    STATE_LOCK_TOKEN_MISMATCH=1
  fi
}

state_owned_slot_write() {
  local slot="$1" value="$2" identity_name="$3" fd identity path_identity mode old_umask restore_noclobber=0 open_status=0
  [ ! -e "${slot}" ] && [ ! -L "${slot}" ] || return 1
  old_umask="$(umask)"
  umask 077
  case $- in *C*) ;; *) set -C; restore_noclobber=1 ;; esac
  { exec {fd}>"${slot}"; } 2>/dev/null || open_status="$?"
  [ "${restore_noclobber}" -eq 0 ] || set +C
  umask "${old_umask}"
  [ "${open_status}" -eq 0 ] || return 1
  identity="$(stat -Lc '%f:%d:%i' -- "/proc/self/fd/${fd}" 2>/dev/null)" || { exec {fd}>&-; return 1; }
  mode="${identity%%:*}"
  if (( (0x${mode} & 0xF000) != 0x8000 )) || ! printf '%s\n' "${value}" >&"${fd}" ||
     ! sync -f "/proc/self/fd/${fd}" 2>/dev/null; then
    exec {fd}>&-
    return 1
  fi
  path_identity="$(stat -Lc '%f:%d:%i' -- "${slot}" 2>/dev/null)" || { exec {fd}>&-; return 1; }
  if [ -L "${slot}" ] || [ "${path_identity}" != "${identity}" ]; then
    exec {fd}>&-
    return 1
  fi
  exec {fd}>&-
  printf -v "${identity_name}" '%s' "${identity}"
}

state_owned_slot_publish() {
  local slot="$1" identity="$2" destination="$3" path_identity
  path_identity="$(stat -Lc '%f:%d:%i' -- "${slot}" 2>/dev/null)" || return 1
  [ ! -L "${slot}" ] && [ "${path_identity}" = "${identity}" ] || return 1
  mv -f -- "${slot}" "${destination}" 2>/dev/null || return 1
  sync -f "$(dirname -- "${destination}")" 2>/dev/null
}

state_receipt_write() {
  local value="$1" receipt_identity
  state_owned_slot_write "${STATE_RECEIPT_FILE}.tmp" "${value}" receipt_identity || return 1
  state_owned_slot_publish "${STATE_RECEIPT_FILE}.tmp" "${receipt_identity}" "${STATE_RECEIPT_FILE}"
}

state_repair_artifacts() {
  local path marker="${STATE_FILE}.failed" fd fd_identity path_identity content
  for path in "${STATE_FILE}.tmp" "${STATE_GUARD_FILE}.tmp" "${STATE_RECEIPT_FILE}.tmp"; do
    [ ! -e "${path}" ] && [ ! -L "${path}" ] || return 1
  done
  [ ! -e "${marker}" ] && [ ! -L "${marker}" ] && return 0
  [ ! -L "${marker}" ] && [ -f "${marker}" ] || return 1
  { exec {fd}<"${marker}"; } 2>/dev/null || return 1
  fd_identity="$(stat -Lc '%f:%d:%i' -- "/proc/self/fd/${fd}" 2>/dev/null)" || { exec {fd}<&-; return 1; }
  IFS= read -r content <&"${fd}" || { exec {fd}<&-; return 1; }
  path_identity="$(stat -Lc '%f:%d:%i' -- "${marker}" 2>/dev/null)" || { exec {fd}<&-; return 1; }
  exec {fd}<&-
  [ "${content}" = failed ] && [ "${fd_identity}" = "${path_identity}" ] || return 1
  rm -f -- "${marker}" 2>/dev/null || return 1
  sync -f "$(dirname -- "${marker}")" 2>/dev/null
}

state_transaction_failed() {
  local marker="${STATE_FILE}.failed" marker_status=0
  if [ ! -e "${marker}" ] && [ ! -L "${marker}" ]; then
    (umask 077; set -C; printf '%s\n' failed >"${marker}") 2>/dev/null || marker_status=1
  fi
  if [ "${marker_status}" -eq 0 ] && [ ! -L "${marker}" ] && [ -f "${marker}" ]; then
    sync -f "${marker}" 2>/dev/null || marker_status=1
    sync -f "$(dirname -- "${marker}")" 2>/dev/null || marker_status=1
  fi
  journal "state_persist_failed" "$(printf '{"phase":%s,"path":%s}' "$(jstr "$1")" "$(jstr "${STATE_FILE}")")"
  [ "${marker_status}" -eq 0 ] || return 1
  return 1
}

persist_state() {
  local repair="${1:-false}" state_dir serialized state_identity guard_identity current_fingerprint
  [ "${MODE}" = --apply ] || return 0
  state_values_valid || return 1
  state_dir="$(dirname -- "${STATE_FILE}")"
  if [ "${repair}" = true ]; then
    state_repair_artifacts || return 1
  elif [ "${STATE_VALID}" -ne 1 ] || state_artifact_exists; then
    return 1
  fi
  state_lock_fd_valid || { state_transaction_failed lock_replaced; return 1; }
  if [ "${RECOVERY_STATUS}" != monitoring ] && ! state_lock_token_owned; then
    state_transaction_failed lock_token_mismatch
    return 1
  fi
  serialized="$(state_serialize)"
  state_receipt_write pending || { state_transaction_failed receipt_pending; return 1; }
  state_owned_slot_write "${STATE_FILE}.tmp" "${serialized}" state_identity || { state_transaction_failed state_write; return 1; }
  if [ "${repair}" != true ]; then
    current_fingerprint="$(stat -Lc '%d:%i:%s:%Y:%Z' -- "${STATE_FILE}" 2>/dev/null)" || { state_transaction_failed state_stat; return 1; }
    [ "${current_fingerprint}" = "${STATE_FINGERPRINT}" ] || { state_transaction_failed state_replaced; return 1; }
    cmp -s -- "${STATE_FILE}" "${STATE_GUARD_FILE}" || { state_transaction_failed state_replaced; return 1; }
  fi
  state_owned_slot_publish "${STATE_FILE}.tmp" "${state_identity}" "${STATE_FILE}" || { state_transaction_failed state_rename; return 1; }
  state_owned_slot_write "${STATE_GUARD_FILE}.tmp" "${serialized}" guard_identity || { state_transaction_failed guard_write; return 1; }
  state_owned_slot_publish "${STATE_GUARD_FILE}.tmp" "${guard_identity}" "${STATE_GUARD_FILE}" || { state_transaction_failed guard_rename; return 1; }
  state_lock_fd_valid || { state_transaction_failed lock_replaced; return 1; }
  if [ "${RECOVERY_STATUS}" != monitoring ] && ! state_lock_token_owned; then
    state_transaction_failed lock_token_mismatch
    return 1
  fi
  state_receipt_write "${serialized}" || { state_transaction_failed receipt_commit; return 1; }
  sync -f "${state_dir}" 2>/dev/null || { state_transaction_failed state_dir_sync; return 1; }
  STATE_FINGERPRINT="$(stat -Lc '%d:%i:%s:%Y:%Z' -- "${STATE_FILE}" 2>/dev/null)" || return 1
  STATE_VALID=1
}

reset_healthy_state() {
  local current_restart_count
  HEALTHY_STATE_CHANGED=0
  current_restart_count="$(restart_count)"
  if [ "${current_restart_count}" -lt 0 ]; then current_restart_count=0; fi
  if [ "${STATE_VALID}" -eq 1 ] && [ "${RC_PREV}" -eq "${current_restart_count}" ] &&
     [ "${PING_FAIL}" -eq 0 ] && [ "${RECOVERY_EPOCH}" -eq 0 ] &&
     [ "${RECOVERY_MUTATIONS}" -eq 0 ] && [ "${COOLDOWN_UNTIL}" -eq 0 ] &&
     [ "${RECOVERY_STATUS}" = monitoring ] && [ -z "${RECOVERY_LOCK_TOKEN}" ]; then
    return 0
  fi
  RC_PREV="${current_restart_count}"
  PING_FAIL=0
  RECOVERY_EPOCH=0
  RECOVERY_MUTATIONS=0
  COOLDOWN_UNTIL=0
  RECOVERY_STATUS=monitoring
  RECOVERY_LOCK_TOKEN=""
  persist_state true || return 1
  HEALTHY_STATE_CHANGED=1
}

reserve_mutation() {
  local current_token
  if ! state_lock_fd_valid || ! state_lock_fd_identity current_token; then
    state_transaction_failed lock_replaced
    return 1
  fi
  if [ "${RECOVERY_MUTATIONS}" -eq 0 ]; then
    [ "${RECOVERY_STATUS}" = monitoring ] && [ -z "${RECOVERY_LOCK_TOKEN}" ] || return 1
    RECOVERY_LOCK_TOKEN="${current_token}"
  else
    [ "${RECOVERY_LOCK_TOKEN}" = "${current_token}" ] || { state_transaction_failed lock_token_mismatch; return 1; }
  fi
  RECOVERY_MUTATIONS=$((RECOVERY_MUTATIONS + 1))
  if [ "${RECOVERY_MUTATIONS}" -eq 1 ]; then
    RECOVERY_STATUS=recovering
    COOLDOWN_UNTIL=$((NOW + RECOVERY_COOLDOWN_SEC))
  else
    RECOVERY_STATUS=manual_intervention_required
    COOLDOWN_UNTIL=0
  fi
  persist_state
}
