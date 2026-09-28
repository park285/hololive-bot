# Queue And Pub/Sub Contracts

## Scope

Alarm dispatch outbox의 current contract와 Pub/Sub 전달 의미를 기록합니다. settings Pub/Sub은 제거됐습니다.

## Alarm Dispatch Outbox

| Field | Value |
|---|---|
| Producer | `alarm-worker` |
| Consumer | `alarm-worker` |
| Storage | `alarm_dispatch_events` (room-agnostic payload), `alarm_dispatch_deliveries` (per-room state) |
| Retry / DLQ / quarantine | delivery `status` `retry` (`next_attempt_at`), `dlq`, `quarantined` |
| Wakeup | `alarm:dispatch:wakeup` payload-free Valkey list token; polling still claims due rows without it |
| Current envelope version | `QueueEnvelopeVersionV1 = 1` |
| Contract package | `hololive/hololive-shared/pkg/contracts/alarm` |
| Fixtures | `hololive/hololive-shared/pkg/contracts/alarm/testdata/envelope_v1.json`, `envelope_unsupported_version.json` |

Current envelope:

```go
type AlarmQueueEnvelope struct {
    Notification  domain.AlarmNotification             `json:"notification"`
    ClaimKeys     []string                             `json:"claim_keys"`
    EnqueuedAt    string                               `json:"enqueued_at"`
    Version       uint8                                `json:"version"`
    Retry         *AlarmQueueRetryMetadata             `json:"retry,omitempty"`
    SourcePayload string                               `json:"source_payload,omitempty"`
    SourceKind    domain.AlarmDispatchSourceKind       `json:"source_kind,omitempty"`
    YouTubeOutbox *domain.YouTubeOutboxDispatchPayload `json:"youtube_outbox,omitempty"`
    XSpace       *domain.XSpaceDispatchPayload        `json:"x_space,omitempty"`
}

type AlarmQueueRetryMetadata struct {
    Attempt       int    `json:"attempt,omitempty"`
    RetryAfterMS  int64  `json:"retry_after_ms,omitempty"`
    NextVisibleAt string `json:"next_visible_at,omitempty"`
    LastError     string `json:"last_error,omitempty"`
    LastErrorCode string `json:"last_error_code,omitempty"`
}
```

Consumer behavior:

- The publisher accepts only `QueueEnvelopeVersionV1`; a missing (`0`) or other version fails the batch before insert.
- YouTube live/video/community/shorts proactive notifications do not use this dispatch outbox in the current production split; `alarm-worker` consumes `youtube_notification_outbox` directly.
- An undecodable event payload, delivery context, or missing event row moves the delivery to `dlq` with the decode error; the event payload stays in `alarm_dispatch_events`.
- Retry returns the delivery to `retry` with `next_attempt_at`; the claimed envelope carries `attempt`, `last_error`, and optional `last_error_code` from the delivery row.
- Replay of `dlq`/`quarantined` deliveries uses the audited requeue in `runbooks/admin-dispatch-operations.md`.
- The retired Redis queue keys `alarm:dispatch:queue`, `alarm:dispatch:retry`, and `alarm:dispatch:dlq` have no reader or writer; their reserved constants were removed in stack-audit 2026-09-26 T11.
- `last_error_code` values are `timeout`, `canceled`, `http_4xx`, `http_5xx`, `network`, `pg`, `payload`, `unknown`, `lease_expired`, `stale_sending`, and `lease_released`.

## X 스페이스 시작 알림

X 스페이스 시작 알림은 `source_kind=x_space`와 전용 `x_space` payload를 사용하며
기존 PostgreSQL dispatch outbox를 통해 발송합니다. 이벤트 키는
`x-space:start:<space_id>`이고, 각 방의 delivery는 기존 원장에 기록합니다.
`x_space_starts`가 첫 payload를 보존하므로 재관측·재시작·제목 변경으로
새 이벤트를 만들지 않습니다. 다른 스페이스 및 YouTube 알림과 묶지 않고
텍스트 경로로 처리합니다. 신규 발송 재시도 경로는 없습니다.

## Settings Changes (No Pub/Sub)

The settings Pub/Sub channel `config:update` and its message types were removed (2026-09-28). Settings changes use the
admin settings API and a single apply path; ACL changes propagate inside the `hololive-api` process. See
`contracts/settings.md`.

## Pub/Sub Delivery Semantics

Valkey Pub/Sub does not provide durable replay for missed messages. Runtime startup must not rely solely on Pub/Sub history; each subscriber needs a startup refresh or source-of-truth read when the setting affects correctness.

Pub/Sub is not durable command transport. Events that need acknowledgement, retry, replay, or auditability must use an internal HTTP contract or a durable queue.

Command-like events that require acknowledgement, retry, or auditability should use documented internal trigger APIs instead of Pub/Sub.

## Related Documents

- `CONTRACT_MAP.md`
- `contracts/alarm.md`
- `contracts/settings.md`
- `runbooks/dlq-replay.md`
