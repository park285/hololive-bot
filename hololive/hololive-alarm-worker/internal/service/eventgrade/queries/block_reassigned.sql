WITH incoming AS (
    SELECT event_id,chapter_id
    FROM unnest($2::text[],$3::text[]) AS x(event_id,chapter_id)
), blocked AS (
    UPDATE event_grade_chapters c
    SET binding_conflicted = true
    FROM incoming i
    WHERE c.region = $1 AND c.chapter_id = i.chapter_id AND c.event_id <> i.event_id
    RETURNING c.region,c.event_id,c.chapter_id
), deleted AS (
    DELETE FROM event_grade_borders b
    USING blocked x
    WHERE (b.region,b.event_id,b.chapter_id) = (x.region,x.event_id,x.chapter_id)
)
SELECT chapter_id FROM blocked
