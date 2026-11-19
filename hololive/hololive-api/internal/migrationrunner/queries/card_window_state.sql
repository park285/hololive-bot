SELECT current_database() = 'hololive' AND current_user = 'hololive_migrator' AND inet_server_addr() IS NULL
    AND EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_pgbouncer_auth' AND rolcanlogin AND rolconnlimit = 1)
    AND EXISTS (SELECT FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
                WHERE n.nspname = 'hololive_pooler_auth' AND p.proname = 'lookup' AND p.proargtypes = '25'::oidvector AND p.prosecdef);
