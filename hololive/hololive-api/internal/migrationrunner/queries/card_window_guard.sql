SELECT EXISTS (
    SELECT FROM pg_catalog.pg_stat_activity a JOIN pg_catalog.pg_locks l USING (pid)
    WHERE a.datname = current_database() AND a.usename = 'postgres_admin' AND a.client_addr IS NULL
      AND a.application_name = 'hololive-pool-drained' AND l.database = a.datid
      AND l.locktype = 'advisory' AND l.mode = 'ExclusiveLock' AND l.granted
      AND l.classid = 0x484f4c4f AND l.objid = 6432 AND l.objsubid = 2
);
