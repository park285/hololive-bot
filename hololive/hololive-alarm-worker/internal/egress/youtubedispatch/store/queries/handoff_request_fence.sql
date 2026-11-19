WITH locked AS MATERIALIZED (
    SELECT r.base_id, r.handoff_request_id, r.generation, r.message_hash, r.member_ids,
           r.handoff_next_check_at, r.handoff_checked_at, r.xmin AS claim_xid
    FROM youtube_notification_send_request r
    WHERE r.base_id = $1
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.base_id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT COALESCE(locked.handoff_next_check_at = $2::TIMESTAMPTZ
                    AND locked.handoff_checked_at IS NULL AND locked.claim_xid = $7::xid, FALSE) AS token_matches,
           COALESCE(locked.handoff_request_id = $3::TEXT
                    AND locked.generation = $4::INT
                    AND locked.message_hash = $5::TEXT
                    AND locked.member_ids = $6::BIGINT[], FALSE) AS request_matches
    FROM locked
)
