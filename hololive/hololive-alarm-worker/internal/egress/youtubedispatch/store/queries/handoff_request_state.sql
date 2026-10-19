-- commit 응답 유실 뒤 고정 요청의 handoff 상태를 primary에서 다시 읽는다. CompleteSent는 admission 기록을, handoff
-- 정산은 claim 토큰이 남았는지(pre)·기대한 정산 결과인지(post)를 이 값으로 판정한다.
SELECT r.handoff_request_id, r.generation, r.handoff_next_check_at, r.handoff_checked_at
FROM youtube_notification_send_request r
WHERE r.base_id = $1
