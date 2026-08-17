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

## Atomic publish

`PublishBatch`는 `COMPLETE` terminal을 유지합니다. Scheduler는 `PARTIAL` output에 `PublishBatchAndDefer`를 사용하여 observation/checkpoint/queue와 `DEFERRED` 및 typed `last_failure_*`를 같은 PostgreSQL transaction에서 기록합니다. 성공한 `COMPLETE`/`PARTIAL` terminal commit 뒤에는 별도 defer를 수행하지 않습니다. collision complete는 `observation_collision/DATA_CONTRACT` durable diagnostic을 남기고, 성공 complete는 `last_error_code`만 지웁니다. Release는 `shutdown_release`/`renew_failed_release`/`superseded_release` shape이며 `last_failure_*`는 보존합니다. migration 177 trigger가 DEFERRED release를 `legacy_collector`로 덮으면 같은 release transaction이 잠근 값을 복원합니다.

Runner input의 `TargetSnapshot`은 canonical job contract가 요청한 kind를 한 번에 읽는 immutable view입니다. 요청 kind가 누락되면 fail-closed로 오류를 반환하며, 최종 authority는 계속 publish transaction의 lease fence/current projection/enabled target 검증입니다. Snapshot은 fallback이나 publish 검증 대체 경로가 아닙니다.

Discovery는 due-only입니다. GLOBAL job도 lease due predicate를 통과한 경우에만 candidate가 되며 매 cycle 무조건 enqueue하지 않습니다. Local queue FULL은 성공이 아니라 explicit `EnqueueFull`이며 해당 discovery cycle의 남은 admission을 중단합니다. Scheduler instance는 single-use입니다. Start는 NEW에서만 성공하고 Stop 또는 fatal 이후 STOPPED instance는 재사용하지 않습니다. fatal은 first-wins이며 runner panic, result invariant, cleanup timeout, impossible queue/lease state만 process fatal입니다. Ordinary provider failure, timeout, cooldown, parser drift는 fatal이 아닙니다.

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Source observations | PostgreSQL | `source_observations` / `source_observation_queue` | `hololive-api` YouTube plane |
| Collector health/ready | H3 | `/health`, `/ready` | Compose/systemd healthcheck |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | lease/checkpoint/observation insert over `verify-full` TLS | collection handoff fails |

## Must not own

- Canonical community/content/live/stats/profile/photo tables
- Notification outbox / observation claim/finalize
- Proactive notification egress owned by `alarm-worker`

## Startup requirements

- PostgreSQL availability
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

YouTube.js helper는 `RuntimeBaseDir` 아래 unique `0700` directory의 private UDS만 사용하고, socket unlink와 directory cleanup은 Go가 단독 소유합니다. 정상 종료는 SIGTERM 뒤 drain을 기다리며 configured timeout에만 SIGKILL을 쓰고, `Close`가 `CLEANUP_TIMED_OUT`이면 runtime은 fatal shutdown입니다. `/ready`는 helper `Healthy`(READY), PostgreSQL queue probe, scheduler RUNNING, 첫 collection terminal commit, processed observation handoff를 증명합니다. provider freshness stale이나 local queue full은 `/ready` 503 조건이 아닙니다.

Canonical success-response ceiling env는 `YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES`입니다. `YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`는 한 compatibility release alias이며, collector fleet `a`/`b`/`c`/`d`가 new env revision으로 안정화된 뒤 cleanup PR에서 Compose old env, loader alias, HC-013을 함께 제거합니다. 둘 다 없으면 documented default이고, 값이 다르면 startup이 실패합니다.

Helper proxy는 `/v1/bootstrap`에서만 설정되고 bootstrap당 `ProxyAgent` 하나를 공유합니다. Collection RPC는 `protocol_version`과 `max_success_response_bytes`를 전달하며 `proxy_url`이나 `max_aggregate_bytes`를 받지 않습니다. Success와 error envelope는 분리되고 unknown field, trailing JSON value, HTTP status/error tuple mismatch는 protocol mismatch로 fail-closed됩니다. Go request cancellation이나 client disconnect는 해당 RPC의 `AbortSignal`에만 전파됩니다.

## Provider HTTP

Holodex와 Official Schedule fetch는 collector-owned `providerhttp` transport만 사용합니다. Redirect는 follow하지 않습니다. Holodex base URL은 clean path prefix(`/api/v2`)를 허용하고 Official Schedule은 origin-only입니다. Request ceiling은 `HOLODEX_TIMEOUT_SECONDS`와 `OFFICIAL_SCHEDULE_TIMEOUT_SECONDS`이며 0/음수는 startup에서 거절합니다. HTTP 401/403은 `configuration_error/CONFIGURATION`이고, 429와 Retry-After가 있는 503은 `cooldown/COOLDOWN`입니다.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f youtube-collector`
- Health: `https://127.0.0.1:30025/health`
- Ready: `https://127.0.0.1:30025/ready`
- Metrics: live-compat publishes `:30096` on `HOLOLIVE_METRICS_PORT_BIND_IP`

## Related docs

- `../runbooks/youtube-collector.md`
