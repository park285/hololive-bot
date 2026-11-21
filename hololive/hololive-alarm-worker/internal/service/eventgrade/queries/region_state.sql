SELECT r.published_seq, o.applied_seq,
       EXISTS (SELECT 1 FROM event_grade_collection_leases l
               WHERE l.region = r.region AND l.kind = 'cutoff' AND l.owner_instance IS NOT NULL
                 AND l.lease_expires_at > statement_timestamp())
FROM event_grade_regions r JOIN event_grade_consumer_offsets o USING (region)
WHERE r.region = $1
