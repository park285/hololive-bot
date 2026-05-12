# Phase 04. Schema, index, retention 정리

## 목표

write-heavy dispatch ledger가 장기 운영에서 index 부하와 retention 문제를 만들지 않게 합니다.

## 작업

1. retention용 terminal timestamp partial index를 추가합니다.
2. 운영 조회용 `(status, created_at)` index가 정말 필요한지 재검토합니다.
3. nested payload room/user guard constraint를 적용합니다.
4. retention delete는 chunked CTE로만 수행합니다.
5. orphan event cleanup을 bounded job으로 추가합니다.
6. admin manual requeue audit를 강화합니다.

## 완료 기준

- sent/dlq/quarantined retention이 bounded query로 수행됩니다.
- terminal row delete가 한 번에 전체 table을 scan/delete하지 않습니다.
- orphan event cleanup도 limit 기반입니다.
- admin requeue는 duplicate risk ack 없이는 실행되지 않습니다.

## 금지

- unbounded DELETE 금지.
- unbounded UPDATE 금지.
- 운영 dashboard용 고빈도 `COUNT(*)` scan 금지.

## 관련 task cards

- `T17-retention-index-refinement.md`
- `T18-bounded-retention-jobs.md`
- `T19-admin-requeue-audit.md`
