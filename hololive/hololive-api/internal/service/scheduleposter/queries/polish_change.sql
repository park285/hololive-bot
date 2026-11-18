UPDATE schedule_poster_changes
SET title_ko = $3, with_ko = $4, place_ko = $5, tags_ko = $6, note_ko = $7, doubt_ko = '', warnings = '{}', edited_at = now(),
    terms = COALESCE((
        SELECT jsonb_agg(t.term ORDER BY t.n)
        FROM jsonb_array_elements(terms) WITH ORDINALITY AS t(term, n)
        WHERE strpos($8, t.term ->> 'ko') > 0
    ), '[]'::jsonb)
WHERE post_id = $1 AND item = $2 AND created_at = $9
  AND channel_id = (SELECT channel_id FROM schedule_posters WHERE id = $10)
