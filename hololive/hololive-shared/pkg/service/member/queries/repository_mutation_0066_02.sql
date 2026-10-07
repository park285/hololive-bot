
		UPDATE members
		SET aliases =
			jsonb_set(
				aliases,
				ARRAY[$2]::text[],
				COALESCE(
					(
						SELECT jsonb_agg(elem)
						FROM jsonb_array_elements(aliases -> $2) AS elem
						WHERE elem <> to_jsonb($3::text)
					),
					'[]'::jsonb
				),
				true
			)
		WHERE id = $1
	