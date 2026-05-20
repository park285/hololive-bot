# Runbook: youtube-producer

## Role

`youtube-producer`는 YouTube scraping/polling, outbox production, Osaka active-active AP runtime을 담당합니다. Osaka에서는 `youtube-producer-a`와 `youtube-producer-b`가 동시에 실행되며, Valkey JobRunGuard가 같은 `poller + channel`의 중복 Poll을 막습니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30005/health` and `http://127.0.0.1:30015/health` return success |
| Ready | both `/ready` payloads show `mode=active-active` |
| Logs | no repeated poller, photo sync, outbox, DB, cache, or proxy errors |
| Queue | produces outbox/tracking state; does not consume alarm dispatch queue |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | polling/outbox persistence fails |
| Valkey | yes | cache/config/coordination degrades |
| Iris | no | final proactive egress is owned by `alarm-worker` |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP health port | yes |
| `YOUTUBE_INGESTION_ENABLED` | must be true for this service | yes |
| `PHOTO_SYNC_ENABLED` | enabled only on APs allowed to run photo sync; active-active wraps it in a Valkey singleton lease | yes |
| `SCRAPER_SCHEDULER_WORKER_COUNT` | per-AP worker cap; Osaka active-active defaults each AP to `2` | Osaka yes |
| `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED` | enables per-job JobRunGuard and disables global runtime lease gate | Osaka yes |
| `YOUTUBE_PRODUCER_INSTANCE_ID` | unique active-active owner token prefix per AP | Osaka yes |
| `YOUTUBE_PRODUCER_LEASE_NAMESPACE` | shared lease namespace for APs in the same environment | Osaka yes |
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false` | producer-only egress boundary | yes |
| `YOUTUBE_PRODUCER_RUNTIME_ALLOWED=true` | must be true only on the owning Osaka host | yes |
| `SCRAPER_*` | poller intervals/workers | yes |
| `SCRAPER_BACKFILL_ENABLED=false` | optional secondary poller identities for coverage; disabled by default | no |
| `SCRAPER_BACKFILL_*_INTERVAL_SECONDS` | backfill poller intervals for shorts/community/live when enabled | no |
| `SCRAPER_BACKFILL_TARGET_GROUP=notification` | initial backfill target group; only `notification` is accepted | no |

## Logs

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml logs -f youtube-producer-a youtube-producer-b
```

Expected startup and sync markers:
- `Photo sync service started`
- `Photo sync completed`

## Metrics

- `youtube_poller_job_claim_total{poller,result}`: active-active claim distribution and Valkey fail-closed errors.
- `youtube_poller_job_lease_renew_total{poller,result}`: lease renew success/error/lost signals.
- `youtube_poller_job_mark_completed_total{poller,result}` and `youtube_poller_job_release_total{poller,result}`: completion/release ownership outcomes.
- `youtube_poller_outbox_insert_total{kind,result}`: outbox insert success/conflict/error counts.
- `youtube_poller_published_at_resolver_*`: pending `published_at` resolver attempts, successes, failures, skips, and enqueue counts.

## Cadence tuning

Active-active cadence is global for the same `(poller_name, channel_id)` identity. When one AP completes a primary poll, `JobRunGuard` writes the shared cooldown, so setting AP A and AP B to different intervals is not a coverage mechanism for the same primary poller.

Use primary interval tuning first, then enable backfill only if metrics show missed observations and the request budget remains acceptable. Starting profile for review, not a default:

```text
SCRAPER_SHORTS_SECONDS=90
SCRAPER_COMMUNITY_SECONDS=300
SCRAPER_LIVE_SECONDS=90
SCRAPER_VIDEOS_SECONDS=900
SCRAPER_STATS_SECONDS=21600
YOUTUBE_PRODUCER_AP_WORKER_COUNT=2
```

Optional backfill pollers use separate names (`shorts_backfill`, `community_backfill`, `live_backfill`) and separate cooldown keys. They reuse the same persistence/outbox path, so duplicate delivery is still guarded by `(kind, content_id)` idempotency and `alarm-worker` delivery claims. Backfill remains disabled by default:

```text
SCRAPER_BACKFILL_ENABLED=false
SCRAPER_BACKFILL_SHORTS_ENABLED=true
SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS=300
SCRAPER_BACKFILL_COMMUNITY_ENABLED=true
SCRAPER_BACKFILL_COMMUNITY_INTERVAL_SECONDS=600
SCRAPER_BACKFILL_LIVE_ENABLED=true
SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS=180
SCRAPER_BACKFILL_TARGET_GROUP=notification
```

Before any live cadence/backfill change, run the local budget and compose gates, then request explicit operator approval for the config write and redeploy. Rollback restores the previous `SCRAPER_*_SECONDS` and `SCRAPER_BACKFILL_*` values, then redeploys the approved `youtube-producer-a`/`youtube-producer-b` services only.

Osaka backfill rollout must be an env/config change, not a hardcoded service default. Set `SCRAPER_BACKFILL_ENABLED=true` and the selected intervals in `/run/hololive-bot/env` or an approved equivalent env override, then redeploy only after explicit operator approval. Monitor:

```text
youtube_poller_job_claim_total{poller="shorts_backfill",result="acquired"}
youtube_poller_job_claim_total{poller="shorts_backfill",result="already_completed"}
youtube_poller_outbox_insert_total{kind=...,result="conflict"}
/ready: mode=active-active, valkey_available=true, scraping_paused=false
```

## Osaka rollout

Use the scoped deployment wrapper for Osaka active-active rollout. It syncs only the active-active runtime files listed in `scripts/deploy/osaka-active-active-rsync-files.txt`; it intentionally excludes `docker-compose.prod.yml`, secrets, docs, tests, `go.sum`, `hololive-alarm-worker/**`, and runtime data.

```bash
./scripts/deploy/osaka-active-active-deploy.sh --dry-run
I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/osaka-active-active-deploy.sh --apply
```

Live `--apply` requires explicit operator approval before execution. The wrapper captures a prechange backup under `backups/osaka-active-active-<timestamp>/`, validates compose config, builds only `youtube-producer-a` and `youtube-producer-b`, recreates both APs with `--no-deps --remove-orphans`, and runs readiness/health/log smoke checks.

After rollout, the read-only completion check must pass:

```bash
CHANGE_STARTED_AT=<change_started_at> ./scripts/deploy/osaka-active-active-completion-check.sh
```

## Common failure modes

### 1. Polling stalls

Symptoms:
- No fresh YouTube polling/outbox activity.
- Health may remain up.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=300 youtube-producer
curl http://127.0.0.1:30005/health
```

Mitigation:
- Check producer interval env, PostgreSQL, Valkey, and YouTube/proxy errors.

Rollback:
- Roll back `youtube-producer` image/config if poller behavior changed.

### 2. Active-active coordination degrades

Symptoms:
- `/ready` does not show `mode=active-active`.
- Logs show repeated `claim poll job` or Valkey errors.
- Poll jobs stop while `/health` remains up.

Diagnosis:
```bash
curl -fsS http://127.0.0.1:30005/ready
curl -fsS http://127.0.0.1:30015/ready
./scripts/logs/osaka-status.sh
```

Mitigation:
- Check Valkey reachability and `YOUTUBE_PRODUCER_LEASE_NAMESPACE`.
- Keep fail-closed behavior: do not disable JobRunGuard to recover active-active traffic.
- Roll back by scaling down to one AP first, then redeploying the previous config.

### 3. Photo sync fails or stalls

Symptoms:
- `Photo sync completed` is absent after startup or sync-related errors repeat.
- Health may remain up while photo state is stale.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=300 youtube-producer
curl http://127.0.0.1:30005/health
```

Mitigation:
- Check `PHOTO_SYNC_ENABLED=true`, PostgreSQL, Valkey, and Holodex/API errors.

Rollback:
- Roll back the previous `youtube-producer` image/config if the startup-owned photo sync path regresses.

### 4. Outbox backlog grows

Symptoms:
- Producer persists events but downstream delivery is delayed.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=300 youtube-producer
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
```

Mitigation:
- Identify whether backlog is producer, alarm-worker, or Iris side.

Rollback:
- Roll back the runtime that introduced the backlog.

## Smoke test

```bash
./scripts/logs/osaka-smoke.sh
```

Default smoke checks both AP `/ready` payloads for `mode=active-active`, `valkey_available=true`, and `scraping_paused=false`, then verifies `/health` inside each container. External CA/egress smoke is optional because it depends on public network reachability:

```bash
OSAKA_SMOKE_EXTERNAL=true ./scripts/logs/osaka-smoke.sh
```

## Observation runtime cutover

`youtube-producer` is the only runtime name after this cleanup. Before rollout, audit existing observation rows so operators know which historical reports were collected under earlier runtime names:

```sql
SELECT runtime_name, count(*)
FROM youtube_community_shorts_observation_windows
GROUP BY runtime_name
ORDER BY runtime_name;

SELECT runtime_name, count(*)
FROM youtube_community_shorts_observation_post_baselines
GROUP BY runtime_name
ORDER BY runtime_name;
```

Post-rollout operational reports must use:

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts continuous-observation \
  -observation-runtime youtube-producer
```

Do not add runtime-name aliases in application code. Historical rows remain historical data; new observation windows and baselines are created under `youtube-producer`.

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `youtube-producer` image/config.
- Scale down `youtube-producer-b` first, confirm `youtube-producer-a` remains healthy, then redeploy the previous image/config.
- Confirm `YOUTUBE_INGESTION_ENABLED=true`, active `/ready`, health on port `30005`, and outbox/photo sync state after rollback.
- The deploy wrapper stores overwritten files and prechange container inventory under `backups/osaka-active-active-<timestamp>/`; use that evidence to restore the previous `docker-compose.prod.yml` and `docker-compose.osaka.yml` if active-active startup fails.
- Dry-run the rollback helper before applying:

```bash
BACKUP_DIR=backups/osaka-active-active-<timestamp> ./scripts/deploy/osaka-active-active-rollback.sh --dry-run
I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true BACKUP_DIR=backups/osaka-active-active-<timestamp> ./scripts/deploy/osaka-active-active-rollback.sh --apply
```

## Related contracts

- `../contracts/alarm.md`
- `../contracts/settings.md`
- `../contracts/iris-boundary.md`
