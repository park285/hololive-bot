-- $8은 고친 한국어 다섯 칸을 이은 글이다. 고친 글에 남지 않은 표기는 같은 주 다른 포스터의 Earlier renderings로 퍼지지 않게 뺀다.
UPDATE schedule_poster_rows
SET title_ko = $3, with_ko = $4, place_ko = $5, tags_ko = $6, note_ko = $7, doubt_ko = '', warnings = '{}', edited_at = now(),
    terms = COALESCE((
        SELECT jsonb_agg(t.term ORDER BY t.n)
        FROM jsonb_array_elements(terms) WITH ORDINALITY AS t(term, n)
        WHERE strpos($8, t.term ->> 'ko') > 0
    ), '[]'::jsonb)
WHERE poster_id = $1 AND ordinal = $2
