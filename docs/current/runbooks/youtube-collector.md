# Runbook: youtube-collector

## Role

`youtube-collector`는 AP fleet(`a`/`b`/`c`/`d`)의 외부 수집 런타임입니다. Holodex / Official / YouTube.js fetch, collection lease, checkpoint, observation Publish만 소유합니다. Canonical persist와 notification intent는 `hololive-api` YouTube plane이 담당합니다. `members.photo`는 hololive-api admin PhotoSync가 담당합니다.

## Deploy completion contract

| Host | Service | Runtime | Port | Ready | Env | DB role |
|---|---|---|---:|---|---|---|
| Osaka `a` | `youtube-collector-a` | host-native `hololive-youtube-collector@youtube-collector-a.service` | 30005 | `https://127.0.0.1:30005/ready` | `/etc/stack-secrets/hololive-bot/youtube-collector.env` + `youtube-collector-host.env` | `hololive_scraper` |
| Seoul `b` | `youtube-collector-b` | Compose | 30015 | `https://127.0.0.1:30015/ready` | `HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE` | `hololive_scraper` |
| Central `c` | `youtube-collector` | Compose | 30025 | `https://127.0.0.1:30025/ready` | `HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE` | `hololive_scraper` |
| Osaka2 `d` | `youtube-collector-d` | host-native `hololive-youtube-collector@youtube-collector-d.service` | 30035 | `https://127.0.0.1:30035/ready` | `/etc/stack-secrets/hololive-bot/youtube-collector.env` + `youtube-collector-host.env` | `hololive_scraper` |

Completion is `scripts/deploy/ap-completion-check.sh <host>` for APs and compose `/ready` for central `c`.

## Normal status

| Check | Expected |
|---|---|
| Health | `https://127.0.0.1:<port>/health` returns success over H3 |
| Ready | `https://127.0.0.1:<port>/ready` returns `status=ready`, `instance_id`, helper/DB/scheduler, `first_success=true`, and processed handoff. It does not prove provider freshness. `due_jobs` is a bounded cycle count (`due_jobs_exact=false`). |
| Logs | startup markers include PostgreSQL connection success |
| Observation store | enabled projection target과 collection lease가 유효하면 `source_observations` / `source_observation_queue` insert |
| Canonical tables | collector 프로세스가 canonical/notification tables를 쓰지 않음 |
| Metrics | `:30096` on the host metrics bind |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes; `verify-full` TLS with postgres CA | Publish fails |
| Cache topology | no | collector startup and `/ready` do not require a cache service |
| `hololive-api` YouTube plane | consumer persist에 필요 | collector만 기동하면 observation이 PENDING으로 남음 |
| Iris | no | final proactive egress is owned by `alarm-worker` |
| YouTube.js helper | yes; unique `0700` runtime dir + private UDS | `/ready` stays `not_ready` (`dependency=youtubejs`) |

Helper process는 Go가 runtime directory/socket/bootstrap/shutdown을 소유하고 Node는 socket을 unlink하지 않습니다. 정상 종료는 SIGTERM 후 drain이며 timeout에만 SIGKILL을 사용합니다. `CLEANUP_TIMED_OUT`은 fatal shutdown입니다. `/ready`는 helper health(READY), PostgreSQL queue, scheduler RUNNING, 첫 collection terminal, processed handoff를 증명하며 ongoing freshness는 `youtube_collection_freshness_seconds`가 소유합니다.

Proxy 설정은 helper bootstrap에만 존재하며 collection RPC별 변경은 지원하지 않습니다. Collection request는 `protocol_version`과 `max_success_response_bytes`를 사용하고, success/error schema 및 HTTP status/error tuple을 strict하게 검증합니다. Unknown field, trailing JSON value, removed `proxy_url`/`max_aggregate_bytes`, 또는 불가능한 tuple은 compatibility fallback 없이 protocol mismatch입니다. RPC client disconnect는 해당 request의 upstream fetch만 취소합니다.

Holodex/Official HTTP는 collector-owned `providerhttp` transport입니다. Redirect follow는 없습니다. Holodex는 path prefix, Official은 origin-only입니다. `HOLODEX_TIMEOUT_SECONDS`와 `OFFICIAL_SCHEDULE_TIMEOUT_SECONDS`가 request ceiling이며 0/음수는 기동 실패입니다. 401/403은 `CONFIGURATION`, 429와 Retry-After가 있는 503은 `COOLDOWN`입니다.

Scheduler는 `COMPLETE` output을 `PublishBatch`로 terminal complete하고, `PARTIAL` output은 `PublishBatchAndDefer`로 observation publish와 same-slot defer를 한 PostgreSQL transaction에서 커밋합니다. 성공한 terminal commit 뒤에는 두 번째 defer/release를 실행하지 않습니다. Release API는 shutdown/renew-fail/superseded reason별 state를 제공하고 durable `last_failure_*`는 유지합니다. mixed-version에서 migration 177 trigger가 채운 `legacy_collector`는 release transaction이 복원합니다.

Discovery는 due-only입니다. GLOBAL job도 lease due predicate를 통과한 경우에만 candidate가 되며 매 cycle 무조건 enqueue하지 않습니다. Local queue FULL은 성공이 아니라 explicit `EnqueueFull`이며 해당 discovery cycle의 남은 admission을 중단합니다. Scheduler instance는 single-use입니다. Start는 NEW에서만 성공하고 Stop 또는 fatal 이후 STOPPED instance는 재사용하지 않습니다. fatal은 first-wins이며 runner panic, result invariant, cleanup timeout, impossible queue/lease state만 process fatal입니다. Ordinary provider failure, timeout, cooldown, parser drift는 fatal이 아닙니다.

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | per-host H3 port | yes |
| `YOUTUBE_INGESTION_ENABLED=true` | collector enablement | yes |
| `YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true` | must be true only on collector hosts | yes |
| `YOUTUBE_COLLECTOR_INSTANCE_ID` | fleet identity `youtube-collector-a/b/c/d` | yes |
| `PHOTO_SYNC_ENABLED=false` | photo product path stays on hololive-api admin | yes |
| `POSTGRES_USER=hololive_scraper` | lease/observation insert only | yes |
| `POSTGRES_SSLMODE=verify-full` | required client verification | yes |
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false` | egress boundary | yes |
| `HOLODEX_TIMEOUT_SECONDS` | Holodex request ceiling; must be positive | yes |
| `OFFICIAL_SCHEDULE_TIMEOUT_SECONDS` | Official Schedule request ceiling; must be positive | yes |
| `YOUTUBE_COLLECTOR_PUBLISH_TIMEOUT_SECONDS` | atomic terminal publish timeout | yes |
| `YOUTUBE_COLLECTOR_DB_TIMEOUT_SECONDS` | lease/control-plane DB timeout | yes |
| `YOUTUBE_COLLECTOR_RENEW_TIMEOUT_SECONDS` | lease renewal timeout | yes |
| `YOUTUBE_COLLECTOR_CLEANUP_TIMEOUT_SECONDS` | canceled runner join and release bound | yes |
| `YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS` | `/ready` total probe budget; default 2s stays below the 5s healthprobe ceiling | yes |
| `YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS` | helper health probe inside the readiness budget; default 1s | yes |
| `YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES` | successful provider response ceiling | yes |
| `YOUTUBE_COLLECTOR_COLLECTION_OVERHEAD_SECONDS` | collection post-fetch overhead; default 5s | yes |
| `YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS` | per-request YouTube.js ceiling; default 30s | yes |

다음 old env는 한 compatibility release 동안만 canonical env의 alias입니다. Canonical host env와 `.env.example`은 new 이름만 사용합니다. Compose는 mixed-version fleet을 위해 각 old/new pair를 함께 render하되, 입력의 unset과 explicit empty를 구분합니다.

| Canonical env | Compatibility alias |
|---|---|
| `YOUTUBE_COLLECTOR_COLLECTION_OVERHEAD_SECONDS` | `YOUTUBE_COLLECTOR_NORMALIZATION_BUDGET_SECONDS` |
| `YOUTUBE_COLLECTOR_PUBLISH_TIMEOUT_SECONDS` | `YOUTUBE_COLLECTOR_PUBLISH_BUDGET_SECONDS` |
| `YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS` | `YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS` |
| `YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES` | `YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES` |

- new only → new 값
- old only → old 값을 new field로 이관
- both equal → 허용
- both differ → startup fail
- either explicitly empty → startup fail; Compose render도 empty를 default로 치환하지 않음
- neither → 각 canonical documented default (`5`, `5`, `30`, `1048576`)

제거 조건: collector fleet `a`/`b`/`c`/`d`가 네 canonical env를 이해하는 revision으로 안정화된 뒤, 별도 cleanup PR에서 Compose old env, loader alias, HC-013을 함께 제거합니다. 이 release에서 alias를 삭제하지 않습니다.

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f youtube-collector
```

## Common failure modes

### 1. Projection/lease load failure

Symptoms:
- collector logs contain `phase=candidate_load` or lease acquire failures
- no new `source_observations` / `source_observation_queue` rows

Diagnosis:
- SSOT manifest의 migration `144`–`174` 적용 여부와 collection projection/target/lease 상태를 확인합니다.
- `POSTGRES_USER=hololive_scraper` grant를 확인합니다.

Mitigation:
- fail-closed가 맞습니다. projection이나 lease 검증을 우회하지 않습니다.

### 2. Outbox insert permission denied

Symptoms:
- Publish errors
- collector uses `hololive_runtime` instead of `hololive_scraper`

Diagnosis:
- rendered `POSTGRES_USER` and `HOLOLIVE_SCRAPER_PASSWORD`를 확인합니다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30025/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30025/ready
```

## Rollback

Config and topology rollback is an exact repository revision. Restore the following artifacts together. Binary-only rollback is forbidden. Schema/data rollback is none. Mixed-version boundaries and the collector cache-topology unit are in [`rollback.md`](rollback.md#runtime-rollback). Production canary was not executed.

```text
collector Go binary/image
bundled Node helper/package-lock
Compose base and AP overlays
host-native env generator/wrapper
service and runbook contract
```

- 이전 `hololive-youtube-collector:rollback-<UTC timestamp>` tag가 있으면 [`rollback.md`](rollback.md#runtime-rollback)의 revision 확인·`prod` 재승격 절차를 사용한 뒤 collector만 무빌드 재생성합니다. Compose overlay와 host-native generator는 같은 revision tree를 써야 합니다.

```bash
export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
sudo -n env COMPOSE_ENV_FILE="$COMPOSE_ENV_FILE" ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-build --no-deps --force-recreate youtube-collector
```

- 승인된 stack-secrets 변경 절차로 중앙 host의 `compose.env`에 `HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR=1`을 설정하면 tracked disable overlay가 replicas를 0으로 유지합니다.
