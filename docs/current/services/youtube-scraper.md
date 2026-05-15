# Service: youtube-scraper

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-stream-ingester` |
| Binary | `youtube-scraper` |
| Compose service | `youtube-scraper` |
| Port | `30005` |
| Health endpoint | `http://127.0.0.1:30005/health` |
| Ready endpoint | 검토 필요 |

## Role

YouTube scraping/polling과 `youtube_notification_outbox` production을 담당하는 dedicated runtime입니다.

## Owns

- YouTube polling/scraping scheduler when `YOUTUBE_INGESTION_ENABLED=true`
- Community/shorts/live/stats polling configuration
- `youtube_notification_outbox` production paths for YouTube-derived events

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| YouTube scraper health | HTTP | `/health` | Compose healthcheck |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | YouTube channel/outbox/tracking state | scraping and handoff pipeline fail |
| Valkey | cache/config/coordination | stale targets or degraded queue behavior |

## Must not own

- Photo sync runtime owned by `stream-ingester`
- Proactive notification egress owned by `alarm-worker`
- Alarm checking owned by `alarm-worker`
- Iris send and direct YouTube outbox dispatch; `alarm-worker` performs final send and delivery state updates

## Startup requirements

- PostgreSQL and Valkey availability
- `YOUTUBE_INGESTION_ENABLED=true`
- scraper interval env values

## Shutdown behavior

- Stop pollers gracefully.
- Preserve pending outbox and tracking state in PostgreSQL.

## Observability

- Logs: `./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f youtube-scraper`
- Health: `http://127.0.0.1:30005/health`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/youtube-scraper.md`
