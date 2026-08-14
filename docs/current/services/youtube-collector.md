# Service: youtube-collector

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-youtube-collector` |
| Binary | `youtube-collector` |
| Compose service | `youtube-collector` |
| Port | `30045` |
| Health endpoint | `https://127.0.0.1:30045/health` over H3 |
| Ready endpoint | `https://127.0.0.1:30045/ready` |

## Role

Community vertical slice collector입니다. YouTube.js (`youtubei.js`) community fetch, normalize, `source_collection_checkpoints`와 `source_observation_outbox` Publish만 소유합니다. Canonical community persist와 notification outbox는 `youtube-producer`가 소유합니다.

## Owns

- Community YouTube.js fetch/normalize when `YOUTUBE_INGESTION_ENABLED=true`
- `Repository.Publish` for `youtube_community` observations (checkpoint + outbox insert)
- Collector DB role `hololive_scraper` (`SELECT, INSERT` on outbox; no outbox UPDATE)

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Community observations | PostgreSQL outbox | `source_observation_outbox` (`source_kind=youtube_community`) | `youtube-producer` consumer |
| Collector health | H3 | `/health` | Compose healthcheck |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | checkpoint/outbox insert over `verify-full` TLS | collection handoff fails |
| Valkey | poll target/cache/coordination | stale targets or degraded scheduling |

## Must not own

- `youtube_community_posts`, watermarks, tracking, `youtube_notification_outbox`
- Observation claim/finalize
- Live/shorts/videos/stats polling
- Holodex photo sync
- Proactive notification egress owned by `alarm-worker`

## Startup requirements

- PostgreSQL and Valkey availability
- `YOUTUBE_INGESTION_ENABLED=true`
- `YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true`
- `YOUTUBE_PRODUCER_RUNTIME_ALLOWED=false`
- `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=false`
- `PHOTO_SYNC_ENABLED=false`
- `POSTGRES_USER=hololive_scraper`
- Holodex is not required; Community collection uses the collector-owned YouTube.js helper, not HTML `GetCommunityPosts`
- `POSTGRES_SSLMODE=verify-full` and `POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem`
- Required central compose service. Default central `up` starts this service. AP overlays pin it to profile `central-only`.

## Shutdown behavior

- Stop the community collector scheduler.
- Do not claim or update observation outbox rows.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f youtube-collector`
- Health: `https://127.0.0.1:30045/health`
- Metrics: live-compat publishes `:30096` on `HOLOLIVE_METRICS_PORT_BIND_IP` for central Prometheus

## Related docs

- `../architecture/youtube-collector-observation-outbox-community-vertical-slice-20260813.md`
- `youtube-producer.md`
- `../runbooks/youtube-collector.md`
