# Current Contracts

이 디렉터리는 current service-to-service 및 외부 source 계약 문서의 진입점입니다.

## Stable Contract IDs

계약 ID·provider·consumer·transport·계약 패키지의 정본은 [CONTRACT_MAP](../CONTRACT_MAP.md)의 Contract Inventory입니다.

## Contract Documents

- `membernews.md` - member news subscription/digest in-process contract
- `majorevent.md` - major event subscription in-process contract
- `trigger.md` - manual notification rerun operator CLI
- `alarm.md` - alarm HTTP API and PostgreSQL alarm dispatch outbox
- `shortlink.md` - fixed-target YouTube short-link route, scraper rejection, and grouped message rendering contract
- `settings.md` - admin settings API and alarm advance apply path
- `admin-api.md` - admin plane `/api/holo/*` producer contract, OpenAPI source, and console generated-copy path
- `iris-boundary.md` - Iris external boundary
- `schedule-hololive-tv-api.md` - Official Schedule API request, mapping, fallback ownership, live non-goal, cache, and observability contract

## Update Rules

계약 변경 PR은 변경 종류에 따라 다음 파일을 함께 검토합니다.

| Change | Required docs |
|---|---|
| Internal HTTP path/method/request/response | `../CONTRACT_MAP.md`, matching `*.md`, `../ERROR_CONTRACT.md` if error changes |
| Public redirect path/method/status | `../CONTRACT_MAP.md`, matching `*.md`, affected runbook |
| Queue key/envelope/retry/DLQ | `../CONTRACT_MAP.md`, `alarm.md`, `../QUEUE_AND_PUBSUB_CONTRACTS.md` |
| Pub/Sub channel/type/payload | `../CONTRACT_MAP.md`, `settings.md`, `../QUEUE_AND_PUBSUB_CONTRACTS.md` |
| External Iris transport or auth | `../CONTRACT_MAP.md`, `iris-boundary.md`, affected runbook |
| External schedule source request/schema/ownership | `schedule-hololive-tv-api.md`, affected provider tests and runbook |

## Version Policy

- Additive fields are compatible when consumers ignore unknown fields.
- Removing or renaming fields requires a documented compatibility window.
- Queue envelope version changes require dual-read or migration guidance.
