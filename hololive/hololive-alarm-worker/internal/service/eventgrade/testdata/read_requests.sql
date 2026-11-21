SELECT count(*), coalesce(max(score),0), coalesce(max(event_id),''),
       coalesce(array_agg(outcome ORDER BY id) FILTER (WHERE published_at IS NOT NULL),'{}'::text[])
FROM event_grade_requests WHERE region=$1 AND ($2::text IS NULL OR event_id=$2)
