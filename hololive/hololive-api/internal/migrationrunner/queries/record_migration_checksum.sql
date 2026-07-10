INSERT INTO schema_migration_checksums (filename, checksum_sha256)
VALUES ($1, $2)
ON CONFLICT (filename) DO UPDATE
SET checksum_sha256 = EXCLUDED.checksum_sha256
WHERE schema_migration_checksums.checksum_sha256 = EXCLUDED.checksum_sha256
