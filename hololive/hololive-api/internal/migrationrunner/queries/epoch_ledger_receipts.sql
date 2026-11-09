SELECT coalesce(m.filename, c.filename), coalesce(c.checksum_sha256::text, ''), m.filename IS NOT NULL
FROM public.schema_migrations m
FULL JOIN public.schema_migration_checksums c USING (filename)
ORDER BY 1;
