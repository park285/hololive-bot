# Service: youtube-producer

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-youtube-producer` |
| Binary | `youtube-producer` |
| Compose service | `youtube-producer` |
| Port | `30005` |
| Health endpoint | `http://127.0.0.1:30005/health` |
| Ready endpoint | `http://127.0.0.1:30005/ready`; Osaka AP-B uses `http://127.0.0.1:30015/ready` |

## Role

YouTube scraping/polling, `youtube_notification_outbox` production, Osaka active-active AP runtime입니다. Osaka 배포에서는 `youtube-producer-a`와 `youtube-producer-b`가 같은 target set을 보며, 각 `poller + channel` 실행은 Valkey-backed JobRunGuard의 lease/cooldown으로 조정합니다.

## Owns

- YouTube polling/scraping scheduler when `YOUTUBE_INGESTION_ENABLED=true`
- Holodex photo sync on the explicitly enabled AP, guarded by a Valkey singleton lease in active-active mode
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
- `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true` on Osaka active-active APs
- `YOUTUBE_PRODUCER_INSTANCE_ID` unique per AP
- `YOUTUBE_PRODUCER_LEASE_NAMESPACE` shared by APs in the same environment
- scraper interval env values

## Shutdown behavior

- Stop pollers gracefully.
- Preserve pending outbox and tracking state in PostgreSQL.

## Observability

- Logs: `./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f youtube-producer`
- Health: `http://127.0.0.1:30005/health`
- Ready on Osaka: `http://127.0.0.1:30005/ready` and `http://127.0.0.1:30015/ready`, both with `mode=active-active`
- Metrics: `youtube_poller_job_claim_total`, `youtube_poller_job_lease_renew_total`, `youtube_poller_job_mark_completed_total`, `youtube_poller_job_release_total`, `youtube_poller_outbox_insert_total`, `youtube_poller_published_at_resolver_*`

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/youtube-producer.md`
