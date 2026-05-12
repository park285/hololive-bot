# Service: stream-ingester

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-stream-ingester` |
| Binary | `stream-ingester` |
| Compose service | `stream-ingester` |
| Port | `30004` |
| Health endpoint | `http://127.0.0.1:30004/health` |
| Ready endpoint | 검토 필요 |

## Role

Photo sync와 ingestion-adjacent runtime 기능을 담당합니다.

## Owns

- Photo sync runtime when `PHOTO_SYNC_ENABLED=true`
- Ingestion-adjacent health/config runtime surfaces
- Shared ingestion bootstrap used by this module

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Stream ingester health | HTTP | `/health` | Compose healthcheck |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | sync and ingestion-adjacent state | sync operations fail |
| Valkey | cache/config | stale or degraded ingestion behavior |
| Iris/cliproxy | configured external interactions | degraded external operations |

## Must not own

- Dedicated YouTube scraping when `youtube-scraper` owns it
- Kakao command routing
- Alarm queue consumption

## Startup requirements

- PostgreSQL and Valkey availability
- `PHOTO_SYNC_ENABLED=true`
- `YOUTUBE_INGESTION_ENABLED=false` in current Compose service

## Shutdown behavior

- Stop runtime workers gracefully.
- Do not claim youtube-scraper ownership on shutdown.

## Observability

- Logs: `docker compose -f docker-compose.prod.yml logs -f stream-ingester`
- Health: `http://127.0.0.1:30004/health`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/stream-ingester.md`
