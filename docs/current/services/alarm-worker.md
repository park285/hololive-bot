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

Alarm checker/scheduler와 alarm dispatch queue publishing을 담당합니다.

## Owns

- Alarm checking and scheduling loops
- Dispatch queue publish path
- Alarm state cache warming and mutation coordination where configured

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Alarm dispatch queue | Valkey list | `alarm:dispatch:queue` | `dispatcher-go` |
| Alarm service state | in-process domain service | `domain.AlarmCRUD` | local scheduler/checker |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | alarm/member/channel state | alarm evaluation fails |
| Valkey | queue, cache, Pub/Sub | dispatch publishing and config updates fail |
| Settings Pub/Sub | config update handling | runtime settings may become stale |

## Must not own

- Iris send and dispatch delivery, owned by `dispatcher-go`
- Kakao command parsing, owned by `bot`
- LLM summary generation, owned by `llm-scheduler`

## Startup requirements

- PostgreSQL and Valkey availability
- `NOTIFICATION_SCHEDULER_ROLE=worker`
- Alarm timing/config env

## Shutdown behavior

- Stop scheduler/checker loops gracefully.
- Do not consume dispatch queue entries during shutdown.

## Observability

- Logs: `docker compose -f docker-compose.prod.yml logs -f hololive-alarm-worker`
- Health: `http://127.0.0.1:30007/health`
- Queue: `alarm:dispatch:queue`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/alarm-worker.md`
