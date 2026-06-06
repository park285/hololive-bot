# Service: alarm-worker

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-alarm-worker` |
| Binary | `alarm-worker` |
| Compose service | `hololive-alarm-worker` |
| Port | `30007` |
| Health endpoint | `http://127.0.0.1:30007/health` |
| Ready endpoint | 검토 필요 |

## Role

Alarm checker/scheduler, alarm dispatch queue publishing/consumption, generic notification delivery outbox consumption, YouTube outbox dispatch, proactive notification egress를 담당합니다.

## Owns

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
| Alarm dispatch egress | Valkey list | `alarm:dispatch:queue` | Iris/Kakao via alarm-worker egress |
| Notification delivery outbox | PostgreSQL table | `notification_delivery_outbox` | Iris/Kakao via alarm-worker egress |
| YouTube outbox dispatch | PostgreSQL table | `youtube_notification_outbox` | Iris/Kakao via alarm-worker egress |
| Alarm service state | in-process domain service | `domain.AlarmCRUD` | local scheduler/checker |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | alarm/member/channel state and notification delivery outbox | alarm evaluation or generic notification delivery fails |
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
- Alarm timing/config env

## Shutdown behavior

- Stop scheduler/checker loops gracefully.
- Stop dispatch queue and YouTube outbox consumers during shutdown.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-alarm-worker`
- Health: `http://127.0.0.1:30007/health`
- Queue: `alarm:dispatch:queue`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/alarm-worker.md`
