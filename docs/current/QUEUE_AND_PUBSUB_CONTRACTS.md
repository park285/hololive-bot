# Queue And Pub/Sub Contracts

## Scope

Alarm dispatch queue와 settings/config Pub/Sub의 current contract를 기록합니다.

## Alarm Dispatch Queue

| Field | Value |
|---|---|
| Producer | `alarm-worker` |
| Consumer | `alarm-worker` |
| Active queue | `alarm:dispatch:queue` |
| Delayed retry queue | `alarm:dispatch:retry` |
| DLQ | `alarm:dispatch:dlq` |
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

- Accepts version `0` and `QueueEnvelopeVersionV1`.
- YouTube live/video/community/shorts proactive notifications do not use this Valkey queue in the current production split; `alarm-worker` consumes `youtube_notification_outbox` directly.
- Rejects unsupported version payloads and preserves raw payloads in `alarm:dispatch:dlq`.
- Invalid JSON from the active queue is preserved raw in `alarm:dispatch:dlq`.
- Invalid delayed retry wrapper payload is preserved raw in `alarm:dispatch:dlq`.
- `MoveToDLQ` preserves original legacy raw payload when available.
- Retry scheduling stores wrapped members in `alarm:dispatch:retry`.
- Retry metadata fields are `attempt`, `retry_after_ms`, `next_visible_at`, `last_error`, and optional `last_error_code`; consumers must round-trip unknown envelope fields when possible.
- `last_error_code` values are `timeout`, `canceled`, `http_4xx`, `http_5xx`, `network`, `pg`, `payload`, `unknown`, `lease_expired`, `stale_sending`, and `lease_released`.

## X 스페이스 시작 알림

X 스페이스 시작 알림은 `source_kind=x_space`와 전용 `x_space` payload를 사용하며
기존 PostgreSQL dispatch outbox를 통해 발송합니다. 이벤트 키는
`x-space:start:<space_id>`이고, 각 방의 delivery는 기존 원장에 기록합니다.
`x_space_starts`가 첫 payload를 보존하므로 재관측·재시작·제목 변경으로
새 이벤트를 만들지 않습니다. 다른 스페이스 및 YouTube 알림과 묶지 않고
텍스트 경로로 처리합니다. 신규 발송 재시도 경로는 없습니다.

## Settings Pub/Sub

| Field | Value |
|---|---|
| Channel | `config:update` |
| Contract package | `hololive/hololive-shared/pkg/contracts/settings` |
| Version constant | `ConfigUpdateVersionV1 = 1` |
| Payload version field | none |

Current message:

```go
type ConfigUpdateV1 struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
}
```

Known update types:

- `scraper_proxy`
- `alarm_advance_minutes`
- `membernews_weekly_run_now`

Subscriber behavior:

- Invalid JSON is logged and ignored.
- Empty `type` is logged and ignored.
- Unknown `type` is logged unless an `Unknown` handler is configured.
- Type-specific payload decode failure is logged and ignored.

## Pub/Sub Delivery Semantics

Valkey Pub/Sub does not provide durable replay for missed messages. Runtime startup must not rely solely on Pub/Sub history; each subscriber needs a startup refresh or source-of-truth read when the setting affects correctness.

Pub/Sub is not durable command transport. Events that need acknowledgement, retry, replay, or auditability must use an internal HTTP contract or a durable queue.

Command-like events that require acknowledgement, retry, or auditability should use documented internal trigger APIs instead of Pub/Sub. This document does not change the current `membernews_weekly_run_now` event.

## Validation

```bash
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-error-contracts.sh
```

## Related Documents

- `CONTRACT_MAP.md`
- `contracts/alarm.md`
- `contracts/settings.md`
- `runbooks/dlq-replay.md`
