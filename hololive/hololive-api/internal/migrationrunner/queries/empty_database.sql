SELECT NOT EXISTS (
    SELECT FROM pg_catalog.pg_namespace
    WHERE nspname NOT IN ('information_schema','public','hololive_writer_admission') AND nspname !~ '^pg_'
) AND NOT EXISTS (
    SELECT FROM pg_catalog.pg_namespace n WHERE n.nspname = 'hololive_writer_admission' AND (
        pg_catalog.pg_get_userbyid(n.nspowner) <> 'postgres_admin' OR NOT EXISTS (
            SELECT FROM pg_catalog.pg_proc p WHERE p.pronamespace = n.oid AND p.proname = 'reject_during_writer_drain'
            AND p.pronargs = 0 AND p.prosecdef AND p.proowner = n.nspowner
        ) OR EXISTS (
            SELECT FROM pg_catalog.pg_depend d WHERE d.refclassid = 'pg_catalog.pg_namespace'::regclass
            AND d.refobjid = n.oid AND NOT (d.classid = 'pg_catalog.pg_proc'::regclass AND EXISTS (
                SELECT FROM pg_catalog.pg_proc p WHERE p.oid = d.objid AND p.proname = 'reject_during_writer_drain' AND p.pronargs = 0))
        )
    )
) AND NOT EXISTS (
    SELECT FROM pg_catalog.pg_depend d
    WHERE d.refclassid = 'pg_catalog.pg_namespace'::regclass
      AND d.refobjid = 'public'::regnamespace
      AND NOT EXISTS (
          SELECT FROM pg_catalog.pg_depend member JOIN pg_catalog.pg_extension e ON e.oid = member.refobjid
          WHERE member.classid = d.classid AND member.objid = d.objid AND member.deptype = 'e'
            AND member.refclassid = 'pg_catalog.pg_extension'::regclass AND e.extname = 'pg_stat_statements'
      ) AND d.classid <> 'pg_catalog.pg_default_acl'::regclass
        AND NOT (d.classid = 'pg_catalog.pg_extension'::regclass AND EXISTS (
            SELECT FROM pg_catalog.pg_extension e WHERE e.oid = d.objid AND e.extname = 'pg_stat_statements'))
) AND NOT EXISTS (SELECT FROM pg_catalog.pg_extension WHERE extname NOT IN ('plpgsql','pg_stat_statements'))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_largeobject_metadata)
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_event_trigger e WHERE NOT (e.evtname = 'hololive_writer_admission' AND e.evtevent = 'login'
      AND EXISTS (SELECT FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
          WHERE p.oid = e.evtfoid AND n.nspname = 'hololive_writer_admission'
            AND p.proname = 'reject_during_writer_drain' AND p.pronargs = 0)))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_foreign_data_wrapper w WHERE NOT EXISTS (
      SELECT FROM pg_catalog.pg_depend d WHERE d.classid='pg_catalog.pg_foreign_data_wrapper'::regclass
      AND d.objid=w.oid AND d.refclassid='pg_catalog.pg_extension'::regclass AND d.deptype='e'))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_foreign_server)
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_publication);
