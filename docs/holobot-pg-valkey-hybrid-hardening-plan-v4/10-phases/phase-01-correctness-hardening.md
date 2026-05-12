# Phase 01. Correctness hardening

## 목표

성능 최적화 전에 correctness 구멍을 막습니다. 이 phase는 전체 전환 전 필수입니다.

## 작업

1. PG mode에서 Valkey wakeup 장애가 dispatcher startup/readiness를 막지 않게 합니다.
2. 동일 batch 내부 event hash conflict를 검출합니다.
3. delivery dedupe key를 raw claim key 의존 구조에서 stable domain key 구조로 정리합니다.
4. `PublishBatchResult`를 상위 계층으로 전달합니다.
5. event payload nested room/user guard를 추가합니다.

## 완료 기준

- Valkey unavailable 상태에서도 PG dispatcher가 fallback scan mode로 기동 가능합니다.
- 같은 `event_key` + 다른 `payload_hash`가 같은 batch 안에 들어오면 실패합니다.
- dedupe key는 `room_id + event_key` 기반의 짧고 예측 가능한 문자열입니다.
- queue publisher가 inserted/duplicate/hash conflict 결과를 metric으로 올릴 수 있습니다.
- payload 안에 nested `notification.room_id/users`가 들어가면 테스트가 실패합니다.

## 금지

- hash conflict를 조용히 overwrite하지 않습니다.
- shadowed row를 자동 pending으로 승격하지 않습니다.
- Valkey wakeup 실패를 publish 실패로 만들지 않습니다.

## 관련 task cards

- `T03-pg-mode-valkey-degraded-startup.md`
- `T04-in-batch-event-hash-conflict.md`
- `T05-stable-delivery-dedupe-key.md`
- `T06-publishbatch-result-propagation.md`
- `T07-nested-payload-guard.md`
