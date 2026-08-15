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
- Dispatch queue consume/render/send path, serialized by PostgreSQL `FOR UPDATE SKIP LOCKED` row claims and the single Compose instance
- Generic `notification_delivery_outbox` consume/send path for major event/member news notification rows
- Alarm state cache warming and mutation coordination where configured
- Pending `youtube_notification_outbox` claim/render/send when `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true`
- v1 YouTube outbox의 optional `shadow|cutover` v3 handoff; claim owner는 기존 dispatcher로 유지
- Birthday and anniversary celebration production. Birthday stream delivery audience is derived from sent deliveries of the matching birthday greeting event; it does not fall back to every alarm room.

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
| Settings Pub/Sub | config update handling | runtime settings may become stale |

## Must not own

- YouTube collection, owned by `youtube-collector`
- YouTube canonical persist and notification intent, owned by `hololive-api` YouTube plane
- Kakao command parsing, owned by `bot`
- LLM summary generation, owned by `llm-scheduler`

## Startup requirements

- PostgreSQL and Valkey availability
- `NOTIFICATION_SCHEDULER_ROLE=worker` in the current deployment; the production validator accepts `worker|off`, and Compose pins `worker` so the single instance always runs the alarm checker/scheduler
- A single running instance: proactive egress exclusivity comes from PostgreSQL `FOR UPDATE SKIP LOCKED` row claims plus the Compose `container_name`/fixed host port, not from a Valkey lease
- `DELIVERY_DISPATCHER_ENABLED=true` for production generic notification delivery outbox egress
- `ALARM_DISPATCH_CONSUMER_ENABLED=true` for production alarm dispatch outbox egress
- `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true` for production YouTube outbox egress
- `YOUTUBE_OUTBOX_V3_HANDOFF_MODE=off` until an approved shadow/cutover procedure is executed
- Alarm timing/config env
- `BIRTHDAY_STREAM_RUNNER_ENABLED=true` only after the birthday stream template is present and full-roster producer discovery has been verified

## Shutdown behavior

- Stop the alarm HTTP listener gracefully.
- Stop scheduler/checker loops gracefully.
- Stop dispatch queue and YouTube outbox consumers during shutdown.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-alarm-worker`
- Health: `https://127.0.0.1:30007/health`
- Ready: `https://127.0.0.1:30007/ready`; authenticated `/internal/ready` reports PostgreSQL, Valkey, and egress flag readiness.
- Queue: `alarm:dispatch:queue`
- Metrics: `hololive_youtube_outbox_v3_handoff_total`, `hololive_delivery_outbox_v3_handoff_total`, alarm-dispatch backlog/retention metrics

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/alarm-worker.md`
