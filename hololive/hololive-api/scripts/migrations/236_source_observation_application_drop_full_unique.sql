-- Deploy the ON CONFLICT ... WHERE observation_id IS NOT NULL writer after 235,
-- and verify all writers before this brief ACCESS EXCLUSIVE lock.
BEGIN;
SET LOCAL lock_timeout = '3s';
DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_index AS index_state
        WHERE index_state.indexrelid =
            'public.uq_source_observation_application_active'::pg_catalog.regclass
          AND index_state.indisvalid
          AND index_state.indisready
          AND index_state.indisunique
    ) THEN
        RAISE EXCEPTION 'active application unique index is not ready for cutover';
    END IF;
END
$migration$;
ALTER TABLE public.source_observation_applications
    DROP CONSTRAINT IF EXISTS uq_source_observation_application;
COMMIT;
