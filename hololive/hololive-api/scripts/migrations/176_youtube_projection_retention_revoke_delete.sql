DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE ON TABLE youtube_collection_job_leases FROM hololive_runtime;
    END IF;
END
$migration$;
