# Service: youtube-collector

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-youtube-collector` |
| Binary | `youtube-collector` |
| Compose service | `youtube-collector` (central `c`); AP overlays `youtube-collector-a/b/d` |
| Ports | `a` 30005, `b` 30015, `c` 30025, `d` 30035 |
| Health endpoint | `https://127.0.0.1:<port>/health` over H3 |
| Ready endpoint | `https://127.0.0.1:<port>/ready` over H3 |
| DB role | `hololive_scraper` |
| TLS | `POSTGRES_SSLMODE=verify-full`, `POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem` |

## Role

AP fleet collector입니다. Holodex, Official Schedule, YouTube.js fetch/normalize와 PostgreSQL collection lease/checkpoint/`source_observations` Publish만 소유합니다. Canonical persist와 notification intent는 `hololive-api` YouTube plane이 소유합니다. `members.photo` product path는 hololive-api admin PhotoSync가 소유합니다.

## Owns

- Provider adapters and bounded collection when `YOUTUBE_INGESTION_ENABLED=true`
- DB job lease/fence and `PublishBatch` (checkpoint + observation insert)
- Collector DB role `hololive_scraper`

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Source observations | PostgreSQL | `source_observations` / `source_observation_queue` | `hololive-api` YouTube plane |
| Collector health/ready | H3 | `/health`, `/ready` | Compose/systemd healthcheck |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | lease/checkpoint/observation insert over `verify-full` TLS | collection handoff fails |
| Valkey | optional contention optimization | scheduling degrades to DB-only |

## Must not own

- Canonical community/content/live/stats/profile/photo tables
- Notification outbox / observation claim/finalize
- Proactive notification egress owned by `alarm-worker`

## Startup requirements

- PostgreSQL and Valkey availability
- `YOUTUBE_INGESTION_ENABLED=true`
- `YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true`
- `YOUTUBE_COLLECTOR_INSTANCE_ID=youtube-collector-{a,b,c,d}`
- `PHOTO_SYNC_ENABLED=false`
- `POSTGRES_USER=hololive_scraper`
- `POSTGRES_SSLMODE=verify-full` and `POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem`
- Central default `up` starts fleet member `c` as compose service `youtube-collector`. AP overlays pin that service to `central-only` and start the host instance.

## Shutdown behavior

- Stop the collector scheduler and YouTube.js helper.
- Do not claim or update observation queue rows.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f youtube-collector`
- Health: `https://127.0.0.1:30025/health`
- Ready: `https://127.0.0.1:30025/ready`
- Metrics: live-compat publishes `:30096` on `HOLOLIVE_METRICS_PORT_BIND_IP`

## Related docs

- `../runbooks/youtube-collector.md`
