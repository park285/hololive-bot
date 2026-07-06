BEGIN;

UPDATE members
SET aliases =
	jsonb_set(
		COALESCE(aliases, '{}'::jsonb),
		ARRAY['ko']::text[],
		CASE
			WHEN jsonb_exists(COALESCE(aliases -> 'ko', '[]'::jsonb), '미코치') THEN COALESCE(aliases -> 'ko', '[]'::jsonb)
			ELSE COALESCE(aliases -> 'ko', '[]'::jsonb) || jsonb_build_array('미코치'::text)
		END,
		true
	)
WHERE slug = 'sakuramiko'
   OR channel_id = 'UC-hM6YJuNYVAmUWxeIr9FeA'
   OR english_name = 'Sakura Miko';

COMMIT;
