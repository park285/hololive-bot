# Service: youtube-collector

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-youtube-collector` |
| Binary | `youtube-collector` |
| Execution | Central `c`: Compose `youtube-collector`; Seoul `b`: Compose `youtube-collector-b`; Osaka `a` / Osaka2 `d`: host-native systemd `hololive-youtube-collector@youtube-collector-{a,d}.service` |
| Ports | `a` 30005, `b` 30015, `c` 30025, `d` 30035 |
| Health endpoint | `https://127.0.0.1:<port>/health` over H3 |
| Ready endpoint | `https://127.0.0.1:<port>/ready` over H3 |
| DB role | `hololive_scraper` |
| TLS | `POSTGRES_SSLMODE=verify-full`; Compose CA `/run/hololive-bot/certs/postgres-ca.pem`, native CA `/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem` |

## Role

순수 job 계약·target snapshot·수집 입력/결과·retry 값은 `hololive/hololive-youtube-collector/internal/runtime/collection`이 소유하며 SQL adapter에 의존하지 않습니다. Lease SQL은 `joblease`, observation publish/checkpoint SQL은 `sourceobservation`이 소유합니다. 공용 envelope·canonical JSON·hash·lease 값은 shared `pkg/contracts/sourceobservation`, consume·canonical/replay/retention과 private reducer는 API `internal/youtube/`에 있습니다. YouTube.js pagination은 `youtubejscollector`, RPC DTO와 로컬 요청 pacing은 `youtubejs`가 소유합니다.

AP fleet collector입니다. Holodex, Official Schedule, YouTube.js fetch/normalize와 PostgreSQL collection lease/checkpoint/`source_observations` Publish만 소유합니다. Canonical persist와 notification intent는 `hololive-api` YouTube plane이 소유합니다. `members.photo` product path는 hololive-api admin PhotoSync가 소유합니다.

## Owns

- Provider adapters and bounded collection under `collection.executor.enabled`
- DB job lease/fence and `PublishBatch` (checkpoint + observation insert)
- Collector DB role `hololive_scraper`

## 라이브 채널·영상 확인

`DEC-20260926-hololive-live-absence-evidence`, `DEC-20260927-live-check-slot-isolation`과 [관측 계약 §3.4](../architecture/youtube-three-provider-convergence-contract-v2-20260814.md#34-라이브-채널영상-확인-관측-2026-09-26)를 따릅니다. `youtubejs_channel_live`는 `live_snapshot`만, 별도 lease의 `youtubejs_channel_live_check`는 `/v1/channel_live_check`의 `channel_live_check`만 발행합니다. snapshot 재시도는 성공한 채널 확인의 다음 슬롯을 막지 않습니다. 영상 확인은 구조적 LIVE/검토 대상 UPCOMING membership을 유지하고 `not_before`가 지난 대상만 `youtubejs_video_live` → `/v1/video_live_check` → `video_live_check`로 확인합니다. 기존 운영 세대의 두 확인 kind는 youtubejs 전용 schema 1/generation 1이며 아래 개정의 별도 cutover 전에는 이를 유지합니다.

수명 정합성 개정의 새 collector는 `live_snapshot` schema 1/generation 3과 `video_live_check` schema 2/generation 2를 요구합니다. `channel_live_check`와 Holodex 세대는 그대로입니다. API는 과거 snapshot generation 2와 영상 확인 generation 1의 의미를 보존합니다. [개정 계획과 검증 기록](../plans/2026-09-30-live-reconciliation-lifecycle.md)을 따르며 실제 세대 전환은 migration bootstrap과 분리한 `hololive/hololive-api/scripts/migrations/manual/youtube_live_lifecycle_cutover.sql`의 승인된 cutover로 수행합니다.

generation 3 snapshot의 `query`는 helper의 `streams` 질의 범위·페이지 수·종료·접근 제한을 증명합니다. 반환 영상 상태나 빈 배열에서 coverage를 추정하지 않습니다. 현행 한 페이지 호출 예산에서 continuation이 남거나 종료 플래그가 없으면 PARTIAL이며 부재 종료에 사용하지 않습니다. 접근 제한 영상도 positive 결과와 분리합니다.

영상 확인 대상은 LIVE를 먼저 확보하고 기존 1,000건 상한의 남은 자리에서 지난 UPCOMING과 예정 시각 없는 legacy_unknown을 선택합니다. 미래 UPCOMING은 전수 확인하지 않습니다. 최근 실제 영상 확인은 UNKNOWN이어도 재확인 빈도를 제한하며 positive clock을 만들지 않습니다. 신규 UPCOMING 검토 target의 우선순위는 기존 영상 확인보다 한 단계 낮고 최초 discovery도 기존 LIVE를 앞서지 않습니다. 요청·worker·retry 상한은 변경하지 않습니다.

`lifecycle_origin`은 일정·Premiere·시작 미확정 메타데이터의 `metadata_only`, 실제 수명 사실의 `observed`, 증거 미확정 기존 행의 `legacy_unknown`입니다. 메타데이터 병합은 observed/legacy_unknown을 낮추지 않습니다. schema 2 영상 확인은 canonical 채널 identity·신뢰·무모순 시각·관측 순서를 확인한 명시적 종료에 한해 시작 미관측 UPCOMING을 정산합니다. 시작 clock과 과거 알림은 만들지 않습니다. UPCOMING positive에는 새 요청의 `scheduled_at`과 `waiting_state_confirmed`가 필요하며 과거 정본 일정은 대체 증거가 아닙니다.

검토 영수증 `closed_unresolved`는 ENDED·전송 성공이 아닙니다. UNKNOWN 확인 이후 운영자가 정확한 현재 원본 digest CAS(`youtube_live_review_snapshot.snapshot_sha256`, 무시한 부재 slot 배열 포함 전체 원본)로 기록하는 append-only 결정이며 canonical/head/dispatch를 바꾸지 않습니다. 저장 snapshot은 head의 무시한 부재 slot 배열을 개수·SHA-256 요약으로 바꿔 256 KiB 상한 안에 둡니다(migration 262). 기록 뒤 면제 판정은 `youtube_live_review_current_receipt`가 소유합니다. 원본이 현재도 검토 가능하고, 영수증 digest가 같거나 기존·신규 형식 모두에서 뽑은 수명 사실이 같을 때만 적용됩니다. 수명 사실은 session 상태·일정·출처·제목·시작/종료, head 상태·positive·종료·부재 근거·종료 후보, pending end, 가용성 판정입니다. 관측·갱신 시각, thumbnail, 무시한 부재 slot, 가용성 재확인 시각·관측 ID·evidence hash만 바뀐 경우는 기존 면제를 유지하고, 수명 사실이 바뀌면 면제를 무효화합니다. 집계와 영상 확인 대상 선택은 같은 함수를 씁니다. 미래 일정의 `legacy_unknown` UPCOMING은 `legacy_unreviewed`와 `legacy_not_due`에 남고 `unresolved_unreviewed`에서는 빠집니다. head와 충돌하면 `state_mismatch`로 계속 셉니다. runtime은 영수증과 snapshot·판정 함수의 조회 권한만 가지며 기록 함수는 운영자가 SERIALIZABLE 트랜잭션에서 사용합니다.

기존 snapshot 함수의 `to_jsonb(availability)`는 동명 열 때문에 전체 행이 아닌 판정 문자열을
저장했습니다. migration 262는 `to_jsonb(availability.*)`로 가용성 전체 행을 기록 CAS에
포함합니다. 따라서 적용 전 preview digest는 재사용하지 않고 적용 후 다시 조회합니다.
기존 영수증의 문자열과 새 snapshot의 객체는 동일한 가용성 판정으로 정규화하되,
과거 영수증에 없던 진단 메타데이터를 추정해 채우지 않습니다. 판정 변경은 재검토 대상이며,
같은 판정의 재확인도 기록 **전**에는 전체 원본 CAS를 무효화합니다.

기본 cadence는 2분, evidence freshness는 270초입니다. 채널 확인은 resolve_url 1회와 선택 영상 player 최대 1회, 영상 확인은 player 1회이며 초기화용 config 조회·HTML·browse 보완·transport retry·자동 redirect를 사용하지 않습니다. 기존 목록 실패로 인한 job-level PARTIAL/defer는 아래 Atomic publish 계약을 유지하며, 새 확인의 UNKNOWN 자체를 추가 재시도의 이유로 삼지 않습니다.

원시 영상·채널 identity, isLive/isLiveNow와 시작·종료 시각을 먼저 판정합니다. UNPLAYABLE은 LIVE/종료 모두에 올 수 있습니다. 회원 offer renderer는 MEMBERS_ONLY, 명시적인 isPrivate=false는 PUBLIC, identity와 isPrivate=true가 함께 확인된 경우만 PUBLIC_UNAVAILABLE입니다. LOGIN_REQUIRED·ERROR·messages·번역 문구만으로 공개 불가를 추정하지 않습니다. 모순·해석 불가와 요청/응답 계약 실패는 UNKNOWN으로 기록하여 과거 음성을 유지하지 않습니다. 취소·lease 상실·설정/내부 불변식 오류는 publish하지 않습니다. Collector는 canonical 테이블에 접근하거나 종료를 직접 적용하지 않습니다.


## 시청자 수 전용 수집

`DEC-20260925-hololive-viewer-collection-retirement`에 따라 신규 `viewer_sample`을 수집하지 않습니다. YouTube.js의 `youtubejs_viewer` 작업과 `/v1/viewer` RPC를 제거했으며, `holodex_live`는 `live_snapshot`만 발행합니다. Holodex의 기존 `/live` 조회와 방송 상태·일정·채널 메타데이터는 유지합니다. 응답에 포함된 시청자 수를 표본으로 만드는 비용은 별개이므로 더 이상 viewer envelope·checkpoint·queue를 생성하지 않습니다.

공유 viewer payload/소비·재처리 경로는 이미 저장된 관측과 기존 큐를 처리하는 계약입니다. 현재 publisher는 양 공급자의 신규 viewer 발행을 거절하며, 기존 consumer는 보관 기간 안의 과거 관측을 처리할 수 있습니다. 저장 축소 변경의 viewer 원본 기본 보관 기간은 7일이며, 제품 samples 삭제는 이 변경에 포함하지 않습니다.

사용자용 라이브 템플릿과 미리보기에는 `ViewerCount`가 없습니다. 미지원 변수는 기존 템플릿 오류로 거절하고 0명으로 대체하지 않습니다. 별도 Holodex 조회를 쓰는 Go Stream API의 `viewer_count`와 Twitch/Chzzk 데이터는 그대로입니다.

Holodex live/schedule 작업은 채널 통계·사진 payload를 만들지 않습니다. 요청한 metadata 작업에서의 충돌은 계속 오류이며, 무관한 metadata 충돌이 방송·일정 관측을 중단시키지 않습니다.

## Atomic publish

`PublishBatch`는 `COMPLETE` terminal을 유지합니다. Scheduler는 `PARTIAL` output에 `PublishBatchAndDefer`를 사용하여 observation/checkpoint/queue와 typed `last_failure_*`를 같은 PostgreSQL transaction에서 기록합니다. 충돌이 없는 PARTIAL은 `DEFERRED`로 같은 슬롯을 재시도합니다. 하나라도 `COLLISION`이면 PARTIAL도 `observation_collision/DATA_CONTRACT`로 완료하여 `IDLE`과 다음 due 슬롯으로 전진합니다. 기존 불변 evidence를 덮어쓰지 않으며 독립 관측은 함께 확정합니다. 성공한 `COMPLETE`/`PARTIAL` terminal commit 뒤에는 별도 defer를 수행하지 않습니다. collision complete는 `observation_collision/DATA_CONTRACT` durable diagnostic을 남기고, 성공 complete는 `last_error_code`만 지웁니다. Release는 `shutdown_release`/`renew_failed_release`/`superseded_release` shape이며 `last_failure_*`는 보존합니다. migration 177/189의 `legacy_collector` backfill trigger는 migration 218에서 지웠고, 기존 행의 `legacy_collector` 값은 이력으로 남습니다.

Runner input의 `TargetSnapshot`은 canonical job contract가 요청한 kind를 한 번에 읽는 immutable view입니다. 요청 kind가 누락되면 fail-closed로 오류를 반환하며, 최종 authority는 계속 publish transaction의 lease fence/current projection/enabled target 검증입니다. Snapshot은 fallback이나 publish 검증 대체 경로가 아닙니다.

membership 개정(migration 259)은 CURRENT guard를 잡은 뒤 lease·현재 target의 연속성을 검증합니다. lease 취득 시 전체 job scope의 kind·정확한 subject 여부·개수를 저장하며, `member_since_generation`으로 무관한 projection 교체를 허용하되 자기 target 제거·재추가(ABA)와 policy 변경을 거부합니다. 취득 generation·owner·epoch·scheduled_for·expiry fence는 유지합니다. `not_before`는 새 취득에만 적용하며 이미 진행 중인 작업을 취소하지 않습니다. 갱신은 guard를 역순으로 잠그지 않습니다. superseded는 실행 취소·합류 후 fenced release하며 합류 실패 때 lease를 풀지 않습니다.

수집 결과는 immutable 관측과 latency만 보관합니다. `collectorruntime.Publisher`가 관측을 한 번 복사해 같은 순서·identity의 checkpoint와 저장 입력을 만듭니다. Repository의 독립 payload·cursor·binding 검증과 방어적 복사는 유지합니다. Job 계약 definition은 불변 handle을 공유하되 호출자에게 주는 kind slice와 확장 가능한 계약 map은 독립 소유입니다.

Discovery는 due-only입니다. GLOBAL job도 lease due predicate를 통과한 경우에만 candidate가 되며 매 cycle 무조건 enqueue하지 않습니다. Local queue FULL은 성공이 아니라 explicit `EnqueueFull`이며 해당 discovery cycle의 남은 admission을 중단합니다. Scheduler instance는 single-use입니다. Start는 NEW에서만 성공하고 Stop 또는 fatal 이후 STOPPED instance는 재사용하지 않습니다.

만료 계약 개정(migration 261)은 generation header의 `valid_until`만 만료 근거로 사용합니다. API는 같은 세대의 `not_before`가 실제로 바뀔 때 header의 `eligibility_version`을 transaction당 한 번 증가시키며, collector는 현재 header·target·lease를 직접 조회합니다. reasons·canonical·방 정보는 읽지 않습니다.

후보 조회는 관계형 bundle·due 필터·정렬과 기존 `LIMIT+1`을 유지합니다. 같은 statement의 DB 시각으로 IDLE/DEFERRED/ACTIVE due와 `not_before`를 검사하고, 신규 대상 우선·영상 LIVE 우선·subject tie-break·queue exclusion을 적용한 뒤 제한된 후보만 전송합니다. collector의 cadence 검증·job key 생성·runner capacity/rotation은 유지합니다. 전체 key 전송, 전체 target 캐시, eligibility delta 및 전체 lease 사실의 AP 정렬은 비용 회귀로 채택하지 않았습니다. 조회 오류나 만료는 빈 성공이나 오래된 캐시로 대체하지 않습니다.

후보 조회는 선점권이나 발행권이 아닙니다. acquire는 guard 이후 target bundle statement에서도 header 만료를 확인하며 DB lease·slot·fence·membership 검증을 유지합니다. Publish/renew/snapshot도 현재 header 만료를 사용하고 `not_before`로 이미 입장한 작업을 취소하지 않습니다. 재시작은 기존 DB lease/checkpoint/retry 상태에서 복구하며 로컬 내구 저장소나 새 fallback은 없습니다.

261은 target의 `valid_until` 열을 제거하므로 기존 API/collector binary와 혼합 운용할 수 없습니다. 실제 적용은 별도 승인된 drain → 호환 API/collector와 migration 261의 조정된 전환 → 재개 순서가 필요합니다. 과거 259/260을 수정하거나 구 binary만 재기동하는 rollback은 하지 않습니다.

성능 검증은 격리 PG의 `joblease.TestProjectionCandidateTraffic`으로 실제 260/261 스키마와 620/1,240/10,000 target을 비교합니다. 260까지의 migration과 `bd498868a` 원본 SQL을 재생하고, 양쪽 모두 독립 pool 4개의 동시 120-cycle·동일 예열·header 조회를 사용합니다. 신규·not-due·동일 due·혼합·서로 다른 due 시각·매 cycle eligibility 변경에서 `Jobs`/`Truncated` 일치와 실제 수신·송신 byte 비증가를 검사합니다. PREPARE/EXECUTE의 custom/generic 카운터를 확인한 EXPLAIN 실행시간·buffer I/O도 기록합니다. `paired_elapsed`는 양쪽 실행을 합친 시간이며 개별 AP/버전 지연 비교값이 아닙니다. 전체 facts를 multirange로 압축한 안은 10,000개 서로 다른 due 시각에서 408,243,360 byte 대 관계형 464,160 byte로 회귀해 제외했습니다. 이 합성 결과는 운영 CPU/RSS/TLS 지연이나 보존 기간 전체의 관측을 대신하지 않습니다.

`collection.queue.max_age`의 fixed 상한을 넘긴 로컬 항목은 dequeue 시 lease 취득 전에 폐기합니다. stale discard·경고를 기록하고 queue 표식을 해제한 뒤 다음 항목을 계속 처리하며, DB terminal이나 즉시 재시도는 만들지 않습니다. Collector `internal/config`는 profile-only preflight와 runtime이 동일한 profile 수치 정책을 적용하도록 소유합니다.

Scheduler가 queue·discovery·lifecycle과 worker·queue·batch·cadence 정책을 소유하고, executor는 단일 collector 설정과 검증된 retry bounds로 수집·발행을 실행합니다. Provider gate는 lease 취득 뒤 snapshot 조회와 수집 동안만 점유하며, 수집 함수가 반환하면 검증·DB publish 전에 정확히 한 번 반환합니다. 반환하지 않은 수집 함수는 계속 슬롯을 점유합니다. Admission timeout의 durable 실패·retry 정책은 그대로입니다. Typed defer는 직접 실패와 PARTIAL 발행에 같은 bounds를 쓰며, 저장 adapter의 진단 마스킹과 DB clock clamp를 유지합니다.

Fatal은 first-wins이며 명시적으로 분류된 INTERNAL/PROTOCOL 오류와 runner panic·result invariant·불가능한 queue 상태가 대상입니다. Ordinary provider failure, timeout, cooldown, parser drift는 fatal이 아닙니다. Lease-run join의 `CLEANUP_TIMED_OUT`은 callback이 실제로 합류하지 못한 경우이며, 자체 request timeout을 반환하고 끝난 callback과 구분합니다. Lease supervision timeout만으로 process fatal을 보고하지 않는 기존 정책을 유지하며 함께 보존된 classified fatal 원인은 보고합니다. 아래 helper process cleanup timeout은 별도의 fatal shutdown 경계입니다.

Official Schedule의 mixed-invalid 응답은 유효한 row를 COMPLETE로 발행하고, 모든 row가 잘못된 응답만 parser drift로 처리합니다. API schedule reducer는 관측한 row를 적용하며 응답에 없는 기존 일정의 삭제 근거로 사용하지 않습니다. 이 COMPLETE는 입력의 모든 row가 유효하다는 보장이 아닙니다.

## 일반 영상 신규성 수집

`video_list` generation 2는 항목별 `publication` 증거를 사용합니다. 목록 두 RPC를 먼저 완료하고 기존 limiter를 통과하는 player RPC 최대 두 번으로 신뢰 가능한 게시 시각 또는 새 최초공개 일정을 확인합니다. 목록 helper 내부의 항목별 player 호출과 상대 게시 날짜 추정은 사용하지 않습니다. 네 RPC의 실행 시한은 기존 profile로 계산하며 fleet rate·worker·retry 상한을 늘리지 않습니다.

같은 슬롯에서 이미 수락된 목록은 재수집하지 않습니다. 후속 슬롯의 확인 진척과 최근 증거는 observation/checkpoint와 같은 transaction의 bounded cursor에 보존합니다. 최근 cache 밖의 긴 목록도 순환하며 새 head를 우선합니다. 현재 generation의 손상 cursor는 `parser_drift/DATA_CONTRACT`로 드러내고 과거 없음으로 초기화하지 않습니다. generation 1 cursor는 새 증거 형식으로 재해석하지 않습니다.

player 근거가 부족하거나 허용된 lookup이 실패하면 목록 자체는 명시적 UNRESOLVED로 보존합니다. 취소·lease 상실·fatal 오류를 성공으로 바꾸지 않습니다. 알림 기준과 과거 영상 억제는 [API 정책](hololive-api.md#일반-영상최초공개-신규성)이 소유합니다.

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
- slot-specific strict `STACK_WORKER_PROFILE_FILE` with the `collection` worker
- `YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true`
- `YOUTUBE_COLLECTOR_INSTANCE_ID=youtube-collector-{a,b,c,d}`
- `PHOTO_SYNC_ENABLED=false`
- `POSTGRES_USER=hololive_scraper`
- `POSTGRES_SSLMODE=verify-full` and `POSTGRES_SSLROOTCERT` set to the Compose/native CA path above
- Central startup uses `docker-compose.prod.yml` + `docker-compose.live-compat.yml` for fleet member `c` and issuer `youtube-po-c`. Seoul uses `docker-compose.prod.yml` + `docker-compose.seoul.yml` for `youtube-collector-b` and issuer `youtube-po-b`; the AP overlay pins central services to `central-only`. Osaka `a` and Osaka2 `d` use native systemd units; their Compose overlays validate configuration/path contracts. All deployments follow the [paired collector/issuer procedure](../runbooks/youtube-collector.md#isolated-po-token-lifecycle).

## Shutdown behavior

- Stop the collector scheduler and YouTube.js helper.
- Do not claim or update observation queue rows.

YouTube.js helper는 `RuntimeBaseDir` 아래 unique `0700` directory의 private UDS만 사용하고, socket unlink와 directory cleanup은 Go가 단독 소유합니다. 정상 종료는 SIGTERM 뒤 drain을 기다리며 configured timeout에만 SIGKILL을 쓰고, `Close`가 `CLEANUP_TIMED_OUT`이면 runtime은 fatal shutdown입니다. `/ready`는 helper `Healthy`(READY), PostgreSQL queue probe, scheduler RUNNING, 첫 collection terminal commit, processed observation handoff를 증명합니다. provider freshness stale이나 local queue full은 `/ready` 503 조건이 아닙니다.

Canonical success-response ceiling env는 `YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES`입니다. 없으면 documented default입니다. 명시적 empty는 startup fail입니다.

Helper bootstrap(`/v1/bootstrap`)은 `protocol_version`과 `limits`만 받고, bootstrap·`/health` 응답에는 proxy 항목이 없습니다. Helper는 proxy 없이 Node 내장 `fetch`로만 YouTube에 접속하며 upstream proxy 설정 경로는 없습니다. `SCRAPER_PROXY_*` 퇴역(`DEC-20260926-hololive-legacy-env-config-retirement`) 뒤 production에서 도달할 수 없던 helper proxy 프로토콜(Go `youtubejs.ProxyConfig`·bootstrap/health proxy 필드, Node proxy bootstrap·`ProxyAgent`·`--shutdown-timeout-ms` transport close 한도)과 `undici` 의존성을 matched pair로 지웠습니다. 두 쪽이 exact-key로 decode하므로 `proxy` 필드를 보내는 이전 collector와 새 helper, 또는 그 반대 조합은 bootstrap protocol mismatch로 fail-closed됩니다(helper는 같은 image의 collector가 띄우므로 정상 배포에서 섞이지 않습니다). Collection RPC는 `protocol_version`과 `max_success_response_bytes`를 전달하며 `proxy_url`이나 `max_aggregate_bytes`를 받지 않습니다. Success와 error envelope는 분리되고 unknown field, trailing JSON value, HTTP status/error tuple mismatch는 protocol mismatch로 fail-closed됩니다. Go request cancellation이나 client disconnect는 해당 RPC의 `AbortSignal`에만 전파됩니다.

YouTube.js upstream 오류 분류는 helper의 공통 분류기가 소유합니다. 라이브러리 HTTP 401/403은 `configuration_error/CONFIGURATION`, 429는 `cooldown/COOLDOWN`이며 본문 연결 종료와 일반 upstream 실패는 `collection_failed/TRANSIENT`입니다. 탭 없음은 파싱 전 원문 목록이 비어 있지 않고 모든 항목의 경로가 확인된 경우에만 확정합니다. 파서가 진단 없이 제거하는 null·빈 객체나 경로 불명 항목이 있으면 부재로 판단하지 않습니다. 요청 탭이 확인된 성공 경로에서는 무관한 탭의 선택적 URL 누락을 허용하며, URL의 query·fragment·후행 slash는 탭 경로 비교에 영향을 주지 않습니다. 반환된 요청 탭의 유일한 선택 상태·본문·제공된 채널 식별자를 검증하고 해당 본문만 수집합니다. 라이브러리가 파싱 오류를 기록한 뒤 노드를 제거하는 경우도 호출별 오류를 보존하여 `parser_drift/DATA_CONTRACT`로 거부합니다. 채널 404·파싱·네트워크 실패를 빈 성공으로 바꾸지 않습니다. SDK의 endpoint 호출, 초기 browse 이동, continuation과 요청 취소 동작을 유지하며 새 재시도나 fallback은 추가하지 않습니다. Helper 자체 불변식·프로그래밍 결함은 INTERNAL, helper 프로토콜 불일치는 PROTOCOL로 유지하며 parser drift는 DATA_CONTRACT입니다. 초기화 중 drain은 늦게 생성된 자원까지 정리한 뒤 bootstrap을 거부하며 READY로 되돌아가지 않습니다.

Channel live snapshot은 정규화 중 출력 크기의 하한을 검사하고, 예정 영상의 metadata가 해결될 때마다 scheduled/LIVE/unavailable 표현으로 하한을 갱신합니다. 한도 초과가 확정되면 다음 player 요청을 중단합니다. 미해결 restricted 중복은 고유 identity로 축소될 수 있으므로 원문의 큰 제목을 그대로 예산에 넣어 거절하지 않습니다. 최종 RPC 검증이 전체 응답 크기를 확인하며 raw upstream 응답의 최대 메모리까지 이 예산으로 제한하지는 않습니다.

## Provider HTTP

Holodex와 Official Schedule fetch는 collector-owned `providerhttp` transport만 사용합니다. Redirect는 follow하지 않습니다. Holodex base URL은 clean path prefix(`/api/v2`)를 허용하고 Official Schedule은 origin-only입니다. Request ceiling은 `HOLODEX_TIMEOUT_SECONDS`와 `OFFICIAL_SCHEDULE_TIMEOUT_SECONDS`이며 0/음수는 startup에서 거절합니다. HTTP 401/403은 `configuration_error/CONFIGURATION`이고, 429와 Retry-After가 있는 503은 `cooldown/COOLDOWN`입니다.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f youtube-collector`
- Health: `https://127.0.0.1:30025/health`
- Ready: `https://127.0.0.1:30025/ready`
- Metrics: live-compat publishes `:30096` on `HOLOLIVE_METRICS_PORT_BIND_IP`
- 취득·갱신·publish의 `superseded`는 phase별로 구분합니다. empty 결과는 durable terminal commit 뒤에만 성공으로 셉니다.
- `youtube_observation_accept_interval_seconds`는 checkpoint가 실제 전진한 두 수락 사이 간격입니다. 첫 관측·중복·collision은 표본을 만들지 않습니다. `youtube_observation_last_accepted_timestamp_seconds`는 마지막 실제 수락을 기록합니다.
- API target 지표는 여섯 collection job의 baseline 수요와 due/eligibility를 표현합니다. freshness 때문에 잠든 `video_live_check`는 누락된 필수 metric으로 간주하지 않습니다. `/ready` 성공은 수집 지연이나 queue full 부재의 증명이 아닙니다.

## Related docs

- `../runbooks/youtube-collector.md`

## Discovery 오류의 진행 범위

후보 조회에서 명시적인 runner/target 계약 오류(`joblease.ErrCandidateContract`)가 발생하면 그 runner의 페이지를 거절하고 독립 runner 조회를 계속합니다. mixed poll interval bundle은 실행하지 않습니다. 실제 조회 지점과 잔여 capacity를 기준으로 cursor를 진행시키되 cycle 실패와 runner 진단은 유지합니다. 취소·stale projection·DB/조회 스트림 오류는 전역 중단하며, local 오류 표식으로 완화하지 않습니다. 기존 queue capacity·enqueue dedup·projection generation·lease/fence·원자적 publish 예산은 그대로 적용합니다.
