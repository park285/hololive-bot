DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT, DELETE ON TABLE youtube_collection_job_leases TO hololive_runtime;
    END IF;
END
$migration$;
