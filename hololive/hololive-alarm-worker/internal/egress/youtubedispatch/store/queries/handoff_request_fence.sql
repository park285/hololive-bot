-- handoff 적용 문장(handoff_complete.sql·handoff_reschedule.sql·handoff_request_lock.sql)이 함께 쓰는 고정 요청 fence
-- 앞부분이며 각 문장의 handoff_request_fence 표시 자리에 들어간다. 요청($1)을 잠근 뒤 clock을 한 번 채취한다. 멤버
-- delivery 행은 이 부분에서 잠그지 않는다.
--   token_matches: soft claim 토큰($2 = handoff_next_check_at)이 그대로이고 확인이 끝나지 않았다.
--   request_matches: Iris requestId($3), 요청 세대($4), 고정 본문 해시($5), 고정 멤버 배열($6)이 claim 때와 같다.
-- 요청이 없으면 checked는 행이 없다.
WITH locked AS MATERIALIZED (
    SELECT r.base_id, r.handoff_request_id, r.generation, r.message_hash, r.member_ids,
           r.handoff_next_check_at, r.handoff_checked_at
    FROM youtube_notification_send_request r
    WHERE r.base_id = $1
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.base_id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT COALESCE(locked.handoff_next_check_at = $2::TIMESTAMPTZ AND locked.handoff_checked_at IS NULL, FALSE)
               AS token_matches,
           COALESCE(locked.handoff_request_id = $3::TEXT
                    AND locked.generation = $4::INT
                    AND locked.message_hash = $5::TEXT
                    AND locked.member_ids = $6::BIGINT[], FALSE) AS request_matches
    FROM locked
)
