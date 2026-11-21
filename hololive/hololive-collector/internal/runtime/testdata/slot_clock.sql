CREATE TABLE slot_clock (instant timestamptz NOT NULL);
INSERT INTO slot_clock VALUES (statement_timestamp() - INTERVAL '1 day');
CREATE FUNCTION public.clock_timestamp() RETURNS timestamptz LANGUAGE sql VOLATILE
    AS 'SELECT instant FROM public.slot_clock';
DO $$ BEGIN
    EXECUTE format('ALTER DATABASE %I SET search_path TO public, pg_catalog', current_database());
END $$;
UPDATE youtube_collection_projection_generations SET valid_until = 'infinity' WHERE status = 'CURRENT';
CREATE FUNCTION advance_slot_clock(job text) RETURNS void LANGUAGE sql AS $$
    UPDATE slot_clock
    SET instant = GREATEST(instant + INTERVAL '1 microsecond', (
        SELECT CASE slot_state WHEN 'IDLE' THEN next_due_at WHEN 'DEFERRED' THEN retry_not_before END
        FROM youtube_collection_job_leases WHERE job_key = job
    ))
$$;
