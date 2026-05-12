# Contract: iris-boundary

## Summary

Iris / Redroid is an external KakaoTalk automation boundary used for webhook ingress and outbound message delivery.

## Provider

- Service: Iris / Redroid instance
- Module: external
- Runtime: external boundary, not an in-repo Go runtime

## Consumers

- Service: `bot`
- Service: `dispatcher-go`
- Usage: Kakao webhook ingress/reply and alarm dispatch send

## Transport

- External HTTP/H3 boundary

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Path/Event/Queue | 검토 필요; configured through `IRIS_BASE_URL`, `IRIS_BASE_URL_FILE`, Iris client APIs |
| Method | 검토 필요 |
| Version | external boundary; no in-repo version field |
| Contract package | none; Iris client dependency and runtime env |

## Request

```go
// External Iris client request shapes are outside hololive-shared contracts.
// Do not invent payload fields in this document.
```

## Response

```go
// External Iris client response shapes are outside hololive-shared contracts.
```

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| transport error | n/a | Iris unreachable or H3 trust failure | retry/diagnose Iris runtime and certs |
| auth error | 검토 필요 | token mismatch or missing token | verify runtime secret files/env |
| send failure | 검토 필요 | Kakao send failed | dispatcher retry/DLQ policy applies for alarm dispatch |

## Timeout and retry policy

- `dispatcher-go` owns alarm send retry/DLQ behavior after Iris send failures.
- `bot` webhook/reply timeout behavior is runtime-specific and 검토 필요.
- H3 certificate/trust changes must follow Iris certificate runbooks.

## Compatibility policy

- Do not treat Iris as an internal contract package.
- Env, auth, cert, transport, and route changes require affected runtime runbook updates.
- External boundary changes should keep rollback guidance in `runbooks/rollback.md`.

## Tests

- Bot router/transport tests: 검토 필요
- Dispatcher Iris client tests: 검토 필요

## Known gaps

- Exact Iris endpoint shapes are intentionally not invented here.
- Auth header/signing details are covered by Iris-specific operational skills/runbooks, not this current docs workpack.
