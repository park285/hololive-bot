# Runbook: youtube-producer

## Role

`youtube-producer`는 YouTube scraping/polling, outbox production, 3-way active-active AP runtime을 담당합니다. Osaka 호스트에서 `youtube-producer-a`(30005, `deploy/compose/docker-compose.osaka.yml`)가, Seoul 호스트에서 `youtube-producer-b`(30015, `deploy/compose/docker-compose.seoul.yml`)가, 메인 호스트에서 `youtube-producer-c`(30025, `deploy/compose/docker-compose.main-ap.yml`, profile `main-ap`)가 동시에 실행됩니다. 셋은 메인 valkey의 동일 lease 백엔드(`production` namespace)를 공유하며, Valkey JobRunGuard가 같은 `poller + channel`의 중복 Poll을 막습니다. 원격 AP 호스트(osaka, seoul)는 `scripts/deploy/ap-hosts/<host>.conf`로 정의되고 `ap-*` 스크립트가 공통 운영 경로를 제공합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | Osaka `http://127.0.0.1:30005/health`, Seoul `:30015/health`, main `:30025/health` return success (각 호스트 로컬 기준) |
| Ready | all three `/ready` payloads show `mode=active-active` |
| Logs | startup markers include PostgreSQL and Valkey connection success; repeated poller, photo sync, outbox, DB, cache, or proxy errors are absent |
| Queue | produces outbox/tracking state; does not consume alarm dispatch queue |
| PostgreSQL TLS | AP `a`, AP `b`, and main `c` render `POSTGRES_SSLMODE=verify-full` and mount `/run/hololive-bot/certs/postgres-ca.pem` |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes; `verify-full` TLS with `/run/hololive-bot/certs/postgres-ca.pem` | polling/outbox persistence fails |
| Valkey | yes | cache/config/coordination degrades |
| Iris | no | final proactive egress is owned by `alarm-worker` |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP health port | yes |
| `YOUTUBE_INGESTION_ENABLED` | must be true for this service | yes |
| `YOUTUBE_PRODUCER_REQUEST_INTERVAL_SECONDS=2` | YouTube producer scraper request ceiling; `2s` means `30 RPM` | yes |
| `PHOTO_SYNC_ENABLED` | AP-A and AP-C run photo sync (`true`); a global Valkey singleton lease keeps only one active with TTL failover. AP-B is `false` | yes |
| `SCRAPER_SCHEDULER_WORKER_COUNT` | per-AP worker cap; remote active-active defaults each AP to `2` | remote AP yes |
| `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED` | enables per-job JobRunGuard and disables global runtime lease gate | remote AP yes |
| `YOUTUBE_PRODUCER_INSTANCE_ID` | unique active-active owner token prefix per AP | remote AP yes |
| `YOUTUBE_PRODUCER_LEASE_NAMESPACE` | shared lease namespace for APs in the same environment | remote AP yes |
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false` | producer-only egress boundary | yes |
| `YOUTUBE_PRODUCER_RUNTIME_ALLOWED=true` | must be true only on owning AP hosts | yes |
| `POSTGRES_SSLMODE=verify-full` | required client verification mode for central and AP PostgreSQL TCP paths | yes |
| `POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem` | CA bundle rendered by OpenBao Agent and mounted read-only into producer containers | yes |
| `SCRAPER_*` | poller intervals/workers | yes |
| `SCRAPER_BACKFILL_ENABLED=false` | optional secondary poller identities for coverage; disabled by default | no |
| `SCRAPER_BACKFILL_*_INTERVAL_SECONDS` | backfill poller intervals for shorts/community/live when enabled | no |
| `SCRAPER_BACKFILL_TARGET_GROUP=notification` | initial backfill target group; only `notification` is accepted | no |

## Logs

```bash
# Osaka a
./scripts/logs/ap-logs.sh osaka youtube-producer-a
# Seoul b
./scripts/logs/ap-logs.sh seoul youtube-producer-b
# 메인 호스트 c (main-ap profile)
COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.main-ap.yml logs -f youtube-producer-c
```

Expected startup and sync markers:
- `Photo sync service started`
- `Photo sync completed`

Photo sync policy: `youtube-producer-a` and `youtube-producer-c` set `PHOTO_SYNC_ENABLED=true`; a global Valkey singleton lease lets only one of them own photo sync at a time, with TTL-based failover. `youtube-producer-b` is a scraping/polling failover peer only (`PHOTO_SYNC_ENABLED=false`) and does not participate in PhotoSync failover.

PostgreSQL TLS policy: all producer instances use `verify-full`. AP hosts receive
only the CA bundle at `/run/hololive-bot/certs/postgres-ca.pem`; the central
PostgreSQL server key lives under `/run/hololive-bot/postgres-tls/` on the
central host.

## Metrics

- `youtube_poller_job_claim_total{poller,result}`: active-active claim distribution and Valkey fail-closed errors.
- `youtube_poller_job_lease_renew_total{poller,result}`: lease renew success/error/lost signals.
- `youtube_poller_job_mark_completed_total{poller,result}` and `youtube_poller_job_release_total{poller,result}`: completion/release ownership outcomes.
- `youtube_poller_outbox_insert_total{kind,result}`: outbox insert success/conflict/error counts.
- `youtube_poller_published_at_resolver_*`: pending `published_at` resolver attempts, successes, failures, skips, and enqueue counts.

Active-active `/ready` fails closed on startup until a lightweight Valkey JobRunGuard probe or later job claim proves lease availability. During that state it reports `valkey_available=false` and `scraping_paused=true` while `/health` can still be up.

`/ready` is readiness state, not recent activity telemetry. Use the `youtube_poller_job_*` metrics above to confirm recent `acquired`, `peer_owned`, `already_completed`, renew, mark-completed, and release activity.

## Cadence tuning

Active-active cadence is global for the same `(poller_name, channel_id)` identity. When one AP completes a primary poll, `JobRunGuard` writes the shared cooldown, so setting AP A and AP B to different intervals is not a coverage mechanism for the same primary poller.

Use primary interval tuning first, then enable backfill only if metrics show missed observations and the request budget remains acceptable. Starting profile for review, not a default:

```text
SCRAPER_SHORTS_SECONDS=90
SCRAPER_COMMUNITY_SECONDS=90
SCRAPER_LIVE_SECONDS=90
SCRAPER_VIDEOS_SECONDS=900
SCRAPER_STATS_SECONDS=21600
YOUTUBE_PRODUCER_AP_WORKER_COUNT=2
YOUTUBE_PRODUCER_REQUEST_INTERVAL_SECONDS=2
```

Primary community polling follows the shorts cadence in youtube-producer; keep `SCRAPER_COMMUNITY_SECONDS` aligned for config readability. Backfill community polling remains separately controlled by `SCRAPER_BACKFILL_COMMUNITY_INTERVAL_SECONDS`.

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

Before any live cadence/backfill change, run the local budget and compose gates, then request explicit operator approval for the config write and redeploy. Rollback restores the previous `SCRAPER_*_SECONDS` and `SCRAPER_BACKFILL_*` values, then redeploys the approved AP services only (`youtube-producer-a` on osaka, `youtube-producer-b` on seoul).

Remote AP backfill rollout must be an env/config change, not a hardcoded service default. Set `SCRAPER_BACKFILL_ENABLED=true` and the selected intervals in `/run/hololive-bot/env` or an approved equivalent env override, then redeploy only after explicit operator approval. Monitor:

```text
youtube_poller_job_claim_total{poller="shorts_backfill",result="acquired"}
youtube_poller_job_claim_total{poller="shorts_backfill",result="already_completed"}
youtube_poller_outbox_insert_total{kind=...,result="conflict"}
/ready: mode=active-active, valkey_available=true, scraping_paused=false
```

## Remote AP rollout (osaka, seoul)

Use the scoped deployment wrapper for remote AP active-active rollout. Host topology is defined in `scripts/deploy/ap-hosts/<host>.conf` (osaka: `youtube-producer-a`, seoul: `youtube-producer-b`). The wrapper syncs only the active-active runtime files listed in `scripts/deploy/ap-rsync-files.txt`; it intentionally excludes secrets, docs, tests, `hololive-alarm-worker/**`, runtime data, and all data paths except the embedded `hololive-shared` profile data required by the producer image.

```bash
./scripts/deploy/ap-deploy.sh osaka --dry-run
I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh osaka --apply

./scripts/deploy/ap-deploy.sh seoul --dry-run
I_APPROVE_SEOUL_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh seoul --apply
```

Live `--apply` requires explicit operator approval before execution, with a per-host approval env var (`I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY`, `I_APPROVE_SEOUL_ACTIVE_ACTIVE_DEPLOY`). The wrapper captures a prechange backup under `backups/<host>-active-active-<timestamp>/`, validates compose config, builds only that host's AP services, recreates them with `--no-deps --remove-orphans`, and runs readiness/health/log smoke checks.

After rollout, the read-only completion check must pass:

```bash
CHANGE_STARTED_AT=<change_started_at> ./scripts/deploy/ap-completion-check.sh <host>
```

The remote wrapper covers that host's AP services only. The main-host `youtube-producer-c` is redeployed separately on the main host, guarded by its `main-ap` profile:

```bash
sudo -n env \
  COMPOSE_FILE=deploy/compose/docker-compose.prod.yml:deploy/compose/docker-compose.live-compat.yml:deploy/compose/docker-compose.main-ap.yml:deploy/compose/docker-compose.main-ap.live-compat.yml \
  COMPOSE_PROFILES=main-ap \
  COMPOSE_ENV_FILE=/run/hololive-bot/env \
  ./scripts/deploy/compose-redeploy-service.sh youtube-producer-c
```

First boot on a newly provisioned AP host (no AP containers yet) requires `AP_PREFLIGHT_ALLOW_FIRST_BOOT=true` so the Iris H3 trust preflight skips its in-container check once; post-start readiness checks still gate the rollout. The wrapper also copies `deploy/compose/docker-compose.prod.yml` and the host overlay into its prechange backup *before* the rsync step, so pre-seed the repo files onto the host once (manual rsync with `--files-from=scripts/deploy/ap-rsync-files.txt`) before the first `--apply`.

## Common failure modes

### 1. Polling stalls

Symptoms:
- No fresh YouTube polling/outbox activity.
- Health may remain up.

Diagnosis:
```bash
./scripts/logs/ap-status.sh osaka
./scripts/logs/ap-status.sh seoul
docker logs --tail 300 hololive-youtube-producer-c
curl -fsS http://127.0.0.1:30025/ready
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
# 각 AP 호스트 로컬에서 /ready 확인 (osaka 30005, seoul 30015, main 30025)
./scripts/logs/ap-status.sh osaka
./scripts/logs/ap-status.sh seoul
curl -fsS http://127.0.0.1:30025/ready
```

Mitigation:
- Check Valkey reachability and `YOUTUBE_PRODUCER_LEASE_NAMESPACE`.
- Keep fail-closed behavior: do not disable JobRunGuard to recover active-active traffic.
- Roll back by scaling down to one AP first, then redeploying the previous config.

Lease TTL policy:
- Scheduler job lease TTL is `SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS + 15s`.
- The minimum lease TTL is 1 minute.
- There is no hard maximum clamp. Do not add one without also constraining poll timeout, because a clamp shorter than a valid in-flight poll can create duplicate work.
- Each successful poll writes a cooldown equal to that job's scheduler interval, so peer APs should report `already_completed` until that cooldown expires.

### 3. Photo sync fails or stalls

Symptoms:
- `Photo sync completed` is absent after startup or sync-related errors repeat.
- Health may remain up while photo state is stale.

Diagnosis:
```bash
SINCE=30m TAIL=600 PATTERN='photo' ./scripts/logs/ap-logs.sh osaka youtube-producer | tail -n 80
docker logs --since 30m hololive-youtube-producer-c 2>&1 | grep -i photo | tail -n 80
```

Mitigation:
- Check `PHOTO_SYNC_ENABLED=true`, PostgreSQL, Valkey, and Holodex/API errors.
- Photo sync is owned by whichever of `youtube-producer-a`/`youtube-producer-c` currently holds the singleton lease; check the lease holder's logs. `youtube-producer-b` never runs PhotoSync.

Rollback:
- Roll back the previous `youtube-producer` image/config if the startup-owned photo sync path regresses.

### 4. Valkey 일시 단절 후 active-active fail-closed가 풀리지 않음

Symptoms:
- 컨테이너 부팅 직후 일회성 `dial tcp <main-valkey>:6379: connect: connection refused` 이후 producer 로그에 `active_active_paused reason=valkey_unavailable_active_active_fail_closed`가 지속.
- `/ready` JSON의 `valkey_available=false`, `scraping_paused=true`가 유지.
- `youtube_notification_outbox.max(created_at)`이 분 단위가 아닌 시간 단위로 정체. alarm-worker `alarm_type` 분포가 SHORTS만 잔존하고 LIVE/COMMUNITY_POST/NEW_VIDEO 등은 사라짐.

Diagnosis:
```bash
ss -tlnp 2>/dev/null | grep ':6379'
docker exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock PING
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o HostKeyAlias=100.100.1.7 ubuntu@kapu-iris-osaka-1 \
  'docker ps --format "{{.Names}}\t{{.Status}}" | grep -iE "youtube|producer"'
SINCE=15m TAIL=600 PATTERN='active_active_paused|active_active_resumed|valkey' \
  ./scripts/logs/ap-logs.sh osaka youtube-producer | tail -80
SINCE=15m TAIL=600 PATTERN='active_active_paused|active_active_resumed|valkey' \
  ./scripts/logs/ap-logs.sh seoul youtube-producer | tail -80
```

Mitigation:
- 백그라운드 recovery loop가 `__readiness_probe__`를 사용해 5초 base interval로 재시도하므로, 메인 valkey가 살아나면 자동으로 `active_active_resumed` 로그가 등장하고 `MarkLeaseAvailable()`이 호출됩니다. 일반적으로 사람 개입 없이 회복됩니다.
- 회복이 5분 이상 걸리면 메인 valkey-cache의 listen/auth와 Tailscale ACL, 호스트 방화벽을 먼저 확인. 그래도 막히면 producer-a(osaka) → 30초 대기 → producer-b(seoul) 순서로 재시작.

Rollback:
- 기존 active-active rollback 절차(`./scripts/deploy/ap-rollback.sh <host>`)를 그대로 사용. recovery loop는 readiness 보조 경로이므로 별도 롤백 대상이 아닙니다.

### 5. Outbox backlog grows

Symptoms:
- Producer persists events but downstream delivery is delayed.

Diagnosis:
```bash
SINCE=30m TAIL=600 PATTERN='outbox' ./scripts/logs/ap-logs.sh osaka youtube-producer | tail -n 80
SINCE=30m TAIL=600 PATTERN='outbox' ./scripts/logs/ap-logs.sh seoul youtube-producer | tail -n 80
docker logs --tail 300 hololive-youtube-producer-c
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
```

Mitigation:
- Identify whether backlog is producer, alarm-worker, or Iris side.

Rollback:
- Roll back the runtime that introduced the backlog.

## Smoke test

```bash
./scripts/logs/ap-smoke.sh osaka
./scripts/logs/ap-smoke.sh seoul
```

Default smoke checks the host's AP `/ready` payloads for `mode=active-active`, `valkey_available=true`, and `scraping_paused=false`, then verifies `/health` inside each container. External CA/egress smoke is optional because it depends on public network reachability:

```bash
AP_SMOKE_EXTERNAL=true ./scripts/logs/ap-smoke.sh <host>
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
- Scale down `youtube-producer-b` (seoul) first, confirm `youtube-producer-a` (osaka) remains healthy, then redeploy the previous image/config.
- Confirm `YOUTUBE_INGESTION_ENABLED=true`, active `/ready`, health on port `30005`, and outbox/photo sync state after rollback.
- The deploy wrapper stores overwritten files and prechange container inventory under `backups/<host>-active-active-<timestamp>/` on that AP host; use that evidence to restore the previous `deploy/compose/docker-compose.prod.yml` and that host's compose override if active-active startup fails.
- Topology-level rollback to the pre-2026-06-04 Osaka `a`+`b` layout is manual: stop `youtube-producer-b` on seoul, restore a pre-cutover `deploy/compose/docker-compose.osaka.yml` that defines `youtube-producer-b` — recover it from git history before the compose-directory refactor (`git show 7558024f^:docker-compose.osaka.yml`) or from the 2026-06-04 cutover backup (`backups/osaka-active-active-20260604T102113Z/`); prechange backups taken by deploys *after* the cutover no longer define `youtube-producer-b` — then start it on osaka with an explicit `up -d --no-deps youtube-producer-b`. `ap-rollback.sh osaka` alone restores files but only recreates the services listed in `ap-hosts/osaka.conf` (now `youtube-producer-a`).
- Dry-run the rollback helper before applying (per-host approval env var):

```bash
BACKUP_DIR=backups/osaka-active-active-<timestamp> ./scripts/deploy/ap-rollback.sh osaka --dry-run
I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true BACKUP_DIR=backups/osaka-active-active-<timestamp> ./scripts/deploy/ap-rollback.sh osaka --apply

BACKUP_DIR=backups/seoul-active-active-<timestamp> ./scripts/deploy/ap-rollback.sh seoul --dry-run
I_APPROVE_SEOUL_ACTIVE_ACTIVE_ROLLBACK=true BACKUP_DIR=backups/seoul-active-active-<timestamp> ./scripts/deploy/ap-rollback.sh seoul --apply
```

## Related contracts

- `../contracts/alarm.md`
- `../contracts/settings.md`
- `../contracts/iris-boundary.md`
