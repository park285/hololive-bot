# Phase 03. Dispatcher hot path 개선

## 목표

dispatcher가 많은 pending row를 처리할 때 PG와 Valkey에 불필요한 압력을 만들지 않게 합니다.

## 작업

1. reconciliation을 매 batch 실행에서 interval throttle로 바꿉니다.
2. retry/DLQ/quarantine update를 batch update로 바꿉니다.
3. room group 간 error isolation을 적용합니다.
4. dispatch loop에 drain budget을 둡니다.
5. blocking wakeup wait는 전용 Valkey client 또는 전용 connection 경로로 분리합니다.
6. wakeup TTL/guard TTL을 정리합니다.

## 완료 기준

- backlog 처리 중 stale recovery query가 매 batch마다 실행되지 않습니다.
- Iris 장애 시 quarantine이 row-by-row update로 폭증하지 않습니다.
- 한 room group 실패가 다른 room group 처리를 취소하지 않습니다.
- wakeup이 없어도 fallback interval로 PG scan이 동작합니다.
- Valkey blocking wait가 다른 cache command를 막지 않습니다.

## 금지

- `sending` ambiguous failure를 Iris idempotency 전 retry로 바꾸지 않습니다.
- `BRPOP`에 여러 key를 넘기지 않습니다.
- dispatcher가 event payload를 delivery 수만큼 join해서 가져오지 않습니다.

## 관련 task cards

- `T12-reconciliation-throttle.md`
- `T13-batch-retry-dlq-quarantine-updates.md`
- `T14-dispatch-group-error-isolation.md`
- `T15-drain-budget-and-loop-control.md`
- `T16-blocking-wakeup-client-split.md`
