# Phase 02. Publisher 고 fan-out 성능 개선

## 목표

`PublishBatch()`가 이름뿐인 batch가 아니라 실제 DB round-trip을 줄이는 set-based write path가 되게 합니다.

## 핵심 문제

현재 repository는 batch input을 받지만 event와 delivery를 루프로 insert합니다. 고 fan-out 이벤트에서는 SQL statement 수가 delivery 수에 비례합니다.

## 작업

1. event insert를 `unnest` 또는 staging CTE 기반 set-based insert로 바꿉니다.
2. delivery insert를 set-based insert로 바꿉니다.
3. batch chunking 의미를 명확히 합니다.
4. chunk 실패 시 claim release 범위를 해당 chunk로 제한합니다.
5. `MAX_EVENTS_PER_BATCH`, `MAX_DELIVERIES_PER_BATCH` config를 추가합니다.
6. insert result metric을 추가합니다.

## 완료 기준

- 1 event + 1,000 delivery publish가 delivery별 SQL 1,000번을 실행하지 않습니다.
- event insert statement는 chunk당 1개 또는 소수입니다.
- delivery insert statement는 chunk당 1개 또는 소수입니다.
- 동일 event payload는 DB에 한 번만 저장됩니다.
- publish latency/load test에서 기존 row-by-row 대비 개선이 확인됩니다.

## 금지

- `PublishBatch()` 내부에서 `Publish()` 반복 호출 금지.
- event payload를 room별로 복제 저장 금지.
- chunk partial success를 숨기고 전체 실패처럼 처리 금지.

## 관련 task cards

- `T08-set-based-event-insert.md`
- `T09-set-based-delivery-insert.md`
- `T10-publisher-chunk-semantics.md`
- `T11-publisher-batch-limits-and-backpressure.md`
