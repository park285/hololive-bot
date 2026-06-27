# Contract: settings

## Summary

Runtime settings changes are broadcast through Valkey Pub/Sub channel `config:update`.

## Contract ID

- `settings.update`

## Provider

- Service: `hololive-api` admin-plane settings update paths
- Module: `hololive-shared` dispatcher helpers are shared
- Runtime: publisher is currently `hololive-api` (admin plane)

## Consumers

- Services: `hololive-api`, `alarm-worker`, `youtube-producer`, ingestion runtimes where `configsub.Subscriber` is configured
- Usage: scraper proxy toggles, alarm advance minutes updates, member news run-now event

## Transport

- Valkey Pub/Sub

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Path/Event/Queue | `config:update` |
| Method | Pub/Sub publish/subscribe |
| Version | `ConfigUpdateVersionV1 = 1`; payload has no `version` field |
| Contract package | `hololive/hololive-shared/pkg/contracts/settings` |

## Request

```go
type ConfigUpdateV1 struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
}

type ScraperProxyPayloadV1 struct {
    Enabled bool `json:"enabled"`
}

type AlarmAdvanceMinutesPayloadV1 struct {
    Minutes int `json:"minutes"`
}
```

Known update types:

- `scraper_proxy`
- `alarm_advance_minutes`
- `membernews_weekly_run_now`

## Response

```go
// Pub/Sub has no response or delivery acknowledgement.
```

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| invalid JSON | n/a | subscriber cannot unmarshal update | log warning and ignore |
| empty type | n/a | missing update type | log warning and ignore |
| unknown type | n/a | no handler for update type | log warning or custom Unknown handler |
| invalid payload | n/a | type-specific payload decode failed | log warning and ignore |

## Timeout and retry policy

- Timeout: Pub/Sub receive is bound to subscriber context.
- Retry: Pub/Sub itself does not retry missed messages.
- Idempotency: handlers should tolerate repeated settings values.

## Compatibility policy

- Adding a new `Type` is additive only if old subscribers ignore unknown values.
- Adding a payload `version` is a contract change because current message shape omits it.
- Command-like events should prefer internal trigger APIs when delivery acknowledgement matters.

## Tests

- Contract constants: `hololive/hololive-shared/pkg/contracts/settings/contracts_test.go`
- Dispatcher behavior: `hololive/hololive-shared/pkg/service/configsub/dispatcher_test.go`
- Runtime subscriber wiring: `hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/bot_config_subscriber_test.go`

## Known gaps

- Publisher ownership is `hololive-api` (admin plane); new publishers must update `CONTRACT_MAP.md` and `CONTRACT_MANIFEST.txt`.
- Pub/Sub startup refresh requirements are runtime-specific and must be checked per subscriber.
