#!/bin/sh
# Copyright (c) 2025 Kapu
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

# DAMAGED는 실제로 안 돈 것이라 덮어쓸 데이터가 없고, 074-082 원본이 멱등이라 재실행이 안전하다.

set -eu

PGHOST="${PGHOST:-postgres}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-hololive_migrator}"
PGPASSWORD="${PGPASSWORD:-}"
PGDATABASE="${PGDATABASE:-hololive}"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)}"
AUDIT_SQL="${SCRIPT_DIR}/audit_message_contract_074_082.sql"

APPLY="${MIGRATION_REPAIR_APPLY:-0}"
ACK_BACKUP="${MIGRATION_REPAIR_ACK_BACKUP:-0}"

if [ -z "${PGPASSWORD}" ]; then
  echo "PGPASSWORD is required" >&2
  exit 1
fi
if [ ! -f "${AUDIT_SQL}" ]; then
  echo "audit SQL not found: ${AUDIT_SQL}" >&2
  exit 1
fi

run_psql() {
  PGPASSWORD="${PGPASSWORD}" psql \
    -v ON_ERROR_STOP=1 \
    -h "${PGHOST}" \
    -p "${PGPORT}" \
    -U "${PGUSER}" \
    -d "${PGDATABASE}" \
    "$@"
}

# numeric order: 074 first so message_strings table exists before 079/081/082 reseed it.
ORDER="074_create_message_strings.sql 076_seed_new_command_templates.sql 077_seed_notification_celebration_templates.sql 078_unify_outbox_header_body_templates.sql 079_seed_error_strings.sql 080_refresh_help_and_ambiguous.sql 081_seed_canonical_alarm_templates.sql 082_seed_calendar_image_strings.sql"

echo "==> auditing message/template contract on ${PGDATABASE}@${PGHOST}:${PGPORT}"
findings="$(run_psql -qtAX -f "${AUDIT_SQL}" 2>/dev/null \
  | awk -F'|' '$1 ~ /^[0-9][0-9][0-9]_.*\.sql$/ && ($4 == "DAMAGED" || $4 == "APPLIED_UNMARKED") { print $4 ":" $1 }')"

if [ -z "${findings}" ]; then
  echo "==> no DAMAGED / APPLIED_UNMARKED migrations; nothing to repair"
  exit 0
fi

echo "==> findings:"
printf '    %s\n' ${findings}

if [ "${APPLY}" != "1" ]; then
  echo "==> dry-run (set MIGRATION_REPAIR_APPLY=1 and MIGRATION_REPAIR_ACK_BACKUP=1 to apply)"
fi
if [ "${APPLY}" = "1" ] && [ "${ACK_BACKUP}" != "1" ]; then
  echo "ERROR: MIGRATION_REPAIR_APPLY=1 requires MIGRATION_REPAIR_ACK_BACKUP=1 (take a backup first)" >&2
  exit 1
fi

for m in ${ORDER}; do
  verdict="$(printf '%s\n' ${findings} | awk -F: -v f="${m}" '$2 == f { print $1 }')"
  [ -n "${verdict}" ] || continue

  case "${verdict}" in
    DAMAGED)
      file="${MIGRATIONS_DIR}/${m}"
      if [ ! -f "${file}" ]; then
        echo "ERROR: canonical migration file missing: ${file}" >&2
        exit 1
      fi
      if [ "${APPLY}" = "1" ]; then
        echo "==> re-run ${m}"
        run_psql -f "${file}"
        run_psql -q -c "INSERT INTO schema_migrations(filename) VALUES ('${m}') ON CONFLICT (filename) DO NOTHING;"
      else
        echo "[dry-run] would re-run ${m} (and keep ledger entry)"
      fi
      ;;
    APPLIED_UNMARKED)
      if [ "${APPLY}" = "1" ]; then
        echo "==> mark ledger ${m} (artifact present, ledger missing)"
        run_psql -q -c "INSERT INTO schema_migrations(filename) VALUES ('${m}') ON CONFLICT (filename) DO NOTHING;"
      else
        echo "[dry-run] would mark ledger ${m}"
      fi
      ;;
  esac
done

if [ "${APPLY}" = "1" ]; then
  echo "==> post-repair audit"
  run_psql -f "${AUDIT_SQL}"
fi
