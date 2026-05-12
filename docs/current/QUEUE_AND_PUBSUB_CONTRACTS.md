# Queue And Pub/Sub Contracts

## Scope

Alarm dispatch queue와 settings/config Pub/Sub의 current contract를 기록합니다.

## Alarm Dispatch Queue

| Field | Value |
|---|---|
| Producer | `alarm-worker` |
| Consumer | `dispatcher-go` |
| Active queue | `alarm:dispatch:queue` |
| Delayed retry queue | `alarm:dispatch:retry` |
| DLQ | `alarm:dispatch:dlq` |
| Envelope version | `QueueEnvelopeVersionV1 = 1` |
| Contract package | `hololive/hololive-shared/pkg/contracts/alarm` |

Current envelope:

```go
type AlarmQueueEnvelope struct {
    Notification  domain.AlarmNotification `json:"notification"`
    ClaimKeys     []string                 `json:"claim_keys"`
    EnqueuedAt    string                   `json:"enqueued_at"`
    Version       uint8                    `json:"version"`
    Retry         *AlarmQueueRetryMetadata `json:"retry,omitempty"`
    SourcePayload string                   `json:"source_payload,omitempty"`
}
```

Consumer behavior:

- Accepts version `0` and `QueueEnvelopeVersionV1`.
- Rejects unsupported versions.
- Invalid active queue JSON is preserved raw in `alarm:dispatch:dlq`.
- Invalid delayed retry wrapper payload is preserved raw in `alarm:dispatch:dlq`.
- `MoveToDLQ` preserves original legacy raw payload when available.
- Retry scheduling stores wrapped members in `alarm:dispatch:retry`.

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
