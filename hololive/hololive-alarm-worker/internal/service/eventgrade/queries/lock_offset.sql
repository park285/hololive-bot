SELECT o.applied_seq, r.published_seq
FROM event_grade_consumer_offsets o JOIN event_grade_regions r ON r.region = o.region
WHERE o.region = $1 FOR UPDATE OF o
