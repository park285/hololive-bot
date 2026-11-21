WITH blocked AS (
    UPDATE event_grade_chapters
    SET binding_conflicted = true
    WHERE region = $1 AND NOT (chapter_id = ANY($2::text[]))
    RETURNING region,event_id,chapter_id
)
DELETE FROM event_grade_borders b
USING blocked x
WHERE (b.region,b.event_id,b.chapter_id) = (x.region,x.event_id,x.chapter_id)
