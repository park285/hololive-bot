# Current Contracts

이 디렉터리는 current service-to-service 계약 문서의 진입점입니다.

## Contract Documents

- `membernews.md` - member news subscription/digest HTTP JSON
- `majorevent.md` - major event subscription HTTP JSON
- `trigger.md` - manual notification trigger HTTP JSON
- `alarm.md` - alarm HTTP API and alarm dispatch queue
- `settings.md` - settings/config update Pub/Sub
- `iris-boundary.md` - Iris external boundary

## Update Rules

계약 변경 PR은 변경 종류에 따라 다음 파일을 함께 검토합니다.

| Change | Required docs |
|---|---|
| Internal HTTP path/method/request/response | `../CONTRACT_MAP.md`, matching `*.md`, `../ERROR_CONTRACT.md` if error changes |
| Queue key/envelope/retry/DLQ | `../CONTRACT_MAP.md`, `alarm.md`, `../QUEUE_AND_PUBSUB_CONTRACTS.md` |
| Pub/Sub channel/type/payload | `../CONTRACT_MAP.md`, `settings.md`, `../QUEUE_AND_PUBSUB_CONTRACTS.md` |
| External Iris transport or auth | `../CONTRACT_MAP.md`, `iris-boundary.md`, affected runbook |

## Version Policy

- Additive fields are compatible when consumers ignore unknown fields.
- Removing or renaming fields requires a documented compatibility window.
- Queue envelope version changes require dual-read or migration guidance.
- Pub/Sub currently has `ConfigUpdateVersionV1` as a code constant but no payload `version`; adding one is a contract change.
