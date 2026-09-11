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

Helper process는 Go가 runtime directory/socket/bootstrap/shutdown을 소유하고 Node는 socket을 unlink하지 않습니다. 기동 시에는 기존 startup budget 안에서 소켓 권한과 listen 준비를 확인한 뒤 bootstrap을 한 번만 전송합니다. 정상 종료는 SIGTERM 후 drain이며 timeout에만 SIGKILL을 사용합니다. `CLEANUP_TIMED_OUT`은 fatal shutdown입니다. `/ready`는 helper health(READY), PostgreSQL queue, scheduler RUNNING, 첫 collection terminal, processed handoff를 증명하며 ongoing freshness는 `youtube_collection_freshness_seconds`가 소유합니다.

Proxy 설정은 helper bootstrap에만 존재하며 collection RPC별 변경은 지원하지 않습니다. Collection request는 `protocol_version`과 `max_success_response_bytes`를 사용하고, success/error schema 및 HTTP status/error tuple을 strict하게 검증합니다. Unknown field, trailing JSON value, removed `proxy_url`/`max_aggregate_bytes`, 또는 불가능한 tuple은 compatibility fallback 없이 protocol mismatch입니다. RPC client disconnect는 해당 request의 upstream fetch만 취소합니다.

Holodex/Official HTTP는 collector-owned `providerhttp` transport입니다. Redirect follow는 없습니다. Holodex는 path prefix, Official은 origin-only입니다. `HOLODEX_TIMEOUT_SECONDS`와 `OFFICIAL_SCHEDULE_TIMEOUT_SECONDS`가 request ceiling이며 0/음수는 기동 실패입니다. 401/403은 `CONFIGURATION`, 429와 Retry-After가 있는 503은 `COOLDOWN`입니다.

Scheduler는 `COMPLETE` output을 `PublishBatch`로 terminal complete하고, `PARTIAL` output은 `PublishBatchAndDefer`로 observation publish와 same-slot defer를 한 PostgreSQL transaction에서 커밋합니다. 성공한 callback은 추가 defer/release를 실행하지 않습니다. Supervisor가 callback 반환과 동시에 cancel/renew 실패를 처리하면 join 전에 release를 시도할 수 있지만, `ACTIVE` 및 owner/fence 조건이 terminal 상태의 재변경을 거부합니다. Release API는 shutdown/renew-fail/superseded reason별 state를 제공하고 durable `last_failure_*`는 유지합니다. mixed-version에서 migration 177 trigger가 채운 `legacy_collector`는 release transaction이 복원합니다.

`youtube_collection_last_success_timestamp_seconds`와 readiness의 첫 성공은 durable terminal commit을 기록하고, `youtube_collection_attempts_total`은 callback과 lease supervision을 포함한 실행 결과를 기록합니다. 따라서 commit 직후 종료·갱신 실패가 겹치면 마지막 성공 시각이 갱신된 실행도 canceled/failed attempt로 집계될 수 있습니다. 이것만으로 terminal commit의 실패나 observation 유실을 판단하지 않습니다. Renew fence loss보다 먼저 buffered callback 결과가 도착한 경우에는 기존 callback 결과 우선 계약을 적용합니다.

Discovery는 due-only입니다. GLOBAL job도 lease due predicate를 통과한 경우에만 candidate가 되며 매 cycle 무조건 enqueue하지 않습니다. Local queue FULL은 성공이 아니라 explicit `EnqueueFull`이며 해당 discovery cycle의 남은 admission을 중단합니다. Scheduler instance는 single-use입니다. Start는 NEW에서만 성공하고 Stop 또는 fatal 이후 STOPPED instance는 재사용하지 않습니다. fatal은 first-wins이며 명시적으로 분류된 INTERNAL/PROTOCOL 오류와 runner panic·result invariant·불가능한 queue 상태가 대상입니다. Ordinary provider failure, timeout, cooldown, parser drift는 fatal이 아닙니다.

Lease-run `CLEANUP_TIMED_OUT`은 cleanup 기한 안에 callback이 합류하지 못했다는 뜻입니다. 종료한 callback의 자체 deadline은 해당 cancel/renew/fence 결과의 원인으로 남으며 join timeout으로 분류하지 않습니다. Lease supervision timeout만으로 process fatal을 보고하지 않는 기존 정책을 유지하지만, 함께 보존된 classified fatal 오류는 보고합니다. 위의 helper process `CLEANUP_TIMED_OUT`과 같은 종료 정책으로 해석하지 않습니다.

## Live metadata contract activation

`live_snapshot` generation `1`은 identity/status/time만 허용하고 generation `2`는 optional `title`, `topic_id`, `thumbnail_url`을 추가합니다. 활성화는 다음 순서를 지킵니다.

1. generation `1`과 `2`를 모두 지원하는 `hololive-api`를 먼저 배포하고 readiness 및 live consumer 처리를 확인합니다.
2. 승인된 internal operation으로 Holodex와 YouTube.js의 `live_snapshot` current generation을 `2`로 전환합니다.
3. 새 collector fleet을 배포하고 각 slot의 readiness, generation `2` observation 발행, canonical metadata 저장을 확인합니다.
4. generation `1` queue가 비고 replay 필요가 없음을 확인할 때까지 API의 generation `1` decoder를 유지합니다.

DB generation 전환은 일반 collector 배포에 포함하지 않으며 별도 운영 승인이 필요합니다. API-first 순서를 지키지 않으면 새 payload가 구 API의 strict decoder에서 거부됩니다.

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | per-host H3 port | yes |
| `METRICS_API_KEY` | H3 internal routes and non-loopback `/metrics` authentication | production yes |
| `HOLOLIVE_OTLP_GRPC_ENDPOINT` | collector trace export endpoint (`host:port`, gRPC) | production yes |
| `OTEL_YOUTUBE_COLLECTOR_<slot>_ENABLED=true` | per-slot trace enablement (`A`/`B`/`C`/`D`) | production yes |
| `STACK_WORKER_PROFILE_FILE` | slot-specific strict `hololive/youtube-collector` profile; `collection.executor.enabled` owns worker enablement | yes |
| `YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true` | must be true only on collector hosts | yes |

`OTEL_EXPORTER_OTLP_ENDPOINT`와 `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`는 URL 문법을
자동 적용하므로 Hololive runtime에서는 지원하지 않습니다. 둘 중 하나가 non-empty이면
`HOLOLIVE_OTLP_GRPC_ENDPOINT` 존재 여부와 무관하게 startup validation이 실패합니다.
| `YOUTUBE_COLLECTOR_INSTANCE_ID` | fleet identity `youtube-collector-a/b/c/d` | yes |
| `PHOTO_SYNC_ENABLED=false` | photo product path stays on hololive-api admin | yes |
| `POSTGRES_USER=hololive_scraper` | lease/observation insert only | yes |
| `POSTGRES_SSLMODE=verify-full` | required client verification | yes |
| `HOLODEX_TIMEOUT_SECONDS` | Holodex request ceiling; must be positive | yes |
| `OFFICIAL_SCHEDULE_TIMEOUT_SECONDS` | Official Schedule request ceiling; must be positive | yes |
| `YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS` | `/ready` total probe budget; default 2s stays below the 5s healthprobe ceiling | yes |
| `YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS` | helper health probe inside the readiness budget; default 1s | yes |
| `YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES` | successful provider response ceiling | yes |
| `YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS` | per-request YouTube.js ceiling; default 30s | yes |

Worker count, local queue capacity, acquisition cadence/batch, lease/renew/cleanup/publish budgets, retry/jitter, and provider in-flight limits are required fields of the `collection` profile. The runtime rejects their retired environment-variable forms instead of translating them.

Collector loader와 Compose는 canonical env만 읽습니다. `YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS`와 `YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`는 폐기되었고, 설정되어 있어도 무시됩니다. Canonical 값이 없으면 documented default(`30`, `1048576`)를 씁니다. 명시적 empty는 startup fail입니다.

## YouTube.js transient recovery

YouTube.js transport는 `https://www.youtube.com/youtubei/v1/{browse,next,player}`의 `POST`만 읽기 전용 재전송 대상으로 봅니다. 알려진 transient network code 또는 HTTP `500`, `502`, `503`, `504`가 발생하면 `100`~`300ms` jitter 뒤 정확히 한 번 재시도하므로 총 시도 수는 최대 2회입니다. 재생할 수 없는 request body, 다른 host/path/method, HTTP `429`와 그 밖의 status, parser/protocol failure에는 transport retry를 적용하지 않습니다. 두 번째 시도 실패는 기존 typed failure와 scheduler defer 계약을 그대로 사용하고 complete-empty나 alternate provider로 바꾸지 않습니다.

각 추가 시도는 `youtubejs_upstream_retry_scheduled` INFO event에 endpoint, trigger, delay, attempt를 기록합니다. 같은 시간대의 `YouTube collection job failed` WARN이 없으면 transport 안에서 복구된 것이며, WARN이 이어지면 bounded retry가 소진된 것입니다. 배포 후 24시간 동안 exhausted `collection_failed` 비율이 감소하지 않거나 `429`, request timeout, upstream request volume이 증가하면 이 정책을 재검토합니다.

## YouTube.js live schedule metadata

Channel RPC의 필수 `kind=live|metadata`가 수집 범위를 지정합니다. live 작업은 streams/player만, metadata 작업은 about과 채널 정보만 조회하므로 일정 접근 제한이 통계·프로필·사진 수집을 중단시키지 않습니다. 변경된 Go binary와 Node helper는 같은 bundle로 교체합니다.

Channel 목록의 `UPCOMING` 행에 기계가독 `scheduled_at`이 없으면 helper가 같은 video ID의 raw `/player`를 순차 조회합니다. 목록 시각이 있으면 상세 조회는 0회이며, 누락된 고유 UPCOMING video ID당 1회, 한 channel collection당 최대 32회입니다. `LIVE`, `ENDED`, `CANCELLED`는 schedule 보강 대상이 아닙니다. 이 횟수는 transport의 transient 재시도 전 논리 요청 수이며, `/player`의 총 transport 시도는 위 정책에 따라 각 요청당 최대 2회입니다.

로컬 adapter는 응답 성공 상태, 요청과 정확히 같은 `videoDetails.videoId`, 존재하는 live/upcoming boolean을 검증합니다. 예정 시각은 RFC3339 `microformat.playerMicroformatRenderer.liveBroadcastDetails.startTimestamp`를 우선 사용하고, 이 값이 없으면 동일 video ID의 `playabilityStatus.liveStreamability.liveStreamabilityRenderer.offlineSlate.liveStreamOfflineSlateRenderer.scheduledStartTime` epoch seconds를 사용합니다. 두 값이 모두 있으면 같은 시각이어야 합니다. 표시 문자열은 사용하지 않습니다. Content 목록의 premiere 분류도 같은 raw adapter를 사용하며 `isUpcoming=true`와 `isLiveContent=false`일 때만 content-owned premiere로 유지합니다.

접근 제한 예외는 `UNPLAYABLE`과 `errorScreen.playerLegacyDesktopYpcOfferRenderer`, 정확한 video ID, `isUpcoming=true`, `isLiveContent=true`가 확인되며 두 예정 시각이 모두 없는 경우에만 적용합니다. 해당 행은 `unavailable_live_sessions`의 ID·채널·`access_restricted` 사유로 분리하고 helper가 `youtubejs_live_schedule_unavailable` WARN에 공개 식별자와 사유를 기록합니다. 번역된 가입 안내문으로 분류하지 않습니다. 멤버십 영상에도 기계가독 시각이 있으면 정상 수집합니다.

제한 목록은 중복·정상 sessions와의 중첩·다른 채널·미지 사유를 거부하며 최대 32개입니다. Go adapter는 유효 sessions만 `PARTIAL` live observation으로 발행하고 해당 poll을 완료합니다. 제한 행만 남은 빈 sessions도 PARTIAL입니다. 다음 기존 poll에서 다시 관측하며 추가 재시도·별도 provider·과거 시각 재사용을 하지 않습니다. 제한 영상의 canonical 상태와 마지막 확인 시각은 갱신하지 않습니다. PARTIAL의 부재는 종료·취소 근거가 아니며 개별 영상의 명시적 종료는 기존 consumer 규칙으로 처리합니다.

`youtube_collection_completeness_total{provider="youtubejs",kind="live_snapshot",completeness="PARTIAL"}`와 위 WARN을 함께 확인합니다. poll 성공·readiness·freshness는 모든 영상의 일정 확보를 뜻하지 않습니다. helper WARN은 원천 관측 기록이며 실제 발행 여부는 observation publish 결과로 확인합니다. Collector 팀이 이 예외를 소유하며 renderer 변경 또는 다른 접근 제한의 독립 재현 근거가 생기면 `DEC-20260911-youtube-restricted-schedule-isolation`에 따라 범위를 재검토합니다.

32개 후보 초과, identity/schema/time drift, 미지의 UNPLAYABLE 또는 위 접근 제한에 해당하지 않는 시각 부재는 terminal `parser_drift`입니다. 해당 collection은 observation과 checkpoint를 저장하지 않습니다. `youtube_collection_attempts_total`과 bounded `YouTube collection job failed` 로그로 판정합니다. 목록과 player 사이에 `LIVE`가 확인되거나 처음부터 `LIVE`로 발견된 방송은 예정 시각을 만들지 않고 정상 live catch-up 경로를 유지합니다.

`youtubei.js@18.0.0`은 session, request context, browse/transport와 범용 parser 기반층으로 고정합니다. Upgrade 전 upstream release note와 로컬 사용 surface를 확인하고 `src/live-metadata.test.mjs`, 전체 helper test, typecheck를 실행합니다. raw field 변화가 있으면 sanitized fixture와 로컬 adapter만 함께 갱신합니다. 전체 fork나 vendoring은 `DEC-20260911-youtube-restricted-schedule-isolation`의 review trigger가 충족될 때만 다시 결정합니다.

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f youtube-collector
```

## Common failure modes

### 1. Metrics authentication or tracing config rejection

Symptoms:
- startup rejects a missing `METRICS_API_KEY`
- startup names a missing `OTEL_YOUTUBE_COLLECTOR_<slot>_ENABLED=true`

Diagnosis:
- `youtube-collector.env`의 key 이름만 확인하고 raw value를 출력하지 않습니다.
- `METRICS_API_KEY`와 canonical collector OTEL toggle을 stack-secrets master에서 수정한 뒤 승인된 sync 절차를 사용합니다.

Mitigation:
- static secret sync와 collector restart/deploy를 같은 승인된 변경으로 수행하고 `/metrics` target 및 Jaeger `youtube-collector` service를 재검증합니다.

### 2. Projection/lease load failure

Symptoms:
- collector logs contain `phase=candidate_load` or lease acquire failures
- no new `source_observations` / `source_observation_queue` rows

Diagnosis:
- SSOT manifest의 migration `144`–`174` 적용 여부와 collection projection/target/lease 상태를 확인합니다.
- `POSTGRES_USER=hololive_scraper` grant를 확인합니다.

Mitigation:
- fail-closed가 맞습니다. projection이나 lease 검증을 우회하지 않습니다.

### 3. Outbox insert permission denied

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
