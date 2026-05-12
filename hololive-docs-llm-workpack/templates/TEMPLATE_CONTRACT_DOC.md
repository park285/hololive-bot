# Contract: <contract-name>

## Summary

계약의 목적을 한 문장으로 설명합니다.

## Provider

- Service:
- Module:
- Runtime:

## Consumers

- Service:
- Module:
- Usage:

## Transport

- HTTP JSON / Valkey queue / Valkey PubSub / external boundary 중 하나
- RPC/gRPC는 이 작업 범위에서 제외합니다.

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Path/Event/Queue |  |
| Method |  |
| Version |  |
| Contract package |  |

## Request

```go
// contract type 또는 현재 사용되는 request shape
```

## Response

```go
// contract type 또는 현재 사용되는 response shape
```

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
|  |  |  |  |

## Timeout and retry policy

- Timeout:
- Retry:
- Idempotency:

## Compatibility policy

- Additive fields:
- Removing fields:
- Renaming fields:
- Version bump:

## Tests

- Provider route test:
- Consumer client test:
- Fixture path:

## Known gaps

- 검토 필요 항목을 숨기지 말고 적습니다.
