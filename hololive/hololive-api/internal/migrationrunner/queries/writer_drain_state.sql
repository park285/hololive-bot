SELECT EXISTS (
    SELECT FROM pg_catalog.pg_event_trigger AS e
    JOIN pg_catalog.pg_proc AS p ON p.oid = e.evtfoid AND p.prosecdef
    JOIN pg_catalog.pg_namespace AS n ON n.oid = p.pronamespace
    WHERE e.evtname = 'hololive_writer_admission' AND e.evtevent = 'login' AND e.evtenabled = 'A'
      AND n.nspname = 'hololive_writer_admission' AND p.proname = 'reject_during_writer_drain'
) AND EXISTS (
    SELECT FROM pg_catalog.pg_stat_activity AS a
    JOIN pg_catalog.pg_roles AS r ON r.oid = a.usesysid AND r.rolsuper
    JOIN pg_catalog.pg_locks AS l ON l.pid = a.pid
    JOIN pg_catalog.pg_database AS d ON d.oid = l.database
    WHERE a.datname = pg_catalog.current_database() AND d.datname = pg_catalog.current_database()
      AND a.application_name = 'hololive-writers-drained'
      AND l.locktype = 'advisory' AND l.granted
      AND l.classid = 1213156431 AND l.objid = 6432 AND l.objsubid = 2
) AND NOT EXISTS (
    SELECT FROM pg_catalog.pg_stat_activity AS a
    WHERE a.usename IN (
        'hololive_runtime', 'hololive_alarm_worker',
        'hololive_collector_a', 'hololive_collector_b',
        'hololive_collector_c', 'hololive_collector_d'
    )
) AND pg_catalog.current_setting('application_name') = 'hololive-migration-window';
