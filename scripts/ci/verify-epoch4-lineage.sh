#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd -- "$(dirname -- "$0")/../.." && pwd -P)
work=$(mktemp -d)
cid=''
cleanup() {
  if [[ -n "$cid" ]]; then podman rm -f "$cid" >/dev/null; fi
  rm -rf -- "$work"
}
trap cleanup EXIT
image=docker.io/library/postgres:18.7-alpine@sha256:b6972e1115a4ff8eba5d7f7d19184926d1e16d7d1136ae1e1a7ebf286da0f015
previous=85c236c00433e55c420dc08f2731c06741746b42
epoch2_head=d288db26ac838e56ebc713e0679624c3875a66cc
build_previous() {
  local ref=$1 bin=$2
  mkdir "$work/$bin-src"
  git -C "$root" archive "$ref" | tar -x -C "$work/$bin-src"
  (cd "$work/$bin-src/hololive/hololive-api" && GOWORK=off CGO_ENABLED=0 go build -mod=readonly -trimpath -o "$work/$bin" ./cmd/db-migrate)
}
build_previous "$epoch2_head" epoch2
build_previous "$previous" epoch3
(cd "$root" && CGO_ENABLED=0 go build -mod=readonly -trimpath -o "$work/epoch4" ./hololive/hololive-api/cmd/db-migrate)
cid=$(podman run -d --pull=never --network=none --tmpfs /var/lib/postgresql:rw,size=1g --memory=1g --cpus=2 \
  -e POSTGRES_HOST_AUTH_METHOD=trust -e 'POSTGRES_INITDB_ARGS=--locale-provider=builtin --builtin-locale=C.UTF-8' "$image" -c listen_addresses='' -c shared_preload_libraries=pg_stat_statements)
for ((n=0; n<100; n++)); do
  if podman exec "$cid" sh -c 'test "$(cat /proc/1/comm)" = postgres && pg_isready -q -U postgres'; then break; fi
  sleep 0.2
done
mkdir -p "$work/future/hololive"
cp -a --reflink=auto "$root/hololive/hololive-api" "$work/future/hololive/"
for module in hololive-shared hololive-dbtest hololive-youtube-collector; do ln -s "$root/hololive/$module" "$work/future/hololive/$module"; done
future="$work/future/hololive/hololive-api/scripts/migrations"
build_future() { (cd "$work/future/hololive/hololive-api" && GOWORK=off CGO_ENABLED=0 go build -mod=readonly -trimpath -o "$work/$1" ./cmd/db-migrate); }
step1=002_epoch4_test_memo.sql
step2=003_epoch4_test_revision.sql
cat >"$future/$step1" <<'SQL'
BEGIN;
CREATE TABLE public.fixture_memo(id boolean PRIMARY KEY, body text NOT NULL);
INSERT INTO public.fixture_memo SELECT id,body FROM public.schedule_poster_translation_memo;
CREATE TABLE public.fixture_parent(k integer PRIMARY KEY);
CREATE TABLE public.fixture_child(k integer CONSTRAINT fixture_parent_fk REFERENCES public.fixture_parent(k) DEFERRABLE INITIALLY DEFERRED);
CREATE TABLE public.fixture_immediate_child(k integer CONSTRAINT fixture_immediate_fk REFERENCES public.fixture_parent(k) DEFERRABLE INITIALLY IMMEDIATE);
SET CONSTRAINTS public.fixture_immediate_fk DEFERRED;
INSERT INTO public.fixture_immediate_child VALUES (1);
INSERT INTO public.fixture_child VALUES (1);
INSERT INTO public.fixture_parent VALUES (1);
INSERT INTO public.fixture_child SELECT 2 FROM public.fixture_memo WHERE body = 'late_parent';
SET LOCAL check_function_bodies = off;
SET LOCAL search_path = pg_temp;
COMMIT;
SQL
cat >"$future/contracts/$step1" <<'SQL'
SELECT pg_catalog.to_regclass('public.fixture_memo') IS NOT NULL
   AND pg_catalog.has_column_privilege('hololive_runtime', 'public.fixture_memo', 'body', 'UPDATE');
SQL
printf '002 %s\n' "$step1" >>"$future/manifest.txt"
build_future plus1
cat >"$future/$step2" <<'SQL'
BEGIN;
DO $settings$
BEGIN
    IF current_setting('check_function_bodies') <> 'on' OR current_setting('search_path') <> 'pg_catalog, public, pg_temp' THEN
        RAISE EXCEPTION 'migration settings leaked';
    END IF;
END
$settings$;
DO $immediate$
BEGIN
    BEGIN
        INSERT INTO public.fixture_immediate_child VALUES (2);
        RAISE EXCEPTION 'initially immediate check leaked its previous deferred mode';
    EXCEPTION WHEN foreign_key_violation THEN NULL;
    END;
END
$immediate$;
CREATE TABLE public.fixture_later_child(k integer CONSTRAINT fixture_later_fk REFERENCES public.fixture_parent(k) DEFERRABLE INITIALLY DEFERRED);
INSERT INTO public.fixture_later_child VALUES (2);
INSERT INTO public.fixture_child VALUES (2);
INSERT INTO public.fixture_parent VALUES (2);
ALTER TABLE public.fixture_memo ADD COLUMN revision integer NOT NULL DEFAULT 0;
UPDATE public.fixture_memo SET revision=revision+1;
ALTER TABLE public.fixture_memo ADD CONSTRAINT fixture_reviewed CHECK (body <> 'reject');
COMMIT;
SQL
cat >"$future/contracts/$step2" <<'SQL'
SELECT pg_catalog.has_column_privilege('hololive_runtime', 'public.fixture_memo', 'revision', 'UPDATE')
   AND EXISTS (SELECT FROM pg_catalog.pg_constraint WHERE conrelid='public.fixture_memo'::regclass AND conname='fixture_reviewed' AND convalidated);
SQL
printf '003 %s\n' "$step2" >>"$future/manifest.txt"
build_future plus2
(cd "$work/future/hololive/hololive-api" && GOWORK=off CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags='-X main.deploymentOnly=1' -o "$work/plus2-deploy" ./cmd/db-migrate)
step3=004_epoch4_test_preservation.sql
cat >"$future/$step3" <<'SQL'
BEGIN;
CREATE FUNCTION public.fixture_late_ledger_write() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    IF NEW.body = 'late_timestamp' THEN
        UPDATE public.schema_migrations SET applied_at = applied_at - interval '1 second' WHERE filename LIKE '001_%';
    ELSE
        UPDATE public.schema_migrations SET applied_at = applied_at WHERE filename = '003_epoch4_test_revision.sql';
    END IF;
    RETURN NULL;
END
$fn$;
CREATE CONSTRAINT TRIGGER fixture_late_ledger_write AFTER UPDATE ON public.fixture_memo
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.fixture_late_ledger_write();
DO $tamper$
BEGIN
    CASE (SELECT body FROM public.fixture_memo WHERE id)
      WHEN 'timestamp' THEN UPDATE public.schema_migrations SET applied_at = applied_at - interval '1 second' WHERE filename LIKE '001_%';
      WHEN 'checksum' THEN UPDATE public.schema_migration_checksums SET checksum_sha256 = repeat('0',64) WHERE filename LIKE '001_%';
      WHEN 'same_call' THEN UPDATE public.schema_migrations SET applied_at = applied_at WHERE filename = '003_epoch4_test_revision.sql';
      WHEN 'late_timestamp', 'late_same_call' THEN UPDATE public.fixture_memo SET revision=revision+1;
      WHEN 'shape' THEN ALTER TABLE public.schema_migrations ADD COLUMN extra integer;
      ELSE RAISE EXCEPTION 'unrecognized preservation fixture';
    END CASE;
END
$tamper$;
COMMIT;
SQL
cp "$future/contracts/$step2" "$future/contracts/$step3"
printf '004 %s\n' "$step3" >>"$future/manifest.txt"
build_future altered
printf '001 %s\n002 %s\n003 %s\n' 001_schema_epoch4_baseline.sql "$step1" "$step2" >"$future/manifest.txt"
. "$root/scripts/deploy/lib/migration-receipts.sh"
collector_proof() {
  local db=$1 position=$2
  migration_collector_proof "$work/future" "$position" | podman exec -i "$cid" psql -U hololive_migrator -d "$db" -XqAtv ON_ERROR_STOP=1 -f -
}
for name in epoch2 epoch3 epoch4 plus1 plus2 plus2-deploy altered; do podman cp "$work/$name" "$cid:/tmp/$name"; done
sql() { podman exec -i "$cid" psql -U postgres -XAtqv ON_ERROR_STOP=1 -d "$@"; }
run() {
  local db=$1 bin=$2
  shift 2
  podman exec -e PGHOST=/var/run/postgresql -e PGUSER=hololive_migrator -e PGPASSWORD=fixture -e PGSSLMODE=disable \
    -e "PGDATABASE=$db" -e "PGOPTIONS=${fixture_options:-}" "$cid" "/tmp/$bin" "$@"
}
sql postgres <<'SQL'
CREATE ROLE postgres_admin LOGIN SUPERUSER;
CREATE ROLE hololive_migrator LOGIN;
CREATE ROLE hololive_runtime LOGIN;
CREATE ROLE hololive_alarm_worker LOGIN;
CREATE ROLE hololive_scraper NOLOGIN;
CREATE ROLE hololive_collector_a LOGIN IN ROLE hololive_scraper;
CREATE ROLE hololive_collector_b LOGIN IN ROLE hololive_scraper;
CREATE ROLE hololive_collector_c LOGIN IN ROLE hololive_scraper;
CREATE ROLE hololive_collector_d LOGIN IN ROLE hololive_scraper;
CREATE DATABASE base OWNER hololive_migrator;
SQL
sql base <<'SQL'
REVOKE ALL ON DATABASE base FROM PUBLIC;
CREATE EXTENSION pg_stat_statements;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO hololive_runtime, hololive_scraper;
ALTER DEFAULT PRIVILEGES FOR ROLE hololive_migrator IN SCHEMA public GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO hololive_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE hololive_migrator IN SCHEMA public GRANT SELECT,USAGE ON SEQUENCES TO hololive_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE hololive_migrator IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO hololive_runtime;
SQL
podman exec -i "$cid" psql -U postgres_admin -Xqv ON_ERROR_STOP=1 -d base <"$root/hololive/hololive-api/cmd/db-migrate/queries/bootstrap_pooler_auth.sql"
clone() { sql postgres -c "CREATE DATABASE $1 TEMPLATE $2 OWNER hololive_migrator"; }
started=$(sql postgres -c 'SELECT clock_timestamp()')
clone hololive base
clone fresh base
run hololive epoch3
run fresh epoch4 --initialize
baseline=001_schema_epoch4_baseline.sql
expected=$(sha256sum "$root/hololive/hololive-api/scripts/migrations/$baseline"); expected=${expected%% *}
[[ $(sql fresh -c "SELECT filename || ' ' || checksum_sha256 FROM schema_migrations FULL JOIN schema_migration_checksums USING(filename)") == "$baseline $expected" ]]
[[ $(sql hololive -c 'SELECT count(*) FROM schema_migrations JOIN schema_migration_checksums USING(filename)') == 32 ]]
for db in hololive fresh; do
  podman exec "$cid" pg_dump -U postgres --restrict-key=fixture --schema-only --exclude-table=public.schema_migrations \
    --exclude-table=public.schema_migration_checksums "$db" | sed '/^[[:space:]]*--/d' >"$work/$db.schema"
  sql "$db" -v "started=$started" <"$root/hololive/hololive-dbtest/testdata/baseline_effects.psql" >"$work/$db.effects"
done
diff -u "$work/hololive.schema" "$work/fresh.schema"
diff -u "$work/hololive.effects" "$work/fresh.effects"
capture() {
  local db=$1
  podman exec "$cid" pg_dump -U postgres --restrict-key=fixture --column-inserts "$db"
  sql "$db" -c 'SELECT xmin,ctid,* FROM schema_migrations ORDER BY filename' -c 'SELECT xmin,ctid,* FROM schema_migration_checksums ORDER BY filename'
}
unchanged() {
  local db=$1 expected_status=$2 status=0
  shift 2
  capture "$db" >"$work/before"
  run "$db" "${candidate_bin:-epoch4}" "$@" || status=$?
  if [[ "$expected_status" == accept ]]; then [[ "$status" == 0 ]]; else [[ "$status" != 0 ]]; fi
  capture "$db" >"$work/after"
  diff -u "$work/before" "$work/after"
}
sql hololive -c "UPDATE member_schedule_card_templates SET template='classic'" -c "UPDATE notification_templates SET body='운영 본문' WHERE template_key='CMD_HELP'"
fixture_options='-c default_transaction_read_only=on' unchanged hololive accept
unchanged fresh accept --initialize
fixture_options='-c default_transaction_read_only=on' unchanged hololive accept --schema-only
sql postgres -c 'ALTER DATABASE hololive RENAME TO delivered' -c 'ALTER DATABASE fresh RENAME TO hololive'
fixture_options='-c default_transaction_read_only=on' unchanged hololive accept --schema-only
sql postgres -c 'ALTER DATABASE hololive RENAME TO fresh' -c 'ALTER DATABASE delivered RENAME TO hololive'
for root_db in hololive fresh; do
  db="suffix_$root_db"
  clone "$db" "$root_db"
  [[ $(collector_proof "$db" applied) == 'on|t' ]]
  [[ $(collector_proof "$db" target) == 'on|f' ]]
  candidate_bin=plus2-deploy unchanged "$db" reject --initialize >"$work/refused-initialize" 2>&1
  grep -Fq "migration pending: $step1" "$work/refused-initialize"

  tamper="tamper_$root_db"
  clone "$tamper" "$db"
  for mutation in timestamp checksum same_call shape late_timestamp late_same_call; do
    sql "$tamper" -c "UPDATE schedule_poster_translation_memo SET body='$mutation'"
    candidate_bin=altered unchanged "$tamper" reject >"$work/refused-tamper" 2>&1
    grep -Fq 'modified a ledger definition or an earlier receipt' "$work/refused-tamper"
    [[ $(sql "$tamper" -c "SELECT to_regclass('public.fixture_memo') IS NULL") == t ]]
  done
  sql "$tamper" -c "UPDATE schedule_poster_translation_memo SET body='late_parent'"
  candidate_bin=plus2 unchanged "$tamper" reject >"$work/refused-fk" 2>&1
  grep -Fq "settle deferred effects of $step1" "$work/refused-fk"
  grep -Fq 'SQLSTATE 23503' "$work/refused-fk"
  [[ $(sql "$tamper" -c "SELECT to_regclass('public.fixture_child') IS NULL") == t ]]
  bad_target="bad_target_$root_db"
  clone "$bad_target" "$db"
  sql "$bad_target" -c 'ALTER DEFAULT PRIVILEGES FOR ROLE hololive_migrator IN SCHEMA public REVOKE UPDATE ON TABLES FROM hololive_runtime'
  candidate_bin=plus1 unchanged "$bad_target" reject
  [[ $(sql "$bad_target" -c "SELECT to_regclass('public.fixture_memo') IS NULL") == t ]]
  sql "$db" -c "UPDATE schedule_poster_translation_memo SET body='reject'"
  candidate_bin=plus2 unchanged "$db" reject --check
  candidate_bin=plus2 unchanged "$db" reject
  [[ $(sql "$db" -c "SELECT to_regclass('public.fixture_memo') IS NULL") == t ]]
  run "$db" plus1
  [[ $(collector_proof "$db" applied) == 'on|t' ]]
  [[ $(collector_proof "$db" target) == 'on|f' ]]

  [[ $(sql "$db" -c "SELECT body FROM fixture_memo") == reject ]]
  candidate_bin=plus2 unchanged "$db" reject --check
  candidate_bin=plus2 unchanged "$db" reject
  [[ $(sql "$db" -c "SELECT count(*) FROM schema_migrations WHERE filename='$step2'") == 0 ]]
  sql "$db" -c "UPDATE fixture_memo SET body='approved'"
  run "$db" plus2
  [[ $(collector_proof "$db" target) == 'on|t' ]]
  fixture_options='-c default_transaction_read_only=on' candidate_bin=plus2-deploy unchanged "$db" accept --initialize
  [[ $(sql "$db" -c 'SELECT revision FROM fixture_memo') == 1 ]]
  for name in "$step1" "$step2"; do
    digest=$(sha256sum "$future/$name"); digest=${digest%% *}
    [[ $(sql "$db" -c "SELECT checksum_sha256 FROM schema_migrations JOIN schema_migration_checksums USING(filename) WHERE filename='$name'") == "$digest" ]]
  done
  shadow="shadow_$root_db"
  clone "$shadow" "$db"
  sql "$shadow" -c 'CREATE SCHEMA decoy; CREATE TABLE decoy.fixture_memo(LIKE public.fixture_memo INCLUDING ALL); ALTER TABLE public.fixture_memo DROP COLUMN revision'
  fixture_options='-c search_path=decoy,public' candidate_bin=plus2 unchanged "$shadow" reject --check
  permissions="permissions_$root_db"
  clone "$permissions" "$db"
  sql "$permissions" -c 'REVOKE UPDATE ON public.fixture_memo FROM hololive_runtime'
  candidate_bin=plus2 unchanged "$permissions" reject --check
  fixture_options='-c default_transaction_read_only=on' candidate_bin=plus2 unchanged "$db" accept --check
  candidate_bin=plus2 unchanged "$db" accept
  candidate_bin=epoch4 unchanged "$db" reject
  sql "$db" -c "DELETE FROM schema_migrations WHERE filename='$step1'; DELETE FROM schema_migration_checksums WHERE filename='$step1'"
  candidate_bin=plus2 unchanged "$db" reject
done
fixture_rows() {
  sql "$1" -c "SELECT r::text FROM public.fixture_memo r ORDER BY id" \
    -c 'SELECT k FROM public.fixture_parent ORDER BY k' -c 'SELECT k FROM public.fixture_child ORDER BY k' \
    -c 'SELECT k FROM public.fixture_immediate_child ORDER BY k' -c 'SELECT k FROM public.fixture_later_child ORDER BY k'
}
for lineage in hololive fresh; do
  combined="grouped_$lineage"
  split="separate_$lineage"
  clone "$combined" "$lineage"
  clone "$split" "$lineage"
  run "$combined" plus2
  run "$split" plus1
  run "$split" plus2
  fixture_rows "$combined" >"$work/grouped-rows"
  fixture_rows "$split" >"$work/separate-rows"
  diff -u "$work/grouped-rows" "$work/separate-rows"
done
clone ambiguous_timing fresh
sql ambiguous_timing <<'SQL'
CREATE TABLE public.fixture_owner(k int PRIMARY KEY);
CREATE TABLE public.fixture_left(k int CONSTRAINT shared_timing REFERENCES public.fixture_owner(k) DEFERRABLE INITIALLY IMMEDIATE);
CREATE TABLE public.fixture_right(k int CONSTRAINT shared_timing REFERENCES public.fixture_owner(k) DEFERRABLE INITIALLY DEFERRED);
SQL
candidate_bin=plus1 unchanged ambiguous_timing reject >"$work/refused-ambiguity" 2>&1
grep -Fq 'conflicting initial timing for constraints named public.shared_timing' "$work/refused-ambiguity"
clone future_empty base
run future_empty plus2 --initialize
[[ $(sql future_empty -c 'SELECT revision FROM fixture_memo') == 1 ]]
candidate_bin=plus2 unchanged future_empty accept --check
for state in orphan missing_checksum corrupt gap unknown mixed dirty bad_worker; do
  clone "$state" hololive
  case "$state" in
    orphan) sql "$state" -c "INSERT INTO schema_migration_checksums VALUES ('stray.sql',repeat('1',64))" ;;
    missing_checksum) sql "$state" -c "DELETE FROM schema_migration_checksums WHERE filename LIKE '006_%'" ;;
    corrupt) sql "$state" -c "UPDATE schema_migration_checksums SET checksum_sha256=repeat('0',64) WHERE filename LIKE '022_%'" ;;
    gap) sql "$state" -c "DELETE FROM schema_migrations WHERE filename LIKE '006_%'; DELETE FROM schema_migration_checksums WHERE filename LIKE '006_%'" ;;
    unknown) sql "$state" -c "INSERT INTO schema_migrations VALUES ('other.sql'); INSERT INTO schema_migration_checksums VALUES ('other.sql',repeat('0',64))" ;;
    mixed) sql "$state" -c "INSERT INTO schema_migrations VALUES ('$baseline'); INSERT INTO schema_migration_checksums VALUES ('$baseline','$expected')" ;;
    bad_worker) sql "$state" -c 'REVOKE USAGE ON SEQUENCE public.alarm_dispatch_events_id_seq FROM hololive_alarm_worker' ;;
    dirty) sql "$state" -c 'ALTER TABLE source_observations DISABLE TRIGGER reject_retired_holodex_channel_profile' ;;
  esac
  unchanged "$state" reject
done
clone epoch2_head base
run epoch2_head epoch2
[[ $(sql epoch2_head -c 'SELECT count(*) FROM schema_migrations JOIN schema_migration_checksums USING(filename)') == 233 ]]
unchanged epoch2_head reject
run epoch2_head epoch3
unchanged epoch2_head accept
for role in hololive_runtime hololive_scraper; do
  db="unusable_$role"
  clone "$db" base
  sql "$db" -c "REVOKE USAGE ON SCHEMA public FROM $role"
  if run "$db" epoch4 --initialize; then echo "initialized without $role namespace access" >&2; exit 1; fi
  [[ $(sql "$db" -c "SELECT to_regclass('public.members') IS NULL AND to_regclass('public.schema_migrations') IS NULL AND to_regclass('public.schema_migration_checksums') IS NULL") == t ]]
  for lineage in hololive fresh; do
    db="revoked_${role}_$lineage"
    clone "$db" "$lineage"
    sql "$db" -c "REVOKE USAGE ON SCHEMA public FROM $role"
    unchanged "$db" reject --check
    [[ $(collector_proof "$db" applied) == 'on|f' ]]
  done
done
clone fdw_only base
sql fdw_only -c 'CREATE FOREIGN DATA WRAPPER standalone NO HANDLER NO VALIDATOR'
if run fdw_only epoch4 --initialize; then echo 'accepted an untracked FDW' >&2; exit 1; fi
[[ $(sql fdw_only -c "SELECT EXISTS (SELECT FROM pg_foreign_data_wrapper WHERE fdwname='standalone') AND to_regclass('public.schema_migrations') IS NULL AND to_regclass('public.schema_migration_checksums') IS NULL AND to_regclass('public.members') IS NULL") == t ]]
clone unprovisioned base
sql unprovisioned -c 'ALTER DEFAULT PRIVILEGES FOR ROLE hololive_migrator IN SCHEMA public REVOKE SELECT ON TABLES FROM hololive_runtime'
if run unprovisioned epoch4 --initialize; then echo 'accepted incomplete application default ACL' >&2; exit 1; fi
[[ $(sql unprovisioned -c "SELECT to_regclass('public.members') IS NULL AND to_regclass('public.schema_migrations') IS NULL") == t ]]
for cutoff in 5 30 31; do
  db="prefix_$cutoff"
  clone "$db" base
  case "$cutoff" in 5) tag='DROP INDEX';; 30) tag='CREATE FUNCTION';; 31) tag='CREATE TABLE';; esac
  sql "$db" <<SQL
CREATE FUNCTION public.fixture_interrupt() RETURNS event_trigger LANGUAGE plpgsql AS \$\$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        IF (SELECT count(*) FROM public.schema_migrations) = $cutoff THEN RAISE EXCEPTION 'fixture stop'; END IF;
    END IF;
END \$\$;
CREATE EVENT TRIGGER fixture_stop ON ddl_command_start WHEN TAG IN ('$tag') EXECUTE FUNCTION public.fixture_interrupt();
SQL
  if run "$db" epoch3; then echo "expected the released runner to stop after $cutoff" >&2; exit 1; fi
  sql "$db" -c 'DROP EVENT TRIGGER fixture_stop; DROP FUNCTION public.fixture_interrupt()'
  [[ $(sql "$db" -c 'SELECT count(*) FROM schema_migrations JOIN schema_migration_checksums USING(filename)') == "$cutoff" ]]
  unchanged "$db" reject
  unchanged "$db" reject --schema-only
  if [[ "$cutoff" == 30 ]]; then
    clone interrupted_card "$db"
    sql interrupted_card <<'SQL'
CREATE FUNCTION fixture_fail_batch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'fixture: stop before the batch finishes'; END $$;
CREATE TRIGGER fixture_interrupt_batch BEFORE UPDATE ON source_observation_queue FOR EACH STATEMENT EXECUTE FUNCTION fixture_fail_batch();
SQL
    if run interrupted_card epoch3; then echo 'legacy batch unexpectedly completed' >&2; exit 1; fi
    sql interrupted_card -c 'DROP TRIGGER fixture_interrupt_batch ON source_observation_queue; DROP FUNCTION fixture_fail_batch()'
    [[ $(sql interrupted_card -c 'SELECT count(*) FROM schema_migrations JOIN schema_migration_checksums USING(filename)') == 30 ]]
    [[ $(sql interrupted_card -c "SELECT count(*) FROM pg_trigger WHERE tgname IN ('terminalize_retired_holodex_channel_profile','reject_retired_holodex_channel_profile')") == 2 ]]
    unchanged interrupted_card reject
    unchanged interrupted_card reject --schema-only
  fi
  if [[ "$cutoff" == 5 ]]; then
    sql "$db" -c "UPDATE notification_templates SET body='부분 적용 운영 본문' WHERE template_key='CMD_HELP'"
    run "$db" epoch3
    [[ $(sql "$db" -c "SELECT bool_and(body='부분 적용 운영 본문') FROM notification_templates WHERE template_key='CMD_HELP'") == t ]]
    unchanged "$db" accept
  fi
done
printf '%s\n' 'Fresh schema, seed, ACL, sequence and database settings match; completed, corrupt and partial ledgers checked.'
