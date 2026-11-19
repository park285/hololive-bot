WITH expected(name, login, collector) AS (VALUES
    ('hololive_runtime',true,false), ('hololive_alarm_worker',true,false), ('hololive_scraper',false,false),
    ('hololive_collector_a',true,true), ('hololive_collector_b',true,true),
    ('hololive_collector_c',true,true), ('hololive_collector_d',true,true)
), defaults AS (
    SELECT d.defaclobjtype AS kind, r.rolname, a.privilege_type
    FROM pg_catalog.pg_default_acl d CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) a
    JOIN pg_catalog.pg_roles r ON r.oid = a.grantee
    WHERE d.defaclrole = current_user::regrole AND d.defaclnamespace IN (0,'public'::regnamespace)
), required(kind, privilege_type) AS (VALUES
    ('r','SELECT'),('r','INSERT'),('r','UPDATE'),('r','DELETE'),('S','USAGE'),('S','SELECT'),('f','EXECUTE')
)
SELECT current_user = 'hololive_migrator'
   AND EXISTS (SELECT FROM pg_catalog.pg_database WHERE datname = current_database() AND datdba = current_user::regrole)
   AND EXISTS (SELECT FROM pg_catalog.pg_extension WHERE extname = 'pg_stat_statements' AND extnamespace = 'public'::regnamespace)
   AND NOT EXISTS (SELECT FROM expected e LEFT JOIN pg_catalog.pg_roles r ON r.rolname = e.name
       WHERE r.oid IS NULL OR r.rolcanlogin <> e.login OR r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls
          OR (e.name IN ('hololive_runtime','hololive_scraper') AND NOT pg_catalog.has_schema_privilege(r.oid,'public','USAGE'))
          OR (e.collector AND NOT EXISTS (SELECT FROM pg_catalog.pg_auth_members m JOIN pg_catalog.pg_roles g ON g.oid = m.roleid
                                         WHERE m.member = r.oid AND g.rolname = 'hololive_scraper' AND m.inherit_option)))
   AND NOT EXISTS (SELECT FROM required r WHERE NOT EXISTS (
       SELECT FROM defaults d WHERE d.kind::text = r.kind AND d.privilege_type = r.privilege_type AND d.rolname = 'hololive_runtime'))
   AND NOT EXISTS (SELECT FROM defaults WHERE rolname = 'hololive_alarm_worker');
