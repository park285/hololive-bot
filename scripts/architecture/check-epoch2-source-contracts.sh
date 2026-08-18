#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MIGRATIONS_DIR="${ROOT_DIR}/hololive/hololive-api/scripts/migrations"
EPOCH2_CONTRACT="${ROOT_DIR}/hololive/hololive-api/internal/migrationrunner/epoch2_legacy_contract.sha256"

check_sources() {
  local label="$1"
  local source_dir="$2"
  shift 2
  local expected_files=("$@")

  if [[ ! -d "${source_dir}" ]]; then
    echo "FAIL: ${label} source directory is missing" >&2
    exit 1
  fi

  local actual_files=()
  mapfile -t actual_files < <(find "${source_dir}" -maxdepth 1 -type f -name '*.sql' -printf '%f\n' | sort)
  if [[ "${actual_files[*]}" != "${expected_files[*]}" ]]; then
    echo "FAIL: ${label} source set drift" >&2
    exit 1
  fi

  local file expected_checksum actual_checksum
  for file in "${expected_files[@]}"; do
    expected_checksum="$(awk -v file="${file}" '$2 == file { print $1 }' "${EPOCH2_CONTRACT}")"
    actual_checksum="$(sha256sum "${source_dir}/${file}" | awk '{print $1}')"
    if [[ -z "${expected_checksum}" || "${actual_checksum}" != "${expected_checksum}" ]]; then
      echo "FAIL: ${label} source checksum drift: ${file}" >&2
      exit 1
    fi
  done
}

check_sources \
  "epoch-1 message-contract repair" \
  "${MIGRATIONS_DIR}/manual/epoch1_message_contract_repair_sources" \
  074_create_message_strings.sql \
  076_seed_new_command_templates.sql \
  077_seed_notification_celebration_templates.sql \
  078_unify_outbox_header_body_templates.sql \
  079_seed_error_strings.sql \
  080_refresh_help_and_ambiguous.sql \
  081_seed_canonical_alarm_templates.sql \
  082_seed_calendar_image_strings.sql

check_sources \
  "epoch-1 recovery" \
  "${MIGRATIONS_DIR}/manual/epoch1_recovery_sources" \
  114_drop_unused_indexes.sql

check_sources \
  "epoch-1 integration" \
  "${ROOT_DIR}/hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/testdata/epoch1_migrations" \
  058_create_alarm_dispatch_outbox.sql \
  059_harden_alarm_dispatch_outbox.sql \
  065_record_alarm_dispatch_event_collisions.sql \
  118_alarm_dispatch_state_shape_check.sql \
  122_alarm_dispatch_last_error_size_check.sql

check_sources \
  "epoch-1 observation integration" \
  "${ROOT_DIR}/hololive/hololive-shared/pkg/service/youtube/tracking/observation/testdata/epoch1_migrations" \
  070_repoint_youtube_content_alarm_tracking_pk_to_canonical.sql
