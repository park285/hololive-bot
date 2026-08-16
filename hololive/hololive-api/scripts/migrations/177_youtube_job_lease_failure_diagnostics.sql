BEGIN;

ALTER TABLE youtube_collection_job_leases
    ADD COLUMN IF NOT EXISTS last_failure_code TEXT;

ALTER TABLE youtube_collection_job_leases
    ADD COLUMN IF NOT EXISTS last_failure_class TEXT;

ALTER TABLE youtube_collection_job_leases
    ADD COLUMN IF NOT EXISTS last_failure_detail TEXT;

ALTER TABLE youtube_collection_job_leases
    ADD COLUMN IF NOT EXISTS last_failure_at TIMESTAMPTZ;

CREATE OR REPLACE FUNCTION populate_youtube_collection_job_lease_failure_diagnostics()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.slot_state = 'DEFERRED'
        AND OLD.last_error_code IS NOT NULL
        AND OLD.last_error_code <> 'shutdown_release'
        AND OLD.last_failure_code IS NULL
        AND OLD.last_failure_class IS NULL
        AND OLD.last_failure_detail IS NULL
        AND OLD.last_failure_at IS NULL
    THEN
        NEW.last_failure_code := OLD.last_error_code;
        NEW.last_failure_class := 'legacy_collector';
        NEW.last_failure_detail := 'legacy_collector';
        NEW.last_failure_at := OLD.updated_at;
    ELSIF OLD.slot_state = 'ACTIVE'
        AND NEW.slot_state = 'DEFERRED'
        AND NEW.last_error_code IS NOT NULL
        AND NEW.last_error_code <> 'shutdown_release'
        AND OLD.last_failure_code IS NOT DISTINCT FROM NEW.last_failure_code
        AND OLD.last_failure_class IS NOT DISTINCT FROM NEW.last_failure_class
        AND OLD.last_failure_detail IS NOT DISTINCT FROM NEW.last_failure_detail
        AND OLD.last_failure_at IS NOT DISTINCT FROM NEW.last_failure_at
    THEN
        NEW.last_failure_code := NEW.last_error_code;
        NEW.last_failure_class := 'legacy_collector';
        NEW.last_failure_detail := 'legacy_collector';
        NEW.last_failure_at := clock_timestamp();
    END IF;
    RETURN NEW;
END
$$;

CREATE OR REPLACE TRIGGER youtube_collection_job_lease_failure_diagnostics_backfill
    BEFORE UPDATE OF slot_state, last_error_code, last_failure_code, last_failure_class,
        last_failure_detail, last_failure_at
    ON youtube_collection_job_leases
    FOR EACH ROW
    EXECUTE FUNCTION populate_youtube_collection_job_lease_failure_diagnostics();

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'youtube_collection_job_leases'::regclass
          AND conname = 'chk_youtube_collection_job_last_failure_shape'
    ) THEN
        ALTER TABLE youtube_collection_job_leases
            ADD CONSTRAINT chk_youtube_collection_job_last_failure_shape
            CHECK (
                (
                    last_failure_code IS NULL
                    AND last_failure_class IS NULL
                    AND last_failure_detail IS NULL
                    AND last_failure_at IS NULL
                )
                OR (
                    last_failure_code IS NOT NULL
                    AND length(last_failure_code) BETWEEN 1 AND 128
                    AND last_failure_class IS NOT NULL
                    AND last_failure_class ~ '^[A-Za-z][A-Za-z0-9_]{0,63}$'
                    AND last_failure_detail IS NOT NULL
                    AND octet_length(last_failure_detail) <= 2048
                    AND last_failure_at IS NOT NULL
                )
            ) NOT VALID;
    END IF;
END
$migration$;

COMMIT;

UPDATE youtube_collection_job_leases
SET last_failure_code = last_error_code,
    last_failure_class = 'legacy_collector',
    last_failure_detail = 'legacy_collector',
    last_failure_at = updated_at
WHERE slot_state = 'DEFERRED'
  AND last_error_code IS NOT NULL
  AND last_error_code <> 'shutdown_release'
  AND last_failure_code IS NULL
  AND last_failure_class IS NULL
  AND last_failure_detail IS NULL
  AND last_failure_at IS NULL;

ALTER TABLE youtube_collection_job_leases
    VALIDATE CONSTRAINT chk_youtube_collection_job_last_failure_shape;
