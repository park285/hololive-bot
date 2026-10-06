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


## Collection membership·video novelty cutover

이 개정은 migration 259/260과 API·collector/helper의 로컬 준비물입니다. 운영 DB 적용·배포·관측 설정 활성화 승인을 대신하지 않습니다. worker `1942a78af`와 API retention `43a5c57a1`의 변경을 보존합니다.

1. 승인된 중단 창에서 기존 collector 전체와 YouTube consumer/target refresh를 drain합니다. 259는 구 generation-lock 함수를 제거하며 260은 video-list current generation을 2로 전환하므로 구 runtime을 실행한 채 전체 manifest를 적용하지 않습니다. 다른 세션의 migration 258은 이 변경에 임의로 포함하지 않습니다.
2. DB 복구점과 실제 manifest 상태를 확인한 뒤 승인된 migration을 적용하고 새 API를 시작합니다. CURRENT guard·연속 membership·`not_before`가 포함된 유효 projection과 generation 1/2 reader를 먼저 확인합니다. 기존 lease의 fail-closed 기본 scope는 새 취득 때 갱신합니다. 과거 관측·pending·canonical·발송 이력을 삭제하거나 replay epoch를 변경하지 않습니다.
3. 새 collector와 helper를 같은 bundle로 a/b/c/d에 반영합니다. 무관한 projection 교체 중 수락 유지, 자기 대상 변경 거부, 실제 checkpoint 전진·consumer 처리·신규성 억제를 확인합니다. 첫 기준 목록과 근거 부족 영상은 알리지 않으며 보존 이력의 자동 backfill은 하지 않습니다.
4. 새 metric 수집 뒤 비활성 observability worktree의 규칙과 alert-log를 별도 승인으로 반영합니다. 실제 rule 발화·Alertmanager 전달·휴대전화 수신을 구분합니다. 격리 Grafana/Prometheus 검증은 휴대전화 도달 증거가 아닙니다.

이 경계에서는 이전 image만 복원하는 일반 rollback을 사용하지 않습니다. 새 발행을 멈춘 뒤 generation 2 backlog/replay가 남아 있으면 해당 API decoder를 유지해야 합니다. 구 lock 함수·계약 세대 복원은 별도 검증·승인된 DB 절차가 필요하며 관측 데이터를 버리는 방법으로 맞추지 않습니다. 중간 호환 artifact를 실제로 준비·검증하지 않은 상태에서 무중단 단계적 전환을 보장하지 않습니다.

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

Worker count, local queue capacity and fixed `queue.max_age`, acquisition cadence/batch, lease/renew/cleanup/publish budgets, retry/jitter, and provider in-flight limits are required fields of the `collection` profile. The runtime rejects their retired environment-variable forms instead of translating them.

로컬 대기 시간이 `collection.queue.max_age.milliseconds`를 넘으면 lease 취득 전에 해당 항목을 버리고 다음 항목을 처리합니다. 경고와 stale discard를 기록하고 중복 방지 표식을 해제하므로 다음 discovery에서 다시 후보가 될 수 있습니다. 아직 lease를 취득하지 않았으므로 DB complete/defer/release는 수행하지 않습니다. `--check-worker-profile`은 `internal/config`의 runtime과 같은 profile 수치 정책(TTL 최대 30분 등)을 검증하며 DB·provider 환경 변수는 요구하지 않습니다.

### 수집 처리량과 대상 신선도

`iris_stack_worker_in_flight / iris_stack_worker_configured_workers`는 작업 슬롯 점유율입니다. CPU 사용률이 아니며, provider admission과 YouTube.js 호출 제한 대기도 포함합니다. AP별 `YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS`는 **helper RPC 전체가 공유하는 간격**입니다. 코드 기본값은 2초(AP 한 대 명목 상한 시간당 1,800 RPC)입니다. 운영은 AP 4대 모두 1초로 override합니다(AP 한 대 초당 1 RPC, fleet 명목 상한 초당 4 RPC). Osaka·Osaka2 native는 static master의 `youtube-collector.env`, Seoul은 `ap-compose.env`, central은 `compose.env`가 값을 소유합니다. Compose `environment`가 `env_file`보다 우선하므로 Seoul·central collector env 파일에는 같은 키를 두지 않습니다. 119채널 roster와 job cadence는 이 override로 바뀌지 않습니다. 워커 수만 늘려 이 상한을 늘릴 수 없습니다. 한 RPC가 외부 HTTP 요청 여러 번을 수행할 수 있으므로 외부 API 호출량과 동일시하지 않습니다.

- `youtubejs_rpc_phase_duration_seconds{operation,phase,outcome}`: `rate_limit` 대기와 `helper` 수행·응답 해석을 분리한 histogram. helper 내부의 외부 응답·파싱 시간은 합산입니다. operation은 community/content/channel/unknown, outcome은 success/timeout/canceled/error입니다.
- `youtubejs_rpc_phase_in_flight{operation,phase}`와 `youtubejs_rpc_request_interval_seconds`: 현재 기다리는 호출, 수행 중인 호출과 설정된 호출 간격입니다. helper phase count는 제한을 통과한 RPC 시도 수입니다.
- `youtube_collection_duration_seconds`: 15/30/60/120/300초 버킷까지 포함합니다. 기존 10초 상한을 넘는 content job의 p95를 10초로 해석하지 않습니다. 롤링 배포 중에는 AP별 histogram을 확인하고 모든 AP가 같은 버킷으로 전환되기 전 fleet 합산 quantile을 해석하지 않습니다.
- API가 노출하는 `hololive_youtube_collection_*`: 현재 유효한 projection의 enabled target을 community/content/channel-live/channel-live-check/channel-metadata/video-live 여섯 작업으로 묶어 집계합니다. `targets`, `stale_targets`, `never_completed_targets`, `due_targets`, `oldest_completion_age_seconds`, `oldest_due_age_seconds`, `required_rpc_rate`를 함께 확인합니다. `not_before`가 미래인 영상 확인은 잠든 membership이며 stale/due 수요로 세지 않습니다. due는 lease와 eligibility 중 늦은 시각을 따르고, lease가 없으면 세대가 바뀌어도 보존한 논리 target 생성 시각을 사용합니다. baseline RPC 수요는 retry·publication enrichment를 포함하지 않습니다. 퇴역 viewer 작업의 과거 lease·지표는 현재 수요에 포함하지 않습니다.
- `hololive_youtube_collection_live_states{state}`와 `live_state_review_targets{reason}`는 현재 유효한 `live_snapshot` 채널 대상에 속한 서로 다른 영상 중 head 또는 서비스 상태가 LIVE/UPCOMING인 집합을 진단합니다. 상태는 head 기준이며 missing/그 밖의 상태는 other입니다. 양방향 상태 불일치를 포함하고, 채널 식별자가 없는 head-only 항목과 양쪽 모두 종료된 이력은 제외합니다. state_mismatch, scheduled_before_now, scheduled_overdue_7d는 겹칠 수 있으며 종료 증거가 아닙니다.

API 집계는 claim 전달과 독립적인 관측 loop에서 최대 30초마다, DB admission을 포함해 1초 예산으로 실행합니다. 실패·유효 projection 부재는 `hololive_youtube_collection_snapshot_success=0`이며 이전 숫자와 마지막 성공 시각을 보존합니다. 성공 지표가 1이고 마지막 성공이 120초 이내일 때만 대상 숫자를 현재값으로 사용합니다. 기존 `youtube_collection_freshness_seconds`는 해당 provider/kind 중 마지막 성공 하나의 경과이며 전체 대상의 신선도를 보장하지 않습니다.

Bot Drilldown의 수집 처리량 섹션과 `HololiveCollectionSnapshotUnavailable`, `HololiveCollectionTargetsStale`, `HololiveCollectionCallBudgetPressure`, `HololiveCollectionLiveStateMismatch`를 확인합니다. 명목 수요가 가동 AP 상한의 85%를 10분 넘게 사용하면 대상·주기·장애 시 여유를 검토합니다. 이 경계값은 초기 운영 기준이며 수집 정책을 자동 변경하지 않습니다. 관측 배포는 API와 AP 계측을 먼저 검증하고 Grafana 생성물·경보를 반영합니다.

`youtube_observation_accept_interval_seconds`는 실제 checkpoint가 전진한 수락 간격이며 첫 표본·중복·collision·정상 empty completion과 구분합니다. 마지막 수락 gauge만으로 탭 부재를 장애로 판정하지 않습니다. publish superseded 비율은 empty를 제외한 전체 publish 결과를 분모로 하며 publish 이전 폐기는 포함하지 않는 하한입니다. 여섯 작업의 due/stale·필수 metric 부재와 실제 RPC admission pressure를 함께 확인합니다. 85% admission은 여유 부족 신호이지 완전 포화나 재시도 포함 총 수요의 측정값이 아닙니다.

Collector loader와 Compose는 canonical env만 읽습니다. `YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS`와 `YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES`가 없으면 documented default(`30`, `1048576`)를 씁니다. 명시적 empty는 startup fail입니다.

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

로컬 adapter는 응답 성공 상태, 요청과 정확히 같은 `videoDetails.videoId`, 존재하는 live/upcoming boolean을 검증합니다. 예정 시각은 RFC3339 `microformat.playerMicroformatRenderer.liveBroadcastDetails.startTimestamp`를 우선 사용하고, 이 값이 없으면 동일 video ID의 `playabilityStatus.liveStreamability.liveStreamabilityRenderer.offlineSlate.liveStreamOfflineSlateRenderer.scheduledStartTime` epoch seconds를 사용합니다. 두 값이 모두 있으면 같은 시각이어야 합니다. 표시 문자열은 사용하지 않습니다. Content 목록은 helper 내부에서 player를 추가 호출하지 않습니다. Go content runner가 별도로 제한된 video-live-check RPC를 호출하여 `isUpcoming=true`, `isLiveContent=false`와 검증된 예정 시각을 신규 최초공개 근거로 사용합니다.

접근 제한 예외는 `UNPLAYABLE`과 `errorScreen.playerLegacyDesktopYpcOfferRenderer`, 정확한 video ID, `isUpcoming=true`, `isLiveContent=true`가 확인되며 두 예정 시각이 모두 없는 경우에만 적용합니다. 해당 행은 `unavailable_live_sessions`의 ID·채널·`access_restricted` 사유로 분리하고 helper가 `youtubejs_live_schedule_unavailable` WARN에 공개 식별자와 사유를 기록합니다. 번역된 가입 안내문으로 분류하지 않습니다. 멤버십 영상에도 기계가독 시각이 있으면 정상 수집합니다.

제한 목록은 중복·정상 sessions와의 중첩·다른 채널·미지 사유를 거부하며 최대 32개입니다. Go adapter는 유효 sessions만 `PARTIAL` live observation으로 발행하고 해당 poll을 완료합니다. 제한 행만 남은 빈 sessions도 PARTIAL입니다. 다음 기존 poll에서 다시 관측하며 추가 재시도·별도 provider·과거 시각 재사용을 하지 않습니다. 제한 영상의 canonical 상태와 마지막 확인 시각은 갱신하지 않습니다. PARTIAL의 부재는 종료·취소 근거가 아니며 개별 영상의 명시적 종료는 기존 consumer 규칙으로 처리합니다.

`youtube_collection_completeness_total{provider="youtubejs",kind="live_snapshot",completeness="PARTIAL"}`와 위 WARN을 함께 확인합니다. poll 성공·readiness·freshness는 모든 영상의 일정 확보를 뜻하지 않습니다. helper WARN은 원천 관측 기록이며 실제 발행 여부는 observation publish 결과로 확인합니다. Collector 팀이 이 예외를 소유하며 renderer 변경 또는 다른 접근 제한의 독립 재현 근거가 생기면 `DEC-20260911-youtube-restricted-schedule-isolation`에 따라 범위를 재검토합니다.

32개 후보 초과, identity/schema/time drift, 미지의 UNPLAYABLE 또는 위 접근 제한에 해당하지 않는 시각 부재는 terminal `parser_drift`입니다. 해당 collection은 observation과 checkpoint를 저장하지 않습니다. `youtube_collection_attempts_total`과 bounded `YouTube collection job failed` 로그로 판정합니다. 목록과 player 사이에 `LIVE`가 확인되거나 처음부터 `LIVE`로 발견된 방송은 예정 시각을 만들지 않고 정상 live catch-up 경로를 유지합니다.

`youtubei.js@18.1.0`은 session, request context, browse/transport와 범용 parser 기반층으로 고정합니다. Upgrade 전 upstream release note와 로컬 사용 surface를 확인하고 `src/live-metadata.test.mjs`, `src/live-check.test.mjs`, 전체 helper test와 typecheck를 실행합니다. raw field 변화가 있으면 sanitized fixture와 로컬 adapter만 함께 갱신합니다. 전체 fork나 vendoring은 `DEC-20260911-youtube-restricted-schedule-isolation`의 review trigger가 충족될 때만 다시 결정합니다.

## Isolated PO Token lifecycle

`DEC-20260927-hololive-egress-po-production`에 따라 정상 PO Token 발급은 trusted helper의 네트워크 controller와 별도 격리 issuer로 나눕니다. `bgutils-js 4.0.3` / `jsdom 24.1.3` interpreter는 collector/helper 안에서 실행하지 않습니다. native issuer는 `RootDirectory`·`DynamicUser`·`PrivateNetwork`·`AF_UNIX`로, Compose issuer는 별도 non-root/read-only/network-none 컨테이너로 실행합니다. 상한은 512MiB, PID 32, CPU 1 core이며 앱 비밀·DB·helper socket을 공유하지 않습니다.

- issuer는 요청을 받기 전에 같은 격리 worker에서 신뢰된 SDK import를 완료하고 `loaded` 확인을 기다립니다. 이 기동 준비 단계의 별도 상한은 30초이며, 실패하면 worker와 listener를 종료합니다. HTTP health 성공은 이 준비가 끝난 뒤에만 가능하고 UA/JSDOM 준비·외부 interpreter 실행은 이후 요청이 소유합니다. native 배포/복원은 Compose와 동일한 30회/2초 간격의 health 관측 후 collector를 시작하며, native 완료 검사도 같은 bounded 준비 대기를 사용합니다. 이 관측은 PO 발급이나 upstream 요청을 만들지 않습니다.
- IPC는 `/run/hololive-youtube-po/worker.sock`만 사용합니다. private protocol 1의 순서는 `session`(UA/JSDOM prepare) → WAA Create → `challenge`(snapshot) → GenerateIT → `activate` → 영상별 `mint`입니다. worker 초기화 완료 전에 challenge를 요청하지 않습니다. collector와 issuer는 반드시 같은 full source SHA로 전환합니다.
- helper는 최초·교체 세대의 IDLE/SDK 준비를 최대 40초 기다립니다. 이는 worker 기동 30초와 이전 세대 종료·서비스 재시작 여유를 포함하며 외부 요청을 만들지 않습니다. 준비 완료 뒤 `session/prepare`부터 발급 전체에 15초를 적용합니다. 외부 요청은 Create/GenerateIT와 필요한 interpreter GET을 합해 최대 3회, 응답별 decoded 512KiB입니다. helper마다 single-flight이며 준비를 포함한 시도 시작 간격은 최소 300초입니다. 개별 broker 연산은 admission·IO를 합해 8초로 제한합니다.
- IPC 요청은 JSON escaping과 envelope를 포함한 **전체 1MiB** 상한을 별도로 적용합니다. 개별 upstream 응답이 512KiB 이내여도 합친 직렬화 값이 이 상한을 넘으면 `broker_request_size`로 발급을 중단합니다. 한도를 늘리거나 해당 cycle을 재시도하지 않습니다.
- nonempty 정상 integrity token과 양의 provider TTL만 허용합니다. monotonic 유효 시간은 provider TTL과 12시간 중 작은 값에서 30초를 뺀 값입니다. 갱신 여유는 최대 5분 또는 TTL의 20%이며 최소 발급 간격을 유지합니다. 각 player에 해당 video ID로 새로 mint하고 header·WEB context·sandbox navigator·GenerateIT의 UA를 일치시킵니다.
- 준비 실패·만료·worker 장애에는 stale/cold-start/fallback token을 쓰지 않습니다. 기존 단일 무토큰 player를 그대로 수행하며 재시도나 UNKNOWN의 음성 확정은 추가하지 않습니다. 채널 확인의 resolve 1회+player 최대 1회, 영상 확인의 player 1회 상한도 유지합니다.
- helper UDS의 `GET /health`에서 `proof.state`, `bootstrap_attempts`, `bootstrap_successes`, `upstream_requests`, `minted_total`, `attached_total`을 확인합니다. `last_error`는 최초 발급·mint 실패를 보존하고, 후속 정리 실패는 별도의 `cleanup_error`에 안전한 오류 코드로 남깁니다. 새 발급 cycle은 두 오류를 초기화합니다. 앱 `/ready` 성공은 PO 준비 완료나 provider 가용성 보장이 아닙니다. token/program/snapshot/visitor data나 원시 worker stderr는 로그·파일에 남기지 않습니다.

빌드·검증은 kapu에서만 수행합니다. native a/d는 `ap-host-native-deploy.sh`가 동일 revision의 collector와 issuer rootfs를 묶고, b는 `ap-deploy.sh seoul`, c는 `PO_PLAN_ID=<승인된 활성 실행 PLN> PO_C_SSH_TARGET=<승인된 중앙 SSH 대상> APPROVE_PO_C_DEPLOY=true scripts/deploy/po-central-cutover.sh deploy`를 사용합니다. 중앙의 `compose-redeploy-service.sh youtube-collector`와 `youtube-po-c`도 같은 paired cutover로 연결되며 같은 env가 필요합니다. `PO_META_ROOT`는 해당 PLN을 소유한 meta checkout입니다. 완료된 최초 PO 도입 계획을 재활성화하거나 gate를 생략하지 않습니다. 이 스크립트의 포괄적 `all` 전환은 지원하지 않습니다. `build-all.sh --build-only --no-bump`는 계속 로컬 빌드 전용입니다.

native a/d 산출물(collector binary와 issuer rootfs의 `po-broker --version`·`po-sandbox/version`)의 version은 `HOLO_BOT_VERSION`입니다. 값이 없으면 `ap-host-native-deploy.sh`가 12자리 short SHA를 씁니다. 릴리스 배포는 `HOLO_BOT_VERSION="$(xargs <hololive/hololive-api/VERSION)" scripts/deploy/ap-host-native-deploy.sh <ap-host> --apply`처럼 Compose image와 같은 version을 명시합니다.

중앙 paired deploy는 Compose가 해석한 collector·migrator의 DB host/port/database 일치를 먼저 확인합니다. 해당 migrator의 접속·TLS 설정과 읽기 전용 CA mount, network를 쓰는 일회성 PostgreSQL client로 운영 ledger의 `222_drop_youtube_job_lease_legacy_failure_trigger.sql` checksum과 read-only guard `on`을 확인합니다. 로컬 `holo-postgres` socket의 ledger로 외부 DB override를 대신 검증하지 않습니다. client는 로컬에 이미 있는 PostgreSQL image만 사용하며 종료 시 자신이 생성한 container·volume을 제거합니다. 적용 부재·checksum 불일치·조회 실패면 기존 서비스 container·source를 바꾸지 않습니다. 검증된 중앙 `hololive-db-migrate`를 먼저 실행한 뒤 collector를 배포합니다.

issuer를 먼저 기동·검증한 뒤 collector만 `--no-build --no-deps`로 교체합니다. b의 소스는 별도 후보 디렉터리에 전송·대조한 뒤 승격하며, 실패 시 snapshot이 이전 파일 내용·mode·symlink·파일 부재까지 복원합니다. rollback은 이전 VERSION/실행 파일과 collector+issuer image/rootfs를 함께 복원하고, 최초 설치였던 issuer는 이전의 부재 상태로 돌립니다. 승인된 rollback artifact는 인수 완료 전 임의 삭제하지 않습니다.

Compose 첫 배포에서 collector 기준점 없이 issuer만 존재하면 전환 전에 거절합니다. 둘 다 없던 첫 배포가 실패하면 새 collector·issuer를 멈추고 비활성을 확인한 뒤 그 container·candidate image tag·active issuer receipt만 제거하고 source snapshot을 복원합니다. volume·backup archive는 보존합니다. 정지·정리·복원이 실패하면 실패 상태를 유지하므로 운영자가 원인을 해결해야 하며 자동 재시도하지 않습니다.

native issuer의 `RootDirectory`는 패키징 때 불변 release 경로로 확정합니다. systemd 249에서는 같은 rootfs라도 symlink 경유 시 226/NAMESPACE가 발생하고 실제 경로는 기동되는 것을 관측했으므로 `current` symlink를 쓰지 않습니다. 설치 unit은 변경 없이 보존하며, `systemd-analyze verify`가 RootDirectory를 고려하지 않는 실행 파일 검사에는 실제 rootfs 실행 경로로 해석한 임시 검사용 사본을 사용합니다. 실제 실행 파일 부재·unit 오류는 계속 차단합니다. verifier가 RootDirectory를 직접 해석하도록 바뀌면 이 검사용 경로 변환을 제거합니다.

Compose의 `image-id` 근거는 검증한 단일 이미지 archive에 묶인 Docker identity 집합입니다. containerd store의 manifest digest와 classic store의 config digest를 최대 두 줄로 기록하고, manifest→config 결합도 검증합니다. 같은 daemon의 rollback snapshot은 실제 ID 한 줄을 기록합니다. 수신측은 이 집합에 없는 ID를 거부하며 full SHA·architecture·version·archive hash와 실행 중 image 일치 검사를 계속 적용합니다. 서울 classic store와 kapu/중앙 containerd store의 표현 차이만 처리하며 임의 image 대체나 검증 생략은 없습니다.

비정상 generation/늦은 응답/취소는 해당 연산의 실제 admission 단계에 따라 처리합니다. 작업 시작 전 취소는 다른 호출의 준비된 세대를 폐기하지 않으며, 실제 worker 연산 중 실패는 전체 VM을 종료합니다. provider TTL 필드, 고정 시계 경계 시험, 실제 장시간 만료·갱신 관측은 서로 다른 증거입니다.

### Issuer generation 교체와 재시작 카운트

`po-broker` 프로세스는 generation 하나만 소유합니다. 그 generation이 다음 사유로 퇴역하면 프로세스는 exit 0으로 종료합니다.

- client의 `DELETE /v1/session`: collector 종료 시 소유 generation 반납, bootstrap 실패 뒤 정리, 약 12시간 주기 갱신의 이전 generation 교체
- lease 만료
- worker 실패나 연산 timeout

native의 `Restart=always`(`RestartSec=1s`)와 Compose의 `restart: unless-stopped`가 exit 0 뒤 새 generation으로 다시 기동합니다. 따라서 Docker `RestartCount`와 systemd `NRestarts`는 generation 교체 횟수이며 실패 횟수가 아닙니다. 0이 아니라는 이유만으로 incident로 판정하지 않고, 카운터를 지우려고 container를 재생성하지 않습니다. 비 0 exit와 OOM은 계속 실패입니다.

v6.0.1 collector·PO 산출물부터 `po-broker`는 종료할 때 `po-broker exit reason=<reason> generation=<id>` 한 줄을 남깁니다(`docker logs <container>`, `journalctl -u hololive-youtube-po.service`). Compose는 image version 6.0.1 이상, native는 `HOLO_BOT_VERSION` 6.0.1 이상이거나 short SHA 산출물이면 revision `12d78df8a` 이후인지로 판별합니다. `reason`은 퇴역·종료 사유의 고정 어휘이며 정확한 목록은 `po-broker` 코드가 소유합니다. `worker_failed`는 요청이 직렬 슬롯을 쥔 동안의 worker 종료(worker IO 전후의 검증·상태 응답 중 포함), `worker_exited`는 직렬 슬롯이 비어 있을 때의 종료입니다. 이 줄에는 token·payload·worker stderr가 없습니다. 기록 대상은 SIGTERM·SIGINT 종료, 퇴역, listener·기동 실패뿐입니다. SIGHUP·SIGQUIT·panic·SIGKILL 종료와 signal 감시 설치 전(기동 직후 수 µs)의 종료는 줄이 없으므로 exit 상태로 판정합니다. v6.0.2부터 기동 때 상속한 무시(SIG_IGN) signal은 감시하지 않아 계속 무시됩니다. v6.0.0 이하 issuer는 종료 사유를 남기지 않으므로 exit 상태와 collector 종료 시각을 대조합니다.

issuer rollout 수용 기준은 `RestartCount=0`/`NRestarts=0`이 아니라 아래 항목을 모두 만족하는 것입니다.

| Runtime | 확인 | 기대값 |
|---|---|---|
| Compose `youtube-po-b`/`youtube-po-c` | `docker inspect <container> --format '{{.State.ExitCode}} {{.State.OOMKilled}} {{.State.Health.Status}}'` | `0 false healthy` |
| Compose, 재시작한 경우 | `docker events --since <container .Created> --until <지금> --filter container=<container> --filter event=die --filter event=oom --format '{{.Action}} {{index .Actor.Attributes "exitCode"}}'` | 모든 `die`가 `0`이고 `oom`이 없음 |
| native `hololive-youtube-po.service` | `systemctl show hololive-youtube-po.service -p Result -p ExecMainStatus -p ActiveState` | `Result=success`, `ExecMainStatus=0`, `ActiveState=active` |
| native, 재시작한 경우 | `journalctl -u hololive-youtube-po.service --since <change_started_at>` | 모든 종료가 `Deactivated successfully`이고 `Failed with result`가 없음 |
| 공통 | broker socket `GET /health` | `state`가 `IDLE` 또는 `READY` |
| 공통 | helper UDS `GET /health`의 `proof.generation` | 값이 있으면 broker `/health`의 `generation`과 같음 |

Docker는 자동 재시작 때 `State.ExitCode`와 `State.OOMKilled`를 초기화하고 systemd도 새 기동에서 `Result`와 `ExecMainStatus`를 다시 씁니다. 그래서 재시작이 있었다면 그 사이의 종료는 `docker events`나 journal로 확인합니다.

Compose b/c의 paired cutover(`ap-deploy.sh`, `po-central-remote.sh`)는 issuer를 먼저 force-recreate하고 healthy를 확인한 뒤 collector를 교체합니다. issuer가 실패하면 이전 collector를 그대로 두기 위한 순서이므로 유지합니다. 그 사이 이전 collector가 새 issuer의 generation을 잡으면 SIGTERM 종료에서 그 generation을 퇴역시키므로, 새 issuer는 이전 collector 종료 시각에 exit 0과 재시작 1회를 보일 수 있습니다. 이 1회는 예상된 교체입니다. 이 밖의 교체는 exit reason 줄로 사유를 확인합니다.

native a/d cutover·실패 복원(`ap-host-native-remote-apply.sh`)과 수동 rollback(`ap-host-native-rollback.sh`)은 issuer socket·service를 collector보다 먼저 멈춥니다. collector 종료의 generation 반납은 `broker_unavailable`로 끝나며 collector는 재전송 없이 무시합니다. 그 짧은 창의 mint·반납 실패는 helper `/health`의 `proof.last_error`에만 남고 로그 줄을 만들지 않으므로 cutover의 journal 오류 검사와 겹치지 않습니다. 이후 issuer health를 확인하고 collector를 기동합니다. 실패 복원은 복원 단계가 하나라도 실패하면 거기서 멈추고 `could not be restored` 경고를 남기며, 배포는 원래 실패 상태로 끝납니다. 이 경고 뒤에는 issuer가 멈춰 있고 collector unit이 disabled일 수 있으므로, 아래 절차로 실행 중 변경과 guard를 확인·해소한 뒤 `scripts/deploy/ap-host-native-rollback.sh <ap> --apply`로 `previous` release와 그 issuer를 다시 적용하고 완료 검사를 확인합니다.

native 배포는 root 소유 `/opt/hololive-bot/youtube-collector/cutover-recovery` guard를 원자적으로 선점하고 단계명·release·소유 shell/worker PID·종료 상태만 기록합니다. HUP/INT/TERM이 배포 shell에만 도착하면 실행 중 변경의 완료를 최대 5초 관찰합니다. worker의 정상 종료 상태와 완료 기록이 일치한 경계에서는 이전 runtime을 한 번 복원하고 129/130/143으로 끝납니다. 변경 명령이나 worker가 신호로 종료되거나 기록을 확인하지 못하면 `outcome_unknown`이며 자동 복원을 시작하지 않습니다. 복원 중 신호도 완료를 확인할 수 없으면 guard를 남깁니다. 최종 journal 조회·grep 실행 오류도 배포 실패이며 grep의 불일치 상태 1만 정상입니다.

guard가 남으면 새 배포와 수동 rollback `--apply` 모두 거절합니다. 준비 중 실패와 복원 실패도 guard를 보존하며 rollback `--dry-run`은 기존 payload 진단에 사용할 수 있습니다. 운영 변경 승인을 받은 뒤 기록의 worker와 자식 process group, 잔존 systemd job 및 collector/issuer 상태를 확인하고 변경 완료 여부를 확정하십시오. PID는 재사용될 수 있으므로 PID 숫자만으로 다른 프로세스를 종료하지 않습니다. 5초 뒤 worker group에 TERM을 전달해도 신호를 무시하는 클라이언트나 daemon 작업의 종료를 보장하지 않습니다. 복구가 확정된 뒤에만 guard를 해제하고 필요한 rollback/fix-forward를 적용합니다. 새 release와 `previous`의 rollback 자료는 이 확인이 끝날 때까지 보존합니다.

### Native AP 배포 산출물 보존

수용 검사 뒤에는 stack의 [배포 산출물 보존 절차](../../../../docs/ops/release-artifact-retention.md)를
배포 완료 기록에 포함합니다. `tools/ops/prune-release-artifacts.py --profile native-ap --ap-name <AP_NAME>`으로
current·previous·최근 세 release와 실행 중인 참조를 보존하는 후보를 산출합니다.
전송 staging도 보존 release와 연결된 것은 남기며 `.incoming-*`와 소유 불명 파일은 삭제하지 않습니다.
이전 staging/release 삭제는 별도 승인된 exact 경로만 `--apply`로 실행하고, 배포와 동시에 실행하지 않습니다.
후보 출력·수용 성공만으로 정리가 완료됐다고 표시하지 않습니다.

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

### 4. Kernel Oops in `unix_fs_perm` on docker-default arm64 hosts

과거 운영 커널에서 발생한 결함입니다. Ubuntu `linux-oracle` 6.17.0-1020과 당시 후보였던 7.0.0-1011에는 upstream AppArmor 수정 `b1aea2c19607` "apparmor: fix race in unix socket mediation when peer_path is used"가 없었습니다. 이 수정은 mainline v7.2-rc1, stable v6.18.40와 v7.1.5부터 들어 있습니다. 미수정 커널에서는 AppArmor가 AF_UNIX 연결의 첫 read에서 peer 소켓 경로를 lock 없이 복사하므로, 같은 시점에 peer가 close되면 NULL `mnt`를 역참조할 수 있습니다.

당시 영향 범위는 collector helper와 PO issuer가 `docker-default (enforce)`로 돌던 arm64 호스트인 hololive-osaka의 collector-c와 iris-seoul의 collector-b였습니다. x86 osaka1/osaka2(a/d)는 6.8 커널에서 unconfined로 실행되어 이 경로에 해당하지 않았습니다.

2026-10-02 확인: 두 arm64 호스트 모두 `7.0.0-1013-oracle`로 재부팅했습니다. 설치 패키지 `7.0.0-1013.13~24.04.1`의 changelog와 [공식 소스 패치](https://packages.ubuntu.com/noble-updates/linux-image-unsigned-7.0.0-1013-oracle)에서 `peer_path`를 `unix_state_lock` 아래 복사하고 `path_get`/`path_put`으로 참조를 보존하는 수정을 확인했습니다. 해당 부팅 이후 관련 Oops는 관측되지 않았고 kernel taint는 두 호스트 모두 `0`이었습니다. collector·PO의 `docker-default` 격리는 유지했습니다. 이는 확인 시점의 상태이며, 이후 커널 교체·롤백 시에는 다시 확인합니다.

Symptoms:
- `journalctl -k`에 `Unable to handle kernel NULL pointer dereference`, `Internal error: Oops`, `pc : unix_fs_perm`, `lr : aa_unix_file_perm`, `vfs_read` call trace가 남습니다. `Comm`은 helper의 `MainThread`입니다.
- `/proc/sys/kernel/tainted`에 TAINT_DIE(128)가 설정됩니다.

Diagnosis:
- `uname -r`와 `journalctl -k`로 커널 버전과 Oops 위치를 확인합니다.
- `sudo docker top <container>`와 `/proc/<pid>/attr/current`로 helper·issuer의 AppArmor label을 확인합니다.

Retained connection policy and unpatched-kernel history:
- v6.0.1부터 broker HTTP 연결은 keep-alive를 쓰고, client가 유휴 연결을 약 1초 뒤 먼저 닫습니다. v6.0.2부터 broker `IdleTimeout`은 30초이며 `po-broker --healthcheck`도 응답을 읽은 뒤 client가 연결을 정리합니다. 커널 수정 후에도 연결 재사용과 유휴 socket의 재사용 경쟁(`EPIPE`) 완화를 위해 이 설정을 유지합니다. 커널 결함 때문에 필요한 임시 완화책으로 취급하지 않으며, 종료 원인 기록과 퇴역 시 `server.Close`도 유지합니다.
- 미수정 커널에서는 이 정책만으로 Oops를 막을 수 없었습니다. 새 연결의 첫 응답이 퇴역·오류 응답이고 곧바로 `server.Close`가 따르는 경우 경쟁 창이 남았습니다. worker가 이미 죽은 경로(`worker_failed`, watchdog)는 응답과 close 간격이 µs 수준이고, `session_closed`·`lease_expired`·`worker_timeout`은 worker SIGKILL부터 reap까지 수 ms(측정: 30MB 약 6ms, 300MB 약 22ms)였습니다. 대표 트리거는 bootstrap 실패 뒤 helper `retireOwned`의 `DELETE /v1/session`이었습니다.
- 커널을 교체하거나 롤백할 때는 `apt-get changelog linux-modules-<version>-oracle | grep -F 'apparmor: fix race in unix socket mediation when peer_path is used'`와 해당 소스 패치로 수정 포함 여부를 확인하고, 재부팅 후 `uname -r`로 실행 커널을 대조합니다. 7.0.0-1011은 이 수정이 없으므로 결함 복구용 교체 대상이 아닙니다.
- AppArmor profile을 완화하거나 unconfined로 바꾸지 않습니다. hololive-osaka 재부팅은 중앙 DB·API를 함께 멈추므로 승인된 유지보수 창에서 수행합니다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30025/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30025/ready
```

## Rollback

Config and topology rollback is an exact repository revision. Restore the following artifacts together. Binary-only rollback is forbidden. Schema/data rollback is none. Mixed-version boundaries and the collector cache-topology unit are in [`rollback.md`](rollback.md#runtime-rollback). Production canary was not executed.

단, migration 259/260 적용 뒤에는 위 [membership·novelty 전환 경계](#collection-membershipvideo-novelty-cutover)가 우선합니다. 구 image만의 자동 복원은 새 lock/contract와 호환되지 않습니다.

```text
collector Go binary/image
bundled Node helper/package-lock
Compose base and AP overlays
host-native env generator/wrapper
service and runbook contract
```

- 이전 `hololive-youtube-collector:rollback-<UTC timestamp>` tag가 있으면 [`rollback.md`](rollback.md#runtime-rollback)의 revision 확인·`prod` 재승격 절차를 사용한 뒤 collector만 무빌드 재생성합니다. Compose overlay와 host-native generator는 같은 revision tree를 써야 합니다.
- AP rollback 기준점은 Compose AP 백업의 `rollback-image-tag`와 `deploy/compose` 경로 prechange 사본, host-native AP의 `previous` collector release 하나입니다. 기준점이 없는 호스트는 되돌릴 이전 collector가 없으므로 `ap-rollback.sh`·`ap-host-native-rollback.sh`가 거절하고 fix-forward합니다. Compose AP 배포가 cutover 뒤 검증에 실패하면, 기준점이 있을 때는 `ap-rollback.sh`가 이전 collector와 issuer를 함께 자동 복원합니다. 기준점이 없는 첫 배포는 새 collector와 issuer 컨테이너를 멈추고 비활성을 확인한 뒤 fix-forward를 안내합니다. host-native AP는 unit을 멈추고 `previous` release와 그 issuer로 자동 복원합니다. 퇴역 producer 첫 cutover 상태를 기록·복원하던 경로와 repo 루트 compose 경로 폴백은 삭제했습니다(stack-audit 2026-09-26 T11).

```bash
export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
sudo -n env COMPOSE_ENV_FILE="$COMPOSE_ENV_FILE" ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-build --no-deps --force-recreate youtube-collector
```

- 승인된 stack-secrets 변경 절차로 중앙 host의 `compose.env`에 `HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR=1`을 설정하면 tracked disable overlay가 replicas를 0으로 유지합니다.
