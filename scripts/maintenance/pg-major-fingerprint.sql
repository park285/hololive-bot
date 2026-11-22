SELECT 'role', a.rolname, a.rolsuper, a.rolinherit, a.rolcreaterole, a.rolcreatedb, a.rolcanlogin, a.rolreplication,
  a.rolbypassrls, a.rolconnlimit, coalesce(a.rolvaliduntil::text, ''), a.rolpassword IS NOT NULL
FROM pg_catalog.pg_authid AS a WHERE a.rolname !~ '^pg_' ORDER BY 2;
SELECT 'member', r.rolname, m.rolname, a.admin_option, a.inherit_option, a.set_option
FROM pg_catalog.pg_auth_members AS a
JOIN pg_catalog.pg_roles AS r ON r.oid = a.roleid
JOIN pg_catalog.pg_roles AS m ON m.oid = a.member
WHERE m.rolname !~ '^pg_' ORDER BY 2, 3;
SELECT 'setting', coalesce(d.datname, '*'), coalesce(r.rolname, '*'),
  (SELECT pg_catalog.string_agg(c, ',' ORDER BY c) FROM pg_catalog.unnest(s.setconfig) AS c)
FROM pg_catalog.pg_db_role_setting AS s
LEFT JOIN pg_catalog.pg_database AS d ON d.oid = s.setdatabase
LEFT JOIN pg_catalog.pg_roles AS r ON r.oid = s.setrole
ORDER BY 2, 3;
SELECT 'database', d.datname, pg_catalog.pg_get_userbyid(d.datdba), pg_catalog.pg_encoding_to_char(d.encoding), d.datlocprovider,
  coalesce(d.datlocale, ''), d.datcollate, d.datctype,
  (SELECT pg_catalog.string_agg(x::text, ',' ORDER BY x::text) FROM pg_catalog.unnest(coalesce(d.datacl, pg_catalog.acldefault('d', d.datdba))) AS x)
FROM pg_catalog.pg_database AS d WHERE d.datname = pg_catalog.current_database();
SELECT 'namespace', n.nspname, pg_catalog.pg_get_userbyid(n.nspowner),
  (SELECT pg_catalog.string_agg(x::text, ',' ORDER BY x::text) FROM pg_catalog.unnest(coalesce(n.nspacl, pg_catalog.acldefault('n', n.nspowner))) AS x)
FROM pg_catalog.pg_namespace AS n WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' ORDER BY 2;
SELECT 'extension', e.extname, e.extnamespace::regnamespace FROM pg_catalog.pg_extension AS e ORDER BY 2;
SELECT 'default-acl', pg_catalog.pg_get_userbyid(d.defaclrole), coalesce(d.defaclnamespace::regnamespace::text, '*'), d.defaclobjtype,
  coalesce((SELECT pg_catalog.string_agg(x::text, ',' ORDER BY x::text) FROM pg_catalog.unnest(d.defaclacl) AS x), '')
FROM pg_catalog.pg_default_acl AS d ORDER BY 2, 3, 4;
SELECT 'relation', n.nspname, c.relname, c.relkind, c.relpersistence, pg_catalog.pg_get_userbyid(c.relowner),
  coalesce((SELECT pg_catalog.string_agg(x::text, ',' ORDER BY x::text)
    FROM pg_catalog.unnest(coalesce(c.relacl, pg_catalog.acldefault(CASE c.relkind WHEN 'S' THEN 's'::"char" ELSE 'r'::"char" END, c.relowner))) AS x), ''),
  coalesce(pg_catalog.array_to_string(c.reloptions, ','), '')
FROM pg_catalog.pg_class AS c JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f', 'i', 'I', 'c')
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_depend AS e WHERE e.classid = 'pg_catalog.pg_class'::regclass AND e.objid = c.oid AND e.deptype = 'e')
ORDER BY 2, 3;
SELECT 'column', n.nspname, c.relname, pg_catalog.row_number() OVER (PARTITION BY c.oid ORDER BY a.attnum), a.attname,
  pg_catalog.format_type(a.atttypid, a.atttypmod), a.attnotnull, coalesce(pg_catalog.pg_get_expr(ad.adbin, ad.adrelid), ''),
  a.attidentity, a.attgenerated,
  coalesce((SELECT pg_catalog.string_agg(x::text, ',' ORDER BY x::text) FROM pg_catalog.unnest(a.attacl) AS x), '')
FROM pg_catalog.pg_attribute AS a
JOIN pg_catalog.pg_class AS c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_attrdef AS ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' AND c.relkind IN ('r', 'p', 'v', 'm', 'f', 'c') AND a.attnum > 0 AND NOT a.attisdropped
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_depend AS e WHERE e.classid = 'pg_catalog.pg_class'::regclass AND e.objid = c.oid AND e.deptype = 'e')
ORDER BY 2, 3, 4;
SELECT 'constraint', n.nspname, c.conrelid::regclass::text, c.contypid::regtype::text, c.conname, c.contype, c.convalidated,
  pg_catalog.md5(pg_catalog.regexp_replace(pg_catalog.pg_get_constraintdef(c.oid), '[()[:space:]]', '', 'g'))
FROM pg_catalog.pg_constraint AS c JOIN pg_catalog.pg_namespace AS n ON n.oid = c.connamespace
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' ORDER BY 2, 3, 4, 5;
SELECT 'index', n.nspname, i.indexrelid::regclass::text, pg_catalog.md5(pg_catalog.regexp_replace(pg_catalog.pg_get_indexdef(i.indexrelid), '[()[:space:]]', '', 'g')), i.indisvalid, i.indisready
FROM pg_catalog.pg_index AS i JOIN pg_catalog.pg_class AS c ON c.oid = i.indrelid JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' ORDER BY 2, 3;
SELECT 'trigger', n.nspname, t.tgrelid::regclass::text, t.tgname, t.tgenabled, pg_catalog.md5(pg_catalog.pg_get_triggerdef(t.oid))
FROM pg_catalog.pg_trigger AS t JOIN pg_catalog.pg_class AS c ON c.oid = t.tgrelid JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' AND NOT t.tgisinternal ORDER BY 2, 3, 4;
SELECT 'function', n.nspname, p.oid::regprocedure::text, pg_catalog.pg_get_userbyid(p.proowner), p.prokind, p.prosecdef,
  coalesce(pg_catalog.array_to_string(p.proconfig, ','), ''), pg_catalog.md5(p.prosrc),
  (SELECT pg_catalog.string_agg(x::text, ',' ORDER BY x::text) FROM pg_catalog.unnest(coalesce(p.proacl, pg_catalog.acldefault('f', p.proowner))) AS x)
FROM pg_catalog.pg_proc AS p JOIN pg_catalog.pg_namespace AS n ON n.oid = p.pronamespace
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_depend AS d WHERE d.classid = 'pg_catalog.pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')
ORDER BY 2, 3;
SELECT 'type', n.nspname, t.typname, t.typtype, pg_catalog.pg_get_userbyid(t.typowner),
  coalesce((SELECT pg_catalog.string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder) FROM pg_catalog.pg_enum AS e WHERE e.enumtypid = t.oid), '')
FROM pg_catalog.pg_type AS t JOIN pg_catalog.pg_namespace AS n ON n.oid = t.typnamespace
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' AND t.typtype IN ('d', 'e', 'r', 'm')
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_depend AS d WHERE d.classid = 'pg_catalog.pg_type'::regclass AND d.objid = t.oid AND d.deptype = 'e')
ORDER BY 2, 3;
SELECT 'sequence', s.schemaname, s.sequencename, s.data_type, s.start_value, s.min_value, s.max_value, s.increment_by, s.cycle, s.cache_size,
  coalesce(s.last_value::text, '')
FROM pg_catalog.pg_sequences AS s WHERE s.schemaname !~ '^pg_' AND s.schemaname <> 'information_schema' ORDER BY 2, 3;
SELECT pg_catalog.format('SELECT %L, %L, %L, pg_catalog.count(*), coalesce(pg_catalog.sum((''x'' || pg_catalog.left(pg_catalog.md5(t::text), 16))::bit(64)::bigint::numeric), 0) FROM ONLY %s AS t',
  'rows', n.nspname, c.relname, c.oid::regclass)
FROM pg_catalog.pg_class AS c JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' AND c.relkind = 'r'
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_depend AS d WHERE d.classid = 'pg_catalog.pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e')
ORDER BY n.nspname, c.relname \gexec
