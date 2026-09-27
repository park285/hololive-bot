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

HTTP trace는 `service.name=youtube-collector`를 유지하고
`youtube.collector.instance_id` span 속성에 검증된 `YOUTUBE_COLLECTOR_INSTANCE_ID`를 기록합니다.
Grafana Traces의 AP별 수신 패널과 `observability_trace_instance_*` 지표는 이 속성으로 Jaeger를 검색합니다.
조회 실패와 성공했지만 trace가 없는 상태는 구분하며, 다른 AP의 trace로 누락 AP를 대체하지 않습니다.

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

Helper bootstrap은 `protocol_version`과 `limits`만 받고 upstream proxy 설정은 없습니다(`proxy` 필드는 unknown field로 거절). Collection request는 `protocol_version`과 `max_success_response_bytes`를 사용하고, success/error schema 및 HTTP status/error tuple을 strict하게 검증합니다. Unknown field, trailing JSON value, removed `proxy_url`/`max_aggregate_bytes`, 또는 불가능한 tuple은 compatibility fallback 없이 protocol mismatch입니다. RPC client disconnect는 해당 request의 upstream fetch만 취소합니다.

Holodex/Official HTTP는 collector-owned `providerhttp` transport입니다. Redirect follow는 없습니다. Holodex는 path prefix, Official은 origin-only입니다. `HOLODEX_TIMEOUT_SECONDS`와 `OFFICIAL_SCHEDULE_TIMEOUT_SECONDS`가 request ceiling이며 0/음수는 기동 실패입니다. 401/403은 `CONFIGURATION`, 429와 Retry-After가 있는 503은 `COOLDOWN`입니다.

Scheduler는 `COMPLETE` output을 `PublishBatch`로 terminal complete하고, `PARTIAL` output은 `PublishBatchAndDefer`로 observation publish와 same-slot defer를 한 PostgreSQL transaction에서 커밋합니다. 성공한 callback은 추가 defer/release를 실행하지 않습니다. Supervisor가 callback 반환과 동시에 cancel/renew 실패를 처리하면 join 전에 release를 시도할 수 있지만, `ACTIVE` 및 owner/fence 조건이 terminal 상태의 재변경을 거부합니다. Release API는 shutdown/renew-fail/superseded reason별 state를 제공하고 durable `last_failure_*`는 유지합니다. migration 177/189의 `legacy_collector` backfill trigger는 migration 218이 지웠으므로(2026-09-26, 트리거 이전 collector rollback 이미지 없음 확인) release transaction은 복원 단계 없이 `last_failure_*`를 건드리지 않습니다. 218은 이 collector를 배포하기 전에 중앙 `db-migrate`로 먼저 적용해야 합니다.

`youtube_collection_last_success_timestamp_seconds`와 readiness의 첫 성공은 durable terminal commit을 기록하고, `youtube_collection_attempts_total`은 callback과 lease supervision을 포함한 실행 결과를 기록합니다. 따라서 commit 직후 종료·갱신 실패가 겹치면 마지막 성공 시각이 갱신된 실행도 canceled/failed attempt로 집계될 수 있습니다. 이것만으로 terminal commit의 실패나 observation 유실을 판단하지 않습니다. Renew fence loss보다 먼저 buffered callback 결과가 도착한 경우에는 기존 callback 결과 우선 계약을 적용합니다.

Discovery는 due-only입니다. GLOBAL job도 lease due predicate를 통과한 경우에만 candidate가 되며 매 cycle 무조건 enqueue하지 않습니다. Local queue FULL은 성공이 아니라 explicit `EnqueueFull`이며 해당 discovery cycle의 남은 admission을 중단합니다. Scheduler instance는 single-use입니다. Start는 NEW에서만 성공하고 Stop 또는 fatal 이후 STOPPED instance는 재사용하지 않습니다. fatal은 first-wins이며 명시적으로 분류된 INTERNAL/PROTOCOL 오류와 runner panic·result invariant·불가능한 queue 상태가 대상입니다. Ordinary provider failure, timeout, cooldown, parser drift는 fatal이 아닙니다. 호출 코드가 durable 계약 밖 code/class tuple을 만들면 code 기본 class로 수리하지 않고 미분류 `collection_internal_invariant`/`INTERNAL`로 지연 처리하며, 원래 tuple은 진단 detail에 남고 `youtube_collection_invalid_failure_tuple_total{provider,kind}`가 위반을 셉니다(stack-audit 2026-09-26 T11). 이 counter가 0이 아니면 collector 코드 결함입니다.

Lease-run `CLEANUP_TIMED_OUT`은 cleanup 기한 안에 callback이 합류하지 못했다는 뜻입니다. 종료한 callback의 자체 deadline은 해당 cancel/renew/fence 결과의 원인으로 남으며 join timeout으로 분류하지 않습니다. Lease supervision timeout만으로 process fatal을 보고하지 않는 기존 정책을 유지하지만, 함께 보존된 classified fatal 오류는 보고합니다. 위의 helper process `CLEANUP_TIMED_OUT`과 같은 종료 정책으로 해석하지 않습니다.

## Live metadata contract

`live_snapshot`은 contract generation `2`만 지원합니다. generation `2`는 identity/status/time에 optional `title`, `topic_id`, `thumbnail_url`을 더합니다. generation `1`(identity/status/time만)에서 `2`로의 활성화는 API 선배포, 승인된 internal operation으로 Holodex·YouTube.js current generation `2` 전환, collector fleet 배포 순서로 끝났습니다. 마지막 단계의 제거 조건(generation `1` queue가 비고 replay 필요가 없음)은 2026-09-26 T18에서 current generation `2`, 미처리 generation `1` 관측 0건으로 확인했고, API의 generation `1` decoder·supported contract 항목과 collector의 generation `1` payload 경로를 지웠습니다(stack-audit 2026-09-26 T11 C6).

- API는 generation `1` 관측을 unsupported contract로 거부합니다.
- collector는 DB current generation이 `2`가 아니면 `configuration_error/CONFIGURATION`으로 수집을 끝내고 다른 형식을 내보내지 않습니다.
- migration `225_live_snapshot_contract_generation_two.sql`은 빈 DB bootstrap과 dbtest의 시드(migration 144의 generation `1`)를 `2`로 맞춥니다. 운영 DB는 이미 `2`라 갱신 대상이 없습니다.
- 새 generation을 도입할 때는 다시 API-first(API가 두 generation을 모두 지원) → DB generation 전환(별도 운영 승인) → collector 배포 순서를 지킵니다.

## Live absence evidence activation

`DEC-20260926-hololive-live-absence-evidence`의 로컬 준비 산출물은 migration 218(새 kind·가용성/채널 확인 저장소), 219(구 LIVE coverage 부분 인덱스 제거), 220(표준 Count 0 문구)입니다. 아직 운영 적용·배포·fleet 활성화 승인이 아닙니다.

1. 승인된 릴리스 계획에서 기존 API reader와 collector의 drain/교체 창을 정합니다. 218의 저장소와 두 새 kind decoder/claim/target을 갖춘 API를 collector보다 먼저 준비합니다. `live_snapshot` 기존 generation은 변경하지 않고 새 kind만 generation 1로 시작합니다.
2. 219는 구 LiveQuery가 더 이상 211 인덱스를 쓰지 않는 전환 시점에 적용합니다. 전체 manifest를 한 번에 적용하려면 구 API reader를 중단한 승인된 교체 창에서 migration → 새 API 순서를 지킵니다. 실행 중 구 API에서 인덱스를 먼저 지워 1초 예산을 훼손하지 않습니다. 212의 scheduled_for 인덱스는 유지합니다.
3. API의 두 kind consume, 5초 target refresh, stale LIVE 영상 target과 freshness clock을 확인한 뒤 새 Go binary와 helper를 같은 bundle로 collector fleet에 반영합니다. `channel_live_check` target이 없는 상태에서 새 channel job을 먼저 활성화하면 exact-subject acquisition이 실패합니다.
4. 새 evidence retention 기본값은 각 7일입니다. `YOUTUBE_PLANE_RETENTION_CHANNEL_LIVE_CHECK_DAYS`, `YOUTUBE_PLANE_RETENTION_VIDEO_LIVE_CHECK_DAYS`는 API 설정이며 기존 production positive-age/승인 검증을 따릅니다. 74채널·2분이면 채널 확인은 하루 53,280건입니다. 운영 storage/WAL·재수집 부하와 generation 전환으로 폐기되는 in-flight를 비교합니다.

`DEC-20260927-live-check-slot-isolation`의 분리 job 계약은 공유-job collector와 혼합 실행하지 않습니다. 기존 공유-job collector를 먼저 drain/중지하고 API의 snapshot-only `youtubejs_channel_live` 및 check-only `youtubejs_channel_live_check` 계약을 반영한 뒤 새 collector를 시작합니다. 미배포 로컬 준비물에는 추가 schema migration이 필요하지 않습니다. 이미 공유-job 버전을 사용한 환경이라면 마지막 채널 확인 슬롯과 겹치는 최초 독립 슬롯의 duplicate/collision 가능성을 전환 계획에서 확인하고, 다음 독립 cadence의 새 관측까지 coverage를 검증합니다. 기존 관측·lease·pending을 임의 삭제하지 않습니다. 롤백도 양쪽 collector를 동시에 발행시키지 않도록 같은 drain 경계를 지킵니다.

롤백은 새 collector의 발행을 먼저 멈추고 새 kind 큐·재처리 요구를 확인합니다. 새 decoder가 필요한 backlog가 있으면 해당 API decoder를 유지하며, 새 canonical/evidence/pending을 삭제하지 않습니다. 219 적용 뒤 구 API로 돌아가려면 구 query의 인덱스 재준비까지 승인된 롤백 절차에 포함해야 합니다. 220은 사용자 지정 본문과 채널 override, 멤버 지정 안내를 건드리지 않습니다.

운영 비교 관측은 별도 승인 뒤 수행합니다: 멤버 한정 LIVE와 음성 /live의 공존, 동시 방송·최초공개 표시, 로봇/비공개/삭제 UNKNOWN 수와 stale LIVE 차단, 유효 endTimestamp와 canonical ended_at 일치, 가용성 만료·재확인 실패 뒤 차단 복원, target 생성/제거 지연과 1초 query 예산. `isPrivate=true`를 포함하는 익명 응답과 실제 로봇 응답은 로컬 실측을 완료했다고 간주하지 않습니다.


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
`HOLOLIVE_OTLP_GRPC_ENDPOINT` 존재 여부와 무관하게 startup validation이 실패합니다. 이 거부는 퇴역 가드가 아니라
외부 표준 이름을 막는 영구 계약이라 제거 조건이 없습니다(`DEC-20260926-hololive-legacy-env-config-retirement`).
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

### 수집 처리량과 대상 신선도

`iris_stack_worker_in_flight / iris_stack_worker_configured_workers`는 작업 슬롯 점유율입니다. CPU 사용률이 아니며, provider admission과 YouTube.js 호출 제한 대기도 포함합니다. AP별 `YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS`는 **helper RPC 전체가 공유하는 간격**입니다. 기본 2초이면 AP 한 대의 명목 상한은 시간당 1,800 RPC입니다. 워커 수만 늘려 이 상한을 늘릴 수 없습니다. 한 RPC가 외부 HTTP 요청 여러 번을 수행할 수 있으므로 외부 API 호출량과 동일시하지 않습니다.

- `youtubejs_rpc_phase_duration_seconds{operation,phase,outcome}`: `rate_limit` 대기와 `helper` 수행·응답 해석을 분리한 histogram. helper 내부의 외부 응답·파싱 시간은 합산입니다. operation은 community/content/channel/unknown, outcome은 success/timeout/canceled/error입니다.
- `youtubejs_rpc_phase_in_flight{operation,phase}`와 `youtubejs_rpc_request_interval_seconds`: 현재 기다리는 호출, 수행 중인 호출과 설정된 호출 간격입니다. helper phase count는 제한을 통과한 RPC 시도 수입니다.
- `youtube_collection_duration_seconds`: 15/30/60/120/300초 버킷까지 포함합니다. 기존 10초 상한을 넘는 content job의 p95를 10초로 해석하지 않습니다. 롤링 배포 중에는 AP별 histogram을 확인하고 모든 AP가 같은 버킷으로 전환되기 전 fleet 합산 quantile을 해석하지 않습니다.
- API가 노출하는 `hololive_youtube_collection_*`: 현재 유효한 projection의 enabled target을 community/content/channel-live/channel-live-check/channel-metadata/video-live 여섯 작업으로 묶어 집계합니다. 채널 확인과 방송 탭은 각자의 완료·due 시각을 사용합니다. `targets`, `stale_targets`(완료 시각이 poll interval보다 오래됨), `never_completed_targets`, `due_targets`, `oldest_completion_age_seconds`, `oldest_due_age_seconds`, `required_rpc_rate`를 함께 확인합니다. 미완료를 완료 경과 0초로 해석하지 않습니다. due에는 AP 로컬 큐에 들어오지 않은 대상과 만료 lease도 포함됩니다. 없던 lease의 due 기준은 현재 target의 created_at입니다. 퇴역 viewer 작업의 과거 lease·지표는 현재 수요에 포함하지 않습니다.
- `hololive_youtube_collection_live_states{state}`와 `live_state_review_targets{reason}`는 현재 유효한 `live_snapshot` 채널 대상에 속한 서로 다른 영상 중 head 또는 서비스 상태가 LIVE/UPCOMING인 집합을 진단합니다. 상태는 head 기준이며 missing/그 밖의 상태는 other입니다. 양방향 상태 불일치를 포함하고, 채널 식별자가 없는 head-only 항목과 양쪽 모두 종료된 이력은 제외합니다. state_mismatch, scheduled_before_now, scheduled_overdue_7d는 겹칠 수 있으며 종료 증거가 아닙니다.

API 집계는 기존 claim 관측 경로에서 최대 30초마다, DB admission을 포함해 1초 예산으로 실행합니다. 실패·유효 projection 부재는 `hololive_youtube_collection_snapshot_success=0`이며 이전 숫자와 마지막 성공 시각을 보존합니다. 성공 지표가 1이고 마지막 성공이 120초 이내일 때만 대상 숫자를 현재값으로 사용합니다. 기존 `youtube_collection_freshness_seconds`는 해당 provider/kind 중 마지막 성공 하나의 경과이며 전체 대상의 신선도를 보장하지 않습니다.

Bot Drilldown의 수집 처리량 섹션과 `HololiveCollectionSnapshotUnavailable`, `HololiveCollectionTargetsStale`, `HololiveCollectionCallBudgetPressure`, `HololiveCollectionLiveStateMismatch`를 확인합니다. 명목 수요가 가동 AP 상한의 85%를 10분 넘게 사용하면 대상·주기·장애 시 여유를 검토합니다. 이 경계값은 초기 운영 기준이며 수집 정책을 자동 변경하지 않습니다. 관측 배포는 API와 AP 계측을 먼저 검증하고 Grafana 생성물·경보를 반영합니다.

Collector loader와 Compose는 canonical env만 읽습니다. 폐기된 `YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS`·`YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`와 퇴역한 `SCRAPER_PROXY_ENABLED`·`SCRAPER_PROXY_URL`은 빈 값이어도 키가 있으면 기동 실패입니다(존재 기준 퇴역 가드, `collector/retired_env.go`, remove_after 2026-12-31). 이 가드가 든 release는 모든 youtube-collector env와 stack-secrets master 사본에서 네 키를 지운 뒤에만 배포합니다. Canonical 값이 없으면 documented default(`30`, `1048576`)를 씁니다. 명시적 empty는 startup fail입니다.

### Viewer 수집 중단 반영과 검증

새 생산은 YouTube.js와 Holodex 양쪽에서 중단하되, 기존 viewer 관측의 소비·재처리는 유지합니다. 적용 순서는 검증된 Go binary와 Node helper를 같은 bundle로 모든 AP에 반영 → 신규 viewer 발행 중단 및 기존 큐 처리 확인 → migration 210과 API의 네 작업 projection/지표 반영 → Grafana dashboard·경보 반영입니다. API projection을 먼저 바꾸면 구형 collector의 viewer가 섞인 Holodex batch 전체가 target 검증에서 거절될 수 있습니다. 롤백은 API의 기존 viewer 대상 복원과 projection 확인을 먼저 하고 구형 AP bundle을 복원합니다.

`YouTubeCollectorFreshnessStale/Unavailable`에서 퇴역 작업을 제외하므로 보존된 viewer 성공 지표가 장애 경보로 남지 않습니다. 기존 상태 불일치 감시는 새 live-state 지표로 유지합니다. 큐가 한 번 0이라는 사실만으로 모든 AP의 신규 생산 중단을 대신하지 않습니다. 과거 계약 row·표본·공유 큐를 삭제하지 않으며 기존 보존 정책은 계속 적용됩니다.

2026-09-25 로컬 자원 비교: 같은 합성 100영상·20채널 응답을 500회 처리했습니다. HTTPS 요청은 전후 1회/배치, live 관측은 20개로 같고 viewer 관측은 100→0, checkpoint는 120→20입니다. DB 저장을 제외한 정규화·관측 생성은 14.086→3.584ms/배치, 누적 할당은 7,207,041→1,767,077bytes/배치였습니다. 로컬 HTTPS를 포함한 실행은 14.338→3.820ms/배치였습니다. 이는 해당 fixture의 측정이며 운영 CPU/RSS나 외부 응답 지연 개선율이 아닙니다.

검증은 collector·API projection/metrics·공유 과거 관측·템플릿의 race 검사, helper protocol/type 검사, 30,000개 종료 이력을 추가한 실제 EXPLAIN 접근량 회귀, schema snapshot과 관측 규칙 검사를 포함합니다. 호출 간격·worker 수·운영 데이터·서비스는 이 로컬 구현에서 변경하지 않았습니다. Fallback delta: none.

## YouTube.js transient recovery

HTTP `429`는 fetch transport에서 응답 body를 폐기한 뒤 기존 `cooldown/COOLDOWN` RPC로 전달합니다. youtubei.js가 status 없는 `InnertubeError`로 바꾸어 내부 치명 오류로 오분류하지 않도록 합니다. 즉시 재전송은 없으며 기존 collection profile의 default same-slot defer를 사용합니다. 응답 body나 URL을 오류 메시지에 넣지 않습니다. Provider rate limit 자체의 해소나 성공 관측을 뜻하지 않습니다.

기존 feed의 YouTube.js transport는 `https://www.youtube.com/youtubei/v1/{browse,next,player}`의 `POST`만 읽기 전용 재전송 대상으로 봅니다. 알려진 transient network code 또는 HTTP `500`, `503`이 발생하면 `100`~`300ms` jitter 뒤 정확히 한 번 재시도하므로 총 시도 수는 최대 2회입니다. 재생할 수 없는 request body, 다른 host/path/method, HTTP `429`, `501`, `502`, `504`와 그 밖의 status, parser/protocol failure에는 transport retry를 적용하지 않습니다. 새 channel/video live check RPC는 이 정책을 사용하지 않고 단일 시도·redirect 거부 transport를 사용합니다. 기존 feed의 두 번째 시도 실패는 typed failure와 scheduler defer 계약을 유지합니다.

각 추가 시도는 `youtubejs_upstream_retry_scheduled` INFO event에 endpoint, trigger, delay, attempt를 기록합니다. 같은 시간대의 `YouTube collection job failed` WARN이 없으면 transport 안에서 복구된 것이며, WARN이 이어지면 bounded retry가 소진된 것입니다. 배포 후 24시간 동안 exhausted `collection_failed` 비율이 감소하지 않거나 `429`, request timeout, upstream request volume이 증가하면 이 정책을 재검토합니다.

## YouTube.js live schedule metadata

Channel RPC의 필수 `kind=live|metadata`가 수집 범위를 지정합니다. live 작업은 streams/player만, metadata 작업은 about과 채널 정보만 조회하므로 일정 접근 제한이 통계·프로필·사진 수집을 중단시키지 않습니다. 변경된 Go binary와 Node helper는 같은 bundle로 교체합니다.

Channel 목록의 `UPCOMING` 행에 기계가독 `scheduled_at`이 없으면 helper가 같은 video ID의 raw `/player`를 순차 조회합니다. 목록 시각이 있으면 상세 조회는 0회이며, 누락된 고유 UPCOMING video ID당 1회, 한 channel collection당 최대 32회입니다. `LIVE`, `ENDED`, `CANCELLED`는 schedule 보강 대상이 아닙니다. 이 횟수는 transport의 transient 재시도 전 논리 요청 수이며, `/player`의 총 transport 시도는 위 정책에 따라 각 요청당 최대 2회입니다.

로컬 adapter는 응답 성공 상태, 요청과 정확히 같은 `videoDetails.videoId`, 존재하는 live/upcoming boolean을 검증합니다. 예정 시각은 RFC3339 `microformat.playerMicroformatRenderer.liveBroadcastDetails.startTimestamp`를 우선 사용하고, 이 값이 없으면 동일 video ID의 `playabilityStatus.liveStreamability.liveStreamabilityRenderer.offlineSlate.liveStreamOfflineSlateRenderer.scheduledStartTime` epoch seconds를 사용합니다. 두 값이 모두 있으면 같은 시각이어야 합니다. 표시 문자열은 사용하지 않습니다. Content 목록의 premiere 분류도 같은 raw adapter를 사용하며 `isUpcoming=true`와 `isLiveContent=false`일 때만 content-owned premiere로 유지합니다.

접근 제한 예외는 `UNPLAYABLE`과 `errorScreen.playerLegacyDesktopYpcOfferRenderer`, 정확한 video ID, `isUpcoming=true`, `isLiveContent=true`가 확인되며 두 예정 시각이 모두 없는 경우에만 적용합니다. 해당 행은 `unavailable_live_sessions`의 ID·채널·`access_restricted` 사유로 분리하고 helper가 `youtubejs_live_schedule_unavailable` WARN에 공개 식별자와 사유를 기록합니다. 번역된 가입 안내문으로 분류하지 않습니다. 멤버십 영상에도 기계가독 시각이 있으면 정상 수집합니다.

제한 목록은 중복·정상 sessions와의 중첩·다른 채널·미지 사유를 거부하며 최대 32개입니다. Go adapter는 유효 sessions만 `PARTIAL` live observation으로 발행하고 해당 poll을 완료합니다. 제한 행만 남은 빈 sessions도 PARTIAL입니다. 다음 기존 poll에서 다시 관측하며 추가 재시도·별도 provider·과거 시각 재사용을 하지 않습니다. 제한 영상의 canonical 상태와 마지막 확인 시각은 갱신하지 않습니다. PARTIAL의 부재는 종료·취소 근거가 아니며 개별 영상의 명시적 종료는 기존 consumer 규칙으로 처리합니다.

`youtube_collection_completeness_total{provider="youtubejs",kind="live_snapshot",completeness="PARTIAL"}`와 위 WARN을 함께 확인합니다. poll 성공·readiness·freshness는 모든 영상의 일정 확보를 뜻하지 않습니다. helper WARN은 원천 관측 기록이며 실제 발행 여부는 observation publish 결과로 확인합니다. Collector 팀이 이 예외를 소유하며 renderer 변경 또는 다른 접근 제한의 독립 재현 근거가 생기면 `DEC-20260911-youtube-restricted-schedule-isolation`에 따라 범위를 재검토합니다.

32개 후보 초과, identity/schema/time drift, 미지의 UNPLAYABLE 또는 위 접근 제한에 해당하지 않는 시각 부재는 terminal `parser_drift`입니다. 해당 collection은 observation과 checkpoint를 저장하지 않습니다. `youtube_collection_attempts_total`과 bounded `YouTube collection job failed` 로그로 판정합니다. 목록과 player 사이에 `LIVE`가 확인되거나 처음부터 `LIVE`로 발견된 방송은 예정 시각을 만들지 않고 정상 live catch-up 경로를 유지합니다.

`youtubei.js@18.1.0`은 session, request context, browse/transport와 범용 parser 기반층으로 고정합니다. Upgrade 전 upstream release note와 로컬 사용 surface를 확인하고 `src/live-metadata.test.mjs`, `src/live-check.test.mjs`, 전체 helper test와 typecheck를 실행합니다. raw field 변화가 있으면 sanitized fixture와 로컬 adapter만 함께 갱신합니다. 전체 fork나 vendoring은 `DEC-20260911-youtube-restricted-schedule-isolation`의 review trigger가 충족될 때만 다시 결정합니다.

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
- AP rollback 기준점은 Compose AP 백업의 `rollback-image-tag`와 `deploy/compose` 경로 prechange 사본, host-native AP의 `previous` collector release 하나입니다. 기준점이 없는 호스트는 되돌릴 이전 collector가 없으므로 `ap-rollback.sh`·`ap-host-native-rollback.sh`가 거절하고 fix-forward합니다. Compose AP 배포가 cutover 뒤 검증에 실패하면, 기준점이 있을 때는 collector를 배포된 상태로 두고 `ap-rollback.sh` 실행을 안내합니다. 기준점이 없는 첫 배포는 새 collector 컨테이너를 멈추고 비활성을 확인한 뒤 fix-forward를 안내합니다. host-native AP는 unit을 멈추고 `previous` release로 자동 복원합니다. 퇴역 producer 첫 cutover 상태를 기록·복원하던 경로와 repo 루트 compose 경로 폴백은 삭제했습니다(stack-audit 2026-09-26 T11).

```bash
export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
sudo -n env COMPOSE_ENV_FILE="$COMPOSE_ENV_FILE" ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-build --no-deps --force-recreate youtube-collector
```

- 승인된 stack-secrets 변경 절차로 중앙 host의 `compose.env`에 `HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR=1`을 설정하면 tracked disable overlay가 replicas를 0으로 유지합니다.
