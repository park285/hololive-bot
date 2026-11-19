#!/usr/bin/env bash

migration_receipts() (
  set -euo pipefail
  cd -- "$1/hololive/hololive-api/scripts/migrations"
  local ordinal name extra expected n=0
  while read -r ordinal name extra || [[ -n "$ordinal" ]]; do
    [[ -z "$ordinal" || "$ordinal" == '#'* ]] && continue
    n=$((n+1))
    printf -v expected '%03d' "$n"
    [[ "$ordinal" =~ ^[0-9]+$ && -z "$extra" ]] || return 1
    if (( n == 1 )); then
      [[ "$name" == 001_schema_epoch4_baseline.sql ]] || return 1
    else
      [[ "$name" =~ ^${expected}_epoch4_[a-z0-9_]+\.sql$ ]] || return 1
    fi
    sha256sum -- "$name" || return 1
  done < manifest.txt
  (( n > 0 ))
)

migration_target_contract() {
  local name="${2##*  }"
  cat -- "$1/hololive/hololive-api/scripts/migrations/contracts/$name"
}

migration_collector_proof() {
  local root="$1" position="$2" receipts head name contract query conditions='' comma=''
  case "$position" in
    applied) conditions='epoch IN (3,4)' ;;
    target) conditions='epoch IN (3,4) AND pending = 0' ;;
    *) return 1 ;;
  esac
  receipts="$(migration_receipts "$root")" || return 1
  head="$(<"$root/hololive/hololive-api/internal/migrationrunner/queries/lineage_head.sql")" || return 1
  printf '%s\n' 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;' 'SET LOCAL search_path = pg_catalog,public,pg_temp;' 'SET LOCAL standard_conforming_strings = on;'
  printf 'PREPARE collector_head(text) AS SELECT CASE WHEN %s THEN (ARRAY[' "$conditions"
  while read -r _ name; do
    contract="$(<"$root/hololive/hololive-api/scripts/migrations/contracts/$name")" || return 1
    query="SELECT current_setting('transaction_read_only'), coalesce((${contract%;}), false)"
    printf "%s'%s'" "$comma" "${query//\'/\'\'}"
    comma=,
  done <<<"$receipts"
  printf "]) [applied+1] ELSE 'SELECT current_setting(''transaction_read_only''), false' END FROM (%s) AS h;\n" "${head%;}"
  printf "EXECUTE collector_head('%s')\n" "$receipts"
  printf '%s\n' '\gexec' 'COMMIT;'
}
