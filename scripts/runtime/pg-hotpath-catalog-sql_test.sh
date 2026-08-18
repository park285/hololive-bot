#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CATALOG_SQL_LIB="${ROOT_DIR}/scripts/runtime/lib/pg-hotpath-catalog-sql.sh"

source "${CATALOG_SQL_LIB}"
sql="$(dead_tuples_sql)"

for token in \
  "source_collection_checkpoints" \
  "source_observation_queue" \
  "source_observations" \
  "youtube_collection_job_leases" \
  "n_tup_newpage_upd" \
  "last_idx_scan" \
  "relation.reloptions" \
  "backend_xmin" \
  "xact_start IS NOT NULL OR backend_xmin IS NOT NULL" \
  "stats.schemaname = 'public'" \
  "indexes.schemaname = 'public'"; do
  if [[ "${sql}" != *"${token}"* ]]; then
    echo "missing MVCC catalog SQL token: ${token}" >&2
    exit 1
  fi
done

if [[ "${sql}" == *"query"* ]]; then
  echo "MVCC catalog SQL must not expose active query text" >&2
  exit 1
fi

echo "ok: pg hotpath catalog SQL preserves bounded secret-free MVCC evidence"
