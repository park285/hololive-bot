-- 확인 시각이 된 확인 대기 고정 요청을 SKIP LOCKED로 $1개까지 고르고 handoff_next_check_at을 clock + $2 밀리초로
-- 밀어 soft claim한다. 밀어 둔 값이 이후 적용의 CAS 토큰이다. 멤버 delivery 행은 잠그지 않고 읽는다. 이 요청에 묶인
-- SENT 멤버만 모아 경과(같은 clock에서 가장 이른 sent_at = admission까지, 그런 멤버가 없으면 NULL), 최대
-- attempt_count, 멤버 id와 row_version을 돌려준다. 멤버 id·row_version은 정산의 행 버전 fence다.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
), due AS MATERIALIZED (
    SELECT r.base_id, r.member_ids
    FROM youtube_notification_send_request r
    WHERE r.handoff_next_check_at <= (SELECT clock.now FROM clock)
    ORDER BY r.handoff_next_check_at ASC, r.base_id ASC
    LIMIT $1
    FOR UPDATE OF r SKIP LOCKED
)
UPDATE youtube_notification_send_request r
SET handoff_next_check_at = clock.now + ($2::BIGINT * INTERVAL '1 millisecond')
FROM due
CROSS JOIN clock
CROSS JOIN LATERAL (
    SELECT floor(EXTRACT(EPOCH FROM (clock.now - min(d.sent_at))) * 1000)::BIGINT AS elapsed_ms,
           COALESCE(max(d.attempt_count), 0) AS attempt_count,
           COALESCE(array_agg(d.id ORDER BY d.id), '{}'::BIGINT[]) AS sent_member_ids,
           COALESCE(array_agg(d.row_version ORDER BY d.id), '{}'::BIGINT[]) AS sent_member_versions
    FROM youtube_notification_delivery d
    WHERE d.id = ANY (due.member_ids)
      AND d.send_request_id = due.base_id
      AND d.status = 'SENT'
) AS members
WHERE r.base_id = due.base_id
RETURNING r.base_id, r.handoff_request_id, r.generation, r.message_hash, r.member_ids, r.handoff_next_check_at,
          members.elapsed_ms, members.attempt_count, members.sent_member_ids, members.sent_member_versions
