-- CompleteSent가 SENT 전이와 같은 트랜잭션에서 Iris requestId($2)와 첫 handoff 확인 시각을 기록한다. 첫 확인은 SENT
-- 전이 문장이 잠금 뒤 채취한 DB 시계($3, owner sent_at = admission)에서 $4 밀리초 뒤이고, 호출자가 그 간격을
-- min(5s, 지평)으로 정해 첫 확인도 admission + 지평을 넘지 않는다. 요청은 같은 트랜잭션의 handoff_admit_lock.sql이
-- 잠갔다.
UPDATE youtube_notification_send_request
SET handoff_request_id = $2,
    handoff_next_check_at = $3::TIMESTAMPTZ + ($4::BIGINT * INTERVAL '1 millisecond'),
    handoff_checked_at = NULL
WHERE base_id = $1
