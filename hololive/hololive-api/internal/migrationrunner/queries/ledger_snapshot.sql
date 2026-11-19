WITH ledgers(oid) AS (VALUES ('public.schema_migrations'::regclass), ('public.schema_migration_checksums'::regclass)),
objects(oid) AS (
    SELECT oid FROM ledgers UNION ALL SELECT indexrelid FROM pg_catalog.pg_index WHERE indrelid IN (SELECT oid FROM ledgers)
), shape_versions(catalog,tid,version) AS (
    SELECT tableoid,ctid,xmin FROM pg_catalog.pg_class WHERE oid IN (SELECT oid FROM objects)
    UNION ALL SELECT tableoid,ctid,xmin FROM pg_catalog.pg_attribute WHERE attrelid IN (SELECT oid FROM objects)
    UNION ALL SELECT tableoid,ctid,xmin FROM pg_catalog.pg_attrdef WHERE adrelid IN (SELECT oid FROM ledgers)
    UNION ALL SELECT tableoid,ctid,xmin FROM pg_catalog.pg_constraint WHERE conrelid IN (SELECT oid FROM ledgers)
    UNION ALL SELECT tableoid,ctid,xmin FROM pg_catalog.pg_trigger WHERE tgrelid IN (SELECT oid FROM ledgers)
    UNION ALL SELECT tableoid,ctid,xmin FROM pg_catalog.pg_index WHERE indrelid IN (SELECT oid FROM ledgers)
    UNION ALL SELECT tableoid,ctid,xmin FROM pg_catalog.pg_rewrite WHERE ev_class IN (SELECT oid FROM ledgers)
    UNION ALL SELECT tableoid,ctid,xmin FROM pg_catalog.pg_policy WHERE polrelid IN (SELECT oid FROM ledgers)
    UNION ALL SELECT tableoid,ctid,xmin FROM pg_catalog.pg_inherits WHERE inhrelid IN (SELECT oid FROM ledgers) OR inhparent IN (SELECT oid FROM ledgers)
)
SELECT CASE WHEN $1 = '' OR EXISTS (
    SELECT FROM public.schema_migrations m JOIN public.schema_migration_checksums c USING (filename)
    WHERE m.filename=$1 AND c.checksum_sha256=$2 AND m.applied_at=transaction_timestamp() AND c.recorded_at=transaction_timestamp()
) THEN sha256(convert_to(jsonb_build_array(
    (SELECT jsonb_agg(jsonb_build_array(catalog,tid,version) ORDER BY catalog,tid) FROM shape_versions),
    (SELECT jsonb_agg(jsonb_build_array(m.ctid,m.xmin,to_jsonb(m)) ORDER BY m.filename COLLATE "C") FROM public.schema_migrations m WHERE m.filename <> $1),
    (SELECT jsonb_agg(jsonb_build_array(c.ctid,c.xmin,to_jsonb(c)) ORDER BY c.filename COLLATE "C") FROM public.schema_migration_checksums c WHERE c.filename <> $1)
)::text,'UTF8')) END;
