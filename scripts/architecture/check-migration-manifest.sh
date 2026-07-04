#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MIGRATIONS_DIR="${ROOT_DIR}/hololive/hololive-api/scripts/migrations"
MANIFEST="${MIGRATIONS_DIR}/manifest.txt"

if [[ ! -f "${MANIFEST}" ]]; then
  echo "FAIL: migration manifest missing: ${MANIFEST}" >&2
  exit 1
fi

manifest_entries=()
while IFS= read -r entry || [[ -n "${entry}" ]]; do
  trimmed_entry="$(printf '%s' "${entry}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  case "${trimmed_entry}" in
    ''|'#'*)
      continue
      ;;
  esac
  manifest_entries+=("${trimmed_entry}")
done < "${MANIFEST}"

mapfile -t sql_files < <(find "${MIGRATIONS_DIR}" -maxdepth 1 -type f -name '[0-9]*.sql' -printf '%f\n' | sort)

if [[ ${#manifest_entries[@]} -eq 0 ]]; then
  echo "FAIL: migration manifest is empty" >&2
  exit 1
fi

manifest_orders=()
manifest_files=()
expected_order=1
for entry in "${manifest_entries[@]}"; do
  read -r order filename extra <<<"${entry}"
  if [[ -z "${order}" || -z "${filename}" || -n "${extra}" ]]; then
    echo "FAIL: invalid migration manifest entry: ${entry}" >&2
    exit 1
  fi

  expected_label="$(printf '%03d' "${expected_order}")"
  if [[ "${order}" != "${expected_label}" ]]; then
    echo "FAIL: migration manifest order drift: expected ${expected_label}, got ${order}" >&2
    exit 1
  fi

  manifest_orders+=("${order}")
  manifest_files+=("${filename}")
  expected_order=$((expected_order + 1))
done

manifest_order_unique="$(printf '%s\n' "${manifest_orders[@]}" | sort | uniq)"
manifest_file_sorted="$(printf '%s\n' "${manifest_files[@]}" | sort)"
manifest_file_unique="$(printf '%s\n' "${manifest_files[@]}" | sort | uniq)"
sql_joined="$(printf '%s\n' "${sql_files[@]}")"

if [[ "$(printf '%s\n' "${manifest_orders[@]}" | sort)" != "${manifest_order_unique}" ]]; then
  echo "FAIL: duplicate order labels in migration manifest" >&2
  exit 1
fi

if [[ "${manifest_file_sorted}" != "${manifest_file_unique}" ]]; then
  echo "FAIL: duplicate filenames in migration manifest" >&2
  exit 1
fi

if [[ "${manifest_file_sorted}" != "${sql_joined}" ]]; then
  echo "FAIL: migration manifest and actual SQL files differ" >&2
  echo "--- manifest only" >&2
  LC_ALL=C comm -23 <(printf '%s\n' "${manifest_files[@]}" | LC_ALL=C sort) <(printf '%s\n' "${sql_files[@]}" | LC_ALL=C sort) >&2 || true
  echo "--- sql only" >&2
  LC_ALL=C comm -13 <(printf '%s\n' "${manifest_files[@]}" | LC_ALL=C sort) <(printf '%s\n' "${sql_files[@]}" | LC_ALL=C sort) >&2 || true
  exit 1
fi

# 과거 브랜치 병행으로 이미 존재하는 번호 충돌(045/051/053)만 예외 — 신규 충돌은 차단한다.
grandfathered_dup_prefixes="045 051 053"
dup_prefixes="$(printf '%s\n' "${sql_files[@]}" | sed -E 's/^([0-9]+).*/\1/' | sort | uniq -d)"
for prefix in ${dup_prefixes}; do
  if [[ " ${grandfathered_dup_prefixes} " != *" ${prefix} "* ]]; then
    echo "FAIL: duplicate migration number prefix ${prefix} (새 파일은 마지막 번호+1을 사용)" >&2
    exit 1
  fi
done

# 무방비 SET NOT NULL은 ACCESS EXCLUSIVE 락을 쥔 채 전 행을 스캔한다.
# 유효한 CHECK가 선재하면 PG가 스캔을 생략하므로, NOT VALID → VALIDATE CONSTRAINT 레시피를
# 같은 파일에서 강제한다 (레시피: scripts/migrations/CONVENTIONS.md). 아래는 레시피 도입 전 파일들.
grandfathered_set_not_null="016-add-multi-group-support.sql 022-add-auth-acl-major-event-tables.sql 034_add_major_event_link_check_columns.sql 045_add_delivery_path_to_youtube_delivery_telemetry.sql 047_add_post_id_to_youtube_delivery_telemetry.sql 050_add_observation_window_to_youtube_delivery_telemetry.sql 053_add_canonical_content_identity_to_youtube_content_alarm_tracking.sql 069_normalize_youtube_delivery_telemetry_observation_runtime.sql"
for file in "${sql_files[@]}"; do
  if grep -qE 'SET[[:space:]]+NOT[[:space:]]+NULL' "${MIGRATIONS_DIR}/${file}"; then
    if [[ " ${grandfathered_set_not_null} " == *" ${file} "* ]]; then
      continue
    fi
    if ! grep -q 'NOT VALID' "${MIGRATIONS_DIR}/${file}" || ! grep -q 'VALIDATE CONSTRAINT' "${MIGRATIONS_DIR}/${file}"; then
      echo "FAIL: ${file} 에 무방비 SET NOT NULL — NOT VALID CHECK + VALIDATE CONSTRAINT 선행 필요 (CONVENTIONS.md 참고)" >&2
      exit 1
    fi
  fi
done

echo "OK: migration manifest matches SQL files"
