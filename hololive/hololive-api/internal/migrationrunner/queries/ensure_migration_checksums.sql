CREATE TABLE IF NOT EXISTS schema_migration_checksums (
	filename text PRIMARY KEY,
	checksum_sha256 char(64) NOT NULL CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
	recorded_at timestamptz NOT NULL DEFAULT now()
)
