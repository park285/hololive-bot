-- 같은 주를 덮는 다른 확정본이 먼저 정한 표기다. 먼저 저장한 포스터, 앞 행, 행 안 순서로 돌려준다.
SELECT t.term ->> 'source', t.term ->> 'ko'
FROM schedule_posters p
JOIN schedule_poster_rows r ON r.poster_id = p.id
CROSS JOIN LATERAL jsonb_array_elements(r.terms) WITH ORDINALITY AS t(term, n)
WHERE p.id <> $1 AND p.review_status = 'confirmed' AND p.week_start <= $3::date AND p.week_end >= $2::date
ORDER BY p.rows_extracted_at, p.id, r.ordinal, t.n
