UPDATE event_grade_requests q
SET outcome = 'unavailable'
FROM event_grade_chapters c
WHERE (c.region,c.event_id,c.chapter_id) = (q.region,q.event_id,q.chapter_id)
  AND q.region = $1
  AND q.outcome IS NULL
  AND greatest(c.event_ends_at,c.reveals_at) + interval '24 hours' <= statement_timestamp()
  AND NOT EXISTS (
      SELECT 1
      FROM event_grade_borders b
      WHERE (b.region,b.event_id,b.chapter_id,b.board_kind) = (q.region,q.event_id,q.chapter_id,q.board_kind)
        AND NOT c.binding_conflicted
        AND b.is_final
        AND b.started_at >= greatest(c.event_ends_at,c.reveals_at)
        AND b.observed_at <= greatest(c.event_ends_at,c.reveals_at) + interval '24 hours'
  )