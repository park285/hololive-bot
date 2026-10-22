# CI·shared·alarm-worker 빅뱅 리팩토링

## 목표와 제약

사용자가 2026-10-21 CI 추가 개선, hololive-shared의 책임 역전·성능·구조 개선, alarm-worker의 구조·성능 개선과
과설계 제거, runtime module 순환 의존 제거를 한 번에 적용하도록 요청했다. 옮긴 이름은 alias·re-export 없이
소비처를 모두 바꾼다. 발송 1회 보장(lease·fence·handoff·outcome_unknown), 세 원장 분리, 단일 인스턴스 egress,
env 키·허용값, wire JSON, Valkey key 형식, metric 이름·label은 바꾸지 않는다. commit·push·배포는 요청 범위가 아니다.

## 조사 근거 (2026-10-21, main `abcc556f8`)

직접 확인한 사실과 병렬 조사(CI·shared·worker 각 1건, 읽기 전용) 결과다. 조사 주장 중 결정에 쓰는 것은 `path:line`을
직접 확인했다. shared 조사의 "worker는 Bot·CORS·Kakao 설정을 쓰지 않는다"는 주장은 틀렸다
(`hololive-alarm-worker/internal/config/runtime.go`가 세 loader를 호출).

### CI
- run 39585902596 총 6분 4초. `max-parallel: 3`으로 shared·collector leg가 약 2분 30초 대기, go-modules는 19초짜리 policy job을 기다린다.
- golangci-lint v2.15.0이 standalone staticcheck profile과 같은 staticcheck·x/tools 판본을 상위 check 집합으로 이미 실행한다.
- `check-go-trigger-route-hardcoding.sh`·`check-go-alarm-contracts.sh`는 다른 게이트·Go 테스트와 중복이다.
- `go-workspace-modules.sh`는 `go work edit -json`의 경로 목록을 얻으려고 모든 게이트에서 Python runtime을 초기화한다. security.yml도 같은 목록을 Python으로 뽑는다.
- shell syntax sweep이 PR policy와 `ci-boundary-gate.sh`에서 두 번 돈다.
- 유지: `check-topology-parity.sh`(hosts.conf 소유값과 방화벽·Compose·nginx 소비처 일치, 보안 설정), tracked artifact, PG capacity, AP rsync manifest, egress ownership.
- runbook이 수동 절차로 참조하는 미연결 테스트 3개(`test-postgres-access-policy.sh`, `test-terminal-live-head-repair.sh`, `postgres-failover-integration_test.sh`)는 유지한다.

### module 순환
- api→worker(`alarmtestkit`, admin 테스트 2개), collector→api(consumer testkit, `collectorruntime/channel_live_slot_test.go` 1개), api→collector(publisher, sourceobservation 테스트 35개), worker→api·collector(dispatchoutbox 테스트 2개).
- 순환 때문에 세 Dockerfile·dockerignore가 형제 `go.mod`/`go.sum`을 복사하고 AP 전송 manifest(`scripts/deploy/ap-rsync-files.txt:442-445`)가 api·worker의 `go.mod`/`go.sum`을 collector 호스트로 보낸다.
- 목표: 데이터 흐름과 같은 단방향 테스트 의존(collector ← api ← worker). collector 테스트의 canonical 반영 단언은 api의 `TestChannelLiveCheckStoresLatestWithoutLiveLifecycle`이 이미 검증한다.

### shared 책임 역전·구조·성능
- api 전용: `contracts/{irisrooms,majorevent,membernews,settings,subscription,trigger}`, `service/internalhttp`, `service/settings`+`BuildSettingsService`, delivery locker, LLM·Gemini·Cliproxy·Exa·Services·Ingestion 설정과 `ValidateRuntimeEnvSyntax`, 템플릿 편집 stack, member mutation, YouTube outbox analytics·telemetry reader, `domain/{command,context}.go`.
- `service/alarm`: worker 전용 Repository·SubscriberResolver·cache warm·metrics·SQL과 wire 계약면(client·handler, 왕복 테스트로 묶임)이 섞여 있다. `service/alarm/keys`는 worker 전용이다.
- `runtimepolicy`가 runtime 이름과 알림 role 정책을, `providers/database`가 pool 이름별 예약 연결 정책을 소유한다.
- `service/delivery`에 Iris egress client(`runtime_iris_*`)가 들어 있다.
- mock 호환 `recover()` 분기와 HMSET 실패 시 필드별 HSET 재시도(`service/alarm/cache_warm_write.go`), `cache.Client`의 중복 `Builder()`/`B()`.
- Lua 스크립트를 매 호출 본문째 EVAL한다(`ratelimit/valkey/sliding_window.go:171`, `cache/service_scripts.go:20`).

### alarm-worker
- 발행 수락 후 방마다 stream 단위 `MarkAsNotified`(HGETALL·HMSET·EXPIRE)를 반복한다(`notifier/notifier_publish.go:53`, key는 `keys.NotifiedKey(streamID)`).
- schedule change 감지가 방과 무관한 stream 조회를 방마다 반복한다(`youtube_checker_upcoming.go:162-188`).
- handoff reconciler 골격이 세 원장에 복제돼 있다. 원장 SQL·상태 어휘는 각자 소유한다.
- 퇴역 `bot` runtime 분기와 config 검증 뒤 env 재해석(`workerapp/build_runtime.go:269-347`), 이미 보장된 nil 재검증.
- 단일 소비 소형 패키지(`cleanupctx`, `service/youtube/logschema`, `service/youtube/outbox/dispatchstate`, `readiness`), 한 줄 위임 러너, 1곳 option, 구현 1개 interface, 운영 도달 불가 테스트 seam.
- 도달 불가: `dispatchoutbox.WithWorkerID`, `NewPgxRepositoryFromPool`.

## 결정

사용자가 2026-10-21 "확인된 항목 전부"를 고르고, `ADMIN_ALLOWED_IPS` 빈 값(`server/middleware/ip_allowlist.go:72`, 전체
허용)은 현행 유지로 정했다. cache warm 실패 정책, YouTube dispatcher 2초 polling, telemetry 이중 로그 경로는 정책이나
소비처 결정이 필요해 바꾸지 않는다.

## 적용한 변경

### CI
- standalone staticcheck·profile·facts 제거, go-modules matrix의 `max-parallel`과 `needs: policy` 제거, 중복 게이트 2개 삭제.
- `go-workspace-modules.sh`와 security.yml의 모듈 목록을 Python 대신 `go work edit -json | jq`로 읽고, 쓰지 않게 된
  `.github/actions/python-runtime`을 지웠다. 중복·go.mod 누락은 go 명령이 거부한다.
- shell syntax sweep을 PR policy job 한 곳에서만 돌린다.
- egress 단일 인스턴스 검사를 prod·live-compat 병합 compose 렌더(JSON) 기준으로 바꿨다. 텍스트 검사는 live-compat override를
  보지 못했다.
- 퇴역 이름이 돌아오는지 보는 grep(`check-production-go-workspace.sh`의 Dockerfile·compose 일부), 스크립트 텍스트만 단언하던
  h3 smoke·AP verify grep, migration suffix 스냅샷(`runner_test.go` legacy ledger fixture와 runtime ledger 검증이 대신 잡는다)을
  지웠다. artifact 게이트는 stage한 삭제를 위반으로 세지 않는다.

### module 순환
- `alarmtestkit`을 지우고 api admin 테스트는 shared `alarmhttp.NewHandler`와 fake 서비스로 worker HTTP 계약을 검증한다.
  collector 테스트는 api consumer 단언을 빼고 발행 key·slot만 본다.
- api go.mod에서 worker, collector go.mod에서 api·worker require/replace를 지웠다. 테스트 의존은 collector ← api ← worker 한
  방향이다. Dockerfile·dockerignore·AP 전송 manifest에서 형제 go.mod 복사를 지웠다.

### shared 책임 역전·구조·성능
- api 전용 contracts 6개, settings 조립(`BuildSettingsService`), bot 명령 타입(`botcommand`), digest 실행 락(`executionlock`),
  admin `ServicesConfig`를 api로 옮겼다.
- `service/alarm`을 worker `alarmstore`·`keys`와 shared `alarmhttp`(양측 wire 계약면)로 나눴다.
  `ResolvePersistedTargetMinutes`는 worker로 옮겼다.
- mock 호환 `recover()`·순차 경로·HMSET 실패 시 HSET 재시도를 지웠고 `cache.Client.Builder()` 중복을 없앴다.
- 1호출 provider 래퍼(cache·member)를 infra 조립에 인라인했고 `member.ServiceAdapter`를 지워 `*member.Cache`가
  `MemberDataProvider`를 직접 구현한다. 테스트 전용 `NewHolodexService` 기본값 생성자를 config를 받는 하나로 합쳤다.
- pool 예약 연결 수를 이름 비교 대신 호출측 `RuntimePool`로 명시한다(값 불변).
- Lua 스크립트 4곳을 `valkey.NewLuaScript`(EVALSHA, NOSCRIPT일 때만 EVAL)로 바꿨다.

### alarm-worker
- 발행 수락 뒤 stream 단위 notified 기록을 batch 안에서 한 번만 쓴다.
- schedule 변경 감지는 stream 단위 hash를 한 번 읽고 방별 index만 방마다 읽는다.
- upcoming 후보 종료를 outcome별 UPDATE로 묶고, backlog 관측을 한 쿼리로 합쳤다.
- Holodex client를 scraper 없이 만들어 기동 시 멤버 identity 색인 적재를 없앴고, 쓰지 않던 공식 일정 설정 적재를 지웠다.
- 퇴역 `bot` scheduler 분기와 nil 재검증을 지우고 publish 설정을 한 번 읽는다.
- 단일 소비 패키지(`cleanupctx`, `logschema`, `dispatchstate`, `readiness`)를 소비처로 흡수했고, publisher 설정 옵션을 묶었으며
  세 원장의 `ReplyStatusClient`를 `handoffcheck` 하나로 합쳤다.

## 적용하지 않은 후보와 근거

- 템플릿 편집 stack: 저장 검증이 renderer의 비공개 parse·실행 guard를 그대로 써야 렌더 결과와 같다.
- member mutation: shared `Repository` 메서드이고 cache epoch 무효화가 프로세스를 가로지르는 계약이다.
- YouTube telemetry·delivery outbox producer: worker가 직접 쓰거나 원장 계약 시드로 쓴다.
- LLM·Gemini·Exa·Ingestion 설정: worker의 `ValidateRuntimeEnvSyntax`가 공유 env 형식을 검사하고 `runtime_test.go`가 이를 고정한다.
- `runtimepolicy`: egress 단일 소유는 runtime을 가로지르는 불변식이다.
- `classify-titles`: 실험 스크립트가 `go run`과 해시 입력으로 쓴다.
- 조립부 결과 struct: `nilnil` 린터가 interface의 `(nil, nil)` 반환을 막는다.
- handoff reconciler 제네릭화: 원장별 로그·panic 이름·시간 예산·metric을 모두 매개변수로 넘겨야 해 추상화가 더 커진다.
- target minutes 재생성 캐시, workerruntime errgroup 평탄화, 1곳 option 정리: 이득 대비 테스트·로그 변경이 크다.
- `WithWorkerID`·`NewPgxRepositoryFromPool`·`TracingEnabledEnvKeys`: 테스트가 lease 시나리오와 env 격리에 쓴다.
- trivy 인자 검사·`check-go-test-json.py`·`test-three-runtime-topology.sh`: 스캔 억제 금지, skip 검출, 배포 계약값을 보는
  제품 검사다.

## 운영 영향

- `hololive_alarm_subscriber_*` metric은 worker에서만 등록된다. api에서는 항상 0이던 series가 사라진다.
- worker는 `OFFICIAL_SCHEDULE_*` env를 더 읽거나 검증하지 않는다(collector와 api는 그대로).

## 검증 결과 (2026-10-21, 마지막 편집 뒤)

- `scripts/ci/local-ci.sh` 통과: architecture 게이트, go work sync, 5 module `go mod tidy -diff`, `GOWORK=off` vet,
  integration-tag vet, golangci-lint 0 issues, NilAway, race 테스트, non-race 테스트.
- integration 테스트(임시 PostgreSQL·Valkey): dispatchoutbox, dispatchops, 두 summarizer, youtubedispatch, joblease 통과.
- `scripts/architecture/ci-boundary-gate.sh`, `check-sql-ownership.sh` 통과.
- egress 게이트에 replicas, role `off`, UDP 포트 제거, collector `IRIS_BASE_URL`, container_name 변경을 하나씩 주입하면 각각 FAIL 1건.
- `./build-all.sh --build-only --no-bump`로 9개 이미지 빌드 통과.
- 독립 검토에서 찾은 backlog snapshot Scan 누락(격리·경과 gauge 미갱신)과 같은 stream의 예정 시각 교차 시 notified hash
  차이를 고치고, 각각 DB 테스트와 재현 테스트로 확인했다.
- 실행하지 않음: `scripts/ci/pre-push-gate.sh`(게시 전 필수, 이번 범위는 commit·push 없음), 운영 배포 검증.

## 운영 반영 결과 (2026-10-22)

- revision: central api·alarm-worker는 `40fcb7841c2bef146ac1f30f01233c94492bf21e`, collector a·b·c·d와 PO issuer는
  `84e54081071f4c826490dfede5c1479d1195d8c4`다. 둘의 차이는 central collector 배포 스크립트 수정과 그 테스트·변경 이력뿐이고
  runtime 코드는 같다.
- Node 26.12.1: 다른 작업자의 미커밋 source 변경을 내용 그대로 `f220d6da1`에 포함했다. 하한이 올라 깨진
  `ap-completion-check_test.sh`의 fake node만 v26.12.1로 맞췄다.
- 검증(`84e540810`, 마지막 편집 뒤): `scripts/ci/pre-push-gate.sh` 통과, 배포 정적 계약 8종 통과. `build-all.sh`는 `40fcb7841`에서 통과.
- central: api `93d28d7630c9`·alarm-worker `54d717dfead1`(arm64)을 no-build로 prepare한 뒤 `db-migrate`(applied=0 skipped=200)를
  실행하고 worker, api 순으로 교체했다. readiness 8종 rc=0, executor 3개 enabled, egress 컨테이너 1개다.
  rollback 지점은 `artifacts/worker-profile/releases/app-40fcb7841-20261022T005852Z`와 `rollback-20261022T005852Z` 태그다.
- central collector-c `a17d8da58e1a`·po-c `8d15b0b5b95b`는 `po-central-cutover.sh deploy`로 교체했고 backup은
  `backups/po-c-20261022T011353Z`다. po-c RestartCount 1은 이전 collector 종료 때의 generation 반납(exit 0,
  `reason=request_aborted`)으로 runbook 수용 기준을 충족한다.
- 첫 collector 시도는 원격 본문이 `compose run`에 stdin으로 읽혀 교체 없이 exit 0으로 끝났다(runtime·태그·설치 파일 무변경).
  `84e540810`으로 고친 뒤 다시 배포했다.
- socket-proxy `1.13.1-go1.27.3` arm64 이미지는 central에 적재만 했다. docker-proxy와 postgres는 재생성하지 않았다.
- AP: b(iris-seoul) backup `backups/seoul-collector-20261022T011608Z`, a(osaka1) current `20261022T011756Z-84e54081071f-osaka`,
  d(osaka2) current `20261022T012254Z-84e54081071f-osaka2`(previous는 둘 다 `20261021-node-26.12.1-108ecb2e60c5`). 세 AP 모두
  completion check를 통과했다.
- 수용 뒤 retention: osaka1·osaka2의 10-18 release·staging 각 4개, central 과거 이미지 태그 13개와 backup·staging·release
  디렉터리 5개, iris-seoul 과거 태그 5개와 backup 1개, kapu 과거 태그 5개를 지웠다. 다른 작업자의 `node-security`·`node-26.12.1`
  태그와 backup은 남겼다.
- 후속: `ADMIN_ALLOWED_IPS` 빈 값의 fail-open은 결정대로 현행 유지했고 별도 과제로 남긴다.

## v12.0.0 릴리스 반영 (2026-10-22)

- 릴리스: v11.0.0 이후 미출시 항목을 `v12.0.0`(`f3d240006b25ca392c1c968e44e605551e288c3f`, #687)으로 묶었다. migration 331·332의
  비가역 적용, `AlarmQueueEnvelope.youtube_outbox` 삭제, 프로필 검증 강화, 텍스트 202 requestId 필수화가 비호환 변경이라 MAJOR다.
  annotated tag와 GitHub Release(이전 게시 릴리스 v11.0.0 기준 자동 생성 본문)를 게시했다.
- 재배포: 코드는 위 배포와 같고 버전 표시만 12.0.0으로 바뀐다. 같은 절차로 central api `bf96a723e5bc`·alarm-worker `d6e61d1dc7d8`
  (`db-migrate` applied=0), collector-c·po-c, AP b·a·d를 교체했다. 모든 컨테이너·서비스가 version 12.0.0, revision `f3d240006`이고
  readiness·completion check를 통과했으며 새 로그에 오류 표지가 없다. po-c broker는 `IDLE`, helper는 아직 generation이 없다.
  po-b RestartCount 1은 exit 0(`reason=session_closed`)이다.
- rollback 지점: central `releases/app-f3d240006-20261022T024132Z`(→ 리팩토링 빌드)와 `app-40fcb7841-20261022T005852Z`
  (→ 리팩토링 이전), po-c `backups/po-c-20261022T024300Z`, AP b `backups/seoul-collector-20261022T024445Z`, AP a·d previous
  `…-84e54081071f-…`. 이번 재배포는 버전 표시만 바꾸므로 24시간 관찰이 끝날 때까지 리팩토링 이전 세대도 명시 보존한다.
  정리 후 두 단계 모두 `compose-rollback-service.sh --check` rc=0이다.
- retention: osaka1·osaka2의 v11.0.0 원본 release·staging(`20261019T015436Z-108ecb2e60c5-final`), central·iris-seoul의
  Node 보안 패치 이전 `po-sandbox:rollback-node-security-20261021` 태그, iris-seoul `backups/node-security-20261021`을 지웠다.
