# Appendix. LLM 작업 프롬프트 모음

이 파일은 phase별 작업을 LLM에게 나눠 맡길 때 바로 복사해서 쓸 수 있도록 만든 지시문입니다. 각 프롬프트는 “범위 제한”, “금지 사항”, “완료 기준”을 포함합니다.

## 공통 시스템 지시

```text
당신은 hololive-bot 저장소의 alarm dispatch/Valkey 리팩토링을 수행합니다.
답변과 코드 변경은 V3 설계를 따라야 합니다.

핵심 원칙:
- PostgreSQL은 alarm_dispatch_events + alarm_dispatch_deliveries 2테이블 durable ledger입니다.
- Valkey는 durable queue가 아니라 wakeup/cache/index helper입니다.
- Valkey dispatch hot path는 O(1) 계열 명령만 사용합니다.
- 고복잡도 명령은 운영 hot path에서 금지합니다.
- event payload는 room-agnostic이어야 합니다.
- PublishBatch()가 기본 API이고 Publish()는 batch size 1 wrapper입니다.
- Iris idempotency 전까지 sending 이후 ambiguous failure는 quarantine입니다.

금지:
- 단일 alarm_dispatch_outbox를 최종 production 기본안으로 구현하지 마세요.
- event payload에 room_id를 넣지 마세요.
- PublishBatch() 내부에서 Publish()를 반복 호출하지 마세요.
- Valkey PUBLISH를 alarm dispatch wakeup 기본 구현으로 쓰지 마세요.
- KEYS, unbounded SCAN, LRANGE 0 -1, SMEMBERS, HGETALL을 dispatch hot path에서 쓰지 마세요.
- stale sending을 자동 retry하지 마세요.
- unbounded UPDATE/DELETE retention SQL을 작성하지 마세요.
```

## Phase 00 prompt

```text
현재 저장소의 alarm dispatch 흐름을 감사하세요.
런타임 동작을 바꾸지 말고 docs/test/audit 중심으로 작업하세요.
Valkey가 durable queue로 간주되는 코드/문서 표현을 찾고, PostgreSQL events+deliveries ledger가 최종 target임을 문서화하세요.
Valkey hot path에서 KEYS, unbounded SCAN, LRANGE 0 -1, SMEMBERS, HGETALL, Pub/Sub PUBLISH wakeup 사용 지점을 목록화하세요.
기존 publisher/dispatcher behavior가 유지되는 golden test 또는 smoke test를 추가하세요.
```

## Phase 01 prompt

```text
alarm_dispatch_events와 alarm_dispatch_deliveries schema, domain model, repository, repository test를 추가하세요.
이 phase에서는 runtime publish/dispatch 동작을 바꾸지 마세요.

요구사항:
- events는 room-agnostic payload를 한 번만 저장합니다.
- deliveries는 room_id, dedupe_key, claim_keys, status, lease/retry/terminal 상태를 저장합니다.
- event_key unique, dedupe_key unique를 둡니다.
- 같은 event_key에 payload_hash가 다르면 overwrite하지 말고 error/hashConflict로 처리합니다.
- ClaimDue는 delivery row만 반환하고 event payload를 join하지 않습니다.
- LoadEventsByID는 distinct event_id 목록으로 payload를 가져옵니다.
- MarkSending은 leased+locked_by 일치에서만 성공합니다.
- MarkSent는 sending+locked_by 일치에서만 성공합니다.
- 모든 retention/reconciliation SQL은 bounded limit을 사용합니다.
```

## Phase 02 prompt

```text
publisher를 PublishBatch() 중심으로 리팩토링하세요.
기존 Publish()는 PublishBatch()에 1건을 넘기는 wrapper로 남깁니다.
PublishBatch() 내부에서 Publish()를 반복 호출하지 마세요.

mode:
- valkey_only: 기존 Valkey legacy queue publish만 수행
- shadow: Valkey legacy publish 성공 후 PG InsertBatch(status=shadowed)
- pg_first: PG InsertBatch(status=pending) commit 후 Valkey wakeup만 best-effort 수행, legacy active queue LPUSH 금지

같은 logical event가 여러 room으로 fan-out되면 event row는 1개, delivery row는 room 수만큼이어야 합니다.
shadow row는 pending이 아니라 shadowed여야 합니다.
wakeup 실패는 publish 실패가 아닙니다.
```

## Phase 03 prompt

```text
alarm dispatch wakeup을 Valkey O(1) fixed-list token 방식으로 구현하세요.
Pub/Sub PUBLISH를 기본 wakeup으로 쓰지 마세요.

publisher wakeup:
- PG commit 이후에만 호출
- SET alarm:dispatch:wakeup:guard 1 NX PX <guardTTL>
- 성공 시 LPUSH alarm:dispatch:wakeup 1
- 이어서 PEXPIRE alarm:dispatch:wakeup <listTTL>
- listTTL은 guardTTL 이하
- Valkey error는 publish 성공을 되돌리지 않음

consumer wait:
- BRPOP alarm:dispatch:wakeup <timeout>
- BRPOP에는 fixed key 1개만 넘김
- wakeup token은 payload가 아니며, token을 받으면 PG ClaimDue 실행
- wakeup이 없어도 fallback scan 실행

테스트에서 PUBLISH/KEYS/SCAN/LRANGE 0 -1 호출이 없음을 검증하세요.
```

## Phase 04 prompt

```text
dispatcher-go에 PostgreSQL config와 runtime wiring을 추가하세요.
이 phase에서는 기본 consumer mode를 valkey로 유지하세요.
consumer_mode=valkey에서는 PostgreSQL env가 없어도 기존 실행이 깨지지 않아야 합니다.
consumer_mode=pg에서는 PostgreSQL env와 DB ping이 readiness에 반영되어야 합니다.
DB pool lifecycle cleanup을 추가하세요.
shutdown 중 sending row를 자동 retry로 되돌리지 마세요.
```

## Phase 05 prompt

```text
PG consumer를 구현하세요.
Valkey wakeup 또는 fallback timeout 후 PostgreSQL ClaimDue를 호출합니다.
ClaimDue는 delivery rows만 반환합니다.
이후 distinct event_id로 LoadEventsByID를 호출하세요.

처리 흐름:
- render 실패: MarkSending 호출 없이 retry/DLQ
- send 직전: MarkSending 호출
- MarkSending updated count가 delivery id 수와 다르면 Iris send 금지
- send 성공: MarkSent 호출
- MarkSent 실패: 즉시 retry 금지, stale sending reconciliation이 처리
- ambiguous send error: Iris idempotency 전까지 quarantine

shadowed row를 claim하지 마세요.
```

## Phase 06 prompt

```text
reconciliation, retention, admin tooling을 구현하세요.
모든 UPDATE/DELETE는 bounded CTE + LIMIT 방식으로 작성하세요.
leased 만료는 retry로 복구할 수 있습니다.
sending stale은 Iris idempotency 전까지 quarantine해야 합니다.
terminal retention은 한 번에 무제한 삭제하지 마세요.
quarantined 수동 requeue에는 operator acknowledgement와 audit log/metric이 필요합니다.
dashboard는 DB COUNT 반복이 아니라 app metric 중심으로 설계하세요.
```

## Phase 07 prompt

```text
cutover와 rollback runbook을 작성하고 필요한 helper를 추가하세요.
shadow mode에서는 publisher=shadow, consumer=valkey 조합으로만 운영합니다.
pg_first 전환 전 legacy Valkey queue drain 절차를 문서화하세요.
pg_first 전환 시 publisher=pg_first와 consumer=pg를 함께 맞추세요.
금지 조합:
- publisher=pg_first, consumer=valkey
- publisher=valkey_only/shadow, consumer=pg
Valkey 운영 확인은 exact key LLEN/ZCARD만 사용하고 KEYS는 쓰지 마세요.
rollback 중 stale sending은 quarantine 권장입니다.
```

## Phase 08 prompt

```text
Valkey cache/index/wakeup 역할을 분리하고 command guard를 추가하세요.
DispatchWakeupClient에는 NotifyDispatchAvailable과 WaitDispatchWakeup 같은 좁은 메서드만 둡니다.
KEYS/SCAN/PUBLISH/LRANGE/SMEMBERS/HGETALL 같은 generic command를 dispatch wakeup interface에 노출하지 마세요.
운영 hot path에서 고복잡도 명령이 호출되지 않도록 fake client test 또는 lint를 추가하세요.
O(log N) command가 필요한 cache/index path에는 hot path가 아님과 bounded 근거를 주석으로 남기세요.
```
