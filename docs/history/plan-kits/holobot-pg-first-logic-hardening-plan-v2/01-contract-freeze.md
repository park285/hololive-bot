# 01. Contract Freeze

## 목적

현재 계약은 그대로 둡니다. 이 문서는 바뀌면 안 되는 것과 바꿔도 되는 것을 명확히 분리합니다.

## 변경 금지 계약

### Alarm envelope

`AlarmQueueEnvelope`는 변경하지 않습니다.

```go
type AlarmQueueEnvelope struct {
    Notification  domain.AlarmNotification
    ClaimKeys     []string
    EnqueuedAt    string
    Version       uint8
    Retry         *AlarmQueueRetryMetadata
    SourcePayload string
    SourceKind    domain.AlarmDispatchSourceKind
    YouTubeOutbox *domain.YouTubeOutboxDispatchPayload
}
```

금지 사항:

- field 추가 금지.
- field 삭제 금지.
- JSON key 변경 금지.
- `QueueEnvelopeVersionV1` 증가 금지.
- 기존 fixture 변경 금지.

### Queue keys

기존 key를 변경하지 않습니다.

```text
alarm:dispatch:queue
alarm:dispatch:retry
alarm:dispatch:dlq
alarm:dispatch:wakeup
alarm:dispatch:wakeup:guard
```

`alarm:dispatch:wakeup`은 payload-free token만 전달합니다. 알림 payload를 담지 않습니다.

### PG outbox tables

기존 table과 column을 변경하지 않습니다.

```text
alarm_dispatch_events
alarm_dispatch_deliveries
alarm_dispatch_admin_actions
```

기존 status도 변경하지 않습니다.

```text
shadowed
pending
retry
leased
sending
sent
dlq
quarantined
cancelled
```

### Mode contract

기존 mode 이름과 steady-state 조합을 유지합니다.

```text
ALARM_DISPATCH_PUBLISH_MODE=valkey_only|shadow|pg_first
ALARM_DISPATCH_CONSUMER_MODE=valkey|pg
```

허용 steady-state:

| Publisher | Consumer | 의미 |
|---|---|---|
| `valkey_only` | `valkey` | legacy |
| `shadow` | `valkey` | PG observation only |
| `pg_first` | `pg` | PG ledger + Valkey wakeup |

금지 steady-state:

| Publisher | Consumer | 이유 |
|---|---|---|
| `pg_first` | `valkey` | PG row가 쌓이고 consumer가 claim하지 않음 |
| `shadow` | `pg` | shadowed는 observation only |
| empty/unknown | `pg` | PG consumer는 명시적 pg_first peer 필요 |

## 변경 가능한 영역

계약을 변경하지 않는 선에서 다음 로직은 변경할 수 있습니다.

- alarm-worker 내장 dispatch runner의 idle wait 방식.
- PG consumer 사용 시 failure classification.
- PG consumer option wiring.
- optional environment defaults.
- retention/maintenance automation.
- metrics, alerts, runbook.
- tests and verification gates.

## 계약 보존형 로직 개선 원칙

1. 새 field를 만들지 말고 기존 status transition을 올바르게 사용합니다.
2. 새 queue key를 만들지 말고 기존 wakeup key를 소비합니다.
3. 새 table을 만들지 말고 기존 outbox ledger를 사용합니다.
4. retry와 quarantine을 상태 의미에 맞게 분리합니다.
5. Valkey 장애는 지연을 만들 수 있지만 dispatch loss를 만들면 안 됩니다.
6. PG 장애는 fail-fast가 맞습니다. Valkey로 silently fallback하지 않습니다.

## optional env에 대한 판단

새 optional env는 public contract가 아니라 runtime tuning knob입니다. 기본값이 있어야 하며, 설정하지 않아도 기존 `pg_first/pg` contract가 깨지면 안 됩니다.

권장 optional env:

```text
ALARM_DISPATCH_IDLE_BACKOFF_MIN_MS
ALARM_DISPATCH_IDLE_BACKOFF_MAX_MS
ALARM_DISPATCH_RETENTION_ENABLED
ALARM_DISPATCH_RETENTION_INTERVAL_MS
ALARM_DISPATCH_RETENTION_LIMIT
```

이 env는 없어도 동작해야 합니다.
