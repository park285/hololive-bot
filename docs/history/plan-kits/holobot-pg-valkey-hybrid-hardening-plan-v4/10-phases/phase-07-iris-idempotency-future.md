# Phase 07. Iris idempotency 후속 설계

## 목표

Iris가 idempotency key를 지원한 뒤 `sending` ambiguous failure의 복구성을 높입니다.

## 현재 정책

Iris idempotency 전에는 `sending` 이후 timeout/connection reset/mark sent 실패가 결과 불명입니다. 따라서 retry가 아니라 quarantine이 기본입니다.

## 향후 선택지

### 선택지 A. per-delivery send

- Iris request 1개 = delivery 1개.
- `Idempotency-Key: alarm-delivery-{delivery_id}` 사용.
- 구현 단순.
- room group message 최적화 약화.

### 선택지 B. send_attempt ledger

- room group send 1개 = send_attempt 1 row.
- `Idempotency-Key: alarm-send-{send_attempt_id}` 사용.
- group send 유지 가능.
- 세 번째 테이블과 상태 머신 추가 필요.

## 권장

현재는 2테이블 ledger를 유지하고 quarantine 정책을 사용합니다. Iris idempotency가 실제로 지원되면 `alarm_dispatch_send_attempts` 도입 여부를 별도 RFC로 결정합니다.

## 금지

- Iris idempotency 구현 전 stale sending retry 금지.
- group send에 단일 delivery idempotency key를 억지로 붙이지 않습니다.
