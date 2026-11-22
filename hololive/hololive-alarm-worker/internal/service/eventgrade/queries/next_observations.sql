SELECT seq,schema_version,kind,started_at,observed_at,payload
FROM event_grade_observations WHERE region = $1 AND seq > $2 ORDER BY seq LIMIT $3
