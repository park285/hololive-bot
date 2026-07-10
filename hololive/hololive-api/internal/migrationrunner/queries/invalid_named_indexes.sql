WITH targets AS (
	SELECT index_name, table_relation
	FROM unnest($1::text[], $2::text[]) AS input(index_name, table_relation)
)
SELECT format('%I.%I', n.nspname, c.relname)
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE NOT i.indisvalid
	AND NOT EXISTS (
		SELECT 1
		FROM pg_stat_progress_create_index p
		WHERE p.index_relid = i.indexrelid
	)
	AND EXISTS (
		SELECT 1
		FROM targets t
		WHERE i.indrelid = to_regclass(t.table_relation)
			AND c.relname = t.index_name
	)
ORDER BY 1
