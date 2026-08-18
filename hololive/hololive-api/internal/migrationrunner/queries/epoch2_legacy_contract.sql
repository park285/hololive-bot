SELECT expected.filename,
       expected.checksum,
       sm.filename IS NOT NULL AS ledger_present,
       mc.filename IS NOT NULL AS checksum_present,
       COALESCE(mc.checksum_sha256::text, '') AS actual_checksum
FROM unnest($1::text[], $2::text[]) WITH ORDINALITY AS expected(filename, checksum, position)
LEFT JOIN schema_migrations AS sm ON sm.filename = expected.filename
LEFT JOIN schema_migration_checksums AS mc ON mc.filename = expected.filename
ORDER BY expected.position
