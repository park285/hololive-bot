
		UPDATE members
		SET aliases =
			jsonb_set(
				COALESCE(aliases, '{}'::jsonb),
				ARRAY[$2]::text[],
				CASE
					WHEN jsonb_exists(COALESCE(aliases -> $2, '[]'::jsonb), $3) THEN COALESCE(aliases -> $2, '[]'::jsonb)
					ELSE COALESCE(aliases -> $2, '[]'::jsonb) || jsonb_build_array($3::text)
				END,
				true
			)
		WHERE id = $1
	