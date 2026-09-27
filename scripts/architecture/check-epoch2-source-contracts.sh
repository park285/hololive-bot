#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
EPOCH2_CONTRACT="${SCRIPT_DIR}/epoch2_legacy_contract.sha256"

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

# epoch-1 message-contract repair(074-082)와 114 복구 소스 사본은 repair_message_contract_074_082.sh·preflight-114-restore.sh와
# 함께 지웠다(DEC-20260926-hololive-retired-rollback-tooling, stack-audit 2026-09-26 T19). T18에서 운영 schema_migrations에
# 001_schema_epoch2_baseline·182_epoch2_legacy_ledger_cleanup이 기록되고 epoch-1 파일명 행이 0건, epoch-1 이미지 보존이 없음을
# 확인해 epoch-1 rollback 창을 닫았다. 아래는 integration test가 쓰는 epoch-1 fixture의 checksum 고정만 남긴다.
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
