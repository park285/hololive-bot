SELECT pg_catalog.set_config('pg_major.bootstrap_roles', :'bootstrap_roles', false) AS bootstrap_roles_set \gset
DO $check$
BEGIN
  IF EXISTS (
    SELECT FROM pg_catalog.pg_db_role_setting AS s
    CROSS JOIN LATERAL pg_catalog.unnest(s.setconfig) AS c(setting)
    WHERE pg_catalog.split_part(c.setting, '=', 1) !~ '^[a-z_][a-z0-9_.]*$'
  ) THEN
    RAISE EXCEPTION 'unsupported role setting name';
  END IF;
  IF EXISTS (
    SELECT FROM pg_catalog.pg_authid AS a
    WHERE a.rolname !~ '^pg_' AND a.rolname <> ALL (pg_catalog.string_to_array(pg_catalog.current_setting('pg_major.bootstrap_roles'), ','))
      AND (a.rolsuper OR a.rolreplication OR a.rolbypassrls)
  ) THEN
    RAISE EXCEPTION 'privileged non-bootstrap role requires manual review';
  END IF;
END
$check$;
SELECT pg_catalog.format('CREATE ROLE %I WITH NOSUPERUSER %s %s %s %s NOREPLICATION NOBYPASSRLS CONNECTION LIMIT %s%s%s;',
  a.rolname,
  CASE WHEN a.rolinherit THEN 'INHERIT' ELSE 'NOINHERIT' END,
  CASE WHEN a.rolcreaterole THEN 'CREATEROLE' ELSE 'NOCREATEROLE' END,
  CASE WHEN a.rolcreatedb THEN 'CREATEDB' ELSE 'NOCREATEDB' END,
  CASE WHEN a.rolcanlogin THEN 'LOGIN' ELSE 'NOLOGIN' END,
  a.rolconnlimit,
  CASE WHEN a.rolpassword IS NULL THEN '' ELSE pg_catalog.format(' PASSWORD %L', a.rolpassword) END,
  CASE WHEN a.rolvaliduntil IS NULL THEN '' ELSE pg_catalog.format(' VALID UNTIL %L', a.rolvaliduntil) END)
FROM pg_catalog.pg_authid AS a
WHERE a.rolname !~ '^pg_' AND a.rolname <> ALL (pg_catalog.string_to_array(:'bootstrap_roles', ','))
ORDER BY a.rolname;
SELECT pg_catalog.format('GRANT %I TO %I WITH ADMIN %s, INHERIT %s, SET %s;', r.rolname, m.rolname,
  pg_catalog.upper(a.admin_option::text), pg_catalog.upper(a.inherit_option::text), pg_catalog.upper(a.set_option::text))
FROM pg_catalog.pg_auth_members AS a
JOIN pg_catalog.pg_roles AS r ON r.oid = a.roleid
JOIN pg_catalog.pg_roles AS m ON m.oid = a.member
WHERE m.rolname !~ '^pg_'
ORDER BY r.rolname, m.rolname;
SELECT pg_catalog.format('ALTER ROLE %I SET %s TO %L;', r.rolname,
  pg_catalog.split_part(c.setting, '=', 1), pg_catalog.substr(c.setting, pg_catalog.strpos(c.setting, '=') + 1))
FROM pg_catalog.pg_db_role_setting AS s
JOIN pg_catalog.pg_roles AS r ON r.oid = s.setrole
CROSS JOIN LATERAL pg_catalog.unnest(s.setconfig) AS c(setting)
WHERE s.setdatabase = 0 AND r.rolname !~ '^pg_'
ORDER BY r.rolname, c.setting;
SELECT pg_catalog.format('ALTER DATABASE %I SET %s TO %L;', pg_catalog.current_database(),
  pg_catalog.split_part(c.setting, '=', 1), pg_catalog.substr(c.setting, pg_catalog.strpos(c.setting, '=') + 1))
FROM pg_catalog.pg_db_role_setting AS s
JOIN pg_catalog.pg_database AS d ON d.oid = s.setdatabase
CROSS JOIN LATERAL pg_catalog.unnest(s.setconfig) AS c(setting)
WHERE s.setrole = 0 AND d.datname = pg_catalog.current_database()
ORDER BY c.setting;
SELECT pg_catalog.format('ALTER ROLE %I IN DATABASE %I SET %s TO %L;', r.rolname, pg_catalog.current_database(),
  pg_catalog.split_part(c.setting, '=', 1), pg_catalog.substr(c.setting, pg_catalog.strpos(c.setting, '=') + 1))
FROM pg_catalog.pg_db_role_setting AS s
JOIN pg_catalog.pg_database AS d ON d.oid = s.setdatabase
JOIN pg_catalog.pg_roles AS r ON r.oid = s.setrole
CROSS JOIN LATERAL pg_catalog.unnest(s.setconfig) AS c(setting)
WHERE d.datname = pg_catalog.current_database()
ORDER BY r.rolname, c.setting;
SELECT pg_catalog.format('GRANT %s ON DATABASE %I TO %s;', a.privilege_type, d.datname,
  CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_catalog.quote_ident(pg_catalog.pg_get_userbyid(a.grantee)) END)
FROM pg_catalog.pg_database AS d
CROSS JOIN LATERAL pg_catalog.aclexplode(coalesce(d.datacl, pg_catalog.acldefault('d', d.datdba))) AS a
WHERE d.datname = pg_catalog.current_database() AND a.grantee <> d.datdba
ORDER BY 1;
