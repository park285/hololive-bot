SELECT
	COALESCE((
		SELECT checksum_sha256::text
		FROM schema_migration_checksums
		WHERE filename = $1
	), ''),
	EXISTS(SELECT 1 FROM schema_migration_checksums WHERE filename = $1)
