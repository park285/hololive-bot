#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MIGRATIONS_DIR="${1:-${ROOT_DIR}/hololive/hololive-api/scripts/migrations}"
GRANDFATHERED_THROUGH=114

if [[ ! -d "${MIGRATIONS_DIR}" ]]; then
  echo "FAIL: migrations directory missing: ${MIGRATIONS_DIR}" >&2
  exit 1
fi

while IFS= read -r file; do
  name="${file##*/}"
  prefix="$(printf '%s' "${name}" | sed -E 's/^([0-9]+).*/\1/')"
  if ((10#${prefix} <= GRANDFATHERED_THROUGH)); then
    continue
  fi

  blocking_lines="$(
    sed 's/--.*$//' "${file}" |
      grep -iE '^[[:space:]]*DROP[[:space:]]+INDEX([[:space:]]|$)' |
      grep -ivE '^[[:space:]]*DROP[[:space:]]+INDEX[[:space:]]+CONCURRENTLY([[:space:]]|$)' || true
  )"
  if [[ -n "${blocking_lines}" ]]; then
    echo "FAIL: ${name} uses blocking DROP INDEX; use DROP INDEX CONCURRENTLY or an explicit maintenance-only non-migration procedure" >&2
    exit 1
  fi
done < <(find "${MIGRATIONS_DIR}" -maxdepth 1 -type f -name '[0-9]*.sql' -print | LC_ALL=C sort)

echo "OK: new migrations avoid blocking DROP INDEX"
