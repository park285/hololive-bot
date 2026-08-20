# 변경 이력

현재 app 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `v<version>` 형식을
사용합니다. 버전 관리 절차는 [릴리즈 runbook](docs/current/runbooks/release.md)을 따릅니다.
기존 `backup-before-footer-cleanup`은 backup 기준점이고 `shared-go/v0.0.1`,
`shared-go/v0.0.2`는 과거 nested module tag입니다. `[날짜-SHA]` 항목은 버전 관리 도입
전의 실제 commit 기준점이며 app version이나 새 tag를 뜻하지 않습니다.

## 미출시

## v3.0.5 - 2026-08-20

### 변경

- settings가 `ALARM_DISPATCH_RETENTION_{INTERVAL_MS,QUERY_TIMEOUT_MS,LIMIT,SENT_DAYS,DLQ_DAYS,QUARANTINED_DAYS,CANCELLED_DAYS,EVENT_DAYS}`,
  `SCRAPER_SCHEDULER_WORKER_COUNT`, `SCRAPER_POLL_{VIDEOS,SHORTS,COMMUNITY,STATS,LIVE}_INTERVAL_SECONDS`의
  잘못된 명시 값(빈 문자열·비정수·0·음수)을 더 이상 기본값으로 되돌리지 않고 config 로딩 오류로
  거절합니다. 미설정은 기존 기본값 그대로입니다. `positiveIntEnv`·`secondsEnv`·`positiveDurationMS`
  helper를 제거하고 기존 `required*` 파서로 통일했습니다.
- live catch-up 억제가 cache 오류·깨진 marker에서 fail-open으로 동작하는 횟수를
  `hololive_youtube_outbox_live_catchup_suppression_total{result}`로 노출합니다. 동작 방향은 그대로입니다.
- messagestrings Store가 DB 로딩 실패와 키 부재를 `hololive_messagestrings_load_failures_total`,
  `hololive_messagestrings_lookup_fallback_total{reason,namespace}`로 구분해 노출합니다. 호출자가 없던
  `GetOr`를 제거했습니다.
- 운영 경로가 쓰지 않던 `alarmservice.AlarmService.WasUpcomingEventNotifiedRecently`와
  `alarmcache.State.WasUpcomingEventNotifiedRecently`(cache 오류를 `false`로 접던 래퍼)를 제거했습니다.
  upcoming 중복 판정은 `dedup.Service`가 계속 담당합니다.
- 이번 릴리스는 `hololive-api`·`hololive-alarm-worker`·`youtube-collector` artifact를 함께 재빌드합니다.
  `hololive-api` VERSION은 `v3.0.5`와 일치시키고, `hololive-alarm-worker` artifact version은 `3.0.3`에서
  `3.0.4`로 올립니다. collector 이미지 version은 `hololive-api` VERSION을 따릅니다.

## v3.0.4 - 2026-08-20

### 변경

- migration runner가 epoch-2 legacy ledger 계약(136건 체크섬 embed와 기동 시 검증)과
  baseline 체크섬 backfill 허용 경로를 더 이상 갖지 않습니다. `182_epoch2_legacy_ledger_cleanup.sql`이
  legacy ledger 행 136건을 `schema_migrations`·`schema_migration_checksums`에서 정리하고, 정리가
  적용된 뒤에도 manifest 밖 행이 남으면 기동을 거부합니다. 체크섬 목록은 M1 게이트 데이터로
  `scripts/architecture/`에 둡니다. (#396)
- YouTube collector loader와 Compose에서 HC-013 compat alias(`YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`,
  `YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS`)를 제거했습니다. canonical 키만 읽습니다. (#394)
- settings 검증 helper 통합, youtubedispatch 파일 재편과 nil-fallback 접근자 제거, collector 실행
  파이프라인 `collectionExecutor` 추출, sourceobservation publish preflight 단일화 등 동작 동등
  구조 정리를 반영했습니다. (#393) 사후 리뷰에서 확인한 테스트 공백을 보강했습니다. (#395)
- 이번 릴리스는 `hololive-api` artifact만 재빌드하므로 해당 VERSION만 `v3.0.4`와 일치시켰습니다.
  `hololive-alarm-worker` artifact version은 `3.0.3`을 유지합니다.

### 수정

- dbtest가 postgres 컨테이너 제거 레이스에서 재시도하도록 수정했습니다. (#392)
- security workflow의 dependency checkout pin을 정렬했습니다. (#391)

## v3.0.3 - 2026-08-20

### 수정

- YouTube collector의 별도 metrics server에도 role-owned worker registry를 연결해
  `iris_stack_worker_*`가 실제 scrape endpoint에 노출되도록 수정했습니다.

### 변경

- `shared-go v1.53.0`으로 갱신하고 제거된 `workerconfig` package를 허용 목록에서도
  삭제했습니다. worker profile과 metrics는 strict `workercontract.Registry`만 소유합니다.
- 함께 재빌드하는 `hololive-api`와 `hololive-alarm-worker` artifact version을 repository
  release `v3.0.3`과 일치시켰습니다.

## v3.0.2 - 2026-08-20

### 수정

- Compose AP의 `youtube-collector-b`와 `youtube-collector-d`가 collector-c에서 상속한
  `STACK_WORKER_PROFILE_FILE`을 유지해 자신이 mount한 role profile을 찾지 못하던 identity
  회귀를 수정했습니다. AP 렌더가 instance ID와 같은 profile 경로를 환경변수와 read-only
  mount에 함께 사용하는지 검증합니다.

## v3.0.1 - 2026-08-20

### 수정

- central live-compat의 `volumes: !override`가 API, alarm-worker, collector-c의 role별
  Stack Worker Profile bind mount를 누락해 v3 runtime이 `profile_file_missing`으로
  기동하지 못하던 배포 회귀를 수정했습니다. 두 central Compose 조합에서 세 mount의
  source, target, read-only 속성을 렌더 결과로 검증합니다.

## v3.0.0 - 2026-08-20

### 추가

- central API·alarm-worker와 collector a/b/c/d에 role별 strict Stack Worker Contract v1
  profile을 도입했습니다. 실제 PostgreSQL/process queue와 executor를 `/diagnostics/workers` 및
  `iris_stack_worker_*`로 노출하고 worker enablement·capacity·timeout의 env 이중 소유를 제거했습니다.
- `iris-client-go/v2 v2.1.1`을 pin하고 bot webhook receiver를 HMAC v3-only 계약으로
  전환했습니다. nonce replay 방지는 명시적 Valkey store를 사용하며 v2 성공 metric을
  제거했습니다.
- YouTube collector의 provider 실패 class와 lease 실패 진단을 durable schema에 보존합니다.
  기존 deferred 실패를 backfill하고 acquire 경합 및 migration 중에도 진단이 유실되지 않도록
  trigger와 constraint 설치·검증 순서를 고정했습니다.

### 호환성이 깨지는 변경

- htmlscraper의 production `NewTestServiceWithHTTPClient`와 `*ForTest` accessor를 제거했습니다.
  custom YouTube/HTTP client가 필요한 구성은 `NewServiceWithDependencies`와
  `ServiceDependencies`를 사용하며, 기존 `NewServiceWithYouTubeClient`와
  `NewServiceWithOfficialSchedule` signature는 유지합니다.

### 변경

- standalone YouTube scrape/outbox runtime 모듈을 제거하고 AP a/b/c/d identity를 `youtube-collector` fleet로 통일했습니다. Canonical consume/notification/retention은 `hololive-api` YouTube plane, `members.photo`는 admin PhotoSync, egress는 `alarm-worker`입니다. production apply는 포함하지 않습니다.
- YouTube.js 18과 current response shape를 채택하고 Official Schedule, Community와 YouTube.js
  관측의 ownership을 collector plane으로 모았습니다. Kakao room catalog의 DB 오류를 not-found나
  빈 관측으로 바꾸지 않으며 retention과 projection 권한을 최소 privilege로 제한합니다.
- Go toolchain과 builder 기준을 `1.26.6`으로, `shared-go`를 `v1.52.3`으로,
  `iris-client-go/v2`를 `v2.1.1`로 갱신하고 일반챗 plaintext/open-chat 판별을 공용
  `kakaoformat` 정본으로 수렴했습니다.
- **service 로그 인코딩이 text에서 JSON으로 바뀝니다.** shared-go 로깅이 text
  인코더를 제거하고 빈 `Format` 기본값이 JSON이 되면서, `hololive-api`,
  `hololive-alarm-worker`, `hololive-youtube-collector`, admin-dashboard backend의
  stdout·파일 로그가 모두 JSON으로 전환됩니다. text 출력을 전제한 로그 수집·grep·
  알림 규칙은 배포 전에 점검해야 합니다. 사람용 ops CLI의 stderr 출력은 종전
  그대로입니다.

### 수정

- collector가 명시적 no-data를 정상 결과로 처리하고 mixed collision batch의 독립 observation과
  checkpoint를 보존하도록 했습니다. missing live tab은 부재 증거로 사용하지 않으며 handoff
  backlog가 재시작 루프를 만들지 않습니다.
- AP/Compose collector의 credential precedence, startup grace, external H3 TLS smoke, readiness와
  rollback 상태 검증을 하나의 fail-closed 계약으로 맞췄습니다. 원격 apply shell fragment도
  독립 ShellCheck가 가능한 구조로 정리했습니다.
- Iris가 `409` + `CLIENT_REQUEST_ID_FAILED`로 답한 reply를 더 이상 즉시 terminal로
  종결하지 않고, generation suffix(`:r1`, `:r2`)를 붙인 새 `clientRequestId`로 같은
  payload를 최대 2세대까지 재전송합니다. 이 code는 durable queue handoff 이전 실패,
  즉 KakaoTalk 부수효과가 없다는 Iris 계약이므로 재전송이 안전합니다. 적용 범위는
  reply(text·markdown)·durable outbox dispatch·비-outbox live media
  (`SendImage`/`SendImages`) 전부입니다. 전송 결과가 불명한 재POST는 동일 id를
  유지하고 FAILED 확정 응답에서만 세대를 올리므로, a9104260이 제거했던 `:aN` 방식의
  중복 발화 위험은 재도입되지 않습니다. `OUTCOME_UNKNOWN`/`PAYLOAD_MISMATCH`/
  `ALREADY_EXISTS`와 code 없는 `409`의 동작은 그대로입니다.

## v2.0.46 - 2026-08-01

### 수정

- heartbeat request는 빈 body를 `idle=false`로 허용하되 JSON body는 1,024 bytes 이하의 단일
  object만 수용하고 `null`, unknown field, 복수 JSON 값과 trailing data를 거부하도록
  OpenAPI·generated client·backend 계약을 일치시켰습니다.
- JSON 및 RSS 응답 크기 상한을 실제 초과 byte까지 읽어 판정하여 limit에서 잘린 유효 prefix를
  정상 응답으로 오인하지 않도록 했습니다.
- 로그인 실패 backoff를 request context가 취소되면 즉시 중단되는 timer로 바꾸고, 내부 Holo API
  base URL은 canonical absolute `http`/`https` origin만 허용하도록 제한했습니다.
- 취소된 request와 분리된 5초 cleanup context로 pgx rollback을 수행하여 오류·panic 경로에서
  transaction을 회수하면서 원래 오류와 panic identity를 보존합니다.
- 관리자 대시보드 heartbeat/WebSocket의 stale callback, reconnect timer와 in-flight ownership
  경합을 차단했습니다.

### 문서·운영

- `youtube-producer`의 현행 4-way Active-Active 토폴로지를 Seoul `b`, main `c`, Osaka `a`,
  Osaka2 `d`와 포트 `30005/30015/30025/30035` 기준으로 README·Project Map·운영 문서에
  정렬했습니다. `b`·`c`는 Docker Compose, `a`·`d`는 host-native systemd가 소유합니다.
- heartbeat OpenAPI SSOT, generated client, backend contract 문서, AP rsync manifest와 Go workspace
  import graph를 최종 코드 경로와 동기화했습니다.

### 의존성

- `shared-go v1.39.0`과 `iris-client-go v1.3.0`을 채택해 durable worker·retry 계약과 Iris
  transport·webhook·Karing 계약을 현재 공개 릴리즈에 고정했습니다.

## v2.0.45 - 2026-07-15

### 문서

- 실제 commit history를 기준으로 app의 주요 과거 릴리즈 기준점을 한국어로
  보완했습니다.

### 릴리즈

- 저장소 app `VERSION`과 runtime artifact 버전을 분리해 정의하고 SemVer 검증 절차를
  도입했습니다.

## [2026-07-13-a9f89640]

### 보안

- Hololive 운영 경계, runtime secret, network·database 접근과 deployment verification을
  강화했습니다.
- worker profile strict envelope와 release provenance를 맞추고 PostgreSQL 18 volume 경계와
  public CI·ephemeral DB ownership 검증을 보강했습니다.

## [2026-07-10-41674269]

### 수정

- dispatch 보상, delivery state machine, migration transaction·timeout을 포함한 SQL 경계
  13건을 수정했습니다.
- migration runner가 `BEGIN`/`COMMIT` block을 실제 transaction으로 재생하고 session timeout을
  pinned connection에 적용합니다.

### 성능

- PostgreSQL `max_connections=60`을 명시하고 hot-path `EXPLAIN` snapshot을 release gate에
  추가했습니다.

## [2026-07-06-9a90f1e7]

### 추가

- YouTube live-session metadata 저장과 alarm-worker live catchup의 표시 phase를
  추가했습니다.
- 방송 이력 분류 규칙을 file로 분리하고 bot의 방송 이력 기능을 Go 1.26 기준으로
  재작성했습니다.

### 제거

- 사용하지 않는 YouTube statistics·milestone subsystem을 제거하되 기존 data는
  보존했습니다.

## [2026-06-28-b36fa988]

### 변경

- 사용자 노출 message를 PostgreSQL SSOT로 전환하고 migration `074`~`082`의 audit·repair
  도구를 추가했습니다.
- `hololive-api`의 bot, admin, llm 세 plane readiness를 dependency-aware `503` 계약으로
  바꿨습니다.

### 수정

- background loop panic을 격리하여 한 plane의 panic이 통합 process 전체를 종료하지 않게
  했습니다.
- `schema_migrations` ledger를 도입해 migration 전체 재적용과 data churn을 차단했습니다.

## [2026-06-26-59ae217a]

### 변경 (호환성 변경)

- production topology를 `hololive-api`, `hololive-alarm-worker`,
  `hololive-youtube-producer`의 세 runtime으로 통합했습니다.
- 기존 kakao-bot, admin-api, llm-scheduler를 각각 `hololive-api`의 bot, admin, llm plane으로
  이동하고 retired service alias와 transitional compose path를 제거했습니다.
- build-first cutover, rollback, health gate, AP sync manifest와 three-runtime CI contract를
  함께 적용했습니다.

## [2026-06-24-3d1fe7d6]

### 변경 (호환성 변경)

- alarm dispatch를 Valkey hybrid 경로에서 PostgreSQL outbox 단일 경로로 전환했습니다.
- community·shorts routing과 published-at resolver의 legacy fadeout 경로를 제거했습니다.

## [2026-05-25-62bb826e]

### 변경

- webhook 전용 `QueuedPool`을 분리 주입하여 Iris와 bot worker-pool 계약을 통일했습니다.
- 공용 utility를 독립 `shared-go` module로 정리하고 Iris worker profile fetch를
  적용했습니다.

## [2026-05-15-4d4b2ae4]

### 변경 (호환성 변경)

- alarm delivery를 PostgreSQL-first outbox로 전환하고 notification egress ownership을
  alarm worker로 이동했습니다.
- retired `dispatcher-go` runtime을 제거했습니다.

### 추가

- Karing alarm delivery, YouTube live session fallback과 delivery guardrail을 추가했습니다.

## [2026-05-10-d6af29a4]

### 변경

- Hololive의 Iris transport를 HTTP/3로 전환하고 OpenBao에서 H3 CA를 render하도록 했습니다.

## [2026-03-15-ef29a66f]

### 추가

- admin-dashboard service를 compose와 deployment script에 추가했습니다.

## [2026-03-09-1220b688]

### 추가

- YouTube scraper service와 runtime lifecycle, 별도 Iris runtime role token을
  추가했습니다.

### 변경

- compose runtime env와 logging entry point를 통합하고 Valkey path·HTTP error 계약을
  정규화했습니다.

## [2026-03-04-f79b4a0a]

### 변경 (호환성 변경)

- alarm, scraper, dispatcher runtime을 Rust에서 Go로 이전하고 deployment를 four-service Go
  topology로 전환했습니다.
- retired Rust·admin module을 제거하고 Go service와 shared contract boundary를
  모듈화했습니다.

## [2026-03-01-1da02d2c]

### 추가

- 기존 llm monorepo에서 Hololive source와 build configuration을 독립 repository로
  이전했습니다.
- 초기 bot, admin, stream ingestion, alarm·scraper·dispatcher runtime과 quality gate를
  구성했습니다.

## [2026-02-28-52cd4f9c]

### 추가

- Kubernetes manifest와 운영 문서를 기반으로 hololive-bot repository를
  초기화했습니다.
