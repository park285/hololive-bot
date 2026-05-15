# T08. Set-based event insert

## 목적

event insert가 event 수만큼 SQL round-trip을 만들지 않게 합니다.

## 작업 대상

- `dispatchoutbox/repository.go`
- `30-sql/001-set-based-event-insert.sql`
- integration tests

## 작업

1. batch event input을 arrays 또는 temp/staging CTE로 전달합니다.
2. 기존 `event_key` row의 `payload_hash`를 검증합니다.
3. conflict가 있으면 전체 chunk를 실패시킵니다.
4. insert 후 event_key -> id map을 반환합니다.

## 완료 기준

- chunk당 event insert SQL이 1개 또는 소수입니다.
- existing event hash mismatch가 실패합니다.
- same key/same hash는 id 재사용됩니다.
- event id map이 delivery insert에 전달됩니다.

## LLM 프롬프트

row-by-row `insertEvent()` 루프를 set-based SQL로 바꾸십시오. pgx 배열 타입과 트랜잭션 처리를 안전하게 구현하고 integration test를 추가하십시오.
