# Service: youtube-producer

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-youtube-producer` |
| Binary | `youtube-producer` |
| Compose service | `youtube-producer` |
| Port | `30005` (AP-A) / `30015` (AP-B) / `30025` (AP-C) |
| Health endpoint | `http://127.0.0.1:30005/health` (AP-A); AP-B `30015`, AP-C `30025` |
| Ready endpoint | AP-A `http://127.0.0.1:30005/ready`; Osaka AP-B `http://127.0.0.1:30015/ready`; main-host AP-C `http://127.0.0.1:30025/ready` |

## Role

YouTube scraping/polling, `youtube_notification_outbox` production, 3-way active-active AP runtime입니다. `youtube-producer-a`/`youtube-producer-b`는 Osaka 호스트(`docker-compose.osaka.yml`), `youtube-producer-c`는 메인 호스트(`docker-compose.main-ap.yml`, profile `main-ap`)에서 같은 target set을 봅니다. Osaka a/b는 메인 valkey에 TCP로, c는 같은 호스트 valkey unix socket으로 붙어 동일한 lease 백엔드(`production` namespace)를 공유하므로, 각 `poller + channel` 실행은 단일 Valkey-backed JobRunGuard의 lease/cooldown으로 N-way 분배됩니다.

## Owns

- YouTube polling/scraping scheduler when `YOUTUBE_INGESTION_ENABLED=true`
- Holodex photo sync on AP-A and AP-C (`PHOTO_SYNC_ENABLED=true`), guarded by a global Valkey singleton lease so only one AP runs it at a time with TTL failover. AP-B (`PHOTO_SYNC_ENABLED=false`) is a scraping/polling failover peer only and does not participate in PhotoSync.
- Community/shorts/live/stats polling configuration
- `youtube_notification_outbox` production paths for YouTube-derived events

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| YouTube producer health | HTTP | `/health` | Compose healthcheck |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | YouTube channel/outbox/tracking state | scraping and handoff pipeline fail |
| Valkey | cache/config/coordination | stale targets or degraded queue behavior |

## Must not own

- Proactive notification egress owned by `alarm-worker`
- Alarm checking owned by `alarm-worker`
- Iris send and direct YouTube outbox dispatch; `alarm-worker` performs final send and delivery state updates
- Exactly-once delivery; duplicate protection is best-effort at scraper coordination plus database idempotency boundaries

## Startup requirements

- PostgreSQL and Valkey availability
- `YOUTUBE_INGESTION_ENABLED=true`
- `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true` on all active-active APs (a/b/c)
- `YOUTUBE_PRODUCER_INSTANCE_ID` unique per AP (`youtube-producer-a/-b/-c`)
- `YOUTUBE_PRODUCER_LEASE_NAMESPACE` shared by all APs in the same environment (`production`)
- `PHOTO_SYNC_ENABLED=true` on `youtube-producer-a` and `youtube-producer-c`, `PHOTO_SYNC_ENABLED=false` on `youtube-producer-b`
- scraper interval env values

## Shutdown behavior

- Stop pollers gracefully.
- Preserve pending outbox and tracking state in PostgreSQL.

## Observability

- Logs: `./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f youtube-producer`
- Health: `http://127.0.0.1:30005/health`
- Ready: `http://127.0.0.1:30005/ready` (a), `http://127.0.0.1:30015/ready` (b), `http://127.0.0.1:30025/ready` (c), all with `mode=active-active`
- Metrics: `youtube_poller_job_claim_total`, `youtube_poller_job_lease_renew_total`, `youtube_poller_job_mark_completed_total`, `youtube_poller_job_release_total`, `youtube_poller_outbox_insert_total`, `youtube_poller_published_at_resolver_*`

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/youtube-producer.md`
