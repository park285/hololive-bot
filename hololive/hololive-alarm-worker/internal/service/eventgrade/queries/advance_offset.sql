UPDATE event_grade_consumer_offsets SET applied_seq = $2 WHERE region = $1 AND applied_seq = $2 - 1
