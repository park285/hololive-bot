#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MIGRATIONS_DIR="${ROOT_DIR}/hololive/hololive-kakao-bot-go/scripts/migrations"
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
  comm -23 <(printf '%s\n' "${manifest_files[@]}" | sort) <(printf '%s\n' "${sql_files[@]}" | sort) >&2 || true
  echo "--- sql only" >&2
  comm -13 <(printf '%s\n' "${manifest_files[@]}" | sort) <(printf '%s\n' "${sql_files[@]}" | sort) >&2 || true
  exit 1
fi

echo "OK: migration manifest matches SQL files"
