# Service: dispatcher-go

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-dispatcher-go` |
| Binary | `dispatcher` |
| Compose service | `dispatcher-go` |
| Port | `30020` |
| Health endpoint | `http://127.0.0.1:30020/ready` |
| Ready endpoint | `http://127.0.0.1:30020/ready` |

## Role

Legacy standalone alarm dispatch queue consumer입니다. Default production compose에서는 `legacy-dispatcher-go` profile 뒤에 있으며 proactive notification egress owner는 `alarm-worker`입니다.

## Owns

- `alarm:dispatch:queue` drain lifecycle
- delayed retry queue and DLQ movement
- Legacy Iris send attempt and retry/claim release behavior when explicitly enabled

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Dispatcher readiness | HTTP | `/ready` | Compose healthcheck |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| Valkey | dispatch queue, retry queue, DLQ, claim keys | alarms stop dispatching |
| Iris | KakaoTalk send automation | dispatch attempts fail/retry/DLQ |
| `hololive-bot` health | startup dependency | service startup can be delayed |

## Must not own

- Alarm rule mutation
- Alarm checking and queue publishing
- User command orchestration
- Default production proactive notification egress

## Startup requirements

- Valkey cache connection
- Iris base URL/cert/token configuration
- `ALARM_DISPATCH_QUEUE_KEY`

## Shutdown behavior

- Stop batch draining and release or preserve claim state according to queue consumer behavior.
- Do not drop raw payloads; invalid payloads must be preserved in DLQ.

## Observability

- Logs: `docker compose -f docker-compose.prod.yml logs -f dispatcher-go`
- Ready: `http://127.0.0.1:30020/ready`
- Metrics: queue drain/retry/DLQ counters in `hololive-shared/pkg/service/alarm/queue/metrics.go`

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/dispatcher-go.md`
