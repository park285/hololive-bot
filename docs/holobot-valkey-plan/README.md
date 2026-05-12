# 홀로봇 Valkey/Alarm Dispatch 재설계 작업계획 V3

상태: V2 압축본 재검수 후, 최종 권장 아키텍처 기준으로 재작성한 LLM 친화형 작업 패키지
작성일: 2026-05-12

## 이 패키지의 목적

이 패키지는 기존 `holobot-valkey-workplan-v2.zip`의 내용을 단순 재포장한 것이 아니라, V2 문서의 핵심 한계를 다시 검수한 뒤 `alarm_dispatch_events + alarm_dispatch_deliveries` 2테이블 ledger, `PublishBatch()`, PostgreSQL batch claim/update, Valkey O(1) wakeup/cache 보조 계층을 기준으로 다시 작성한 실행 지시서입니다.

V2는 안전한 전환 최소안으로 의미가 있었지만, 최종 production 기본안으로는 단일 `alarm_dispatch_outbox`가 부족합니다. room fan-out이 커지면 같은 JSON payload가 room 수만큼 저장되고, `Publish()`당 DB insert가 누적되며, 짧은 polling을 통해 PostgreSQL 부하가 커질 수 있습니다. V3는 이 문제를 구조적으로 줄입니다.

## 최종 설계 요약

PostgreSQL은 durable ledger입니다. `alarm_dispatch_events`는 room-agnostic event payload를 한 번만 저장합니다. `alarm_dispatch_deliveries`는 room별 delivery 상태, dedupe key, lease, retry, terminal 상태를 관리합니다.

Valkey는 durable queue가 아닙니다. Valkey는 wakeup, cache, 재구성 가능한 index만 맡습니다. 특히 dispatch wakeup은 payload를 싣지 않는 단일 token 방식이어야 하며, correctness는 항상 PostgreSQL에 의존해야 합니다.

기본 publisher API는 `PublishBatch()`입니다. 기존 `Publish()`는 batch size 1 wrapper로만 남깁니다. production path에서 알림 N개가 DB round-trip N개로 이어지면 안 됩니다.

Iris idempotency key가 생기기 전까지, 외부 send 시작 이후의 ambiguous failure는 retry가 아니라 quarantine이 기본값입니다. 네트워크 timeout은 “실패”가 아니라 “결과 불명”입니다.

## 사용자 추가 제약 반영

Valkey hot path에서는 높은 복잡도 명령을 금지합니다. 기본은 O(1) 계열 명령만 사용합니다. 예외적으로 O(log N) 또는 script성 명령이 필요한 경우에는 다음 조건을 모두 만족해야 합니다.

1. 해당 경로가 hot dispatch correctness path가 아니어야 합니다.
2. key cardinality 또는 batch size가 명시적으로 bounded여야 합니다.
3. 코드에 왜 예외가 필요한지 주석을 남겨야 합니다.
4. 테스트 또는 lint guard로 unbounded 사용을 막아야 합니다.

특히 운영 hot path에서 `KEYS`, unbounded `SCAN`, unbounded `LRANGE`, unbounded `SMEMBERS`, unbounded `HGETALL`, 대량 `SORT`, loop가 있는 Lua script, Pub/Sub `PUBLISH` default wakeup은 금지합니다.

## 문서 구성

권장 읽기 순서:

1. `00-critical-rereview-v2-to-v3.md`
2. `01-final-contract-and-invariants.md`
3. `02-target-architecture.md`
4. `phases/phase-00-preflight-contract-and-audit.md`
5. `phases/phase-01-schema-and-repository.md`
6. `phases/phase-02-publisher-publishbatch.md`
7. `phases/phase-03-valkey-o1-wakeup.md`
8. `phases/phase-04-dispatcher-db-wiring.md`
9. `phases/phase-05-pg-consumer-state-machine.md`
10. `phases/phase-06-reconciliation-retention-admin.md`
11. `phases/phase-07-cutover-rollback-runbook.md`
12. `phases/phase-08-valkey-cache-index-hygiene.md`
13. `appendix/test-matrix.md`
14. `appendix/llm-prompts.md`
15. `appendix/valkey-command-policy.md`
16. `appendix/sql-patterns.md`
17. `sql/001_alarm_dispatch_events_deliveries.sql`

## 절대 금지 사항

- Valkey AOF/RDB 내구성 강화로 dispatch 유실 문제를 해결하려고 하지 않습니다.
- Valkey를 durable queue로 간주하지 않습니다.
- `alarm_dispatch_events.payload`에 `room_id` 또는 room별 데이터를 넣지 않습니다.
- shadow row를 자동으로 pending 승격하지 않습니다.
- `sent`, `dlq`, `quarantined`, `cancelled`를 자동 pending/retry로 되돌리지 않습니다.
- Iris idempotency가 없는 상태에서 stale `sending`을 자동 retry하지 않습니다.
- 운영 hot path에서 고복잡도 Valkey 명령을 사용하지 않습니다.
- recurring job에서 unbounded SQL `DELETE`, `UPDATE`, `COUNT GROUP BY`를 짧은 주기로 실행하지 않습니다.

## 최종 한 줄 원칙

PostgreSQL은 event payload와 room delivery 상태를 분리한 durable ledger이고, Valkey는 payload 없는 O(1) wakeup/cache/index 보조 수단이며, Iris idempotency 전까지 sending 이후 결과 불명은 retry가 아니라 quarantine입니다.
