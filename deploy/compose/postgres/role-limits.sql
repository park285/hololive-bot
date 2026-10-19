-- Explicit admin-socket operation, NOT an application schema migration.
-- No production execution is authorized by shipping this file.
-- Apply only after all four collector pools use the reviewed max=7 policy and
-- renewal/finalization contention acceptance passes. Capture previous rolconnlimit
-- values for rollback; do not restore larger pools before restoring matching limits.
-- Mirrors scripts/ci/postgres-capacity-policy.tsv role limits across ALL pools.
BEGIN;
ALTER ROLE hololive_runtime CONNECTION LIMIT 25;
ALTER ROLE hololive_scraper CONNECTION LIMIT 28;
ALTER ROLE hololive_migrator CONNECTION LIMIT 1;
ALTER ROLE postgres_exporter CONNECTION LIMIT 1;
ALTER ROLE iris_db_nightly CONNECTION LIMIT 2;
COMMIT;
