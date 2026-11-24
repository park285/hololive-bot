UPDATE event_grade_consumer_offsets SET applied_seq = $3 WHERE region = $1 AND applied_seq = $2
