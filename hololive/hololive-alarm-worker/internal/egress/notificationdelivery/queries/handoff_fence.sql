-- handoff 적용 문장(handoff_*.sql)이 함께 쓰는 fence 앞부분이며 각 문장의 handoff_fence 표시 자리에 들어간다.
-- 행($1)을 잠근 뒤 clock을 한 번 채취한다. 조건은 sendfence 관측 열이다. 소유자 없는 확인이라 owner_matches와
-- lease_valid는 TRUE이고, 단일 행 원장이라 membership_matches와 members_non_terminal도 TRUE다.
--   status_matches: 행이 SENT다.
--   version_matches: soft claim 토큰($2 = handoff_next_check_at)이 그대로이고 확인이 끝나지 않았다.
--   request_matches: Iris requestId($3)와 고정 요청 세대($4)가 claim 때와 같고 요청이 소진되지 않았다.
-- 다음 확인 시각은 확인 대기 SENT 행에만 있으므로(chk_notification_delivery_outbox_handoff_shape) 토큰이 맞으면 행은
-- SENT이고 요청 ID·세대도 claim 때 값이다. 그래서 토큰이 남은 채 다른 조건만 어긋나 확인을 따로 닫을 경우는 없다.
-- passed는 모든 조건이 참인 행이다.
WITH locked AS MATERIALIZED (
    SELECT o.id, o.status, o.sent_at, o.handoff_request_id, o.handoff_next_check_at, o.handoff_checked_at,
           o.payload->'request' AS request
    FROM notification_delivery_outbox o
    WHERE o.id = $1
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status, locked.sent_at,
           locked.status = 'SENT' AS status_matches,
           COALESCE(locked.handoff_next_check_at = $2::TIMESTAMPTZ AND locked.handoff_checked_at IS NULL, FALSE)
               AS version_matches,
           COALESCE(locked.handoff_request_id = $3
                    AND (locked.request->>'generation')::INT = $4::INT
                    AND COALESCE(locked.request->>'exhausted', 'false') <> 'true', FALSE) AS request_matches
    FROM locked
), passed AS MATERIALIZED (
    SELECT checked.id, checked.sent_at
    FROM checked
    WHERE checked.status_matches AND checked.version_matches AND checked.request_matches
)
