SELECT event_id,chapter_id,greatest(event_ends_at,reveals_at) AS final_after,c.observed_at
FROM event_grade_chapters c JOIN event_grade_catalogs g USING(region,observed_at)
WHERE region=$1 AND NOT c.binding_conflicted AND starts_at<=statement_timestamp() AND ends_at>statement_timestamp()
AND observed_at BETWEEN statement_timestamp()-interval '2 minutes' AND statement_timestamp()
UNION ALL
SELECT c.event_id,c.chapter_id,greatest(c.event_ends_at,c.reveals_at) AS final_after,c.observed_at
FROM event_grade_chapters c
WHERE c.region=$1 AND NOT c.binding_conflicted AND greatest(c.event_ends_at,c.reveals_at)
    BETWEEN statement_timestamp()-interval '24 hours' AND statement_timestamp()
AND NOT EXISTS (
    SELECT 1 FROM event_grade_borders b
    WHERE (b.region,b.event_id,b.chapter_id)=(c.region,c.event_id,c.chapter_id)
      AND b.board_kind='marathon_score' AND b.is_final
)
ORDER BY final_after,event_id,chapter_id
