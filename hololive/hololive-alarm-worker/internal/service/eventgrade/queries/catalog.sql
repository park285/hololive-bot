INSERT INTO event_grade_catalogs(region,observed_at)
SELECT $1,$2::timestamptz WHERE $2 <= statement_timestamp()
ON CONFLICT (region) DO UPDATE SET observed_at = EXCLUDED.observed_at
WHERE event_grade_catalogs.observed_at < EXCLUDED.observed_at
