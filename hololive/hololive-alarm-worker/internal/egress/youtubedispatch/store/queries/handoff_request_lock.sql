-- handoff 실패 정산 트랜잭션의 첫 문장이다. 고정 요청을 잠그고 claim fence만 돌려준다. 호출자는 fence가 맞을 때만 같은
-- 트랜잭션에서 멤버 논리 그룹의 부모 outbox(id 순서), 그 그룹 delivery, 원장 순서로 잠그므로 잠금 순서는 요청 → outbox
-- → delivery → 원장이다(보존 정리·fanout과 같은 부모 → 자식). fence가 어긋나면 outbox·delivery를 잠그기 전에 끝내 같은
-- 요청을 먼저 잠근 CompleteSent·claim과 서로 기다리지 않는다.
/* handoff_request_fence */
SELECT checked.token_matches, checked.request_matches, TRUE AS members_match, FALSE AS applied
FROM checked
