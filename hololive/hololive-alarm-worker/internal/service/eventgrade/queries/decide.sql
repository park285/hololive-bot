WITH ready AS MATERIALIZED (
    SELECT q.id, q.score, b.ranks, b.scores
    FROM event_grade_requests q
    JOIN event_grade_chapters c USING (region,event_id,chapter_id)
    LEFT JOIN LATERAL (
        SELECT array_agg(b.rank ORDER BY b.rank) AS ranks,
               array_agg(b.score ORDER BY b.rank) AS scores
        FROM event_grade_borders b
        WHERE (b.region,b.event_id,b.chapter_id,b.board_kind) = (q.region,q.event_id,q.chapter_id,q.board_kind)
          AND NOT c.binding_conflicted AND b.is_final AND b.started_at >= greatest(c.event_ends_at,c.reveals_at) AND b.observed_at <= least(statement_timestamp(),greatest(c.event_ends_at,c.reveals_at)+interval '24 hours')
        GROUP BY b.observed_at
        HAVING count(*) BETWEEN 1 AND 32
    ) b ON true
    WHERE q.region = ANY($1::text[]) AND q.outcome IS NULL AND greatest(c.event_ends_at,c.reveals_at) <= statement_timestamp()
      AND (b.ranks IS NOT NULL OR greatest(c.event_ends_at,c.reveals_at)+interval '24 hours' <= statement_timestamp())
    ORDER BY greatest(c.event_ends_at,c.reveals_at),q.id LIMIT 100
    FOR SHARE OF c SKIP LOCKED FOR UPDATE OF q SKIP LOCKED
)
UPDATE event_grade_requests q SET outcome = CASE
    WHEN r.ranks IS NULL THEN 'unavailable'
    WHEN r.score = ANY(r.scores) THEN 'boundary'
    ELSE COALESCE(
        'top' || (SELECT min(cut.rank)::text FROM unnest(r.ranks,r.scores) cut(rank,score) WHERE r.score > cut.score),
        'outside'
    ) END
FROM ready r WHERE q.id=r.id
