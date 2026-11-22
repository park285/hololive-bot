DO $check$
BEGIN
  IF EXISTS (SELECT FROM pg_catalog.pg_default_acl WHERE defaclnamespace = 0) THEN
    RAISE EXCEPTION 'new cluster has global default privileges before restore';
  END IF;
END
$check$;
SELECT pg_catalog.format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I REVOKE ALL ON %s FROM %s',
  pg_catalog.pg_get_userbyid(d.defaclrole), n.nspname,
  CASE d.defaclobjtype WHEN 'r' THEN 'TABLES' WHEN 'S' THEN 'SEQUENCES' WHEN 'f' THEN 'FUNCTIONS' WHEN 'T' THEN 'TYPES' END,
  CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_catalog.quote_ident(pg_catalog.pg_get_userbyid(a.grantee)) END)
FROM pg_catalog.pg_default_acl AS d
JOIN pg_catalog.pg_namespace AS n ON n.oid = d.defaclnamespace
CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) AS a \gexec
SELECT pg_catalog.count(*) FROM pg_catalog.pg_default_acl;
