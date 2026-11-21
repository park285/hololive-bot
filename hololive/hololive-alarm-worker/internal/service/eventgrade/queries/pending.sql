SELECT id,room_id,region,score,outcome FROM event_grade_requests
WHERE outcome IS NOT NULL AND published_at IS NULL AND dispatch_rejection IS NULL ORDER BY id LIMIT $1
