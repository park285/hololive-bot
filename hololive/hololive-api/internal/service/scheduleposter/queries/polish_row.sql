UPDATE schedule_poster_rows
SET title_ko = $3, with_ko = $4, place_ko = $5, tags_ko = $6, note_ko = $7, doubt_ko = '', warnings = '{}', edited_at = now(),
    terms = COALESCE((
        SELECT jsonb_agg(t.term ORDER BY t.n)
        FROM jsonb_array_elements(terms) WITH ORDINALITY AS t(term, n)
        WHERE strpos($8, t.term ->> 'ko') > 0
    ), '[]'::jsonb)
WHERE poster_id = $1 AND ordinal = $2
