-- CompleteSent 트랜잭션의 첫 문장이다. 고정 요청을 멤버 delivery보다 먼저 잠가 claim(transition_claim_pending.sql)과
-- handoff reconciler와 같은 요청 → 멤버 순서를 지킨다. 요청 세대($2)와 고정 멤버 집합(이번 발송의 owner 전원 $3)이
-- 맞는 요청만 잠그고 돌려준다. 행이 없으면 호출자는 SENT 전이 없이 트랜잭션을 되돌린다.
SELECT r.base_id
FROM youtube_notification_send_request r
WHERE r.base_id = $1
  AND r.generation = $2::INT
  AND r.member_ids @> $3::BIGINT[]
  AND r.member_ids <@ $3::BIGINT[]
FOR UPDATE
