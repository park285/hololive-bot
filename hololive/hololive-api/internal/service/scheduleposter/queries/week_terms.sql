SELECT t.term ->> 'source', t.term ->> 'ko'
FROM schedule_posters p
JOIN schedule_poster_rows r ON r.poster_id = p.id
CROSS JOIN LATERAL jsonb_array_elements(r.terms) WITH ORDINALITY AS t(term, n)
WHERE p.id <> $1 AND p.review_status = 'confirmed' AND p.week_start <= $3::date AND p.week_end >= $2::date
ORDER BY p.rows_extracted_at, p.id, r.ordinal, t.n
