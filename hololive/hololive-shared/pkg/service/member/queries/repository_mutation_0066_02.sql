
		UPDATE members
		SET aliases =
			jsonb_set(
				COALESCE(aliases, '{}'::jsonb),
				ARRAY[$2]::text[],
				COALESCE(
					(
						SELECT jsonb_agg(elem)
						FROM jsonb_array_elements(COALESCE(aliases -> $2, '[]'::jsonb)) AS elem
						WHERE elem <> to_jsonb($3::text)
					),
					'[]'::jsonb
				),
				true
			)
		WHERE id = $1
	