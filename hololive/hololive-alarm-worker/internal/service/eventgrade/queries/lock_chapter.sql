SELECT 1 FROM event_grade_chapters c
WHERE (c.region,c.event_id,c.chapter_id)=($1,$2,$3) AND NOT c.binding_conflicted
  AND $5::timestamptz <= statement_timestamp() AND $4::timestamptz <= $5::timestamptz
  AND (NOT $6 OR ($4 >= greatest(c.event_ends_at,c.reveals_at) AND $5 <= greatest(c.event_ends_at,c.reveals_at)+interval '24 hours'))
FOR UPDATE
