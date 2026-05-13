# Queue And Pub/Sub Contracts

## Scope

Alarm dispatch queue와 settings/config Pub/Sub의 current contract를 기록합니다.

## Alarm Dispatch Queue

| Field | Value |
|---|---|
| Producer | `alarm-worker` |
| Consumer | legacy `dispatcher-go` where enabled; production proactive egress owner is `alarm-worker` |
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
- Retry metadata fields are `attempt`, `retry_after_ms`, `next_visible_at`, and `last_error`; consumers must round-trip unknown envelope fields when possible.

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
