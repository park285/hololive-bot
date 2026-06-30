# Service: alarm-worker

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-alarm-worker` |
| Binary | `alarm-worker` |
| Compose service | `hololive-alarm-worker` |
| Port | `30007` |
| Health endpoint | `https://127.0.0.1:30007/health` over H3 |
| Ready endpoint | `https://127.0.0.1:30007/ready`; diagnostic `https://127.0.0.1:30007/internal/ready` with `X-API-Key` |

## Role

Alarm checker/scheduler, alarm HTTP provider, alarm dispatch queue publishing/consumption, generic notification delivery outbox consumption, YouTube outbox dispatch, proactive notification egress를 담당합니다.

## Owns

- Alarm HTTP provider route registration for `/internal/alarm/*` during the staged provider migration
- Alarm checking and scheduling loops
- Dispatch queue publish path
- Dispatch queue consume/render/send path under the notification egress lease
- Generic `notification_delivery_outbox` consume/send path for major event/member news notification rows
- Alarm state cache warming and mutation coordination where configured
- Pending `youtube_notification_outbox` claim/render/send when `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true`
- Notification egress lease `notification:egress-owner:alarm-worker` when `ALARM_WORKER_EGRESS_LEASE_ENABLED=true`

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Alarm HTTP provider | internal HTTP JSON | `/internal/alarm/*` | `bot`, `admin-api` facade |
| Alarm dispatch egress | Valkey list | `alarm:dispatch:queue` | Iris/Kakao via alarm-worker egress |
| Notification delivery outbox | PostgreSQL table | `notification_delivery_outbox` | Iris/Kakao via alarm-worker egress |
| YouTube outbox dispatch | PostgreSQL table | `youtube_notification_outbox` | Iris/Kakao via alarm-worker egress |
| Alarm service state | in-process domain service | `domain.AlarmCRUD` | local scheduler/checker and alarm HTTP provider |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | alarm/member/channel state and notification delivery outbox | alarm evaluation, alarm HTTP CRUD/query, or generic notification delivery fails |
| PostgreSQL YouTube outbox | claim, render, per-room delivery, and final send state | YouTube notification dispatch pauses |
| Valkey | queue, cache, Pub/Sub | dispatch publishing and config updates fail |
| Valkey egress lease | single active proactive egress owner | dispatch queue and YouTube outbox egress do not start if held or unavailable |
| Settings Pub/Sub | config update handling | runtime settings may become stale |

## Must not own

- YouTube scraping/outbox production, owned by `youtube-producer`
- Kakao command parsing, owned by `bot`
- LLM summary generation, owned by `llm-scheduler`

## Startup requirements

- PostgreSQL and Valkey availability
- `NOTIFICATION_SCHEDULER_ROLE=worker`
- `ALARM_WORKER_EGRESS_LEASE_ENABLED=true` for production proactive egress
- `DELIVERY_DISPATCHER_ENABLED=true` for production generic notification delivery outbox egress
- `ALARM_DISPATCH_CONSUMER_ENABLED=true` for production alarm dispatch outbox egress
- `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true` for production YouTube outbox egress
- Alarm timing/config env

## Shutdown behavior

- Stop the alarm HTTP listener gracefully.
- Stop scheduler/checker loops gracefully.
- Stop dispatch queue and YouTube outbox consumers during shutdown.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-alarm-worker`
- Health: `https://127.0.0.1:30007/health`
- Ready: `https://127.0.0.1:30007/ready`; authenticated `/internal/ready` reports PostgreSQL, Valkey, and egress flag readiness.
- Queue: `alarm:dispatch:queue`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/alarm-worker.md`
