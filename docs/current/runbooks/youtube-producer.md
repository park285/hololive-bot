# Runbook: youtube-producer

> 실제 tailnet 주소/호스트는 private ops evidence 참조.

## Role

`youtube-producer`는 YouTube scraping/polling, outbox production, active-active AP runtime을 담당합니다. Seoul 호스트에서 `youtube-producer-b`(30015, `deploy/compose/docker-compose.seoul.yml`)가, 메인 호스트에서 `youtube-producer-c`(30025, `deploy/compose/docker-compose.main-ap.yml`, profile `main-ap`)가 동시에 실행됩니다. Osaka `youtube-producer-a`(30005, host `<tailnet-osaka-a>`)와 Osaka2 `youtube-producer-d`(30035, host `<tailnet-osaka2-d>`)는 tiny VPS host-native `systemd` 런타임으로 live 운영 중이며, repo-side Docker Compose overlays는 compose 경로/계약 검증용으로 유지합니다. 모든 AP는 메인 valkey의 동일 lease 백엔드(`production` namespace)를 공유하며, Valkey JobRunGuard가 같은 `poller + channel`의 중복 Poll을 막습니다. 원격 AP 호스트는 `scripts/deploy/ap-hosts/<host>.conf`로 정의되고 `ap-*` 스크립트가 공통 운영 경로를 제공합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | active AP local `/health` returns success (`a` 30005, `b` 30015, `c` 30025, `d` 30035) |
| Ready | active AP `/ready` payloads show `mode=active-active` |
| Logs | startup markers include PostgreSQL and Valkey connection success; repeated poller, photo sync, outbox, DB, cache, or proxy errors are absent |
| Queue | produces outbox/tracking state; does not consume alarm dispatch queue |
| PostgreSQL TLS | AP `b` and main `c` render `POSTGRES_SSLMODE=verify-full` and mount `/run/hololive-bot/certs/postgres-ca.pem` |

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
| `PHOTO_SYNC_ENABLED` | AP-C runs photo sync (`true`); a global Valkey singleton lease keeps only one active with TTL failover. AP-B is `false` | yes |
| `SCRAPER_SCHEDULER_WORKER_COUNT` | per-AP worker cap; remote active-active defaults each AP to `2` | remote AP yes |
| `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED` | enables per-job JobRunGuard and disables global runtime lease gate | remote AP yes |
| `YOUTUBE_PRODUCER_INSTANCE_ID` | unique active-active owner token prefix per AP | remote AP yes |
| `YOUTUBE_PRODUCER_LEASE_NAMESPACE` | shared lease namespace for APs in the same environment | remote AP yes |
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false` | producer-only egress boundary | yes |
| `YOUTUBE_PRODUCER_RUNTIME_ALLOWED=true` | must be true only on owning AP hosts | yes |
| `POSTGRES_SSLMODE=verify-full` | required client verification mode for central and AP PostgreSQL TCP paths | yes |
| `POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem` | CA bundle rendered by OpenBao Agent and mounted read-only into producer containers | yes |
| `SCRAPER_FETCHER_ENGINE` | container-local page fetch engine; defaults to `nethttp`, use `goscrapy` only for scoped evaluation | no |
| `YOUTUBE_PRODUCER_A_FETCHER_ENGINE`, `YOUTUBE_PRODUCER_B_FETCHER_ENGINE`, `YOUTUBE_PRODUCER_C_FETCHER_ENGINE`, `YOUTUBE_PRODUCER_D_FETCHER_ENGINE` | compose/OpenBao per-instance source for `SCRAPER_FETCHER_ENGINE`; default is `nethttp` | no |
| `SCRAPER_*` | poller intervals/workers | yes |
| `SCRAPER_BACKFILL_ENABLED=false` | optional secondary poller identities for coverage; disabled by default | no |
| `SCRAPER_BACKFILL_*_INTERVAL_SECONDS` | backfill poller intervals for shorts/community/live when enabled | no |
| `SCRAPER_BACKFILL_TARGET_GROUP=notification` | initial backfill target group; only `notification` is accepted | no |
| `YOUTUBE_PRODUCER_RETENTION_STATS_HISTORY_DAYS` | delete `youtube_stats_history` rows older than N days; `0` (default) disables cleanup (infinite retention) | no |
| `YOUTUBE_PRODUCER_RETENTION_CHANNEL_SNAPSHOTS_DAYS` | delete `youtube_channel_stats_snapshots` rows older than N days; `0` (default) disables cleanup | no |
| `YOUTUBE_PRODUCER_RETENTION_LIVE_SESSIONS_DAYS` | delete `ENDED` `youtube_live_sessions` rows whose `ended_at` is older than N days; `0` (default) disables cleanup | no |
| `YOUTUBE_PRODUCER_RETENTION_VIEWER_SAMPLES_DAYS` | delete `youtube_live_viewer_samples` rows older than N days; `0` (default) disables cleanup. live_sessions cleanup only removes sessions with no remaining samples, so enable this together with `LIVE_SESSIONS_DAYS` to actually reclaim sessions | no |

## Logs

```bash
# Seoul b
./scripts/logs/ap-logs.sh seoul youtube-producer-b
# 메인 호스트 c (main-ap profile)
COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.main-ap.yml logs -f youtube-producer-c
```

Expected startup and sync markers:
- `Photo sync service started`
- `Photo sync completed`

Photo sync policy: `youtube-producer-c` sets `PHOTO_SYNC_ENABLED=true`; a global Valkey singleton lease keeps it the sole photo sync owner, with TTL-based failover. `youtube-producer-b` is a scraping/polling failover peer only (`PHOTO_SYNC_ENABLED=false`) and does not participate in PhotoSync failover.

PostgreSQL TLS policy: all producer instances use `verify-full`. AP hosts receive
only the CA bundle at `/run/hololive-bot/certs/postgres-ca.pem`; the central
PostgreSQL server key lives under `/run/hololive-bot/postgres-tls/` on the
central host.

## Metrics

- `/ready` reports `scraper_fetcher_engine` for the currently rendered fetch engine.
- Startup logs include `scraper_fetcher_engine` on `ingestion_runtime_configured`.
- `hololive_youtube_scraper_fetch_requests_total{engine,outcome,reason,status_code}`: scraper page fetch success/error outcomes by fetcher engine.
- `hololive_youtube_scraper_fetch_duration_seconds{engine,outcome,reason}`: scraper page fetch latency by fetcher engine.
- `hololive_youtube_scraper_fetch_fallback_total{from_engine,to_engine,reason}`: fallback count when `goscrapy` fails before a response and falls back to `nethttp`.
- `youtube_poller_job_claim_total{poller,result}`: active-active claim distribution and Valkey fail-closed errors.
- `youtube_poller_job_lease_renew_total{poller,result}`: lease renew success/error/lost signals.
- `youtube_poller_job_mark_completed_total{poller,result}` and `youtube_poller_job_release_total{poller,result}`: completion/release ownership outcomes.
- `youtube_poller_outbox_insert_total{kind,result}`: outbox insert success/conflict/error counts.

Active-active `/ready` fails closed on startup until a lightweight Valkey JobRunGuard probe or later job claim proves lease availability. During that state it reports `valkey_available=false` and `scraping_paused=true` while `/health` can still be up.

`/ready` is readiness state, not recent activity telemetry. Use the `youtube_poller_job_*` metrics above to confirm recent `acquired`, `peer_owned`, `already_completed`, renew, mark-completed, and release activity.

`/metrics` is protected by `X-API-Key` when `API_SECRET_KEY` is configured. Producer metrics are served from the plain HTTP metrics listener on `:30095`, separate from the H3 app port. Docker AP metrics are published on each host Tailscale IP only (`a` `<tailnet-osaka-a>:30095`, `b` `<tailnet-seoul-b>:30095`, `d` `<tailnet-osaka2-d>:30095`) so central Prometheus can scrape them with the shared API key header. For operator-local checks, run the probe from inside the target container so the secret stays in the container environment and is not passed as a command-line value:

```bash
# Seoul b host
docker exec hololive-youtube-producer-b ./bin/healthcheck --body-api-key-env API_SECRET_KEY http://127.0.0.1:30095/metrics

# Main c host
docker exec hololive-youtube-producer-c ./bin/healthcheck --body-api-key-env API_SECRET_KEY http://127.0.0.1:30095/metrics
```

For `goscrapy` canary, compare `youtube-producer-b` before and after the scoped change:

```text
/ready: scraper_fetcher_engine=goscrapy, mode=active-active, valkey_available=true, scraping_paused=false
hololive_youtube_scraper_fetch_requests_total{engine="goscrapy",outcome="success",...}
hololive_youtube_scraper_fetch_requests_total{engine="goscrapy",outcome="error",...}
hololive_youtube_scraper_fetch_fallback_total{from_engine="goscrapy",to_engine="nethttp",...}
```

Use `youtube-producer-c` as the `nethttp` baseline and `youtube-producer-b` as the `goscrapy` canary unless a rollout explicitly changes the per-instance `YOUTUBE_PRODUCER_*_FETCHER_ENGINE` values.

Rollback restores `YOUTUBE_PRODUCER_B_FETCHER_ENGINE=nethttp`, rerenders the AP env, and redeploys only `youtube-producer-b`.

## Cadence tuning

Active-active cadence is global for the same `(poller_name, channel_id)` identity. When one AP completes a primary poll, `JobRunGuard` writes the shared cooldown, so setting AP B and AP C to different intervals is not a coverage mechanism for the same primary poller.

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

Before any live cadence/backfill change, run the local budget and compose gates, then request explicit operator approval for the config write and redeploy. Rollback restores the previous `SCRAPER_*_SECONDS` and `SCRAPER_BACKFILL_*` values, then redeploys the approved AP services only (`youtube-producer-b` on seoul).

Remote AP backfill rollout must be an env/config change, not a hardcoded service default. Set `SCRAPER_BACKFILL_ENABLED=true` and the selected intervals in `/run/hololive-bot/env` or an approved equivalent env override, then redeploy only after explicit operator approval. Monitor:

```text
youtube_poller_job_claim_total{poller="shorts_backfill",result="acquired"}
youtube_poller_job_claim_total{poller="shorts_backfill",result="already_completed"}
youtube_poller_outbox_insert_total{kind=...,result="conflict"}
/ready: mode=active-active, valkey_available=true, scraping_paused=false
```

## Remote AP rollout (Docker Compose path)

Use the scoped deployment wrapper for remote AP active-active rollout when the chosen runtime mode is Docker Compose. Host topology is defined in `scripts/deploy/ap-hosts/<host>.conf` (`osaka`: `youtube-producer-a`, `osaka2`: `youtube-producer-d`, `seoul`: `youtube-producer-b`). The wrapper syncs only the active-active runtime files listed in `scripts/deploy/ap-rsync-files.txt`; it intentionally excludes secrets, docs, tests, `hololive-alarm-worker/**`, runtime data, and all data paths except the embedded `hololive-shared` profile data required by the producer image.

```bash
./scripts/deploy/ap-deploy.sh seoul --dry-run
I_APPROVE_SEOUL_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh seoul --apply
```

Live `--apply` requires explicit operator approval before execution, with a per-host approval env var (`I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY`, `I_APPROVE_OSAKA2_ACTIVE_ACTIVE_DEPLOY`, or `I_APPROVE_SEOUL_ACTIVE_ACTIVE_DEPLOY`). The wrapper captures a prechange backup under `backups/<host>-active-active-<timestamp>/`, validates compose config, builds only that host's AP services, recreates them with `--no-deps --remove-orphans`, and runs readiness/health/log smoke checks.

After rollout, the read-only completion check must pass:

```bash
CHANGE_STARTED_AT=<change_started_at> ./scripts/deploy/ap-completion-check.sh <host>
```

The remote wrapper covers that host's AP services only. The main-host `youtube-producer-c` is redeployed separately on the main host, guarded by its `main-ap` profile:

```bash
sudo -n env \
  COMPOSE_FILE=deploy/compose/docker-compose.prod.yml:deploy/compose/docker-compose.live-compat.yml:deploy/compose/docker-compose.main-ap.yml:deploy/compose/docker-compose.main-ap.live-compat.yml \
  COMPOSE_PROFILES=main-ap \
  COMPOSE_ENV_FILE=/run/hololive-bot/compose.env \
  ./scripts/deploy/compose-redeploy-service.sh youtube-producer-c
```

First boot on a newly provisioned AP host (no AP containers yet) requires `AP_PREFLIGHT_ALLOW_FIRST_BOOT=true` so the Iris H3 trust preflight skips its in-container check once; post-start readiness checks still gate the rollout. The wrapper also copies `deploy/compose/docker-compose.prod.yml` and the host overlay into its prechange backup *before* the rsync step, so pre-seed the repo files onto the host once (manual rsync with `--files-from=scripts/deploy/ap-rsync-files.txt`) before the first `--apply`.

## Tiny VPS host-native AP runtime

Osaka `youtube-producer-a` and Osaka2 `youtube-producer-d` run as host-native
`systemd` services on 1 vCPU / 1GB RAM Oracle VPS hosts dedicated only to
YouTube scraping. The main risk on these hosts is not the steady-state producer
process; it is running Go/Docker builds and retaining Docker build cache on the
AP host.

Use `scripts/deploy/ap-host-native-deploy.sh`,
`scripts/deploy/ap-host-native-rollback.sh`, and
`scripts/logs/ap-host-native-status.sh` for this runtime. Keep live `systemd`
mutation behind explicit operator approval.

Host-native AP invariants:
- Build artifacts are produced on the central/build host, not on the 1GB AP host.
- The AP host receives only the runtime artifact set: `bin/youtube-producer`,
  `bin/healthcheck`, and `internal/domain/data`.
- OpenBao Agent still renders the AP runtime contract under `/run/hololive-bot/`:
  `youtube-producer.env`, `ap-compose.env`, and the cert files listed in this
  runbook. Raw secrets stay out of repository files and command output.
- PostgreSQL remains central over Tailscale with `POSTGRES_HOST=<tailnet-central>`,
  `POSTGRES_PORT=5433`, `POSTGRES_SSLMODE=verify-full`, and
  `POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem`.
- Valkey remains central over Tailscale with `CACHE_HOST=<tailnet-central>` and
  `CACHE_PORT=6379`.
- Each AP has a unique `YOUTUBE_PRODUCER_INSTANCE_ID` and `SERVER_PORT`.
- Tiny VPS scraper APs are scraper-only by default:
  `PHOTO_SYNC_ENABLED=false` and `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false`.
- Start with `SCRAPER_SCHEDULER_WORKER_COUNT=1` on 1GB hosts. Raise to `2` only
  after memory, fetch error, and lease metrics are stable.
- Set a host memory ceiling with both `GOMEMLIMIT` and `systemd` `MemoryMax`;
  use swap for deploy safety, not as normal steady-state capacity.
- Maintain a low-priority `/swapfile` (`vm.swappiness=10`) as deploy/OOM
  headroom on 1GB hosts.
- Rotate `/var/log/hololive-bot/*.log` into `/var/log/hololive-bot/archive/`
  so central log mirroring retains rotated AP logs.

Suggested artifact layout:

```text
/opt/hololive-bot/youtube-producer/
  current/
    bin/youtube-producer
    bin/healthcheck
    internal/domain/data/...
/etc/hololive-bot/youtube-producer-host.env
/run/hololive-bot/youtube-producer.env
/run/hololive-bot/certs/postgres-ca.pem
/run/hololive-bot/certs/hololive-h3.crt
/run/hololive-bot/certs/hololive-h3.key
```

Host override env should contain only instance-local non-secret values:

```text
YOUTUBE_PRODUCER_RUNTIME_ALLOWED=true
YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true
YOUTUBE_PRODUCER_LEASE_NAMESPACE=production
YOUTUBE_PRODUCER_INSTANCE_ID=youtube-producer-d
YOUTUBE_PRODUCER_LOG_FILE_NAME=youtube-producer-d.log
YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false
YOUTUBE_INGESTION_ENABLED=true
SERVER_PORT=30035
HOLOLIVE_HTTP_TRANSPORTS=h3
HOLOLIVE_H3_ADDR=:30035
HOLOLIVE_METRICS_ADDR=100.100.1.x:30095
HOLOLIVE_H3_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt
HOLOLIVE_H3_KEY_FILE=/run/hololive-bot/certs/hololive-h3.key
HOLOLIVE_INTERNAL_H3_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt
HOLOLIVE_INTERNAL_H3_SERVER_NAME=127.0.0.1
HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt
HEALTHCHECK_SERVER_NAME=127.0.0.1
PHOTO_SYNC_ENABLED=false
SCRAPER_FETCHER_ENGINE=nethttp
SCRAPER_SCHEDULER_WORKER_COUNT=1
GOMEMLIMIT=384MiB
GOGC=100
GIN_MODE=release
LOG_DIR=/var/log/hololive-bot
LOG_LEVEL=info
CACHE_SOCKET_PATH=
CACHE_HOST=<tailnet-central>
CACHE_PORT=6379
POSTGRES_SOCKET_PATH=
POSTGRES_HOST=<tailnet-central>
POSTGRES_PORT=5433
POSTGRES_SSLMODE=verify-full
POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem
CLIPROXY_BASE_URL=http://<tailnet-central>:8787/v1
```

Systemd unit shape:

```ini
[Unit]
Description=Hololive youtube-producer AP
After=network-online.target openbao-agent-hololive-bot.service
Wants=network-online.target

[Service]
Type=simple
User=hololive
Group=hololive
WorkingDirectory=/opt/hololive-bot/youtube-producer/current
EnvironmentFile=/run/hololive-bot/youtube-producer.env
EnvironmentFile=/etc/hololive-bot/youtube-producer-host.env
ExecStart=/opt/hololive-bot/youtube-producer/current/bin/youtube-producer
Restart=always
RestartSec=5s
TimeoutStopSec=30s
MemoryMax=768M
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/run/hololive-bot /var/log/hololive-bot /tmp

[Install]
WantedBy=multi-user.target
```

Host-native deploy automation must add these gates before first live use:
- Build or receive the artifact on a non-tiny host for the target architecture.
- Rsync artifacts atomically into a timestamped release directory, then switch
  `current`.
- Validate rendered env key presence without printing values.
- Run `systemd-analyze verify` for the unit.
- Start or restart only the target AP unit.
- Verify `StartedAt >= change_started_at`, `/health`, `/ready`, Valkey
  availability, PostgreSQL TLS, and filtered logs after `change_started_at`.

Host-native verification:

```bash
systemctl is-active hololive-youtube-producer@youtube-producer-d.service
systemctl show hololive-youtube-producer@youtube-producer-d.service \
  -p ActiveState -p SubState -p ExecMainPID -p MemoryCurrent -p NRestarts
HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
  /opt/hololive-bot/youtube-producer/current/bin/healthcheck \
  https://127.0.0.1:30035/health
HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
  /opt/hololive-bot/youtube-producer/current/bin/healthcheck --body \
  https://127.0.0.1:30035/ready
journalctl -u hololive-youtube-producer@youtube-producer-d.service \
  --since "$CHANGE_STARTED_AT" --no-pager |
  grep -E 'PostgreSQL|Valkey|active_active|ERR|panic|permission denied|x509|no such file' || true
```

Host-native rollback is release-symlink based: point `current` back to the
previous artifact directory, restart the same unit, and rerun the verification
above. If the failure is topology-level, stop the new tiny AP first and confirm
the existing APs remain `mode=active-active`, `valkey_available=true`, and
`scraping_paused=false`.

## Common failure modes

### 1. Polling stalls

Symptoms:
- No fresh YouTube polling/outbox activity.
- Health may remain up.

Diagnosis:
```bash
./scripts/logs/ap-status.sh seoul
docker logs --tail 300 hololive-youtube-producer-c
COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.main-ap.yml exec -T youtube-producer-c ./bin/healthcheck https://127.0.0.1:30025/ready
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
# 각 AP 호스트 로컬에서 /ready 확인 (seoul 30015, main 30025)
./scripts/logs/ap-status.sh seoul
COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.main-ap.yml exec -T youtube-producer-c ./bin/healthcheck https://127.0.0.1:30025/ready
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
docker logs --since 30m hololive-youtube-producer-c 2>&1 | grep -i photo | tail -n 80
```

Mitigation:
- Check `PHOTO_SYNC_ENABLED=true`, PostgreSQL, Valkey, and Holodex/API errors.
- Photo sync is owned by `youtube-producer-c` holding the singleton lease; check the lease holder's logs. `youtube-producer-b` never runs PhotoSync.

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
SINCE=15m TAIL=600 PATTERN='active_active_paused|active_active_resumed|valkey' \
  ./scripts/logs/ap-logs.sh seoul youtube-producer | tail -80
```

Mitigation:
- 백그라운드 recovery loop가 `__readiness_probe__`를 사용해 5초 base interval로 재시도하므로, 메인 valkey가 살아나면 자동으로 `active_active_resumed` 로그가 등장하고 `MarkLeaseAvailable()`이 호출됩니다. 일반적으로 사람 개입 없이 회복됩니다.
- 회복이 5분 이상 걸리면 메인 valkey-cache의 listen/auth와 Tailscale ACL, 호스트 방화벽을 먼저 확인. 그래도 막히면 producer-b(seoul)를 재시작.

Rollback:
- 기존 active-active rollback 절차(`./scripts/deploy/ap-rollback.sh <host>`)를 그대로 사용. recovery loop는 readiness 보조 경로이므로 별도 롤백 대상이 아닙니다.

### 5. Outbox backlog grows

Symptoms:
- Producer persists events but downstream delivery is delayed.

Diagnosis:
```bash
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
./scripts/logs/ap-smoke.sh seoul
```

Default smoke checks the host's AP `/ready` payloads for `mode=active-active`, `valkey_available=true`, and `scraping_paused=false`, then verifies `/health` inside each container. External CA/egress smoke is optional because it depends on public network reachability:

```bash
AP_SMOKE_EXTERNAL=true ./scripts/logs/ap-smoke.sh <host>
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `youtube-producer` image/config.
- Scale down `youtube-producer-b` (seoul) first, confirm `youtube-producer-c` (main) remains healthy, then redeploy the previous image/config.
- Confirm `YOUTUBE_INGESTION_ENABLED=true`, active `/ready`, health on port `30015`, and outbox/photo sync state after rollback.
- The deploy wrapper stores overwritten files and prechange container inventory under `backups/<host>-active-active-<timestamp>/` on that AP host; use that evidence to restore the previous `deploy/compose/docker-compose.prod.yml` and that host's compose override if active-active startup fails.
- Topology-level rollback to the pre-2026-06-04 Osaka `a`+`b` layout is manual: stop `youtube-producer-b` on seoul, restore a pre-cutover `deploy/compose/docker-compose.osaka.yml` that defines `youtube-producer-b` — recover it from git history before the compose-directory refactor (`git show 7558024f^:docker-compose.osaka.yml`) or from the 2026-06-04 cutover backup (`backups/osaka-active-active-20260604T102113Z/`); prechange backups taken by deploys *after* the cutover no longer define `youtube-producer-b` — then start it on osaka with an explicit `up -d --no-deps youtube-producer-b`. `ap-rollback.sh osaka` alone restores files but only recreates the services listed in `ap-hosts/osaka.conf` (Osaka AP는 host-native `systemd` 런타임(`AP_RUNTIME_MODE=native`)으로 가동 중이고, compose 경로의 `docker-compose.osaka.yml`은 계약 검증·rollback 대비용으로 보존).
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
