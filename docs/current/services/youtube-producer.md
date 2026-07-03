# Service: youtube-producer

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-youtube-producer` |
| Binary | `youtube-producer` |
| Compose service | `youtube-producer` |
| Port | `30005` (AP-A) / `30015` (AP-B) / `30025` (AP-C) / `30035` (AP-D) |
| Health endpoint | remote AP local H3 port through `./bin/healthcheck`; AP-C `https://127.0.0.1:30025/health` on the main host |
| Ready endpoint | remote AP local port; all active APs report `mode=active-active` |

## Role

YouTube scraping/polling, `youtube_notification_outbox` production, active-active AP runtime입니다. `youtube-producer-b`는 Seoul 호스트(`deploy/compose/docker-compose.seoul.yml`), `youtube-producer-c`는 메인 호스트(`deploy/compose/docker-compose.main-ap.yml`, profile `main-ap`)에서 같은 target set을 봅니다. Osaka `youtube-producer-a`와 Osaka2 `youtube-producer-d`는 tiny VPS host-native `systemd` 런타임으로 live 운영 중입니다. 모든 AP는 동일한 lease 백엔드(`production` namespace)를 공유하므로, 각 `poller + channel` 실행은 단일 Valkey-backed JobRunGuard의 lease/cooldown으로 N-way 분배됩니다.

Remote AP deployment automation is split by runtime: Seoul uses Docker Compose;
Osaka and Osaka2 use `scripts/deploy/ap-host-native-deploy.sh`,
`scripts/deploy/ap-host-native-rollback.sh`, and
`scripts/logs/ap-host-native-status.sh`. The Docker Compose Osaka overlays remain
as repo-side contract definitions and compose-path validation inputs.

## Owns

- YouTube polling/scraping scheduler when `YOUTUBE_INGESTION_ENABLED=true`
- Holodex photo sync on AP-C (`PHOTO_SYNC_ENABLED=true`), guarded by a global Valkey singleton lease with TTL failover. AP-B (`PHOTO_SYNC_ENABLED=false`) is a scraping/polling failover peer only and does not participate in PhotoSync.
- Community/shorts/live/stats polling configuration
- `youtube_notification_outbox` production paths for YouTube-derived events
- Time-series retention cleanup for `youtube_stats_history`, `youtube_channel_stats_snapshots`, `youtube_live_sessions`, and `youtube_live_viewer_samples` (bounded batch deletes, advisory-locked single-runner, default off)

## Retention cleanup

App-level retention job (`internal/runtime/retention`) deletes rows older than a per-table cutoff in bounded batches, guarded by a single Postgres advisory lock so only one AP runs it at a time. It is opt-in and defaults to off. Within one tick the runner cleans `youtube_live_viewer_samples` first (delegating to the shared `poller.ViewerSampleCleaner`, which holds its own advisory lock), then the three table passes.

| Env key | Default | Effect |
|---|---|---|
| `YOUTUBE_PRODUCER_RETENTION_STATS_HISTORY_DAYS` | `0` | `youtube_stats_history` rows with `time < now - N days` are batch-deleted; `0`/negative disables. |
| `YOUTUBE_PRODUCER_RETENTION_CHANNEL_SNAPSHOTS_DAYS` | `0` | `youtube_channel_stats_snapshots` rows with `captured_at < now - N days`; `0`/negative disables. |
| `YOUTUBE_PRODUCER_RETENTION_LIVE_SESSIONS_DAYS` | `0` | `youtube_live_sessions` rows with `status='ENDED' AND ended_at < now - N days`; `0`/negative disables. |
| `YOUTUBE_PRODUCER_RETENTION_VIEWER_SAMPLES_DAYS` | `0` | `youtube_live_viewer_samples` for ENDED sessions older than N days (shared `ViewerSampleCleaner`); `0`/negative disables. |

Default `0` on every key is the safety contract: with all keys unset the runner is not even started, so no production rows are deleted until an operator explicitly opts in. Every key clamps negative values to `0` so a misconfiguration cannot push the cutoff into the future and delete the whole history.

`youtube_live_sessions` deletion is intentionally conservative: a session row is deleted only once it has no remaining `youtube_live_viewer_samples`, because the viewer-sample cleanup uses the session row as its join gate (there is no FK/cascade between the two tables). Running viewer-sample cleanup first in the same tick opens that gate. Consequently, enabling `..._LIVE_SESSIONS_DAYS` while `..._VIEWER_SAMPLES_DAYS` stays `0` leaves sessions that still have samples uncleaned — enable `..._VIEWER_SAMPLES_DAYS` as well to reclaim them.

Batch size and interval default to `1000` rows/batch and `1h`.

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| YouTube producer health | H3 | `/health` | Compose healthcheck |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | YouTube channel/outbox/tracking state over `verify-full` TLS with `/run/hololive-bot/certs/postgres-ca.pem` | scraping and handoff pipeline fail |
| Valkey | cache/config/coordination | stale targets or degraded queue behavior |

## Must not own

- Proactive notification egress owned by `alarm-worker`
- Alarm checking owned by `alarm-worker`
- Iris send and direct YouTube outbox dispatch; `alarm-worker` performs final send and delivery state updates
- Exactly-once delivery; duplicate protection is best-effort at scraper coordination plus database idempotency boundaries

## Startup requirements

- PostgreSQL and Valkey availability
- `YOUTUBE_INGESTION_ENABLED=true`
- `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true` on all active-active APs
- `YOUTUBE_PRODUCER_INSTANCE_ID` unique per AP (`youtube-producer-a/-b/-c/-d`)
- `YOUTUBE_PRODUCER_LEASE_NAMESPACE` shared by all APs in the same environment (`production`)
- `PHOTO_SYNC_ENABLED=true` on `youtube-producer-c`, `PHOTO_SYNC_ENABLED=false` on `youtube-producer-b`
- `POSTGRES_SSLMODE=verify-full` and `POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem`
- scraper interval env values

Host-native tiny VPS APs keep the same application contract but receive env
through `systemd` `EnvironmentFile` entries instead of Compose. They must remain
scraper-only by default with `PHOTO_SYNC_ENABLED=false`,
`YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false`, a conservative
`SCRAPER_SCHEDULER_WORKER_COUNT`, `/swapfile` deploy headroom, and
`/var/log/hololive-bot/archive/` log rotation for central mirroring.

## Shutdown behavior

- Stop pollers gracefully.
- Preserve pending outbox and tracking state in PostgreSQL.

## Observability

- Logs: `docker logs -f hololive-youtube-producer-c` (main), `SINCE=15m TAIL=600 ./scripts/logs/ap-logs.sh <ap-host> youtube-producer`
- Health: remote AP local H3 port (`30005`/`30015`/`30035`), AP-C `https://127.0.0.1:30025/health`
- Ready: remote AP local H3 port (`30005`/`30015`/`30035`), AP-C `https://127.0.0.1:30025/ready`, all with `mode=active-active`
- Metrics: `youtube_poller_job_claim_total`, `youtube_poller_job_lease_renew_total`, `youtube_poller_job_mark_completed_total`, `youtube_poller_job_release_total`, `youtube_poller_outbox_insert_total`

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/youtube-producer.md`
