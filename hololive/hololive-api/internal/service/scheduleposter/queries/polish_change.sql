-- $9는 읽은 변경의 created_at이다. 그 사이 글을 다시 대응해 변경이 새로 들어왔으면 다른 번역이라 바꾸지 않는다.
-- terms는 polish_row.sql과 같은 기준으로 거른다.
UPDATE schedule_poster_changes
SET title_ko = $3, with_ko = $4, place_ko = $5, tags_ko = $6, note_ko = $7, doubt_ko = '', warnings = '{}', edited_at = now(),
    terms = COALESCE((
        SELECT jsonb_agg(t.term ORDER BY t.n)
        FROM jsonb_array_elements(terms) WITH ORDINALITY AS t(term, n)
        WHERE strpos($8, t.term ->> 'ko') > 0
    ), '[]'::jsonb)
WHERE post_id = $1 AND item = $2 AND created_at = $9
