# 04. V3 자체 검수 체크리스트

이 파일은 V3 설계를 구현하기 전, 그리고 각 PR review 때 확인할 항목입니다.

## 1. 아키텍처 검수

- [ ] 최종 설계가 단일 `alarm_dispatch_outbox`가 아니라 `events + deliveries`인가?
- [ ] `alarm_dispatch_events.payload`가 room-agnostic인가?
- [ ] room별 상태는 `alarm_dispatch_deliveries`에만 있는가?
- [ ] `event_key`와 `dedupe_key`의 의미가 분리되어 있는가?
- [ ] 같은 event fan-out에서 event row가 1개만 생기는가?
- [ ] claim 후 event payload를 distinct event id로 로드하는가?

## 2. Publisher 검수

- [ ] `PublishBatch()`가 기본 API인가?
- [ ] `Publish()`는 batch size 1 wrapper인가?
- [ ] `PublishBatch()`가 내부에서 `Publish()`를 반복 호출하지 않는가?
- [ ] batch size 상한이 있는가?
- [ ] shadow mode는 Valkey success 후 PG shadow insert인가?
- [ ] shadow row status는 `shadowed`인가?
- [ ] pg_first mode는 PG commit 후 wakeup인가?
- [ ] pg_first mode에서 legacy queue LPUSH가 금지되어 있는가?
- [ ] wakeup 실패가 publish 실패로 전파되지 않는가?

## 3. Valkey 검수

- [ ] dispatch wakeup이 Pub/Sub `PUBLISH` 기본 구현이 아닌가?
- [ ] wakeup token에 payload가 없는가?
- [ ] `SET guard NX PX` + `LPUSH one token` + `PEXPIRE` 구조인가?
- [ ] list TTL이 guard TTL 이하인가?
- [ ] dispatcher `BRPOP`이 fixed key 1개만 사용하는가?
- [ ] fallback scan이 존재하는가?
- [ ] dispatch hot path에 `KEYS`, unbounded `SCAN`, `LRANGE 0 -1`, `SMEMBERS`, `HGETALL`이 없는가?
- [ ] O(log N) 이상 command 예외에 주석과 bounded 근거가 있는가?

## 4. Dispatcher/state machine 검수

- [ ] PG consumer가 `pending`, `retry`만 claim하는가?
- [ ] `shadowed`를 claim하지 않는가?
- [ ] `MarkSending`은 `leased + locked_by`에서만 성공하는가?
- [ ] `MarkSent`는 `sending + locked_by`에서만 성공하는가?
- [ ] render 실패는 MarkSending 전 retry/DLQ인가?
- [ ] MarkSending conflict 시 Iris send를 금지하는가?
- [ ] MarkSent 실패 후 즉시 retry하지 않는가?
- [ ] ambiguous send error는 quarantine인가?
- [ ] stale sending은 idempotency 전까지 quarantine인가?

## 5. Reconciliation/retention 검수

- [ ] 모든 recurring UPDATE/DELETE가 bounded CTE + LIMIT인가?
- [ ] stale leased는 retry 복구인가?
- [ ] stale sending은 quarantine인가?
- [ ] terminal retention이 한 번에 무제한 삭제하지 않는가?
- [ ] orphan event cleanup이 `event_id` index를 활용하는가?
- [ ] manual requeue에 operator acknowledgement/audit가 있는가?

## 6. Cutover 검수

- [ ] shadow mode에서 consumer는 valkey인가?
- [ ] pg_first 전환 전 legacy queue drain 절차가 있는가?
- [ ] `publisher=pg_first, consumer=valkey` 금지 조합이 차단/경고되는가?
- [ ] `publisher=valkey_only/shadow, consumer=pg` 금지 조합이 차단/경고되는가?
- [ ] rollback 시 PG pending/sending 처리 정책이 문서화되어 있는가?
- [ ] exact key Valkey 확인만 사용하고 `KEYS`를 쓰지 않는가?

## 7. Metrics 검수

- [ ] metric label에 `event_key`, `room_id`, `dedupe_key`, `delivery_id`가 없는가?
- [ ] publish inserted/duplicate/hashConflict metric이 있는가?
- [ ] claim empty/processed latency metric이 있는가?
- [ ] MarkSending conflict metric이 있는가?
- [ ] quarantine/DLQ/retry metric이 있는가?
- [ ] wakeup suppressed/error metric이 있는가?
- [ ] reconciliation job duration/error metric이 있는가?
