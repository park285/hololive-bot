#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
. "${ROOT_DIR}/scripts/ci/python-runtime.sh"
repo_python_init
MIGRATIONS_DIR="${ROOT_DIR}/hololive/hololive-api/scripts/migrations"
MANIFEST="${MIGRATIONS_DIR}/manifest.txt"
EPOCH2_BASELINE="001_schema_epoch2_baseline.sql"
EPOCH2_CONTRACT="${SCRIPT_DIR}/epoch2_legacy_contract.sha256"
EPOCH2_ACL_TAIL="${MIGRATIONS_DIR}/manual/epoch2_acl_tail.sql"
EPOCH2_SUFFIX_CONTRACT="${ROOT_DIR}/scripts/architecture/epoch2_suffix_contract.txt"
EPOCH2_NORMALIZER="${ROOT_DIR}/scripts/architecture/normalize-epoch2-baseline.py"

sql_statement_count() {
  "${CI_PYTHON_BIN}" - "$1" <<'PY'
import sys
from pathlib import Path

text = Path(sys.argv[1]).read_text()
count = 0
buf = []
i = 0

def flush():
    global count, buf
    if "".join(buf).strip():
        count += 1
    buf = []

def dollar_tag(pos):
    if text[pos] != "$":
        return None
    j = pos + 1
    while j < len(text):
        ch = text[j]
        if ch == "$":
            return text[pos : j + 1]
        if not (ch == "_" or ch.isalnum()):
            return None
        j += 1
    return None

while i < len(text):
    if text.startswith("--", i):
        end = text.find("\n", i)
        if end < 0:
            break
        buf.append(" ")
        i = end
        continue
    if text.startswith("/*", i):
        end = text.find("*/", i + 2)
        if end < 0:
            break
        buf.append(" ")
        i = end + 2
        continue
    if text[i] in ("'", '"'):
        quote = text[i]
        buf.append(text[i])
        i += 1
        while i < len(text):
            buf.append(text[i])
            if text[i] == quote:
                if i + 1 < len(text) and text[i + 1] == quote:
                    buf.append(text[i + 1])
                    i += 2
                    continue
                i += 1
                break
            i += 1
        continue
    if text[i] == "$":
        tag = dollar_tag(i)
        if tag is not None:
            end = text.find(tag, i + len(tag))
            if end < 0:
                buf.append(text[i:])
                i = len(text)
            else:
                buf.append(text[i : end + len(tag)])
                i = end + len(tag)
            continue
    if text[i] == ";":
        flush()
        i += 1
        continue
    buf.append(text[i])
    i += 1

flush()
print(count)
PY
}

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

if [[ "${manifest_files[0]}" != "${EPOCH2_BASELINE}" ]]; then
  echo "FAIL: epoch-2 manifest must begin with ${EPOCH2_BASELINE}" >&2
  exit 1
fi
for required in "${EPOCH2_CONTRACT}" "${EPOCH2_ACL_TAIL}" "${EPOCH2_SUFFIX_CONTRACT}"; do
  if [[ ! -s "${required}" ]]; then
    echo "FAIL: epoch-2 contract artifact missing or empty: ${required}" >&2
    exit 1
  fi
done

PYTHONDONTWRITEBYTECODE=1 "${CI_PYTHON_BIN}" - "${EPOCH2_CONTRACT}" <<'PY'
import re
import sys
from pathlib import Path

lines = Path(sys.argv[1]).read_text().splitlines()
if len(lines) != 136:
    raise SystemExit(f"FAIL: epoch-2 legacy contract has {len(lines)} lines, want 136")
entries = []
for number, line in enumerate(lines, 1):
    match = re.fullmatch(r"([0-9a-f]{64})  ([^ /]+\.sql)", line)
    if match is None:
        raise SystemExit(f"FAIL: malformed epoch-2 legacy contract line {number}")
    entries.append(match.group(2))
if entries[0] != "006-base-runtime-tables.sql" or entries[-1] != "140_epoch2_checkpoint.sql":
    raise SystemExit("FAIL: epoch-2 legacy contract boundary drift")
if len(entries) != len(set(entries)):
    raise SystemExit("FAIL: duplicate filename in epoch-2 legacy contract")
PY

while read -r _ legacy_file; do
  if [[ -e "${MIGRATIONS_DIR}/${legacy_file}" ]]; then
    echo "FAIL: legacy/checkpoint migration remains active: ${legacy_file}" >&2
    exit 1
  fi
done < "${EPOCH2_CONTRACT}"

bash "${SCRIPT_DIR}/check-epoch2-source-contracts.sh"

mapfile -t epoch2_suffix < "${EPOCH2_SUFFIX_CONTRACT}"
if (( ${#manifest_files[@]} - 1 < ${#epoch2_suffix[@]} )); then
  echo "FAIL: epoch-2 retained suffix is incomplete" >&2
  exit 1
fi
for index in "${!epoch2_suffix[@]}"; do
  if [[ "${manifest_files[index + 1]}" != "${epoch2_suffix[index]}" ]]; then
    echo "FAIL: epoch-2 retained suffix drift at position $((index + 2)): expected ${epoch2_suffix[index]}, got ${manifest_files[index + 1]}" >&2
    exit 1
  fi
done

baseline_path="${MIGRATIONS_DIR}/${EPOCH2_BASELINE}"
if ! grep -qE '^-- Source commit: [0-9a-f]{40}$' "${baseline_path}" ||
   ! grep -qxF -- '-- Legacy cutoff: 139_trust_alarm_short_links.sql' "${baseline_path}" ||
   ! grep -qxF -- '-- Compatibility checkpoint: 140_epoch2_checkpoint.sql' "${baseline_path}"; then
  echo "FAIL: epoch-2 baseline header contract drift" >&2
  exit 1
fi

PYTHONDONTWRITEBYTECODE=1 "${CI_PYTHON_BIN}" "${EPOCH2_NORMALIZER}" --check-existing "${baseline_path}"

PYTHONDONTWRITEBYTECODE=1 "${CI_PYTHON_BIN}" - "${baseline_path}" "${EPOCH2_ACL_TAIL}" <<'PY'
import re
import sys
from pathlib import Path

baseline = Path(sys.argv[1]).read_text()
acl_tail = Path(sys.argv[2]).read_text().strip()
if acl_tail not in baseline:
    raise SystemExit("FAIL: epoch-2 ACL tail is not embedded verbatim in baseline")
if len(re.findall(r"(?m)^BEGIN;$", baseline)) != 1 or len(re.findall(r"(?m)^COMMIT;$", baseline)) != 1:
    raise SystemExit("FAIL: epoch-2 baseline must be one top-level transaction")
if baseline.rstrip().splitlines()[-1] != "COMMIT;":
    raise SystemExit("FAIL: epoch-2 baseline must end with COMMIT;")
PY

for retired_runner in apply-all.sh bootstrap-and-apply.sh; do
  retired_output="$(
    env PGPASSWORD=unused \
      POSTGRES_ADMIN_PASSWORD=unused \
      MIGRATIONS_DIR="${MIGRATIONS_DIR}" \
      MIGRATION_MANIFEST="${MANIFEST}" \
    /bin/sh "${MIGRATIONS_DIR}/${retired_runner}" 2>&1
  )" && {
    echo "FAIL: ${retired_runner} must refuse epoch-2 manifests" >&2
    exit 1
  }
  if [[ "${retired_output}" != *"disabled for epoch-2 manifests"* ]]; then
    echo "FAIL: ${retired_runner} epoch-2 refusal is missing or ambiguous" >&2
    exit 1
  fi
done

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

# sqlsplit.Segments가 적용 시점에 거부하는 규칙의 보수적 초과 차단(superset)이다 — 한쪽만 고치면 안 된다.
for file in "${sql_files[@]}"; do
  path="${MIGRATIONS_DIR}/${file}"
  if ! grep -qiE '^[[:space:]]*(BEGIN([[:space:]]+(WORK|TRANSACTION))?|START[[:space:]]+TRANSACTION)[[:space:]]*;' "${path}"; then
    continue
  fi
  if sed 's/--.*$//' "${path}" | grep -qiE '\bCONCURRENTLY\b'; then
    echo "FAIL: ${file} wraps CONCURRENTLY in a top-level BEGIN;/COMMIT; block (mutually exclusive — runner runs the block as a real transaction)" >&2
    exit 1
  fi
  if ! grep -qiE '^[[:space:]]*(COMMIT|END)([[:space:]]+(WORK|TRANSACTION))?[[:space:]]*;' "${path}"; then
    echo "FAIL: ${file} has a top-level BEGIN; without a matching COMMIT;" >&2
    exit 1
  fi
done

grandfathered_concurrently_multi="060_add_alarm_dispatch_events_live_stream_index.sql 061_add_youtube_live_first_seen_guardrail.sql 067_align_claim_index_due_first.sql 086_add_sending_stale_indexes.sql 095_cleanup_redundant_indexes.sql 096_sql_integrity_retention_followups.sql 097_integrity_and_type_unification.sql"
for file in "${sql_files[@]}"; do
  path="${MIGRATIONS_DIR}/${file}"
  if ! grep -qiE '\bCONCURRENTLY\b' "${path}"; then
    continue
  fi
  statement_count="$(sql_statement_count "${path}")"
  if [[ "${statement_count}" == "1" ]]; then
    continue
  fi
  if [[ " ${grandfathered_concurrently_multi} " == *" ${file} "* ]]; then
    continue
  fi
  echo "FAIL: ${file} uses CONCURRENTLY with ${statement_count} SQL statements; keep CONCURRENTLY migrations single-statement" >&2
  exit 1
done

for file in "${sql_files[@]}"; do
  prefix="${file%%_*}"
  if [[ ! "${prefix}" =~ ^[0-9]+$ ]] || (( 10#${prefix} < 140 )); then
    continue
  fi
  if sed 's/--.*$//' "${MIGRATIONS_DIR}/${file}" | grep -qiE '^[[:space:]]*CREATE[[:space:]]+(UNIQUE[[:space:]]+)?INDEX[[:space:]]+(IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+)?' &&
     ! sed 's/--.*$//' "${MIGRATIONS_DIR}/${file}" | grep -qiE '^[[:space:]]*CREATE[[:space:]]+(UNIQUE[[:space:]]+)?INDEX[[:space:]]+CONCURRENTLY[[:space:]]+'; then
    echo "FAIL: ${file} creates a blocking index; use CREATE INDEX CONCURRENTLY in a single-statement migration" >&2
    exit 1
  fi
done

echo "OK: migration manifest matches SQL files"
