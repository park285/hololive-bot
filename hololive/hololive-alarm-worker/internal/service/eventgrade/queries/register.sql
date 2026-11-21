WITH active AS MATERIALIZED (
    SELECT c.region,c.event_id,c.chapter_id,c.event_type,c.ends_at,c.event_ends_at,c.reveals_at
    FROM event_grade_chapters c JOIN event_grade_catalogs g USING (region,observed_at)
    WHERE region = $1 AND NOT c.binding_conflicted AND c.starts_at <= statement_timestamp() AND c.ends_at > statement_timestamp()
      AND g.observed_at BETWEEN statement_timestamp() - interval '2 minutes' AND statement_timestamp()
    LIMIT 2
    FOR SHARE OF g,c
), registered AS (
    INSERT INTO event_grade_requests (room_id,user_id,region,event_id,chapter_id,board_kind,score)
    SELECT $2,$3,region,event_id,chapter_id,'marathon_score',$4
    FROM active WHERE (SELECT count(*) FROM active) = 1
    ON CONFLICT (room_id,user_id,region,event_id,chapter_id,board_kind) DO UPDATE SET score=EXCLUDED.score
    WHERE event_grade_requests.outcome IS NULL
    RETURNING region,event_id,chapter_id
)
SELECT a.event_type,a.ends_at,greatest(a.event_ends_at,a.reveals_at)
FROM registered JOIN active a USING (region,event_id,chapter_id)
