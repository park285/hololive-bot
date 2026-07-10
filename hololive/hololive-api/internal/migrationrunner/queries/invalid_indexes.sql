
		SELECT format('%I.%I', n.nspname, c.relname)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE NOT i.indisvalid
		  AND NOT EXISTS (
		      SELECT 1 FROM pg_stat_progress_create_index p
		      WHERE p.index_relid = i.indexrelid
		  )
		ORDER BY 1