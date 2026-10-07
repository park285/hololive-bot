
		UPDATE members
		SET aliases =
			jsonb_set(
				aliases,
				ARRAY[$2]::text[],
				CASE
					WHEN jsonb_exists(aliases -> $2, $3) THEN aliases -> $2
					ELSE (aliases -> $2) || jsonb_build_array($3::text)
				END,
				true
			)
		WHERE id = $1
	