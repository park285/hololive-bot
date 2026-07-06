# Current Runbooks

현재 운영 runbook의 루트 인덱스입니다.

## Runtime Runbooks

| Runtime | Runbook |
|---|---|
| `hololive-api` | `hololive-api.md` |
| `alarm-worker` | `alarm-worker.md` |
| `youtube-producer` | `youtube-producer.md` |
| `admin-dashboard` | `admin-dashboard.md` |

## Infra And Release

- `../../runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md` - Compose deployment guide
- `dlq-replay.md` - alarm dispatch DLQ 확인/재처리 기준
- `release.md` - release checklist
- `rollback.md` - rollback 기준
- `../../runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md` - release notes template
- `host-migration-root-to-kapu.md` - root → kapu 호스트 계정 풀 마이그레이션 절차

## Existing Operational Reports

- `YOUTUBE_COMMUNITY_SHORTS_TARGET_BASELINE.md`
- `YOUTUBE_COMMUNITY_SHORTS_CHANNEL_ROUTE_VERIFICATION.md`
- `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_CHANNEL_SUMMARY_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md`
- `YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_LATENCY_PERIOD_SUMMARY.md`
- `YOUTUBE_COMMUNITY_SHORTS_LATENCY_CAUSE_REPORT.md`
- `YOUTUBE_LIVE_CATCHUP_ALARM_RENDERING_20260705.md`

## Rule

- 새로운 현재 운영 runbook은 이 인덱스에서 발견 가능해야 합니다.
- Runtime runbook은 `docs/current/PROJECT_MAP.md`의 runbook link와 일치해야 합니다.
