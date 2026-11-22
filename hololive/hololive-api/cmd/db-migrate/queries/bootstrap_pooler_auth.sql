DO $bootstrap$
DECLARE
    admin_oid oid;
    auth_oid oid;
    database_oid oid;
    schema_oid oid;
    lookup_oid oid;
    grantee_name text;
BEGIN
    SELECT oid INTO admin_oid FROM pg_catalog.pg_roles WHERE rolname = CURRENT_USER AND rolsuper;
    IF admin_oid IS NULL THEN
        RAISE EXCEPTION 'pooler authentication bootstrap requires the privileged admin' USING ERRCODE = '42501';
    END IF;

    SELECT oid INTO database_oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database();
    SELECT oid INTO schema_oid FROM pg_catalog.pg_namespace WHERE nspname = 'hololive_pooler_auth';
    IF schema_oid IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_namespace WHERE oid = schema_oid AND nspowner = admin_oid
    ) THEN
        RAISE EXCEPTION 'unexpected pooler authentication schema owner' USING ERRCODE = '42501';
    END IF;

    IF schema_oid IS NOT NULL THEN
        lookup_oid := pg_catalog.to_regprocedure('hololive_pooler_auth.lookup(text)');
    END IF;
    IF lookup_oid IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_proc WHERE oid = lookup_oid AND proowner = admin_oid
    ) THEN
        RAISE EXCEPTION 'unexpected pooler authentication function owner' USING ERRCODE = '42501';
    END IF;

    SELECT oid INTO auth_oid FROM pg_catalog.pg_roles WHERE rolname = 'hololive_pgbouncer_auth';
    IF auth_oid IS NULL THEN
        CREATE ROLE hololive_pgbouncer_auth LOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE
            NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 1;
        SELECT oid INTO auth_oid FROM pg_catalog.pg_roles WHERE rolname = 'hololive_pgbouncer_auth';
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_namespace AS n
        CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(n.nspacl, pg_catalog.acldefault('n', n.nspowner))) AS a
        WHERE n.oid = schema_oid AND (
            a.grantee NOT IN (admin_oid, auth_oid)
            OR (a.grantee = auth_oid AND (a.privilege_type <> 'USAGE' OR a.is_grantable))
        )
    ) OR EXISTS (
        SELECT 1 FROM pg_catalog.pg_proc AS p
        CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(p.proacl, pg_catalog.acldefault('f', p.proowner))) AS a
        WHERE p.oid = lookup_oid AND (
            a.grantee NOT IN (admin_oid, auth_oid)
            OR (a.grantee = auth_oid AND (a.privilege_type <> 'EXECUTE' OR a.is_grantable))
        )
    ) OR EXISTS (
        SELECT 1 FROM pg_catalog.pg_depend AS d
        WHERE d.refclassid = 'pg_catalog.pg_namespace'::pg_catalog.regclass AND d.refobjid = schema_oid
          AND NOT (d.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass AND d.objid = COALESCE(lookup_oid, 0))
    ) THEN
        RAISE EXCEPTION 'unexpected pooler authentication schema contents or grants' USING ERRCODE = '42501';
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_auth_members WHERE member = auth_oid OR roleid = auth_oid
    ) OR EXISTS (
        SELECT 1 FROM pg_catalog.pg_shdepend
        WHERE refclassid = 'pg_catalog.pg_authid'::pg_catalog.regclass AND refobjid = auth_oid
          AND deptype = 'o'
    ) THEN
        RAISE EXCEPTION 'unexpected pooler authentication role membership or ownership' USING ERRCODE = '42501';
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_shdepend
        WHERE refclassid = 'pg_catalog.pg_authid'::pg_catalog.regclass AND refobjid = auth_oid
          AND deptype = 'a'
          AND NOT (
              (classid = 'pg_catalog.pg_database'::pg_catalog.regclass AND objid = database_oid)
              OR (dbid = database_oid AND classid = 'pg_catalog.pg_namespace'::pg_catalog.regclass AND objid = COALESCE(schema_oid, 0))
              OR (dbid = database_oid AND classid = 'pg_catalog.pg_proc'::pg_catalog.regclass AND objid = COALESCE(lookup_oid, 0))
          )
    ) THEN
        RAISE EXCEPTION 'unexpected pooler authentication role grants' USING ERRCODE = '42501';
    END IF;

    ALTER ROLE hololive_pgbouncer_auth LOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE
        NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 1 VALID UNTIL 'infinity';

    IF pg_catalog.has_database_privilege(auth_oid, database_oid, 'CREATE,TEMPORARY')
       OR pg_catalog.has_table_privilege(auth_oid, 'pg_catalog.pg_authid', 'SELECT')
       OR EXISTS (
           SELECT 1 FROM pg_catalog.pg_database AS d
           CROSS JOIN LATERAL pg_catalog.aclexplode(d.datacl) AS a
           WHERE d.oid = database_oid AND a.grantee = auth_oid AND a.is_grantable
       ) OR EXISTS (
           SELECT 1 FROM pg_catalog.pg_namespace
           WHERE nspname NOT IN ('hololive_pooler_auth', 'pg_catalog', 'information_schema')
             AND nspname NOT LIKE 'pg_toast%' AND nspname NOT LIKE 'pg_temp_%'
             AND pg_catalog.has_schema_privilege(auth_oid, oid, 'USAGE,CREATE')
       ) OR EXISTS (
           SELECT 1 FROM pg_catalog.pg_class AS c
           JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
           WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_toast%'
             AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
             AND (pg_catalog.has_table_privilege(auth_oid, c.oid, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
                  OR pg_catalog.has_any_column_privilege(auth_oid, c.oid, 'SELECT,INSERT,UPDATE,REFERENCES'))
             AND NOT EXISTS (
                 SELECT 1 FROM pg_catalog.pg_depend AS d
                 JOIN pg_catalog.pg_extension AS e ON e.oid = d.refobjid
                 WHERE d.classid = 'pg_catalog.pg_class'::pg_catalog.regclass AND d.objid = c.oid AND d.objsubid = 0
                   AND d.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass AND d.refobjsubid = 0 AND d.deptype = 'e'
                   AND e.extname = 'pg_stat_statements' AND e.extversion = '1.13' AND e.extnamespace = n.oid
                   AND n.nspname = 'public' AND NOT pg_catalog.has_schema_privilege(auth_oid, n.oid, 'USAGE')
                   AND c.relkind = 'v' AND c.relname IN ('pg_stat_statements', 'pg_stat_statements_info')
                   AND (SELECT pg_catalog.bool_and(a.grantor = c.relowner AND a.privilege_type = 'SELECT' AND NOT a.is_grantable)
                        FROM pg_catalog.aclexplode(c.relacl) AS a WHERE a.grantee = 0)
                   AND NOT EXISTS (
                       SELECT 1 FROM pg_catalog.pg_attribute AS col
                       CROSS JOIN LATERAL pg_catalog.aclexplode(col.attacl) AS a
                       WHERE col.attrelid = c.oid AND a.grantee = 0
                   )
             )
       ) THEN
        RAISE EXCEPTION 'unexpected pooler authentication effective privileges' USING ERRCODE = '42501';
    END IF;

    IF schema_oid IS NULL THEN
        CREATE SCHEMA hololive_pooler_auth;
    END IF;

    EXECUTE $definition$
        CREATE OR REPLACE FUNCTION hololive_pooler_auth.lookup(requested_username text)
        RETURNS TABLE(username text, password text)
        LANGUAGE sql SECURITY DEFINER
        SET search_path = pg_catalog, pg_temp
        AS $lookup$
            SELECT r.rolname::pg_catalog.text, r.rolpassword::pg_catalog.text
            FROM pg_catalog.pg_authid AS r
            WHERE r.rolname = requested_username
              AND r.rolname IN (
                  'hololive_runtime', 'hololive_alarm_worker',
                  'hololive_collector_a', 'hololive_collector_b',
                  'hololive_collector_c', 'hololive_collector_d'
              )
              AND r.rolcanlogin
              AND (r.rolvaliduntil IS NULL OR r.rolvaliduntil > pg_catalog.statement_timestamp())
              AND NOT (r.rolsuper OR r.rolreplication OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole)
              AND r.rolpassword LIKE 'SCRAM-SHA-256$%'
              AND NOT EXISTS (
                  SELECT FROM pg_catalog.pg_stat_activity a
                  WHERE a.datname = pg_catalog.current_database() AND a.client_addr IS NULL
                    AND ((a.usename = 'postgres_admin' AND a.application_name IN ('hololive-pool-draining', 'hololive-pool-drained'))
                      OR (a.usename = 'hololive_migrator' AND a.application_name = 'hololive-pool-migrating'))
              )
        $lookup$
    $definition$;

    REVOKE ALL ON SCHEMA hololive_pooler_auth FROM PUBLIC;
    REVOKE ALL ON FUNCTION hololive_pooler_auth.lookup(text) FROM PUBLIC;
    FOR grantee_name IN
        SELECT DISTINCT r.rolname
        FROM pg_catalog.pg_namespace AS n
        CROSS JOIN LATERAL pg_catalog.aclexplode(n.nspacl) AS a
        JOIN pg_catalog.pg_roles AS r ON r.oid = a.grantee
        WHERE n.nspname = 'hololive_pooler_auth' AND a.grantee <> admin_oid
    LOOP
        EXECUTE pg_catalog.format('REVOKE ALL ON SCHEMA hololive_pooler_auth FROM %I', grantee_name);
    END LOOP;
    FOR grantee_name IN
        SELECT DISTINCT r.rolname
        FROM pg_catalog.pg_proc AS p
        CROSS JOIN LATERAL pg_catalog.aclexplode(p.proacl) AS a
        JOIN pg_catalog.pg_roles AS r ON r.oid = a.grantee
        WHERE p.oid = 'hololive_pooler_auth.lookup(text)'::pg_catalog.regprocedure AND a.grantee <> admin_oid
    LOOP
        EXECUTE pg_catalog.format('REVOKE ALL ON FUNCTION hololive_pooler_auth.lookup(text) FROM %I', grantee_name);
    END LOOP;

    EXECUTE pg_catalog.format('GRANT CONNECT ON DATABASE %I TO hololive_pgbouncer_auth', pg_catalog.current_database());
    GRANT USAGE ON SCHEMA hololive_pooler_auth TO hololive_pgbouncer_auth;
    GRANT EXECUTE ON FUNCTION hololive_pooler_auth.lookup(text) TO hololive_pgbouncer_auth;
END
$bootstrap$;
