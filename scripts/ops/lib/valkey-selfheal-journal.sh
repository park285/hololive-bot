#!/usr/bin/env bash

JOURNAL_GUARD="${JOURNAL}.guard"
JOURNAL_ACTIVE_SIZE=0
JOURNAL_ACTIVE_HASH=""

jstr() {
  local value="$1"
  value="${value:0:${JOURNAL_FIELD_MAX_CHARS}}"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '"%s"' "${value}"
}

json_array() {
  local first=1 item
  printf '['
  for item in "$@"; do
    if [ "${first}" -eq 1 ]; then first=0; else printf ','; fi
    jstr "${item}"
  done
  printf ']'
}

journal_path_safe() {
  local path="$1" owner mode_hex
  [ ! -L "${path}" ] || return 1
  [ ! -e "${path}" ] || [ -f "${path}" ] || return 1
  [ ! -e "${path}" ] && return 0
  owner="$(stat -c '%u' -- "${path}" 2>/dev/null)" || return 1
  [ "${owner}" = 0 ] || [ "${owner}" = "$(id -u)" ] || return 1
  mode_hex="$(stat -c '%f' -- "${path}" 2>/dev/null)" || return 1
  (( (0x${mode_hex} & 0x0012) == 0 ))
}

journal_temp_artifact_exists() {
  local path
  for path in "${JOURNAL}.tmp" "${JOURNAL_GUARD}.tmp" "${JOURNAL}.previous.tmp"; do
    [ ! -e "${path}" ] && [ ! -L "${path}" ] || return 0
  done
  return 1
}

journal_slot_open() {
  local slot="$1" fd_name="$2" identity_name="$3" opened_fd opened_identity mode old_umask restore_noclobber=0 open_status=0
  [ ! -e "${slot}" ] && [ ! -L "${slot}" ] || return 1
  old_umask="$(umask)"
  umask 077
  case $- in *C*) ;; *) set -C; restore_noclobber=1 ;; esac
  { exec {opened_fd}>"${slot}"; } 2>/dev/null || open_status="$?"
  [ "${restore_noclobber}" -eq 0 ] || set +C
  umask "${old_umask}"
  [ "${open_status}" -eq 0 ] || return 1
  opened_identity="$(stat -Lc '%f:%d:%i' -- "/proc/self/fd/${opened_fd}" 2>/dev/null)" || { exec {opened_fd}>&-; return 1; }
  mode="${opened_identity%%:*}"
  if (( (0x${mode} & 0xF000) != 0x8000 )); then exec {opened_fd}>&-; return 1; fi
  printf -v "${fd_name}" '%s' "${opened_fd}"
  printf -v "${identity_name}" '%s' "${opened_identity}"
}

journal_slot_matches() {
  local slot="$1" identity="$2" path_identity
  path_identity="$(stat -Lc '%f:%d:%i' -- "${slot}" 2>/dev/null)" || return 1
  [ ! -L "${slot}" ] && [ "${path_identity}" = "${identity}" ]
}

journal_slot_publish() {
  local slot="$1" identity="$2" destination="$3"
  journal_slot_matches "${slot}" "${identity}" || return 1
  journal_path_safe "${destination}" || return 1
  mv -f -- "${slot}" "${destination}" 2>/dev/null || return 1
  sync -f "$(dirname -- "${destination}")" 2>/dev/null
}

journal_atomic_text() {
  local path="$1" value="$2" fd identity size
  local slot="${path}.tmp"
  journal_path_safe "${path}" || return 1
  journal_slot_open "${slot}" fd identity || return 1
  if ! printf '%s\n' "${value}" >&"${fd}"; then exec {fd}>&-; return 1; fi
  size="$(stat -Lc '%s' -- "/proc/self/fd/${fd}" 2>/dev/null)" || { exec {fd}>&-; return 1; }
  if [ "${size}" -gt "${JOURNAL_MAX_BYTES}" ] || ! sync -f "/proc/self/fd/${fd}" 2>/dev/null ||
     ! journal_slot_matches "${slot}" "${identity}"; then
    exec {fd}>&-
    return 1
  fi
  exec {fd}>&-
  journal_slot_publish "${slot}" "${identity}" "${path}"
}

journal_guard_write() {
  local size hash
  size="$(stat -c '%s' -- "${JOURNAL}" 2>/dev/null)" || return 1
  hash="$(sha256sum -- "${JOURNAL}" 2>/dev/null)" || return 1
  hash="${hash%% *}"
  journal_atomic_text "${JOURNAL_GUARD}" "${size} ${hash}"
}

journal_reset_invalid() {
  local reason="$1" marker
  marker="$(printf '{"ts":%s,"mode":"%s","event":"journal_rejected","detail":{"reason":%s}}' "${NOW}" "${MODE}" "$(jstr "${reason}")")"
  journal_atomic_text "${JOURNAL}" "${marker}" || return 1
  journal_guard_write
}

journal_previous_bound() {
  local previous="${JOURNAL}.previous" size
  journal_path_safe "${previous}" || return 1
  [ -e "${previous}" ] || return 0
  size="$(stat -c '%s' -- "${previous}" 2>/dev/null)" || return 1
  if [ "${size}" -gt "${JOURNAL_MAX_BYTES}" ]; then
    journal_atomic_text "${previous}" "$(printf '{"ts":%s,"mode":"%s","event":"journal_rejected","detail":{"reason":"previous_oversize"}}' "${NOW}" "${MODE}")" || return 1
    return 2
  fi
}

journal_active_verify() {
  local guard_line guard_size guard_hash actual_hash
  JOURNAL_ACTIVE_SIZE=0
  JOURNAL_ACTIVE_HASH=""
  journal_temp_artifact_exists && return 1
  journal_path_safe "${JOURNAL}" && journal_path_safe "${JOURNAL_GUARD}" || return 1
  if [ ! -e "${JOURNAL}" ]; then
    [ ! -e "${JOURNAL_GUARD}" ] || return 1
    return 0
  fi
  JOURNAL_ACTIVE_SIZE="$(stat -c '%s' -- "${JOURNAL}" 2>/dev/null)" || return 1
  if [ "${JOURNAL_ACTIVE_SIZE}" -gt "${JOURNAL_MAX_BYTES}" ]; then
    journal_reset_invalid active_oversize
    return 2
  fi
  if [ ! -e "${JOURNAL_GUARD}" ]; then
    [ "${JOURNAL_ACTIVE_SIZE}" -eq 0 ] && return 0
    journal_reset_invalid active_guard_missing
    return 2
  fi
  if [ "$(stat -c '%s' -- "${JOURNAL_GUARD}" 2>/dev/null)" -gt 96 ]; then
    journal_reset_invalid active_guard_oversize
    return 2
  fi
  IFS= read -r guard_line <"${JOURNAL_GUARD}" || { journal_reset_invalid active_guard_unreadable; return 2; }
  [[ "${guard_line}" =~ ^([0-9]+)[[:space:]]([0-9a-f]{64})$ ]] || { journal_reset_invalid active_guard_invalid; return 2; }
  guard_size="${BASH_REMATCH[1]}"
  guard_hash="${BASH_REMATCH[2]}"
  [ "${guard_size}" = "${JOURNAL_ACTIVE_SIZE}" ] || { journal_reset_invalid active_size_mismatch; return 2; }
  actual_hash="$(sha256sum -- "${JOURNAL}" 2>/dev/null)" || return 1
  actual_hash="${actual_hash%% *}"
  [ "${actual_hash}" = "${guard_hash}" ] || { journal_reset_invalid active_hash_mismatch; return 2; }
  JOURNAL_ACTIVE_HASH="${actual_hash}"
}

journal_copy_active_to_slot() {
  local slot="$1" fd_name="$2" identity_name="$3" copy_fd copy_identity copied_hash size
  journal_slot_open "${slot}" copy_fd copy_identity || return 1
  if ! head -c "$((JOURNAL_MAX_BYTES + 1))" -- "${JOURNAL}" 1>&"${copy_fd}" 2>/dev/null; then exec {copy_fd}>&-; return 1; fi
  size="$(stat -Lc '%s' -- "/proc/self/fd/${copy_fd}" 2>/dev/null)" || { exec {copy_fd}>&-; return 1; }
  copied_hash="$(sha256sum -- "/proc/self/fd/${copy_fd}" 2>/dev/null)" || { exec {copy_fd}>&-; return 1; }
  if [ "${size}" -ne "${JOURNAL_ACTIVE_SIZE}" ] || [ "${copied_hash%% *}" != "${JOURNAL_ACTIVE_HASH}" ]; then
    exec {copy_fd}>&-
    return 1
  fi
  printf -v "${fd_name}" '%s' "${copy_fd}"
  printf -v "${identity_name}" '%s' "${copy_identity}"
}

journal_publish() {
  local line="$1" rotate="$2" slot="${JOURNAL}.tmp" fd identity size previous_fd previous_identity
  if [ "${rotate}" = false ] && [ "${JOURNAL_ACTIVE_SIZE}" -gt 0 ]; then
    journal_copy_active_to_slot "${slot}" fd identity || return 1
  else
    journal_slot_open "${slot}" fd identity || return 1
  fi
  if ! printf '%s\n' "${line}" >&"${fd}"; then exec {fd}>&-; return 1; fi
  size="$(stat -Lc '%s' -- "/proc/self/fd/${fd}" 2>/dev/null)" || { exec {fd}>&-; return 1; }
  if [ "${size}" -gt "${JOURNAL_MAX_BYTES}" ] || ! sync -f "/proc/self/fd/${fd}" 2>/dev/null ||
     ! journal_slot_matches "${slot}" "${identity}"; then
    exec {fd}>&-
    return 1
  fi
  exec {fd}>&-
  if [ "${rotate}" = true ] && [ "${JOURNAL_ACTIVE_SIZE}" -gt 0 ]; then
    journal_copy_active_to_slot "${JOURNAL}.previous.tmp" previous_fd previous_identity || return 1
    if ! sync -f "/proc/self/fd/${previous_fd}" 2>/dev/null ||
       ! journal_slot_matches "${JOURNAL}.previous.tmp" "${previous_identity}"; then
      exec {previous_fd}>&-
      return 1
    fi
    exec {previous_fd}>&-
    journal_slot_publish "${JOURNAL}.previous.tmp" "${previous_identity}" "${JOURNAL}.previous" || return 1
  fi
  journal_slot_publish "${slot}" "${identity}" "${JOURNAL}" || return 1
  journal_guard_write
}

journal_observe() {
  local detail="${2:-}" line
  [ -n "${detail}" ] || detail='{}'
  line="$(printf '{"ts":%s,"mode":"%s","event":"%s","detail":%s}' "${NOW}" "${MODE}" "$1" "${detail}")"
  printf '[selfheal] %s\n' "${line}" >&2
}

journal() {
  local detail="${2:-}" line rotate=false previous_status=0 active_status=0
  [ -n "${detail}" ] || detail='{}'
  line="$(printf '{"ts":%s,"mode":"%s","event":"%s","detail":%s}' "${NOW}" "${MODE}" "$1" "${detail}")"
  if [ "${MODE}" = --apply ]; then
    journal_previous_bound || previous_status="$?"
    [ "${previous_status}" -eq 0 ] || return 1
    journal_active_verify || active_status="$?"
    [ "${active_status}" -eq 0 ] || return 1
    [ $((JOURNAL_ACTIVE_SIZE + ${#line} + 1)) -le "${JOURNAL_MAX_BYTES}" ] || rotate=true
    journal_publish "${line}" "${rotate}" || return 1
  fi
  printf '[selfheal] %s\n' "${line}" >&2
}
