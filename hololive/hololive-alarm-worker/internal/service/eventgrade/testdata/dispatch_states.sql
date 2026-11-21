SELECT coalesce(array_agg(user_id ORDER BY id) FILTER (WHERE published_at IS NOT NULL),'{}'),
       coalesce(array_agg(user_id || ':' || dispatch_rejection ORDER BY id) FILTER (WHERE dispatch_rejection IS NOT NULL),'{}'),
       coalesce(array_agg(user_id ORDER BY id) FILTER (WHERE published_at IS NULL AND dispatch_rejection IS NULL),'{}')
FROM event_grade_requests WHERE region=$1
