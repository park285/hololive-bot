/* fragment:handoff_request_fence */, members AS MATERIALIZED (
    SELECT count(d.id) = cardinality($6::BIGINT[]) AS members_match,
           min(d.sent_at) AS admitted_at
    FROM unnest($8::BIGINT[], $9::BIGINT[]) AS claimed(id, row_version)
    JOIN youtube_notification_delivery d
      ON d.id = claimed.id
     AND d.row_version = claimed.row_version
     AND d.status = 'SENT'
     AND d.send_request_id = $1
), updated AS (
    UPDATE youtube_notification_send_request r
    SET handoff_next_check_at = CASE
            WHEN members.members_match THEN LEAST(
                clock.now + ($10::BIGINT * INTERVAL '1 millisecond'),
                members.admitted_at + ($11::BIGINT * INTERVAL '1 millisecond'))
        END,
        handoff_checked_at = CASE WHEN members.members_match THEN NULL ELSE clock.now END
    FROM checked
    CROSS JOIN clock
    CROSS JOIN members
    WHERE r.base_id = $1
      AND checked.token_matches
      AND checked.request_matches
    RETURNING r.base_id
)
SELECT checked.token_matches, checked.request_matches, members.members_match,
       EXISTS (SELECT 1 FROM updated) AS applied
FROM checked
CROSS JOIN members
