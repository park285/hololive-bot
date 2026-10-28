# 변경 이력

현재 app 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `v<version>` 형식을
사용합니다. 버전 관리 절차는 [릴리즈 runbook](docs/current/runbooks/release.md)을 따릅니다.
기존 `backup-before-footer-cleanup`은 backup 기준점이고 `shared-go/v0.0.1`,
`shared-go/v0.0.2`는 과거 nested module tag입니다. `[날짜-SHA]` 항목은 버전 관리 도입
전의 실제 commit 기준점이며 app version이나 새 tag를 뜻하지 않습니다.

## 미출시

## v12.4.0 - 2026-10-28

- **동적 제목의 스포일러 해석 방지:** 공통 `MarkdownNeutralize`가 파이프에도 기존 단발 ZWSP 보호를 적용합니다. 영상·방송·출처 메타데이터의 `||`는 가시 문자를 보존하며 숨김 범위가 되지 않고, 템플릿과 답변의 의도된 서식은 유지됩니다.
- **새 Markdown 발송을 RichText로 전환:** bot 및 alarm/notification/YouTube의 새 Markdown 요청은 `SendRichText`와 durable `richtext` kind/route를 사용합니다. `BOT_MARKDOWN_REPLIES`, 기존 방별 선택과 텍스트 경로, 본문·thread·request ID, admission/handoff 정산은 유지합니다. 기존 frozen `markdown`·`text`는 같은 타입으로 재생하며 실패 시 형식을 바꾸지 않습니다. SDK와 보안 workflow checkout은 검증된 `iris-client-go v3.3.0`에 고정합니다.
- **RichText 저장 경로 허용:** migration `345`·`346`은 alarm send unit·YouTube 고정 요청의 CHECK에 `richtext`를 추가하고 기존 행은 수정하지 않습니다. writer 배포 전에 두 migration을 적용해야 하며 새 RichText 요청을 저장한 뒤에는 이를 모르는 구형 writer로 롤백하지 않습니다. disposable PostgreSQL migration·재생 검사와 bot/worker 실제 SDK HTTP 발송 검증을 통과했습니다. 운영 적용은 검증된 API/alarm 이미지와 기존 worker profile을 같은 배포 창에서 교체합니다.
- **PostgreSQL 실행 계획 로그와 버퍼·인덱스 진단:** 중앙 `holo-postgres`가 `auto_explain`을 preload하여 1초 이상 걸린 문장의 계획을 PostgreSQL 로그(Loki)에 남깁니다. 보존 함수 안의 문장도 포함하며, 실제 행 수와 버퍼는 기록하고 노드별 시간 계측과 바인드 값은 남기지 않습니다. 같은 활성화 SQL이 `pg_buffercache` 1.6과 `amcheck` 1.5를 `hololive_observability` 스키마에 만듭니다. 버퍼 조회는 `pg_monitor`가, evict 함수와 amcheck 검사는 관리자만 실행할 수 있습니다. 2026-10-27 서울 primary에 적용했고 preload 키는 `HOLOLIVE_POSTGRES_PRELOAD_LIBRARIES`가 계속 소유합니다. 절차와 복구는 [PostgreSQL observability runbook](docs/current/runbooks/postgres-observability.md)에 있습니다.
- **collector 후보 조회 계획 재사용:** 후보 조회(`repository_candidates_0144_02.sql`)는 runner마다 1초 주기로 실행되며, 운영에서는 하루 약 200만 회입니다. 이 조회는 custom plan 추정 비용이 항상 더 낮아 호출할 때마다 계획을 다시 세웠습니다(운영 계획 0.5~0.7ms, 실행 0.6ms, pg_stat_kcache 실행 CPU 1위). migration 342는 `youtube_collection_targets (projection_generation, observation_kind, subject_key)` 인덱스를 `CONCURRENTLY`로 만듭니다. 343은 기존 본문을 의미 그대로 옮긴 `youtube_collection_job_candidates()` SQL 함수를 만들고, 함수 안에서 generic plan을 고정합니다. 그래서 PG 18의 SQL 함수 plan cache가 세션마다 계획을 한 번만 세웁니다. 후보 판정·순서와 wire 바이트는 같습니다. 343은 PUBLIC의 실행 권한을 회수하고 `hololive_scraper`에 EXECUTE를 줍니다. `hololive_runtime`은 다른 migrator 함수와 마찬가지로 기존 default privileges에 따라 EXECUTE를 갖습니다.
  - 운영 통계를 복원한 로컬 PG 18 측정에서 내부 문장은 6,002회 호출 중 3회만 계획했습니다. 호출당 시간은 848~857µs에서 470~474µs로 줄었습니다.
  - 배포 순서: 새 collector는 함수가 없으면 후보 조회가 실패합니다. 따라서 `hololive-db-migrate`로 342·343을 먼저 적용한 뒤 collector 네 곳을 바꿉니다. 이전 collector는 함수가 생겨도 기존 SQL로 계속 동작합니다. 343은 342의 인덱스가 없거나 invalid이면 함수를 만들지 않고 실패합니다. 인덱스 없이 generic plan을 고정하면 운영 통계에서 targets 전체 parallel seq scan(약 60ms)이 되기 때문입니다.
  - 운영 적용(2026-10-27): 17:47Z `hololive-db-migrate`가 342·343을 적용했습니다(applied=2). 이어서 collector c(17:49Z)·b(17:51Z)·a(17:55Z)·d(17:57Z)를 `ac9577d3d`로 교체했고, 모두 READY와 first_success를 확인했습니다. 같은 snapshot에서 runner 5종의 기존 SQL 결과와 함수 결과가 같았습니다. 교체 뒤 15분 동안 lease 실패와 collector 오류는 0건이었습니다.
  - 운영 실측: 함수 호출은 초당 약 24회이고, pg_stat_kcache CPU는 호출당 1.02ms입니다. 기존 문장은 실행 CPU 0.90ms에, 기록되지 않던 계획 0.5~0.7ms가 더해졌습니다. 절감 폭은 로컬 측정보다 작습니다. 운영 `youtube_collection_job_leases`의 heap이 583행인데도 362페이지로 비대해, generic plan이 매 호출 이 heap을 seq scan(약 360버퍼)한 뒤 hash join하기 때문입니다.
  - lease heap 압축(2026-10-27 23:22Z): 비대는 v11.0.0 전환 직후 lease가 일시적으로 10,519행까지 늘었던 흔적입니다. live 행이 361번 블록까지 흩어져 있어 autovacuum이 줄이지 못했습니다. collector를 운영 중인 상태에서 `lock_timeout=1s`로 `VACUUM (FULL, ANALYZE)`를 실행해 44ms 만에 heap을 362→27페이지, 인덱스를 1,644→18페이지로 줄였습니다. 직후 110초 동안 함수의 호출당 버퍼는 377→57, CPU는 1.02→0.85ms였고, lease 실패는 0건이었습니다.
- **collector global 후보 조회와 lease membership 판정의 계획 재사용:** global 후보 조회(`repository_candidates_global_0144_17.sql`, 운영 하루 약 130만 회)는 `(NOT $3 OR subject_key = $4)` 조건 때문에 custom plan 추정이 항상 낮아, 호출마다 계획을 다시 세웠습니다(운영 계획 0.51~0.58ms, 실행 0.16~0.26ms). generic plan의 실행 시간은 0.22~0.31ms입니다. migration 344는 이 본문을 그대로 옮긴 `youtube_collection_job_global_candidates()` SQL 함수를 만들고 함수 안에서 generic plan을 고정합니다. lease membership 검사(하루 약 40만 회)가 부르는 `youtube_collection_membership_valid()`에도 같은 OR 조건이 있습니다. 그래서 `plan_cache_mode = force_generic_plan`을 지정합니다(운영 0.37~0.66ms → 0.19~0.49ms). 판정과 wire 바이트는 같습니다. 새 함수 권한은 343과 같습니다. PUBLIC 권한을 회수하고 `hololive_scraper`에 EXECUTE를 주며, `hololive_runtime`은 기존 default privileges에 따라 EXECUTE를 갖습니다.
  - 배포 순서: 342·343과 같습니다. `hololive-db-migrate`로 344를 먼저 적용한 뒤 collector 네 곳을 바꿉니다. 344도 342 인덱스가 없거나 invalid이면 실패합니다.

## v12.3.1 - 2026-10-27

- **의존성(shared-go v2.17.0·iris-client-go v3.2.0):** 두 라이브러리가 공개 API를 지우거나 이름·시그니처를 바꿨기 때문에 같은 변경에서 코드를 함께 옮깁니다. hololive-api·hololive-alarm-worker·hololive-shared는 두 모듈을 모두 올리고, hololive-youtube-collector·hololive-dbtest는 shared-go만 올립니다. go.sum에서는 이 두 모듈 줄만 바뀝니다.
  - `workercontract.LoadProfileFromEnv`가 서비스·역할 문자열 대신 `Identity`를 받습니다. 그래서 각 runtime이 자기 worker 집합을 직접 소유합니다. api는 `APIWorkerIdentity()`(`bot_reply_outbox`·`bot_webhook_inbox`·`source_observation`), alarm-worker는 `WorkerIdentity()`(`alarm_dispatch`·`notification_delivery`·`youtube_delivery`), collector는 `WorkerIdentity()`(`collection`)를 둡니다. 세 함수 모두 호출할 때마다 새 슬라이스를 돌려주며, 값은 shared-go에서 지운 내장 표와 같습니다. `settingstest.LoadProfileFixture`는 이제 `(t, identity, name)`을 받습니다.
  - 세 main의 `Initialize`에서 `automaxprocs.Init(nil)`을 지웠습니다. NewLogger가 있어 쓰이지 않던 `LoggerFileName`도 지웠으며, 로그 파일 이름은 전처럼 각 NewLogger가 정합니다. `NormalizeRuntimeBuildInputs`가 이제 정규화한 logger도 함께 돌려주므로, alarm-worker·admin·bot runtime 조립은 그 logger를 씁니다.
  - bot H3 인증서 갱신은 자체 ticker(`runBotCertificateReload`) 대신 `h3.CertificateReloader.Run`을 씁니다. 실행 위치는 기존 `bot-h3-certificate-reload` 작업(tasksWG·panicguard) 안입니다. 갱신 주기와 실패 로그 `h3 certificate reload failed; using previous certificate`는 그대로입니다. reloader를 정상 생성한 뒤에는 닿지 않던 bot 전용 Warn `bot_h3_certificate_reload_failed`는 없어집니다.
  - bot webhook은 이제 `webhook.NewDurableHandler`로 만들고, `WithDedupTimeout` 대신 `WithNonceTimeout`(profile `dedup_timeout_ms`)을 넘깁니다. durable 경로에서 쓰이지 않던 `WithDedupTTL`과 `WebhookConfig.DedupTTL`은 지웠습니다. profile의 `dedup_ttl_ms`는 계약이므로 계속 읽고 검증합니다. `ManagedIrisClient`는 삭제된 `iris.Client` 대신 `iris.BotClient`를 내장하고, H3 dial guard는 `iris.WithH3DialGuard`로 등록합니다. webhook token 필수 검사와 HTTP status 매핑은 바뀌지 않습니다.
  - 테스트에서는 `logging.NewTestLogger`를 `slog.New(slog.DiscardHandler)`로 바꿨습니다. `httputil.NewJSONClient`·`IsStatus`는 `NewJSONClientWithHTTPClient`·`errors.AsType[*httputil.APIError]`로 바꿨고, 삭제된 `GetConfig`는 stub에서 뺐습니다.
  - 보안 workflow(`security.yml`)가 sibling으로 checkout하는 shared-go·iris-client-go 고정 SHA도 v2.17.0·v3.2.0으로 올렸고, `deploy/compose/README.md`의 pin 표기도 맞췄습니다. 이 workflow는 go.work로 sibling 소스를 컴파일하므로, 옛 SHA로 두면 gosec·NilAway·govulncheck가 새 API 호출에서 컴파일에 실패합니다.
- **bot webhook 지표 삭제:** hololive-api가 다음 지표를 더 등록하지 않습니다. `hololive_bot_webhook_dedup_latency_seconds`·`hololive_bot_webhook_enqueue_wait_seconds` histogram(`_bucket`·`_sum`·`_count`)과 `hololive_bot_webhook_queue_depth` gauge입니다. iris-client-go v3.2.0이 세 관측 콜백을 지웠습니다. hololive가 쓰는 durable 수신 경로는 이전에도 이 콜백을 부르지 않았으므로, 세 지표는 등록만 되어 늘 0(histogram은 `_count` 0)으로 노출됐습니다. 이 이름을 조회하는 대시보드·알림·쿼리는 함께 지웁니다. 나머지 `hololive_bot_webhook_*` 지표의 이름과 값은 그대로입니다.
- **기동 로그·오류 문구(shared-go v2.17.0):** 아래 기동 로그와 오류 문구가 바뀝니다.
  - hololive-api·alarm-worker·youtube-collector는 기동할 때 서비스 logger로 automaxprocs 결정을 1줄 남깁니다. 기본값은 `automaxprocs skipped; Go 1.25+ runtime handles container-aware GOMAXPROCS`이고, `AUTOMAXPROCS_DISABLE`을 켜면 `automaxprocs disabled by env`입니다. 이 결정 시점은 프로세스 시작 직후에서 설정 로드·logger 생성 뒤로 옮겨지지만 `BuildRuntime`보다는 여전히 먼저입니다. 시점 차이는 `AUTOMAXPROCS_FORCE`를 켤 때만 드러나며, 저장소의 Compose와 env 예시는 두 env를 설정하지 않습니다.
  - shared-go 공개 경계 오류가 `<pkg>: `로 시작하므로 기동 실패와 runtime 오류 로그의 문구가 바뀝니다. 예는 다음과 같습니다.
    - `read int64 env: envutil: invalid int64 env <KEY> (...)`
    - `workercontract: STACK_WORKER_PROFILE_FILE is required`
    - `httpserver: listen: ...`(이전 `http server listen: ...`)
    - `lifecycle: run: ...`
    - `h3: load certificate pair: ...`
    - `promptguard: guard unavailable`(이전 `prompt guard unavailable`)
    - `panicguard: <작업>: recovered panic: ...`
    - plane 간 내부 HTTP client의 `httputil: status <N> ...`·`httputil: decode JSON: ...`
    - collector healthcheck smoke의 `healthprobe: fetch URL: ...`
  - 중간 wrap도 일부 바뀝니다. migration runner의 dbmigrate advisory lock 오류에서는 `fn: `·`acquire advisory lock: `이, worker profile 로드 오류(기동 실패·`--check-worker-profile` 출력)에서는 `load profile file: `이 빠집니다. 종료 오류에는 `lifecycle: shutdown: `이, 종료 때 stderr에 남는 logger 닫기 오류에는 `logging: close log lanes: `가 새로 붙습니다. wrap은 모두 `%w`라 오류 판정은 같습니다. exit code·env 이름·metric 이름·HTTP 응답 본문도 바뀌지 않습니다.
- **Iris client 응답 처리(iris-client-go v3.2.0):** reply 전송처럼 2xx 본문을 버리는 요청의 본문이 멈추면, 이제 요청 deadline까지 기다리지 않습니다. `min(100ms, Timeout)`이 지나면 요청을 끊고 성공으로 반환합니다. typed 응답의 trailer drain과 Close는 호출 goroutine에서 실행합니다. 그래서 `iris: response body close timed out` 오류와 `iris_client_response_body_drain_panic`·`iris_client_response_body_close_panic` 로그가 없어지고, panic은 호출자로 전파됩니다. `IRIS_H3_CA_RELOAD_INTERVAL`은 더 읽지 않으며, H3 CA 파일은 client를 만들 때 한 번만 읽습니다. 저장소 설정에는 이 env가 없으므로 동작은 같습니다. CA 파일을 설정하지 않았을 때의 오류 문구는 `iris: no H3 CA cert file configured; set IRIS_H3_CA_CERT_FILE or opt in with IRIS_H3_ALLOW_SYSTEM_ROOTS`입니다.
- **plane 종료 panic 격리:** hololive-api의 plane 묶음(`applifecycle.GroupRuntime`)은 plane을 역순으로 종료합니다. 이제 이 과정에서 한 plane의 Shutdown이 panic하면 `panicguard`가 잡아 오류로 바꾸고, 남은 plane 정지를 계속합니다. 이때 `background goroutine panic recovered`(`guard`=plane 이름)와 `runtime component shutdown failed: <plane>` 로그가 남습니다. 종료 오류는 `shutdown <plane>: panicguard: <plane>: recovered panic: ...` 형태로 합쳐집니다. 이전에는 panic이 종료 반복문을 빠져나가 뒤 순서의 plane 정지를 건너뛰었습니다.
- **Seoul AP 전송 파일 축소:** `ap-deploy.sh`가 AP로 보내는 `scripts/deploy/ap-rsync-files.txt`를 453줄에서 23줄로 줄였습니다. AP는 kapu가 빌드한 이미지 아카이브를 받을 뿐 이미지를 직접 빌드하지 않으므로, AP가 읽거나 실행하는 파일만 남겼습니다.
  - 남은 파일은 Compose 파일 3개(collector·prod·seoul), `compose.sh`와 lib, 원격 확인·rollback 스크립트(`po-ap-rollback-remote.sh`·`worker-profile-pair.py` 등), runtime VERSION 2개, `postgres-capacity-policy.tsv`입니다.
  - 더 보내지 않는 파일은 Go 소스·SQL·`go.mod`·`go.sum`·`go.work`·`go.work.sum`, Dockerfile과 po-sandbox 소스, `scripts/build/*`, `ap-hosts/*.conf`와 kapu에서만 실행하는 `ap-collector-preflight.sh`·`lib/ap-host.sh`·`ap-rsync-files.txt`, osaka·osaka2 Compose 파일, `deploy/topology/hosts.conf`, 루트 `VERSION`, shared-go 파일입니다.
  - collector go.mod가 고정한 shared-go tag를 `git archive`로 꺼내던 단계와 `SHARED_GO_DIR`도 지웠습니다. 이제 shared-go pin과 AP 배포가 묶이지 않습니다.
  - source snapshot·source-candidate·rollback 범위도 새 목록을 따릅니다. `rsync --files-from`은 원격 파일을 지우지 않으므로, 이전 배포가 AP의 `~/hololive-bot`·`~/shared-go`에 남긴 사본은 그대로 남습니다. 현재 revision의 명령은 이 사본을 읽지 않습니다. 다만 변경 전 백업이나 source snapshot으로 rollback하면 되돌아온 이전 `compose.sh`가 `~/shared-go`를 필수로 확인하므로, 변경 전 백업이 모두 만료되고 live `compose.sh`가 새 revision으로 확정될 때까지 AP의 `~/shared-go`를 지우지 않습니다(youtube-collector runbook).
- **AP 전송 목록 검사:** `check-ap-rsync-manifest.sh`를 `go list` 없이 다시 써서 이제 Go 없이 실행됩니다. 검사 항목은 다음과 같습니다.
  - 저장소 안의 symlink가 아닌 일반 파일인지, 중복 항목이 없는지 확인합니다.
  - 배포 범위를 지키는지 확인합니다. 비밀·데이터·문서·로그·백업·`_test.go`는 제외하고, alarm-worker에서는 `VERSION`만 허용합니다.
  - AP 원격 명령 입력 10개와 Compose 모드 AP host overlay가 목록에 있는지 확인합니다.
  - 보내는 스크립트가 참조하는 `scripts/…`·runtime VERSION이 포함됐는지 확인합니다.
  - `ap-deploy.sh`는 이전의 자체 범위 반복문과 전송 tree 준비 뒤 재검사 대신, 원격에 접속하기 전에 이 검사를 한 번 실행합니다. alarm-worker의 `go.mod`·`go.sum`은 이제 목록 검사와 rsync preview 검사(`check-ap-rsync-preview.sh`) 양쪽에서 거부됩니다.
- **shared-go workspace 요구 제거:** `compose.sh`·`build-all.sh`·`compose-redeploy-service.sh`가 형제·내장 `shared-go` 디렉터리를 더 요구하지 않습니다(build-all은 `iris-client-go`도 마찬가지입니다). 그래서 `[ERROR] Active shared-go workspace not found`로 멈추는 일이 없어집니다. 세 스크립트는 `SHARED_GO_WORKSPACE_PATH`·`IRIS_CLIENT_GO_WORKSPACE_PATH`를 export하거나 `[INFO]`로 출력하지도 않습니다. `compose-env.sh`도 두 키를 허용 shell 제어 키에서 뺐습니다. Compose 파일은 이 값을 보간하지 않고 Dockerfile은 `GOWORK=off`로 go.mod pin을 빌드하므로, 렌더·빌드 결과는 같습니다. 새 전송 목록만으로 만든 AP tree와 rollback source-candidate에서도 `compose.sh`가 이 검사 때문에 실패하지 않습니다. architecture 검사 스크립트 2개는 여전히 `SHARED_GO_WORKSPACE_PATH`를 선택 경로로 읽습니다.
- **관리 앱 표기:** `admin-bind.sh`의 admin source 거부 오류가 `[ERROR] Admin sources must be loopback or the approved Iris Console host`로 바뀝니다(이전 `... Iris Admin host`). 허용 대상(`127.0.0.1/32`·`::1/128`·`100.100.1.5/32`)은 그대로입니다. `.env.example`·`deploy/compose/README.md`·`scripts/README.md`와 현행 문서는 관리 앱 소유자를 iris-seoul의 Iris Console(`iris-console.service`)로 적습니다. 발송 원장 runbook은 iris-console의 OpenAPI 정본과 Go gateway 경로(`/admin/api/holo/dispatch/...`)를 기준으로 고쳤습니다.
- **Valkey·PostgreSQL client 세부 동작:**
  - alarm-worker의 멤버 이름 조회(`cache.BatchHGet`)는 필드마다 보내던 HGET pipeline 대신 HMGET 한 명령을 보냅니다. 빈 값은 전처럼 없는 값으로 봅니다. 실패하면 `hmget` 작업의 cache 오류를 돌려주고 `Cache batch hget failed`를 한 번 기록합니다. 응답 개수가 필드 수와 다르면 오류입니다.
  - `GetJSON`은 응답 바이트를 바로 decode하며, 실패 wrap이 `get string:`에서 `get:`으로 바뀝니다.
  - `PostgresService.Close`는 이제 pool을 nil로 바꾸지 않습니다. 그래서 닫힌 뒤의 `Ping`은 `ping database: pool is nil` 대신 pgxpool의 닫힌 pool 오류를 돌려줍니다.
- **source observation 직렬화의 UTF-8 검증:** `canonicalJSON`·`MarshalPayloadV1`에서 reflect로 모든 문자열을 미리 훑던 검사를 지우고, 거부를 jsonv2 인코더에 맡깁니다. map key와 중첩 값을 포함해 잘못된 UTF-8은 계속 거부됩니다. 다만 오류 문구가 `canonical json string contains invalid UTF-8` 대신 인코더 오류(`marshal: ...`·`marshal source observation payload: ...`)로 나옵니다. 중첩 깊이 상한(128)은 이제 `CanonicalizeJSON`이 JSON 깊이 기준으로만 검사하므로, 깊이 초과 문구는 `canonical json value nesting exceeds 128` 대신 `canonical json nesting exceeds 128`입니다.
- **그 밖의 로그·오류 문구:** alarm-worker의 구독자 cache 조회 실패 오류가 짧아집니다. 대상은 cache 성공이 필수인 resolver이며, 새 문구는 `resolve channel subscribers by type: lookup channel <id> type <type>: ...`입니다(이전 `... lookup channel subscribers by type: channel <id> type <type>: ...`). Holodex 사진 동기화 로그 `Syncing photos from Holodex`에서는 늘 false였던 `force_all` 속성이 빠집니다.
- **alarm HTTP 서버 쪽을 worker로 이동:** `/internal/alarm/*`의 handler·route registrar(`NewInternalRouteRegistrar`)·binding DTO를 hololive-shared `pkg/service/alarmhttp`에서 alarm-worker `internal/service/alarm/httpapi`로 옮기고, service interface 이름을 `AlarmService`로 바꿨습니다. shared `alarmhttp`에는 hololive-api가 쓰는 client만 남습니다. route·인증·HTTP status·응답 바이트는 같습니다. handler↔client 왕복 테스트는 worker로 옮겼고, api admin 테스트는 worker 응답 envelope를 돌려주는 net/http stub으로 바꿨습니다.
- **cache 의존 축소:** 운영 코드에서 `cache.Client` 전체를 받던 소비자가 이제 실제로 쓰는 작업만 묶은 interface를 받습니다. 대상은 다음과 같습니다.
  - alarm-worker: 구독 resolver·rebuild·dedup·queue publisher·scheduler·YouTube checker·youtubedispatch
  - hololive-api: 관리자 인증·router·bot·LLM scheduler·delivery module
  - Holodex provider
  - `cache.Client`는 process가 소유하는 `InfraModule`에만 남습니다. readiness의 `PostgresCheck`·`ValkeyCheck`는 `readiness.Pinger`·`ConnectionChecker`를 받습니다. 호출자가 하나뿐이던 `LookupChannelSubscribersByType`는 resolver 안으로 합쳤고, 쓰이지 않던 `handlercore.Dependencies.Cache`는 지웠습니다.
- **미사용 코드 삭제:** 쓰이지 않던 코드를 지웠습니다. 운영 SQL·테이블명과 CHECK 제약 값인 `MajorEventStatusCanceled`는 그대로 둡니다.
  - observation 단건 래퍼(`Upsert`·`UpsertAlarmState`·`UpsertSourcePost`)와 `ReadRepository`·`WriteRepository`·`Repository` interface를 지웠고, `NewRepositoryContext`는 `NewRepository`로 합쳤습니다. 배치 쓰기는 그대로입니다.
  - member 저장소의 조회 4개(`GetMemberWithPhotoByChannelID`·`FindAllByName`·`FindByNameAndOrg`·`GetPhotoByChannelID`)와 SQL 파일 4개를 지웠습니다.
  - 호출자가 없던 `PhotoSyncService.SyncAll`과 전체 동기화 경로를 지웠습니다.
  - domain의 GORM 잔재 `TableName` 16개·`YouTubeChannelProfile`·`YouTubeModels`를 지웠습니다.
  - `MajorEvent.EventDates`·`HasEventDates`·`SetEventDatesFromParsed`·`IsNotified`, `Member.HasAlias`, `Channel.IsHololive`·`GetPhotoURL`, `Stream.TimeUntilStart`, `AlarmNotification.UserCount`를 지웠습니다.
  - `KakaoConfig.SnapshotACL`·`SetACLEnabled`, `SendResult.Merge`, `constants.IrisWebhookDedupTimeout`, `CommunitySchemaVersionV1`을 지웠습니다.
  - collection failure accessor 4개와 `scannableTime.Require`를 지웠습니다.
  - `envload.TrimmedEnv`를 지우고, trim 의미가 같은 `envutil.String`으로 대체했습니다.
- **패키지·구현 정리:** 패키지 위치와 내부 구현을 정리했습니다.
  - `ratelimit.Decision`을 `ratelimit/valkey`로 옮기고, `internal/testutil`(cacheclient 포함)을 `pkg/testutil`로 합쳤습니다.
  - HTTP/3 전송 별칭 판정은 서버 전송과 Iris runtime 전송이 `settings.IsHTTP3Transport` 하나를 함께 씁니다.
  - `applifecycle`에서 운영에 쓰이지 않던 `StartHooks.StartBot`·`StartH3CertReload`를 지우고, `ShutdownHooks`를 scheduler 취소와 HTTP 종료로 줄였습니다. bot runtime이 HTTP→webhook·durability→bot 종료를 직접 이어 실행하고 오류를 합칩니다. 종료 순서·로그·오류 문구는 같습니다.
  - httpserver의 `StartH3Server`·`NewH3Server`와 middleware의 `NoRouteHandler`·`ParseClientHints`는 비공개로 바꿨습니다.
  - Prometheus 지표 지연 초기화는 `sync.OnceValue`·`sync.OnceFunc`로 바꿨습니다. 지표 이름과 label은 같습니다.
- **테스트·gate 정리:** 지운 코드만 검증하던 테스트는 함께 지웠습니다. 새로 더한 테스트는 bot runtime 종료 순서, plane 종료 panic 격리, map key·중첩 값의 잘못된 UTF-8 거부입니다. `public-pr-frontend-gate.sh`에서는 이미 지운 이름(`IRIS_ADMIN_WEB_*` env, `iris-admin-credentials` mount)이 없는지 보던 검사를 빼고, docker-proxy 연결과 30192 shortlink ingress만 검사합니다. 배포 shell 테스트는 shared-go·iris-client-go 가짜 workspace를 더 만들지 않습니다. `test-compose-services.sh`는 자체 목록 단언 대신 `check-ap-rsync-manifest.sh`를 실행합니다.
- **문서:** 현행 문서(`docs/current`)의 존재하지 않는 경로와 지운 심볼 참조를 고쳤습니다. alarm HTTP 소유 경로도 맞췄습니다(`CONTRACT_MAP.md`·`CONTRACT_MANIFEST.txt`·`PROJECT_MAP.md`·`SERVICE_OWNERSHIP.md`, contracts·runbooks·services). `docs/README.md`·`repo-tree-policy.md`에서는 저장소에 없는 `docs/superpowers` 안내를 지웠습니다. migration `CONVENTIONS.md`의 dbtest 경로는 `hololive/hololive-dbtest`로 바로잡았습니다.
- **활성 worker-profile pair 저장 위치:** 중앙 collector-c PO 배포와 서울 AP collector-b 배포가 활성 pair를 수용 후 정리되는 백업 디렉터리(`backups/po-c-*`, `backups/seoul-collector-*`)에 두었습니다. 2026-10-26 백업 정리 뒤 Compose가 지워진 경로를 bind하려다 중앙 부팅 경로가 `youtube-collector`에서 실패했습니다. 이제 두 배포와 AP 롤백은 pair를 `artifacts/worker-profile/releases/`에 만들고, `worker-profile-pair.py activate`는 그 밖의 pair를 거부합니다.

- **`/opt` 동기화의 형제 저장소 복사 삭제:** `scripts/deploy/sync-opt-current.sh`가 `/opt/hololive-bot/compose/shared-go`·`iris-client-go`를 더 갱신하지 않습니다. 이번 릴리스의 `compose.sh`는 형제 저장소를 요구하지 않습니다. 기존 디렉터리는 지우지 않으므로, 변경 전 백업으로 되돌린 `compose.sh`가 확인하는 경로는 그대로 남습니다.

## v12.3.0 - 2026-10-26

- **방송 유형 분류 보완(규칙 `2026-10-26.1`):** 방송 이력의 유형 표시와 `type` 필터에 쓰는 분류기가 게임으로 매핑한 Holodex 주제 이름을 첫 제목 태그에도 적용해, 주제 없이 `【Forza Horizon 6】`처럼 게임명만 있는 방송을 게임으로 봅니다. 신규 게임 주제 68개와 VSPO 게임 표기(`VALO`·`#LTK`·`TFT`·`スクリム`), 공식 MV·커버, 切り抜き 동시시청, 인사말 잡담 등을 추가했고, 슈퍼챗 밀린 읽기(`SC KETCHUP`)를 게임에서 잡담으로 바로잡았습니다. 종료 방송 10,807건 기준 미분류가 9.8%에서 5.3%(2026-09-11 이후 14.6%에서 3.8%)로 줄었고 기존 유형은 24건이 바뀌었습니다. 결과는 [3차 보완 보고서](docs/review/classifier-refinement-20261026.md)에 있습니다.

## v12.2.2 - 2026-10-26

- **인프라 이미지 CA 차단을 빌드에 고정:** PostgreSQL·Nginx·Valkey 이미지가 Alpine `ca-certificates-bundle`을 `20261020-r0`으로 고정합니다. 이 판은 2026-10-22 eMudhra 대응으로 차단한 두 루트(emSign ECC Root CA - G3, AAA Certificate Services)를 bundle에서 제거했습니다. 운영의 세 서비스는 2026-10-22에 base 이미지 위에 차단 bundle을 덧입힌 파생 이미지로 돌고 있어, 저장소 recipe로 다시 빌드하면 차단이 빠졌습니다(고정 base의 `20260909-r0`에는 한 루트가 남아 있습니다). 이제 세 이미지의 bundle이 같고(SHA-256 `5961c0a3c6545993…`) 두 루트가 없습니다. PostgreSQL은 이미 `libcurl` 의존성으로 이 판을 받고 있었으며, 고정으로 그 사실을 명시합니다.

- **node 런타임 musl 로더 1.2.6-r3:** alarm-worker·youtube-collector·po-sandbox 이미지가 node alpine base에서 복사하던 `ld-musl`(alpine 3.24.2의 musl `1.2.6-r2`, CVE-2026-107891) 대신 digest로 고정한 `alpine:3.24.3`의 `musl 1.2.6-r3` 로더를 복사합니다. build-time `apk add`나 대상 아키텍처 실행이 없어, production 이미지의 apk 설치 금지 계약과 에뮬레이션 없는 arm64 교차 빌드(`ap-deploy.sh`, issuer artifact)가 그대로 유지됩니다. 이 이미지들은 apk DB가 없어 Trivy가 로더 판을 보지 못합니다. node·libgcc·libstdc++·libatomic은 이전처럼 node base에서 가져옵니다.

- **Alpine musl 1.2.6-r3 반영:** PostgreSQL·Nginx·Valkey 이미지가 고정 base의 `musl`·`musl-utils`를 `1.2.6-r3`(CVE-2026-107891)으로 올립니다. PostgreSQL은 `brotli-libs`를 `1.2.1-r0`(CVE-2026-111497)으로, `libcurl`을 `8.23.0-r0`(CVE-2026-92392 등 24건)으로 올립니다. 2026-10-26 security 재실행이 PostgreSQL 이미지의 `musl 1.2.6-r2`를 Go 밖 취약점으로 잡아 실패했고, 스캔이 첫 실패에서 멈춰 아직 검사되지 않은 Nginx·Valkey base도 같은 판이었습니다. 로컬 이미지 태그(`…-zlib1.3.2-r1`)는 바꾸지 않습니다.

- **distroless ca-certificates 갱신:** hololive-api·alarm-worker의 runtime base `gcr.io/distroless/static-debian13`을 `ca-certificates 20250419+deb13u1`(Debian DSA-6572-1)이 든 다이제스트로 올렸습니다. 2026-10-25 예약 security 스캔이 이전 base의 `ca-certificates 20250419`를 Go 밖 취약점으로 잡아 실패했습니다. 새 패키지가 빌드 단계에서 빼던 두 루트(emSign ECC Root CA - G3, AAA Certificate Services)를 직접 제거해, 두 이미지의 `ca-distrust.sh` 파생 단계는 지울 루트가 없어 빌드를 실패시키므로 제거했습니다. 이미지의 CA bundle은 이전 파생 bundle과 같습니다(SHA-256 `e9c7b63ee2623386…`). collector는 alpine bundle에서 계속 두 루트를 뺍니다.

## v12.2.1 - 2026-10-25

- **logger 닫기 오류 보고(shared-go v2.16.1):** api·alarm-worker·collector가 종료할 때 logger closer의 오류(로그 파일 닫기 실패 등)를 버리지 않고 마스킹해 stderr에 남깁니다. exit code는 runtime 결과를 그대로 따릅니다.
- **hololive-api Fx 제거:** `go.uber.org/fx`를 걷어내고 youtube-collector·alarm-worker와 같은 shared-go `bootstrap`·`lifecycle.Run` 조립으로 기동·종료합니다. 포트·설정·plane 계약, 종료 순서, drain 10초·process stop 30초 강제 상한, fatal 시 exit 1과 즉시 기록되는 `hololive-api runtime error`는 그대로입니다. Fx 전용 로그(`OnStart hook failed`, `hololive-api Fx stop failed` 등)는 사라지고 종료 요약은 `Runtime stopped with error`로 남습니다. 달라진 점: telemetry flush 실패는 로그만 남기고 exit code를 바꾸지 않으며, drain 중 두 번째 SIGTERM/SIGINT는 정리 없이 즉시 종료합니다.

## v12.2.0 - 2026-10-25

- **원본 보존이 보호된 창을 지나 같은 tick에서 계속 정리:** 원본(`source_observations`) 보존은 tick마다 만료 후보 창(최대 1,000건) 하나만 처리했습니다. 운영에서는 창의 대부분이 LIVE·UPCOMING 보호 행이라 삭제 가능한 뒤쪽 정리가 늦어졌습니다.
  - 한 tick에서 창을 최대 8개까지 이어서 처리합니다. 삭제 0건이어도 커서가 전진하면 다음 창으로 넘어가고, 순회 완료·진척 없음·창 상한·시간 예산·오류에서 멈춥니다.
  - 각 창은 이전처럼 독립 트랜잭션으로 커서와 삭제를 함께 커밋합니다. cutoff는 tick 시작 값으로 고정하고, 기존 DB 작업 시한(기본 10초)을 공유합니다. 첫 창 뒤에는 남은 시한의 절반 안에 끝날 것으로 보이는 창만 시작해, payload·absence·checkpoint 정리와 적체 측정에 시간을 남깁니다.
  - 뒤 창이 실패해도 앞 창의 확정 삭제량은 보고하며, 실패한 창은 다시 실행하지 않습니다.
  - 새 지표: `hololive_youtube_plane_retention_source_scanned_total`(커서가 지난 후보 수, 삭제분 포함), `hololive_youtube_plane_retention_source_traversals_total`(순회 완료 수).
  - migration 341: 창 하나의 결과(삭제 id, 지난 후보 수, 순회 완료)를 돌려주는 `delete_source_observation_retention_window`를 추가하고, 기존 `delete_source_observation_retention_batch`는 반환 계약을 유지한 채 이 함수에 위임합니다. 보호 조건, 잠금 순서, 창 크기, 커서 규칙은 바뀌지 않습니다.

## v12.1.2 - 2026-10-25

- **방송 탭 예정 영상 조회 지불 누락 보완:** v12.1.0은 두 경우에 helper가 이미 보낸 예정 영상 player 조회를 Go limiter에 지불하지 않았습니다. 이 수정으로 두 경우 모두 다음 RPC들이 그만큼 간격을 둡니다.
  - 지불을 기다리는 중 수집이 끝나면, 남은 조회를 기다리지 않고 limiter에 예약합니다.
  - helper에 요청을 다 보냈는데 검증된 helper 본문을 받지 못하면(HTTP 시한 초과·취소·연결 끊김·잘못된 본문), 요청한 상한(32)만큼 예약합니다. helper는 요청이 끊기면 조회를 중단하므로 상한이 실제 조회 수의 상한입니다.
  - 요청을 보내기 전에 실패했거나, helper 오류 본문이 조회 수를 생략했으면(0건) 예약하지 않습니다.
  - wire 계약, 오류 분류, 재시도 동작은 그대로입니다.

## v12.1.1 - 2026-10-25

- **이미지 CA 신뢰 차단을 빌드에 고정:** 2026-10-22 eMudhra 중간 CA 침해 대응으로 운영 이미지에 덧입혀 두었던 CA bundle 차단(`emSign ECC Root CA - G3`, `AAA Certificate Services`)이, 저장소 배포 진입점이 이미지를 다시 빌드할 때 빠졌습니다. v12.1.0 collector c·b가 그 상태로 배포됐습니다.
  - 이제 collector·hololive-api·alarm-worker Dockerfile의 빌드 단계에서 `scripts/build/ca-distrust.sh`가 `ca-distrust.sha256`의 루트를 bundle에서 뺍니다. bundle에 PEM 블록이 아닌 내용이 있거나 차단 루트를 하나도 찾지 못하면 빌드가 실패합니다.
  - 결과 bundle은 이전에 검증한 파생 이미지 bundle과 인증서 순서까지 같습니다.
  - alarm-worker artifact도 함께 다시 빌드하므로 `hololive/hololive-alarm-worker/VERSION`을 12.1.1로 맞춥니다.

## v12.1.0 - 2026-10-25

- **의존성:** `openai-go/v3` v3.86.0·`quic-go` v0.64.1과 Go 전이 의존성, collector의 `youtubei.js` 18.3.0을 갱신합니다.
- **YouTube.js helper 판정 소유권:** YouTube.js 18.2부터 `Video.is_live`가 재생 시간 표시가 없으면 라이브로 추정하고 `contentType` 없는 lockup을 오류 없이 영상 목록에서 뺍니다. helper는 더 이상 SDK `is_live` 추정값을 쓰지 않고 원문의 구조화된 표시(LIVE badge style, `Upcoming`·재생 시간 배지, LIVE_NOW 배지·time-status·`upcomingEventData`)로만 streams 상태를 정하며, 표시가 없는 행은 종료로 추정하지 않고 `parser_drift`로 거부합니다(범프 전에는 배지 없는 lockup을 ENDED로 보고했습니다). 본문·continuation 페이지의 원문 행 수와 SDK 목록 길이를 대조해 미지 renderer·null·분류되지 않은 lockup의 조용한 누락과 중첩 shelf의 추가를 거부하고, 제목 노드 없는 lockup도 거부합니다. 공유 게시물 안의 원본 게시물은 행에서 제외해 중복 수집도 사라집니다. continuation은 SDK와 같은 endpoint 호출을 유지하면서 원문을 보존합니다. 범프 전부터 SDK가 진단 없이 빼던 본문 행이 성공 응답으로 통과하던 문제도 함께 닫힙니다.
- **YouTube.js helper wire 계약 검증:** Go 클라이언트와 helper가 같은 fixture(`youtubejs/testdata/wire-contract.json`)로 실패 tuple·retry hint·endpoint별 유효·무효 본문을 검증하고, 실제 `server.mjs`를 띄운 오프라인 왕복 테스트를 둡니다. Go의 최소 성공 응답 크기는 DTO를 marshal해 계산하며 `/v1/channel`의 171·160 불일치가 사라집니다.
- **방송 탭 예정 영상 조회 예산:** live 채널 RPC가 조회 상한 `max_schedule_lookups`(32)를 보내고, 시각 없는 예정 영상이 상한을 넘으면 player 요청 전에 `parser_drift` 대신 `response_too_large/RESOURCE_LIMIT`로 거부합니다. helper는 성공·실패 응답에 실제 조회 수를 싣고 collector는 그 수만큼 기존 limiter 예약을 하나씩 기다리며, `youtubejs_channel_live` 호출 예산은 RPC 1회와 조회 32회입니다. 기본 설정의 수집 시한은 약 39초에서 약 21분으로 늘어납니다. collector와 helper는 exact-key 짝 변경이라 같은 image로만 동작합니다.
- **예정 최초공개 판정:** 게시 시각 판정이 `waiting_state_confirmed`를 직접 요구하고, helper는 `scheduled_at`이 있으면 대기 상태 확인을 필수로 검증합니다.
- **continuation 일시 간극:** 부분 결과(`continuation_transient`)를 연결·본문 수신 실패, transport가 분류한 5xx, 시간 초과로 한정합니다. HTTP 4xx는 기존 분류로 실패하고 그 밖의 SDK 예외는 `parser_drift`입니다.
- **helper PO 발급 상태 메트릭:** scrape마다 helper `/health`를 읽어 `youtubejs_po_proof_state{state}`, `youtubejs_po_proof_last_error{code}`, 발급·부착 누계와 `youtubejs_helper_health_scrape_success`를 냅니다. 새 소스를 AP rsync 목록에 추가했습니다.
- **공유 수집 Innertube 초기화:** bootstrap 중 READY 전에 upstream config 조회 없이(`retrieve_innertube_config: false`) 만들고, 실패하면 helper가 FAULTED로 종료합니다.
- **취소된 PO mint:** collector RPC 취소로 중단된 mint 뒤에는 broker가 퇴역시킨 세대를 재사용하지 않고 `UNAVAILABLE`·`last_error=mint_aborted`로 다음 발급을 기다립니다.
- **Node helper heap 상한:** helper를 `--max-old-space-size=192`로 띄워 V8 old space를 collector 메모리 상한 안의 Node 몫으로 제한합니다.

## v12.0.1 - 2026-10-25

- **publish set 임시 파일 원인 제거(migration 340):** `lock_source_observation_identity`는 `uq_source_observation_identity` 전체 키 조회라 최대 1행인데 SETOF 기본 추정 1000행이 남아 있어, collector publish set의 `existing` CTE가 입력 100행 × 1000으로 과대 추정되고 checkpoint 조인이 표 전체 seq scan + 다중 batch hash join으로 풀려 발행마다 작은 임시 파일을 만들었습니다(2026-10-24 운영 관측: `log_temp_files=0`에서 하루 약 6,800만 줄, `source_collection_checkpoints` seq scan 217만 회). `ALTER FUNCTION ... ROWS 1`만 적용하며 함수 본문·권한·발행 결과는 그대로입니다. 적용 뒤 checkpoint 조인은 기본 키 index scan으로 바뀌고 임시 파일이 생기지 않습니다(격리 PostgreSQL EXPLAIN 회귀 테스트 추가).
- **holo-postgres 로그·exporter 연결 예산:** `log_temp_files`를 0에서 1MB로 올려 `work_mem` 아래의 작은 hash batch 임시 파일(139~156바이트)마다 다중 행 STATEMENT 전문이 기록되던 하루 약 6,800만 줄의 PostgreSQL 로그를 멈춥니다. `postgres_exporter` role 연결 한도를 1에서 2로, `max_connections`를 60에서 61로 올려 exporter의 collector 연결이 매 scrape마다 거부되던 상태(2026-10-19부터 `pg_up`만 남음)를 복구하며 용량 gate의 여유 3은 유지합니다. 운영 반영은 `role-limits.sql`의 관리자 소켓 실행과 `holo-postgres` 재생성이 필요합니다.
- **로컬 생성물 정리:** 승인된 알림 미리보기 이미지를 제거하고 Python 빌드 도구의 `__pycache__`를 Git 추적 대상에서 제외했습니다. 실행 코드·설정·배포 이미지는 바꾸지 않습니다.

## v12.0.0 - 2026-10-22

- **central collector 배포 무교체 종료 수정:** `po-central-cutover.sh deploy`가 ssh `bash -s`로 보낸 원격 본문을 Docker Compose v5.6.0의 `compose run`이 stdin으로 읽어, 이전 profile 확인 직후 collector `c`·issuer 교체 없이 exit 0으로 끝나던 문제를 고칩니다. 원격 실행 구간을 끝까지 읽은 뒤 실행하며, 중단된 실행은 runtime·태그·설치 파일을 바꾸지 않았습니다.
- **Node.js 26.12.1 보안 갱신:** collector·PO 발급자·X Spaces 이미지의 Node 버전과 공식 이미지 digest를 보안 릴리스로 고정하고, npm 엔진 및 native AP 배포의 최소 버전을 `26.12.1`로 올립니다. 재배포 시 취약한 `26.12.0`을 다시 도입하지 않도록 하며 앱 의존성·프로필·DB 스키마는 변경하지 않습니다.
- **CI·shared 책임 역전·alarm-worker 구조 정리(api·alarm-worker·collector·shared):** CI는 standalone staticcheck와 중복 게이트를 지우고 matrix 대기를 없애며, egress 단일 인스턴스 검사는 병합 compose 렌더로 판정합니다. api 전용 contracts·settings 조립·bot 명령 타입·실행 락·`ServicesConfig`를 api로, 알림 저장소·key를 worker로 옮기고 shared에는 alarm HTTP wire 계약면(`alarmhttp`)만 남기며 별칭·re-export는 두지 않습니다. runtime module 테스트 의존을 collector ← api ← worker 한 방향으로 정리해 Dockerfile·AP 전송 manifest가 형제 module을 복사하지 않습니다. alarm-worker는 notified 기록·stream 단위 조회를 batch당 한 번으로 줄이고 upcoming 후보 종료와 backlog 관측 쿼리를 묶으며, mock 호환 폴백과 HSET 재시도를 지워 cache warm·구독자 조회 실패를 그대로 드러냅니다. `hololive_alarm_subscriber_*` metric은 worker에서만 등록하고, worker는 `OFFICIAL_SCHEDULE_*` env를 읽지 않습니다. 발송 원장·env 키·wire JSON·Valkey key·metric 이름은 그대로입니다.
- **로컬 게이트 테스트 시간 단축:** Go 테스트가 패키지마다 testcontainers PostgreSQL을 띄우던 방식을 일회용 PostgreSQL 하나를 공유하는 방식으로 바꾸고 race 병렬도 제한(`-p 4`)을 없앱니다. `hololive-dbtest`가 테스트마다 템플릿을 복제한 데이터베이스로 격리하므로 검사 범위는 같습니다. 전체 범위 race 단계가 328초에서 168초로 줄고, 실행 중 남던 컨테이너 수십 개(약 10GiB)도 생기지 않습니다. CI의 `public-pr-go-gate.sh`도 같은 공유 DB를 쓰며 러너 메모리 제한(`-p 2`)은 그대로 둡니다.
- **느린 테스트 정리:** admin auth 테스트는 bcrypt 최소 비용을 써 race 실행 시간이 109초에서 7초로 줄고, 설정 비용 반영 테스트는 기본값과 다른 비용으로 검사합니다. `hololive-dbtest`·`sourceobservation`·`joblease`의 격리된 DB 테스트는 병렬로 실행합니다. non-race 전용 단계는 race 빌드에서 빠지는 테스트 함수만 실행해 28초에서 2초로 줄고, collector production JSON 테스트는 collector 모듈이 Go 범위에 있을 때만 실행합니다. PostgreSQL failover·compose 보안 기본값 테스트는 다른 배포 스크립트 테스트처럼 입력이 바뀐 push에서 실행합니다.
- **golangci-lint 2.15.0(shared·collector·api):** 로컬 게이트의 최소 버전을 v2.15.0으로 올립니다. HTTP/3 종료 시한에 Shutdown을 끊는 순간 정상 종료가 먼저 끝나 실패를 돌려주면, idle 연결만 남았더라도 그 실패를 종료 실패로 반환합니다(이전에는 무시). pobroker·internalhttp 테스트 cleanup은 `Serve` 결과를 검사하고, `NewAmbiguousMatchError` 생성자는 스택 lint 템플릿 예외로 둡니다.
- **AP 배포·로그 미러 SSH 키 위치:** 배포 키 기본 경로를 저장소 안 `KR.key`에서 `~/.ssh/KR.key`로 옮겨, 저장소를 다시 받거나 지워도 키가 함께 사라지지 않게 합니다. `SSH_KEY`·`HOL_LOG_*_SSH_KEY` 지정은 그대로 우선하며, 로그 미러 systemd 유닛의 중복 키 경로 지정을 제거합니다.
- **pre-push 범위 정리:** 로컬 게이트의 모듈별 tidy·canonical vet·govulncheck·VERSION 전용 빌드와 전체 범위 테스트·staticcheck·NilAway가 형제 저장소(`../iris-client-go`, `../shared-go`)를 반복하지 않습니다. 각 저장소 게이트가 그 모듈을 검증하며, workspace 빌드·테스트는 형제 HEAD를 계속 사용합니다. 메모리 scope 재실행을 게이트 맨 앞으로 옮겨 준비 단계를 한 번만 실행합니다. 셸 스크립트 ShellCheck는 메타 포인터 push 대신 이 저장소의 push에서 실행됩니다. shared-go가 스택 서브모듈로 옮겨진 뒤 매칭될 수 없던 `shared-go/` 변경 범위 조건도 제거합니다.
- **Go 1.27.3 보안 갱신:** 다섯 모듈과 workspace, 앱 이미지 빌더(`golang:1.27.3-alpine3.24`), staticcheck·NilAway 프로필의 Go 버전을 1.27.3으로 올려 `crypto/x509` 취약점 GO-2026-6809를 해소합니다. 운영 인프라 이미지(gosu·socket-proxy·deunhealth) 정의와 socket-proxy 태그도 1.27.3으로 올립니다. 실행 중인 서비스와 인프라는 재빌드·배포 후 반영됩니다.
- **CI 경량화:** 공개 Go matrix의 불필요한 Python 준비와 중복 checkout·환경 출력 단계를 제거하고 보안 스캔의 모듈 선택을 한 번으로 합칩니다. 정확한 Markdown·세 VERSION 파일 전용 push는 릴리스 정합성만 검사하며, VERSION이 바뀌면 정본 workspace 모듈을 모두 컴파일합니다. `FULL_PRE_PUSH=true`는 fast 설정보다 우선하고 코드·의존성 변경의 기존 검사와 공개 필수 job은 유지합니다.
- **promptguard 일본어 공격 차단(api·alarm-worker·collector·shared·dbtest):** 다섯 모듈과 security 워크플로의 shared-go를 v2.16.0으로 올립니다. 주요 이벤트·멤버 뉴스·웹 검색 결과 같은 일본어 외부 소스의 지시 무시·시스템 프롬프트 유출 문구를 차단합니다. v2.15.0은 일본어 문구를 score 0으로 통과시켰습니다. 운영 주요 이벤트 399건과 이 저장소의 일본어 문장 1,391줄에서 판정이 바뀐 것은 없습니다.
- **promptguard 오탐 축소(api·alarm-worker·collector·shared·dbtest):** 다섯 모듈의 shared-go pin을 v2.15.0으로 올리고 security 워크플로의 shared-go checkout도 같은 커밋으로 맞춥니다. LLM guard가 단어 내부·코드 식별자·부정·방법 질문 같은 정상 문맥과 해석 미완료로 정상 입력을 막는 경우가 줄고, 대문자·위장 문자 지시 변경과 표지 없는 hex 공격은 새로 탐지합니다. 엔진은 `promptguard-engine-v3.2.7`입니다. 운영 반영에는 재배포가 필요합니다.
- **YouTube outbox dispatch 출처 퇴역(alarm-worker·shared):** 생산자가 이미 삭제된 `AlarmQueueEnvelope.youtube_outbox` wire 필드와 `youtube_outbox` source kind, 그 전용 검증·정규 식별자·렌더·dedupe·그룹 분기를 지웁니다. 2026-10-20 운영 `alarm_dispatch_events`의 해당 출처는 0건이었습니다(read-only 집계). 남은 행이 있어도 저장 payload decode는 성공하고, runner가 렌더 전에 `has no egress path` 발송 전 실패로 재시도하다 시도 상한에서 DLQ로 보냅니다. 다른 출처의 dedupe key와 `youtube_notification_outbox` 발송 경로는 바뀌지 않습니다.
- **Live 수명 모델 명시 갱신:** migration 328이 `youtube_live_runtime_state`의 원본 문장 트리거와 MVCC guard 표를 제거합니다. 관측 확정과 종료 finalizer는 writer가 모은 영상 key로 같은 READ COMMITTED 트랜잭션 끝에서 `refresh_youtube_live_runtime_state`를 한 번 호출하며, 함수는 영상 bucket(1024개) advisory 잠금 뒤 새 snapshot으로 다시 계산합니다. 전역 잠금은 같은 advisory key의 shared(관측 writer)·exclusive(증거 retention·수동 영수증·head 복구·data migration)로 나뉘어 관측 writer끼리는 원본 행과 영상 잠금으로만 직렬화됩니다. 325의 key 비교 지표는 삭제합니다. **merge 조건:** 운영 325 지표로 path마다 trigger_keys 증가 > 0, 전 path missing 증가 0, `check-runtime-projections.sh`의 live 결과 0|0|0을 확인합니다. 관찰 기간에 자연 입력(새 예정 Premiere)이 없던 `video_list`는 격리 PostgreSQL에서 실제 publisher·consumer로 같은 경로를 트리거한 검증으로 대체합니다. 배포는 API 정지 → db-migrate(328) → 신규 API 시작 → `bash scripts/maintenance/check-runtime-projections.sh`에서 `live_runtime_state missing=0 extra=0 changed=0` 확인 순서이며 이미지만 되돌리는 롤백은 불가합니다. 328은 트리거 DROP부터 전체 재계산·COMMIT까지 원본 표 잠금을 유지하므로 migration 동안 source 관측 INSERT와 worker·bot 세션 읽기도 일시 대기할 수 있습니다. `record_youtube_live_review`는 이제 READ COMMITTED를 요구하므로 수동 검토를 SERIALIZABLE로 열던 절차는 거절됩니다.
- **`!알람` 멤버 조회 장애 노출과 미도달 코드 정리(hololive-api·collector·shared·youtubejs):** `!알람`이 matcher 원천 조회 실패를 "멤버 없음" 응답으로 바꿔 정상 처리로 끝내던 동작을 고칩니다. 동명이인만 응답하고 원천 장애는 다른 멤버 명령처럼 오류로 전파하며, matcher가 found인데 채널이 없으면 handled 대신 오류로 드러냅니다. 운영에서 닿지 않는 사본·시험 전용 래퍼 15개와 무검증 시험을 지우고 해당 시험을 운영 경로로 옮깁니다. admin `dispatchops` integration 시험을 integration 단계에 포함하고, LLM integration 시험은 `HOLOLIVE_API_ENV_FILE`로 지정한 파일만 읽으며 읽지 못하면 실패합니다.
- **YouTube 알람 상태 선점 열 삭제(migration 331, hololive-api·alarm-worker·shared):** `youtube_community_shorts_alarm_states`의 발송 전 선점 열 `authorized_at`과 부분 색인 `idx_ycsas_authorized_at`을 지우고, `delivery_status` 어휘 CHECK에서 `ENQUEUED`를 뺍니다. 남은 `ENQUEUED` 행은 미발송이면 `DETECTED`, 발송 시각이 있으면 `SENT`로 맞추며 다른 행은 바꾸지 않습니다. 재적용하면 변경 없이 끝납니다. **배포 게이트:** post 단위 claim 제거 릴리스가 `hololive-api`와 `hololive-alarm-worker` 모두에 운영 반영되고 그 이전 image가 0개일 때만 별도 승인으로 적용합니다. claim 제거 릴리스의 binary는 이 열을 참조하지 않으므로 migration을 먼저 적용하는 일반 순서를 따릅니다. 적용 뒤에는 worker·API를 claim 제거 릴리스 이전으로 되돌릴 수 없습니다(`docs/current/runbooks/alarm-worker.md`).
- **봇 응답 accepted 엄격 shape:** migration 332가 `bot_reply_outbox`의 shape CHECK를 엄격형으로 바꿔 lease는 `submitting`만, `accepted`는 lease 없이 admission 시각과 handoff 확인 예정 시각을 갖게 합니다. lease 있는 `accepted`가 남아 있으면 332는 제약을 바꾸지 않고 중단합니다. 334·335는 만료 lease index를 `submitting` 전용으로 CONCURRENTLY 교체합니다. 만료 lease reclaim의 이전 binary용 `accepted` 분기와 `bot_reply_outbox_accepted_reclaimed_total` 지표를 삭제했습니다. 직전 릴리스가 운영에서 돌아 lease 있는 `accepted`가 0행인지 확인한 뒤에만 적용하며, 이후에는 326 이전 binary로 되돌릴 수 없습니다.
- **주석 정리:** 소스·테스트·배포 설정의 영어 설명을 한국어로 바꾸고 자명한 마이그레이션 이력 설명을 제거했습니다. NilAway 정본 사본을 동기화하며 실행 코드·설정값·체크섬 고정 마이그레이션은 보존합니다.
- **Live 수명 모델 갱신 key 비교:** migration 325가 원본 문장 트리거가 refresh한 `youtube_live_runtime_state` key를 트랜잭션 설정에 남기고, 관측 확정과 종료 finalizer가 같은 트랜잭션 끝에서 Go writer가 모은 key와 비교합니다. 커밋된 트랜잭션만 `hololive_youtube_live_runtime_key_audit_trigger_keys_total`·`_missing_total`·`_extra_total{path}`로 세므로, path마다 비교가 실제로 갱신 key를 봤는지와 누락이 없는지를 함께 판정할 수 있습니다. 트리거·guard와 저장 동작은 그대로이며, 명시 갱신으로 옮기기 전에 key를 빠뜨리는 writer 경로를 운영에서 찾기 위한 측정입니다. 325는 함수만 교체해 이전 API와 호환되며 db-migrate를 API보다 먼저 적용합니다.
- **Live 세션 정책 AP 일원화:** live 세션의 status·관측 시각·메타데이터 병합을 SQL CASE에서 reducer로 옮기고, 잠금 안에서 읽은 행 그대로일 때만 적용되는 조건부 INSERT/UPDATE로 저장합니다. 0행 적용은 영상 단위 충돌 오류로 보고되어 트랜잭션 전체를 재시도하며, 저장된 채널과 `ended_at`을 유지하는 규칙을 reducer에 더했습니다. claim의 replay epoch 만료·시도 소진 DEAD_LETTER 판정은 Go 재시도 정책이 정하고 같은 트랜잭션의 집합 UPDATE 한 번으로 기록하며, live 경로별 잠금 순서표를 hololive-api 문서에 정리했습니다.
- **봇 응답 handoff 확인 분리:** 응답 발송은 Iris 202 접수와 requestId 저장에서 끝나고 lease 없는 `accepted` 행을 API 내 reconciler가 250ms부터 60s까지 늘어나는 간격으로 `executor attempt_timeout` 지평까지 확인합니다. `failed`는 같은 행을 `:r1`/`:r2` 재발급(세대 상한 2)으로, 예산 소진은 `dead`로, 결과 불명·404·지평 미확인은 payload를 보존한 `manual_review`로 정산하며 조회 오류는 전이 없이 다시 확인합니다. 같은 방 후속 응답은 확인 전까지 보류합니다. Reply outbox attempt 성공은 이제 admission 완료를 뜻하며 reconciler 결과는 로그와 manual review backlog로 관측합니다. migration 326은 두 accepted 모양을 모두 받는 완화 CHECK와 열을, 333은 확인 대상 index를 추가합니다. 이전 binary로 되돌리려면 reconciler가 `accepted`를 0으로 배수해야 하며 기존 rollback preflight가 남은 행을 막습니다.
- **Iris handoff 확인을 reconciler로 일원화, sender 인라인 polling 제거(alarm-worker):** 공유 sender(`IrisMessageSender`)가 Markdown 202 뒤 `/reply-status`를 250ms마다 조회하던 polling을 지웠습니다. 텍스트·Markdown 모두 202가 durable admission을 증명하면 requestId를 바로 돌려주므로 sender 성공은 admission만 뜻하고, handoff 결과(`handoff_completed`·`failed`·`outcome_unknown`·진행 중)는 alarm dispatch·notification·youtube handoff reconciler만 각자의 원장에서 확인합니다. admission 규칙은 같습니다(Markdown은 `success`·`delivery=queued`·requestId, 텍스트는 requestId만). 각 executor `attempt_timeout`은 이제 admission 호출에만 걸리고 handoff 확인 지평으로 쓰입니다. Markdown의 handoff `failed`는 발송 단계의 격리(alarm dispatch)·영구 실패(youtube)·같은 ID 재시도(notification) 대신 reconciler의 세대 재발급 또는 소진으로 정산됩니다. `egress.IrisClient`는 `SendMessageAccepted`·`SendMarkdown`만 요구합니다. `sendoutcome.ErrHandoffFailed`와 그 분기를 지웠고, `sendoutcome.ErrHandoffOutcomeUnknown`은 별칭 없이 `ErrAdmissionOutcomeUnknown`(오류 문구 `iris reply admission outcome unknown`)으로 바꿨습니다. 409 `CLIENT_REQUEST_ID_OUTCOME_UNKNOWN`과 admission을 증명하지 못한 202는 계속 `OutcomeUnknown`이며, transport 재시도와 409 `CLIENT_REQUEST_ID_FAILED` 세대 재발급 라우팅은 바뀌지 않습니다. alarm dispatch consumer는 `quarantine_threshold_ms`가 없거나 lease보다 작으면 `3 × lease` 기본값·lease clamp로 바꾸지 않고 생성 오류를 내고, 프로필 검증도 `alarm_dispatch quarantine_threshold_ms must be at least lease_ms`로 기동을 거부합니다. 설정 키·스키마·migration은 바뀌지 않으며, 배포 전에 운영 프로필의 `quarantine_threshold_ms ≥ lease_ms`를 확인해야 합니다.
- **YouTube 그룹 조회 창 완전성 검사:** 별칭 후보가 전역 조회 한도를 채우면 반환된 owner나 일부 follower만으로 발송 준비·stale 격리·handoff 정산을 허용하지 않습니다. claim 기반 해석과 stale sweep은 원장 조회 전 오류로 끝나고, handoff는 행·원장·세대를 보존한 채 확인만 닫습니다. 고정 멤버의 실제 잠금 여부도 확인하므로 잠금 전 행 정보로 정산하지 않으며, 부분 그룹만 전이하거나 원장·세대만 바뀌는 것을 막습니다.
- **외부 Go worktree lint 소유권:** local-ci가 상위 workspace의 외부 모듈을 worktree 이름과 무관하게 자체 lint 설정의 소유로 분류합니다. 별도 이름의 shared-go checkout에 hololive의 import 정렬 규칙이 적용되던 오탐을 없애며, 외부 모듈의 vet·staticcheck·build·race·non-race 검사는 유지합니다.
- **youtube delivery Iris handoff 확인 reconciler 추가(alarm-worker):** youtube 발송이 202 응답의 Iris requestId를 버리던 경로를 고쳐, `CompleteSent`가 SENT 전이·tracking·telemetry·SENT 원장과 같은 트랜잭션에서 고정 요청 `youtube_notification_send_request`에 `handoff_request_id`와 첫 확인 시각(SENT 전이의 DB 시각 + min(5s, `youtube_delivery` executor `attempt_timeout`))을 기록합니다. 고정 요청을 멤버 delivery보다 먼저 잠그고(claim과 같은 요청 → 멤버 순서), 요청 세대·고정 멤버가 이번 발송과 다르거나 requestId가 비면 SENT 전이 없이 SENDING으로 남겨 stale sweep 격리에 맡깁니다. 새 `youtube-delivery-handoff-reconciler`(youtube_delivery가 켜졌을 때만 등록)는 notification·alarm dispatch reconciler와 같은 `handoffcheck` 판정·간격과 soft claim 15s·조회 5s·정산 5s(슬롯 대기 포함, 일반 연결 획득)·batch 32·정산 동시 2건으로 지평(`attempt_timeout`)까지 확인합니다. 정산은 고정 요청 → 영향 부모 outbox(id 순서) → 논리 그룹 delivery → 원장 순서로 잠근 한 트랜잭션에서 soft claim 토큰·requestId·세대·본문 해시·고정 멤버와 멤버 row_version을 다시 확인합니다. 부모를 자식보다 먼저 잠가 보존 정리·fanout과 같은 부모 → 자식 순서를 지키고, 부모 집합을 미리 읽은 뒤 같은 논리 키의 follower·outbox가 새로 들어와 집합이 늘었으면 새 부모를 더 잠그지 않고 아무것도 바꾸지 않은 fence 미스로 끝내며 확인을 닫지 않아 soft claim 뒤 다음 확인이 늘어난 집합으로 정산합니다. 완료는 확인 종료, `failed`는 세대 < 2이고 `max_retries` 예산이 남으면 고정 멤버 밖 중복 물리행(follower)까지 논리 그룹의 SENT 행을 `retry_backoff_ms` 뒤 PENDING으로 되돌리고 세대를 올려 같은 고정 본문을 `:r1`/`:r2`로 재발송하며, 아니면 FAILED로 끝냅니다. 두 경우 모두 같은 트랜잭션에서 그 owner를 출처로 한 SENT 원장만 지웁니다. 결과 불명·404·지평 미확인은 QUARANTINED와 같은 출처의 QUARANTINED 원장입니다. 재발급·소진·격리는 바뀐 delivery의 부모 outbox 집계도 이미 잠근 부모에 기존 집계 문장으로 같은 트랜잭션에서 다시 계산하므로, 집계가 실패하면 delivery·원장·요청 갱신까지 함께 되돌아가고 soft claim 뒤 다음 확인이 다시 정산합니다(정산 뒤 별도 집계나 보정 scan은 없습니다). 집계는 기존대로 outbox `sent_at`을 지우지 않아, 한 번 SENT였던 부모는 FAILED·PENDING이 되어도 처음 SENT 시각을 유지합니다. 원장 출처가 바뀌었거나 멤버가 claim 뒤 바뀌었으면 전이 없이 확인을 닫고 오류 로그를 남깁니다. post 단위 tracking·알람 발송 표시는 다른 방 발송의 결과일 수 있어 되돌리지 않습니다. 로그에는 delivery id와 세대만 남기고 Iris requestId·SDK 오류 원문은 남기지 않습니다. migration 336(열 3개와 shape CHECK, `NOT VALID` 뒤 검증)과 339(`CONCURRENTLY` 확인 예정 부분 index)를 바이너리보다 먼저 적용합니다.
- **notification delivery Iris handoff 확인 reconciler 추가(alarm-worker):** notification 발송이 202 응답의 Iris requestId를 버리던 경로를 고쳐, `MarkSent`가 같은 문장에서 `notification_delivery_outbox.handoff_request_id`와 첫 확인 시각(DB 시각 + min(5s, `attempt_timeout`))을 기록합니다. 첫 확인은 지평보다 늦지 않으며, alarm dispatch의 `MarkSent`도 같은 규칙으로 첫 확인을 지평에서 자릅니다. requestId가 비면 전이하지 않고 SENDING으로 남겨 stale sweep에 맡깁니다. 새 `notification-delivery-handoff-reconciler`는 alarm dispatch reconciler와 같은 soft claim·트랜잭션 밖 `/reply-status` 조회·예산·간격으로 지평(`notification_delivery` executor `attempt_timeout`)까지 확인하고, 정산은 행을 잠근 단일 문장에서 soft claim 토큰·requestId·고정 요청 세대를 다시 확인합니다. 정산 슬롯 대기를 포함해 정산 예산을 넘긴 정산은 적용하지 않고 다음 확인이 다시 판정합니다. 완료는 확인 종료, `failed`는 세대 < 2이고 발송 실패와 같은 `max_retries` 예산이 남으면 `retry_backoff_ms` 뒤 같은 본문을 `:r1`/`:r2`로 재발송하는 PENDING, 아니면 `exhausted` 종료 FAILED, 결과 불명·404·지평 미확인은 QUARANTINED입니다. `last_error_code`가 없는 원장이라 `error`가 handoff 코드로 시작합니다. handoff 확인이 끝나지 않은 SENT 행(330 CHECK상 `handoff_next_check_at`이 있는 행)은 같은 방 후행 행의 claim을 막아, Markdown·텍스트 모두 같은 방 다음 발송이 handoff 확인이 끝날 때까지(지평 이내) 보류됩니다. 재발급된 행은 새 `next_attempt_at`으로 기존 `(next_attempt_at, created_at, id)` 방 순서에 다시 들어가므로, 이미 due인 같은 방 후행 행이 먼저 나갈 수 있습니다(엄격한 생성 순서는 보장하지 않음). 정산 fence 미스는 `hololive_notification_delivery_fence_miss_total`에 `action=handoff_*`로 남습니다. migration 330(열 3개와 shape CHECK, `NOT VALID` 뒤 검증)과 338(`CONCURRENTLY` 확인 예정 부분 index)을 바이너리보다 먼저 적용합니다.
- **alarm dispatch Iris handoff 확인을 reconciler로 분리, 텍스트 202 requestId 필수(alarm-worker):** 텍스트 발송이 `SendMessageAccepted`로 202 응답을 받고 requestId가 없으면 결과 불명으로 처리합니다. 운영 중인 bot plane 텍스트 경로와 같이 `delivery`·`success` 값은 보지 않습니다(Iris 계약이 텍스트 202의 `delivery`를 정하지 않음). 이 텍스트 sender는 youtube·notification 발송도 함께 쓰므로, 이전에는 성공이던 requestId 없는 텍스트 202가 alarm dispatch에서는 격리, youtube에서는 결과 불명 실패, notification에서는 `QUARANTINED`가 됩니다. Markdown은 기존대로 `delivery=queued`와 requestId를 요구합니다. alarm dispatch는 두 lane의 requestId를 `MarkSent`와 같은 문장에서 send unit의 `handoff_request_id`·`handoff_next_check_at`(DB 시각 + min(5s, `attempt_timeout`))에 저장합니다. 새 `alarm-dispatch-handoff-reconciler`가 확인 시각이 된 단위를 `SKIP LOCKED`로 soft claim하고 트랜잭션 밖에서 `/reply-status`를 조회한 뒤, 15s·60s·5분 간격으로 지평(`attempt_timeout`)까지 확인합니다. 정산은 일반 연결 획득으로 동시 2건까지만 돌아 alarm_worker 풀을 독점하지 않고, 슬롯 대기를 포함한 정산 예산 5s를 넘긴 정산은 포기해 다음 확인이 다시 판정하도록 경고로 남깁니다. `handoff_completed`는 확인 종료, `failed`는 세대·시도 예산 안이면 멤버 retry와 `:r1`/`:r2` 세대 전진을 한 문장에, 아니면 DLQ와 commit 뒤 정산 예산과 별도인 5s 예산의 claim 키 해제, 결과 불명·404·지평 미확인은 `handoff_outcome_unknown`·`handoff_status_missing`·`handoff_unconfirmed`로 격리합니다. 토큰·요청은 맞지만 멤버가 더 이상 모두 `sent`가 아니면(멤버가 모두 지워진 단위 포함) 전이 없이 확인을 닫고, 소유 상실 경고와 구분되는 "check closed without transition" 오류 로그를 관측 행동·코드와 함께 남깁니다. 조회 오류 로그에는 Iris requestId가 섞인 원문 대신 HTTP 상태와 오류 class만 남깁니다. `delivery.PreparedSender.SendPreparedMessage`는 Iris requestId를 함께 돌려줍니다. migration 329(열·shape CHECK, 잠금 대기 3s를 runner 세션 값과 분리)와 337(`CONCURRENTLY` 부분 index)을 바이너리보다 먼저 적용합니다.
- **alarm dispatch 소유자 전이 fence 정리(alarm-worker):** 발송 진입·MarkSent·실패 정산·재대기·반환·DLQ·격리·세대 재발급이 모두 행의 `(status, locked_by, attempt_count)`를 claim과 맞추는 한 SQL 문장이 되고, 대상 행을 잠근 뒤 채취한 DB 시각 하나로 lease를 판정하고 시각을 기록합니다. 재시도 시각은 Go가 계산하지 않고 `retry_after_ms`로 넘겨 같은 send unit 멤버가 같은 `next_attempt_at`을 받으며, lease·stale 격리 기준은 초 절사 없이 밀리초로 적용합니다(1ms 미만은 설정 오류). 어긋난 전이는 bool이나 부분 적용 오류 대신 대상별 원인을 담은 `sendfence.MissError`로 돌아오고, `alarm_dispatch_pg_transition_partial_total`에 `action`·`reason` label이 붙습니다. 발송 전 멤버십 변경은 격리하지 않고 claim을 반환하며 종료된 멤버가 있을 때만 격리합니다. 발송 진입과 세대 재발급은 각각 단일 문장이라 왕복이 줄었고, 발송 진입 중 lease 만료는 같은 attempt 재대기 대신 만료 회수로 넘어갑니다. 공유 `AlarmQueueRetryMetadata`에서 쓰이지 않던 `next_visible_at`을 지웠습니다. 재시도 한도·지연·시도 예산은 바뀌지 않고 migration은 없습니다.
- **발송 원장 공통 fence 도입과 notification 소비자 적용(alarm-worker):** 외부 발송 원장이 함께 쓰는 `internal/egress/sendfence`를 추가했습니다. 전이 SQL이 대상 행을 잠근 같은 문장에서 계산한 조건값으로 미스 원인(`missing`·`status_changed`·`owner_changed`·`version_changed`·`lease_expired`·`member_terminal`·`membership_changed`·`request_changed`·`peer_missed`)을 분류하고, 적용되지 않은 대상이 있으면 `*sendfence.MissError`를 돌려줍니다. notification-delivery의 발송 전이·요청 고정·완료·실패·격리·세대 재발급은 모두 상태·`locked_by`·claim 당시 `attempt_count`(발송 전이면 lease 포함)를 같은 `clock_timestamp()`로 검사하고 bool 대신 이 오류를 반환합니다. 같은 worker 이름이 재시도 뒤 다시 claim한 행에 이전 claim의 늦은 완료·실패·격리가 적용되던 경로가 막힙니다. 저장 요청 fence는 JSON 전체 등가 대신 미고정·세대 등가·미소진으로 바꾸고 재발급은 세대나 `exhausted`만 갱신합니다. 보존 정리 기한은 Go 시각 대신 DB `statement_timestamp()`에서 계산하며, 저장소의 0 이하 lease·임계값 기본값 대입은 오류로 바꿨습니다. 미스 지표는 `hololive_notification_delivery_fence_miss_total`(action·reason·result)로 일반화했습니다. 순서(room FIFO)·재시도·격리·rearm·identity 의미와 스키마는 바뀌지 않습니다.
- **YouTube community·shorts 발송 전 post 단위 claim 제거(alarm-worker·hololive-api·shared):** 발송 전에 `youtube_community_shorts_alarm_states`를 선점하지 않습니다. 같은 post의 room delivery는 room별 claim·send fence와 logical group gate로만 직렬화하므로, 한 방의 발송 결과가 불명이어도 같은 post의 다른 방은 다음 라운드에서 미뤄지지 않고 바로 발송됩니다. 성공 전이(`CompleteSent`)가 같은 트랜잭션에서 `MarkAlarmSentBatch`로 tracking에 발송 시각을 기록하고, post 단위 알람 상태는 tracking 행으로부터 기록합니다(이미 SENT면 가장 이른 시각 유지). commit 응답 유실 판정의 read-back에서 post 단위 SENT 표시(알람 상태 또는 tracking)가 없으면 atomicity breach(`Indeterminate`)로 분류합니다. 알람 상태 upsert, `delivery_status` 계산, 발송 시각 기록은 `authorized_at`을 읽거나 쓰지 않으며 미발송 행은 `DETECTED`로 남습니다. 구 워커 claim이 남긴 미발송 `ENQUEUED` 행은 발송 시각 기록에서 SENT로 수렴합니다. 이 릴리스의 binary가 열을 참조하지 않으므로 다음 릴리스의 열 삭제 migration은 일반 순서(migration 먼저)로 적용할 수 있습니다. 운영 관측 스크립트의 claim 적체 조회도 지웠습니다. alarm-worker와 hololive-api는 같은 릴리스로 배포합니다.
- **YouTube 발송 시각을 DB 시계로 통일(alarm-worker):** delivery·fanout outbox의 claim, lease 만료·신선도 판정, `locked_at`·`sent_at`·`next_attempt_at`·`terminal_at` 기록(outbox 집계 projector가 쓰는 `sent_at`·`terminal_at` 포함)을 Go 시각 대신 각 SQL 문장이 한 번 채취한 PostgreSQL `clock_timestamp()`로 정합니다. delivery 전이와 fanout 실패 문장은 대상 행을, 집계 projector는 부모 행을 잠근 뒤 시계를 채취하므로, 잠금을 기다린 `BeginSending`의 발송 lease나 delivery·fanout 실패의 재시도 시각이 대기 이전 시각으로 앞당겨지지 않습니다. 재시도·보류는 `GREATEST(DB 시계 + 지연, DB 유래 하한)`로 계산하고, CompleteSent는 SENT 전이가 기록한 시각을 tracking·ledger·telemetry에 그대로 씁니다. commit 응답 유실 판정은 시각 열을 비교하지 않고 `(status, row_version, attempt_count, locked_by, error)`로 post-state를 식별합니다. 종결 실패의 follower는 owner의 `next_attempt_at`을 복사하지 않고 자기 값을 유지합니다. migration은 없습니다.
- **YouTube 발송 claim fence를 호출자 claim 세대로 통일(alarm-worker):** `youtube_notification_delivery`에 claim 소유자 열 `locked_by`를 추가하고(migration 327, 기본값 없는 nullable 열), 행 fence를 `(status, locked_by, row_version)`으로 정리합니다. `PrepareClaimed`·`BeginSending`·`ApplyPreparedFailure`는 같은 트랜잭션에서 다시 읽은 행이 아니라 호출자가 받은 claim의 버전·소유자와 비교하므로, lease 만료 뒤 다른 실행이 재claim한 행을 이전 소유자가 SENDING으로 바꾸거나 실패 처리하지 못합니다. `BeginSending`은 claim lease(`locked_at + LockTimeout`)가 DB 시계 기준으로 살아 있을 때만 발송을 시작합니다. 동어반복 검증이던 `PreparationLease`를 지우고, 전이 SQL의 `attempt_count`·`locked_at` 비교는 `row_version` 등가에 함의되므로 `locked_by` 비교로 바꿨습니다. fence 미스는 원인별 `sendfence.MissError`로 오류와 함께 돌려주고 아무 행도 바꾸지 않습니다. 이전 워커는 새 열을 읽거나 쓰지 않으므로 migration을 먼저 적용해도 안전합니다.
- **send unit 일부 claim으로 인한 격리 방지(alarm-worker):** 발송 claim이 ready 멤버가 있는 send unit의 대기(`pending`·`retry`) 멤버 전원을 `next_attempt_at`과 관계없이 함께 lease합니다. 실패 라우팅이 봉투마다 시각을 계산해 같은 unit 멤버의 재시도 시각이 마이크로초 단위로 갈리면, 그 사이에 실행된 claim이 일부만 가져가 발송 fence가 미발송 unit 전체를 격리했습니다. `dlq`·`quarantined` 등 종단 멤버는 끌어오지 않으며 시도 횟수·DLQ 예산은 바뀌지 않습니다. migration은 없습니다.
- **notification 재발급 fence 미스 처리:** notification-delivery의 요청 고정 저장과 409 `CLIENT_REQUEST_ID_FAILED` 세대 재발급이 0행이면 bool을 버리지 않고, 같은 문장의 행 잠금 아래에서 관측한 원인(행 없음·상태 변경·소유자 변경·lease 만료·시도 횟수 변경·요청 변경)을 typed error로 올립니다. 소유권이 남은 요청 변경만 소유자·시도 횟수 fence가 걸린 실패 전이로 끝냅니다. 재발급은 같은 문장에서 저장된 요청에 `exhausted`를 세운 종료 FAILED라 producer rearm이 되살리지 않고, 발송 전 저장은 일반 재시도 정책을 따릅니다. 소유권을 잃은 행은 건드리지 않습니다. 확정된 pre-handoff 충돌이 SENDING에 남아 stale sweep에서 결과 불명으로 격리되던 경로가 없어지고, 모든 미스는 `hololive_notification_delivery_fence_miss_total`(action·reason·result)과 로그로 남습니다. migration 변경은 없습니다.
- **alarm 발송 lease와 Iris 자동 재발송 지평 검증:** alarm-worker 프로필 검증이 `alarm_dispatch`의 `lease_ms + recovery_interval_ms`가 Iris 자동 재발송 지평(`irisdurable.AutomaticReplayHorizon`, admission 보존 168h − 여유 24h = 144h)보다 짧은지 확인하고, 같거나 길면 기동을 거부합니다. 결과 불명 발송을 같은 client request ID로 다시 보내는 dedup은 Iris가 그 ID를 기억하는 동안에만 유효하므로, 최소 회수 지연이 지평 안에 있어야 한다는 필요조건입니다. 실제 재발송 창에 더해지는 backoff·attempt timeout·claim 주기는 24h 여유가 흡수합니다. `notification_delivery`의 lease는 발송 전 claim만 회수하고 `youtube_delivery`의 만료 SENDING은 격리되므로 두 경로에는 적용하지 않습니다. 설정 키·스키마·fixture는 바꾸지 않습니다.
- **알림 발송 완료 전이 축소:** notification outbox의 발송 완료 정산이 현재 worker가 소유한 SENDING 행만 SENT로 확정합니다. 발송 전이를 거치지 않은 PENDING 행은 소유자가 같아도 전이하지 않고 fence 거부로 돌려주므로, 정산 가능한 상태가 실제 발송 경로와 일치합니다. lease 만료 후에도 SENDING 소유자의 성공 정산은 그대로 허용됩니다.
- **SQL 시계 선택 검사:** 기존 SQL 소유권 검사에서 collector lease와 alarm의 잠금 후 fence는 wall clock, 후보·projection·live 조회는 statement clock을 요구합니다. 주석·문자열·인용 식별자를 구분하고 인자 없는 실제 함수 호출만 검사하여 CTE·별칭의 열 선언을 오인하지 않습니다. 범위 밖의 기록용 transaction clock과 기존 migration은 유지하며, 잠금 순서의 정확성은 기존 DB 경합 시험이 계속 검증합니다.
- **동기 집계 정합 진단:** dbtest 오라클을 공유 SQL 자산으로 옮겨 bundle·live 모델의 누락·잉여·값 변경과 매핑의 값·중복도 불일치를 읽기 전용 RR snapshot에서 검사합니다. 서비스 파일과 무관하게 연결 제한을 명시적 인자로, 조회 제한을 SET LOCAL로 적용하고 유효값을 확인합니다. 식별자·원문 없이 건수만 반환하며 정상/불일치/확인 실패를 종료 코드 0/1/2로 구분합니다. dbtest는 집계와 매핑을 직접 손상시켰을 때 불일치 종류마다 한 건씩 세는지도 확인합니다. 새 migration·자동 복구·주기 실행·운영 적용은 없습니다.
- **YouTube terminal outbox cleanup과 stale SENDING sweep 교착 수정(alarm-worker):** cleanup이 후보 부모·자식을 쥔 채 stale sweep이 잠근 같은 논리 그룹의 SENDING owner를 기다리고, 이어진 stale sweep 그룹 조회가 cleanup이 쥔 FAILED sibling을 기다려 교착(40P01)하면 cleanup batch 전체가 롤백됐습니다. cleanup은 자식과 same-logical 행을 `SKIP LOCKED`로 잠그고 건너뛴 행을 관측 누락 증거로 돌려받아, 경쟁 잠금·같은 논리 키 행의 `LogicalGroupScanLimit` 초과·그 키의 조회 한도 도달로 완전 관측을 증명하지 못한 후보를 삭제하지 않고 새 guard 사유 `incomplete_logical_group`(`hololive_youtube_delivery_cleanup_guard_total`의 `reason` 값)으로 셉니다. same-logical 조회 한도는 batch 전체가 아니라 요청 논리 키마다 `LogicalGroupScanLimit+1`행이라, 과대·잠긴 그룹은 자기 후보만 보존하고 같은 batch에서 완전 관측한 다른 논리 키의 후보는 계속 삭제합니다. 보존한 후보는 다음 cleanup 주기에서 다시 검사합니다. ledger는 완전 관측한 논리 키만 조회합니다. 후보 선정·cursor·삭제 수·발송/격리 전이는 바뀌지 않으며 migration은 없습니다.
- **퇴역 admin-dashboard 디렉터리 삭제:** 관리자 웹은 Iris Console이 소유하므로 경계 안내 문서와 퇴역 BFF 설계·bigbang 전환 증거만 남아 있던 `admin-dashboard/`를 지웁니다. 웹 소유권·운영 절차는 루트 `AGENTS.md`와 `docs/current/runbooks/admin-dashboard.md`가 계속 안내하고, 과거 증거는 Git 이력에서 조회합니다. 중앙 단축 링크 `admin-dashboard-ingress` 서비스와 이름은 그대로입니다.

## v11.0.0 - 2026-10-19

- **최종 워커·DB 계약 전환:** 10.2.1의 구 프로필 호환 baseline을 실제 운영에서 수용한 뒤 새 프로필을 활성화합니다. `source_observation`은 batch가 workers 이상이어야 하며 lease 하한은 `batch × attempt_timeout + 10000ms`입니다. eligible 큐·nullable 진단·공유 cache 수명·DB control reserve와 lease/attempt 정산을 함께 유지하며, 322–324와 방송 시각 확정·멤버십 아카이브 동작은 보존합니다.

- **미전송 알림 lease 복구:** 직렬 발송의 뒤 그룹에서 아직 보내지 않은 claim이 만료되면 영구 격리하지 않고 기존 whole-unit recovery에 남깁니다. frozen request·generation·소유권 fence와 provider별 기한은 유지하며, 실제 DB에서 회수·재claim 후 두 발송 완료와 attempt 집계를 확인했습니다.
- **paired rollback 복원성:** central은 이전 collector의 실행/정지 상태를 기록하고 정지된 writer를 자동 시작하지 않습니다. profile 게시 실패는 issuer 제거 전에 반환합니다. 호출별 임시 파일을 모두 준비한 뒤 개별 rename하여 남은 `.next` 파일이 복원을 막지 않게 하며, AP dry-run은 retained overlay를 바꾸지 않고 apply가 검사한 바이트를 게시합니다. 다중 파일 전체의 원자성이나 동시 배포 직렬화를 보장하는 변경은 아닙니다.
- **준비 실패의 image tag 복원:** central의 issuer-absent 기준점과 AP의 최초 배포/issuer-absent 기준점에서 후보 `prod` 태그가 남아 다음 배포를 막던 경로를 수정했습니다. 기존 태그는 rollback reference로 돌리고 원래 없던 태그만 정확한 reference로 제거합니다. 조회/제거 실패는 복원 실패로 남기며 원래 종료 상태·live runtime·rollback archive/다른 태그는 보존합니다.
- **DB 제어 용량(D09, 로컬):** runtime pool 전체에 5초 acquire 예산을 적용하고 bot·YouTube source·alarm-worker·collector만 짧은 lease 갱신/정산용 연결 하나를 남깁니다. provider/worker 전체를 우회시키지 않으며 admin·LLM은 reserve 0, maintenance/replay/migrator는 nil 정책입니다. max1을 거부하되 min0·collector admitted deadline·API 공유 cache 수명은 유지합니다. pgx Stat wait는 외부 gate 대기를 포함하지 않습니다. 격리 DB 경합·race·종료 경로를 검증했으며 운영 활성화는 하지 않았습니다.
- **DB 배포 정책 준비(미활성):** AP collector/issuer Compose를 중앙 비밀·DB/cache 정의에서 분리하고 중앙 c만 DB 의존성을 추가합니다. 이전 Compose·이미지·프로필 rollback 짝과 전송 목록을 함께 보존합니다. migrator/admin bootstrap은 격리 Unix socket으로 전환하며, native primary/pool 값은 topology/capacity 정본에서 읽습니다. exporter 1·nightly 2를 포함한 collector max7 후보는 할당54/여유3입니다. 실제 격리 PG18에서 privileged TCP/replication 거부와 runtime·scraper·exporter TLS, socket migration을 확인했습니다. keepalive60/10/6·자격 SQL 비노출 로그·기존 WAL900s/2GB/lz4를 명시하되 auto.conf 정리·역할 제한·재기동·운영 배포는 별도 승인입니다.
- **워커 프로필·기한:** 실제 age 폐기가 없는 워커는 `none`, YouTube 발송 freshness는 profile max_age 정본을 사용합니다. 중복 dispatch/transaction/send timeout을 제거하고 attempt·lease 관계에서 파생합니다. collector는 admission 때 확정한 kind별 budget/deadline을 보존하며 후속 실행에서 연장하지 않습니다. profile-only 사전 검사·immutable binary/profile 짝·source 변경 감지와 paired rollback을 함께 준비했습니다.
- **공유 자원·실패 정산:** API 프로세스의 Valkey/member cache를 한 소유자로 통합하고 모든 borrower 종료 뒤 backing bot pool과 함께 정리합니다. 시작하지 않은 admin plane이 종료를 막던 경로와, 취소된 inbox claim context가 fenced release를 막던 경로를 수정했습니다. 유한 정리 예산·원래 fencing·불명 결과의 무재생은 유지합니다.
- **SCRAM·역할 정본:** scraper/runtime 이름을 고정 계약으로 통일합니다. bootstrap은 UTF-8 입력의 PostgreSQL/pgx 준비 결과를 대조한 후 클라이언트 SCRAM verifier만 전송하며 불일치는 role 변경 전에 거부합니다. 원생 PostgreSQL·pgx·libpq의 Unicode/긴 결합문자 경계, 잘못된 자격 거부, 실패 SQL과 noisy server/client 로그의 비노출을 격리 환경에서 확인했습니다.
- **비활성 워커 진단:** `source_observation`이 꺼져도 YouTube runtime과 durable 큐 sampler를 구성하여 depth를 보고합니다. 비활성 경로는 claim·projection 준비·maintenance를 실행하지 않습니다. bot inbox/outbox의 attempt 종료는 defer로 보장하여 panic 뒤 inFlight가 남지 않습니다.
- **DB 연결·정리:** migrator의 pool·bootstrap 연결을 shared-go의 명시적 sslmode·비밀 보호 파싱으로 통일하며 `PGSSLMODE` 생략을 거부합니다. 기존 `pgxutil`을 제거하고 모든 rollback 소비자를 취소 독립 5초 공통 경로로 옮깁니다. 다섯 소비자는 provenance를 검증한 `shared-go/v2 v2.14.0`을 사용하며, source 통합만으로 운영 활성화·수용을 뜻하지는 않습니다.
- **보존 backlog 측정:** kind별 제한된 후보에서 첫 비보호 행을 찾으면 보호 판정을 중단합니다. 실제 migration을 적용한 격리 DB에서 1,000개 후보의 보호 함수 호출이 첫 행 적격이면 1회, 보호 선두 3개이면 4회로 줄고 oldest·known은 유지됨을 확인했습니다. migration 315 삭제 경로는 별도로 측정했으며 잠금·커서·재검사 계약을 보존하기 위해 변경하지 않았습니다.
- **회귀 검사의 오류 경계:** SCRAM 역할 실패의 원인·비밀 비노출 비교 전에 nil을 명시적으로 거부하고, source_observation의 정상·거절 입력을 별도 분기로 검사합니다. backlog 범위 검사도 실제 runtime처럼 필수 프로필을 적용한 뒤 설정을 검증합니다. 기본값·오류 문구·단순 로드 성공에만 의존한 단언은 제거하고 범위·파싱 실패 동작을 유지했습니다.

## v10.2.1 - 2026-10-18

- **프로필 짝의 유한 진단 조회:** loaded hash를 전체 `/metrics` 대신 인증된 `/diagnostics/workers`의 v1 `profile.hash`에서 확인합니다. unrelated metric 수가 늘어도 최초 capture·native 준비·배포 후 검증이 healthcheck 본문 상한을 넘지 않으며, 기존 응답 크기·시간 상한과 서비스별 인증키·프로세스/바이트 일치 검사는 유지합니다. 실제 운영의 Compose 네 슬롯과 native a/d 진단으로 확인했으며 자격 대체나 상한 완화는 없습니다.

## v10.2.0 - 2026-10-18

- **구 프로필 호환 pairing baseline:** API·alarm의 기존 settings와 fixed max_age, collector의 기존 per_job/bounded/fixed 스키마를 유지합니다. 실제 후보 CLI가 release profile을 검증하고 image/binary와 profile checksum을 함께 보존합니다. 인증된 loaded hash를 확인한 최초 도입만 허용하며, effective release와 desired source를 분리합니다. source 디렉터리 mount로 원자적 파일 교체도 관측하고 rollback은 secret mirror가 아니라 이전 artifact/profile 짝을 복원합니다.
- **배포·복구 지속성:** Compose active pair는 sync·재부팅 뒤에도 유지하고, AP/central 준비 실패는 runtime을 다시 만들지 않고 source·image tag 기준점을 복원합니다. native 완료 검사는 실제 실행 binary와 인증된 loaded profile hash를 확인합니다. 비활성 source_observation도 sampler를 유지하되 claim·projection 준비·maintenance는 실행하지 않습니다. DB 역할·HBA·WAL·Valkey·capacity 정책과 새 settings 활성화는 이 baseline에 포함하지 않습니다.
- **독립 릴리스:** provenance를 검증한 `shared-go/v2 v2.14.0`을 다섯 Go 소비자에 고정합니다. 이 기준본의 구 프로필 CLI·source drift·paired rollback을 실제 운영에서 먼저 수용한 뒤 비호환 프로필 이전을 진행합니다.
- **프로필 인증:** API·alarm은 `API_SECRET_KEY`, collector는 `METRICS_API_KEY`로 loaded profile을 확인합니다. 서로 다른 키를 둔 실제 healthcheck·HTTP 실행에서 수정 전 API/alarm의 인증 거절과 수정 후 네 Compose 슬롯의 수용을 확인했습니다. 자격 대체 경로는 추가하지 않습니다.
- **배포 영수증 충돌:** 초 단위 시각이 같은 연속 배포도 서로 다른 원자적 임시 디렉터리를 사용해 이전 profile pair를 덮어쓰지 않습니다. 실제 Compose 렌더링을 거치는 provenance fixture에서 충돌을 재현하고 수정 후 잘못된 image의 migration·cutover 거절을 확인했습니다.

- **`!방송기록` 방송 길이와 `!라이브` 경과 시간:** `!방송기록`이 시작 시각을 기준으로 표시하고, 종료 시각이 마지막 LIVE 확인 시각과 5분 안에서 맞을 때만 종료 시각과 방송 길이를 함께 표시합니다. 종료를 확인할 수 없으면 시작 시각만 표시합니다. 시작 시각이 없거나 수명 추적 도입 전 기록(`lifecycle_origin='legacy_unknown'`)이면 저장된 종료·예정·마지막 확인 시각을 그 의미와 함께 표시합니다. 옛 기록의 `started_at`은 실제 시작과 수 시간씩 어긋나기 때문입니다. `!라이브` 템플릿 자료에 조회 시각 기준 경과 시간(`Elapsed`)을 추가하며, 템플릿 본문은 바꾸지 않습니다. 경과는 저장된 시작 시각 기준이라, 시작 시각 없이 처음 감지한 방송은 감지 시각부터 셉니다(보통 1분 안팎 짧음).
- **`!라이브` 템플릿 경과 시간 (migration 322):** 220 표준 전역 `CMD_LIVE_STREAMS` 본문일 때만 방송명 줄 끝에 `⏱ 경과` 표기를 붙이는 CAS UPDATE입니다. 사용자 지정 본문과 채널 override는 보존합니다. 이전 바이너리에는 `.Elapsed`가 없어 `!라이브` 렌더링이 실패하므로, 경과 시간 자료를 내는 바이너리의 배포를 확인한 뒤에 적용합니다. 그보다 이전 버전으로 되돌릴 때는 전역 본문을 220 본문으로 먼저 복원합니다.
- **방송 시작·종료 시각 확정 (migration 323):** 종료된 방송을 `video_live_check`로 한 번 더 확인해 YouTube 아카이브의 시작·종료 시각으로 바꾸고 `youtube_live_sessions.times_source`를 `provider`로 둡니다. 종료 판정과 reducer는 바꾸지 않으며, 저장 시각과 크게 어긋나는 응답은 `conflict`로 남기고 쓰지 않습니다. 시작 시각 없이 늦게 감지한 방송은 저장 시작이 감지 시각이라, 아카이브 시작이 저장 시작보다 이른 것은 충돌로 보지 않습니다. 확인할 수 없는 응답은 종료 관측 기준 3분·30분·3시간·24시간에 최대 4회, `request_failed`는 10분 간격 최대 12회 다시 확인한 뒤 `exhausted`로 끝냅니다(진행 기록 `youtube_live_time_finalizations`). 확정 확인은 priority 18로 LIVE·UPCOMING 확인보다 뒤에 두고, 기존 기록 보정 묶음 크기 `YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE`의 기본값은 0입니다. 기존 기록 보정은 첫 확인을 한 번에 한 영상씩 열고, 재시도 중인 행과 합쳐 2,000개를 넘기지 않아 전체 수집 target 상한에 닿지 않습니다. `!방송기록`은 확정 시각이면 시작~종료와 길이를, 아카이브를 끝내 확인하지 못하면 `아카이브를 확인할 수 없어 종료 시각 미확인`을 표시합니다. migration 323을 이 바이너리보다 먼저 적용합니다.
- **운영 roster 밖 legacy LIVE 정리 (migration 324):** 운영 roster에서 빠진 채널의 head 없는 `legacy_unknown` LIVE 세션은 어떤 수집으로도 종료를 관측할 수 없어 LIVE로 남았습니다. 324가 이 세션을 마지막 관측 시각(`last_seen_at`)의 `ENDED`로 정리하고, 확정 대기열이 아카이브 시각으로 다시 확인합니다. 확정 대상은 운영 roster로 거르지 않습니다. 운영 데이터 쓰기이므로 323·확정 바이너리 배포 뒤 별도 승인으로 적용합니다.
- **멤버십 아카이브 시각:** 익명 player가 시각을 주지 않는 멤버십 아카이브는 같은 RPC 슬롯에서 watch 페이지를 한 번 더 읽어, 영상·채널 identity와 `isLiveNow=false`가 맞을 때만 시작·종료 시각을 채웁니다. 실패하면 지금처럼 시각 없는 관측을 보냅니다.
- **의존성·런타임 갱신:** Go module graph, Node 26.12.0 Current·npm 12.2.0·Corepack 0.36.0, Node helper·실험용 Python 의존성, uv 0.13.1·CI 도구와 이미지 pin을 최신 안정판으로 갱신했습니다. scratch 이미지에 Node 26의 `libatomic` 런타임 의존성을 포함합니다. PostgreSQL·Nginx·Valkey는 zlib 1.3.2-r1을 적용하고 socket-proxy 1.13.1은 Go 1.27.2로 재빌드하며, 서버·UID·소켓 접근 정책은 유지합니다. 운영 배포는 포함하지 않습니다.
- **이미지 검사·전환 정합성:** 보안 스캔 목록은 순서가 아닌 정확한 이미지 집합·중복 여부를 비교하며, 새 Valkey/socket-proxy의 attestation 비활성화는 로컬 스캔 overlay에만 적용합니다. 빌드·재배포의 revision 검사는 고정 tag 표가 아니라 실제 Compose CLI·env file·overlay에서 해석한 서비스 이미지를 사용합니다. 의존 서비스 이미지까지 출력하는 `config --images` 대신 렌더링 결과에서 대상 서비스의 이미지만 읽어, `depends_on`이 있는 서비스의 빌드 후 검사가 항상 실패하던 결함을 고쳤습니다. env-only override와 잘못된 revision의 전환 전 거절을 실제 Compose 렌더링으로 검증했습니다.
- **실패 종료 방의 반복 catchup 차단:** live catchup이 자기 catchup delivery가 DLQ·격리·취소로 끝난 방을 근거로 인정합니다. ledger가 `rejected_terminal`로 거절할 재발행을 매분 만들어 scheduler 오류가 반복되던 경로를 없애고, guardrail은 그 방을 매분 경고하지 않고 `terminal_delivery` 지표로 구분합니다. N분 전 알림의 실패는 catchup을 막지 않으며, 아무 결과가 없는 방의 경고와 DLQ 지표는 유지합니다.
- **실제 시작 시각이 없는 live의 근거 하한:** 실제 시작 시각이 없으면 처음 LIVE로 관측한 시각을 근거 하한의 기준으로 씁니다. 24시간을 넘긴 그런 방송에 같은 방 catchup을 다시 만들지 않습니다.
- **native 점검 창 실패 복원:** host-native AP 배포가 변경 전 collector unit의 실행·활성화 상태를 기록합니다. 실패 복원은 실행 중이던 unit만 이전 release로 다시 시작하고, 점검 창에서 멈춰 둔 unit은 시작하지 않습니다.
- **장시간 live의 반복 catchup 차단:** live catchup·guardrail의 기존 알림 근거 하한을 평가 시각이 아니라 방송의 실제 시작 시각 기준 24시간 전으로 둡니다. 24시간 넘게 이어지는 방송에서 최초 SENT가 창 밖으로 밀려 같은 방에 catchup을 매분 다시 만들던 경로와 guardrail `missing_dispatch` 오경보를 없앱니다. 막 시작한 방송의 근거 범위는 시작 후 경과분만큼만 앞당겨지며 catchup 허용 조건은 바꾸지 않습니다.
- **event payload identity 정규화:** 관측마다 바뀌고 본문에 쓰지 않는 `stream.viewer_count`를 canonical event payload에서 제외합니다. 같은 event에 나중에 합류한 방의 delivery가 시청자 수 차이만으로 hash 충돌로 거절되던 경로를 막고, 이미 보낸 방의 재발행은 `duplicate_sent`로 수용합니다. 제목 등 본문 field의 충돌 거절 계약은 유지합니다. 배포 전에 만들어진 진행 중 event는 이전 hash를 유지하므로, 배포는 미발행 upcoming 후보가 없을 때 진행합니다.
- **점검 창 Seoul 배포:** `AP_COLLECTOR_MAINTENANCE_STOPPED=true scripts/deploy/ap-deploy.sh seoul`이 schema 점검 창에서 정지한 b를 사전 검사에서 받습니다. cutover가 실패하면 이전 image로 되돌리되 collector는 시작하지 않습니다. collector·issuer 기준점의 revision·version 불일치는 rollback을 걸기 전에 거절하여 불필요한 재시작을 막습니다.
- **원본 출처 사전화·키 압축:** migration 317이 출처 3열을 `source_observation_provenances`의 정수 FK로, canonical observation_key를 32바이트 bytea로 바꿉니다. 5,000행별 backfill을 확정하고 기존 text는 제약 검증 뒤 원자적 cutover에서 제거합니다. publish·claim·replay·lock의 공개 hex/문자열과 ACTIVE lease 보호는 유지합니다. 원본 표 재작성은 별도 승인된 점검 창에서만 적용합니다.
- **감사 어휘 코드화:** migration 318–321이 application의 entity_kind·decision을 사전 smallint 코드로 바꾸고 UNIQUE index를 교체합니다. 318은 5,000행씩 commit하여 후반 실패에도 완료분을 보존하며 재실행으로 재개합니다. 감사·보호 의미는 유지하고, 제거 열의 물리 공간 회수는 별도 승인된 VACUUM FULL 단계입니다.
- **원본 retention 진행 복구:** migration 315가 보호 검사 전 최대 1,000개 후보 창과 영속 `(received_at,id)` 커서·고정 순회 상한을 도입합니다. 보호/잠금 prefix를 tick마다 넘기고 순회 후 재방문하며, 삭제와 커서는 함께 commit/rollback합니다. 보호 조건·live guard 우선 잠금·감사 CASCADE·unknown backlog·앞 단계 확정 수량은 유지합니다. 자식 FK fan-out과 실행 시간의 절대 상한을 보장하지는 않습니다.
- **315 적용 순서:** migration을 신규 API보다 먼저 적용합니다. 기존 함수 signature/EXECUTE 권한은 유지하고 runtime·scraper의 커서 직접 접근은 차단합니다. 운영 DB 변경·배포는 이번 로컬 수정에 포함하지 않았습니다.
- **live 동기 refresh 입력 필터:** migration 316이 session/head/pending/availability의 실제 소비·검토 입력이 같은 UPDATE를 제외합니다. 원본 metadata·ignored-history 저장은 유지하며 모델 재조회·행 잠금을 줄입니다. title/topic/premiere·availability 시각·old/new key와 문장별 동기 결과, BEFORE MVCC guard·stale snapshot 거절·영수증·권한은 보존합니다. 운영 적용·배포는 포함하지 않았습니다.
- **통합 빌드·회귀 보완:** AP 전송 목록에 payload 크기 검증·collector 판정 모듈을 포함하고 notification cleanup은 비NULL ID로 후보·삭제 수를 집계합니다. Holodex 회귀 fixture는 대상 endpoint만 원자적으로 계상하며 circuit threshold 기대값은 유지합니다.
- **알림 cleanup 경합:** 최종 DELETE가 최신 terminal 상태와 cutoff를 재검사하여 enqueue가 재활성화한 PENDING 또는 시각이 갱신된 행을 삭제하지 않습니다. immutable request·exhausted 정책은 유지합니다.
- **cleanup 배치 진행 보정:** 최신 상태 재검사로 실제 삭제 수가 줄어도 가득 찬 후보 창 뒤의 만료 행을 계속 정리합니다. 종료는 후보 수, 반환 집계는 실제 삭제 수를 사용하며 재활성화된 PENDING과 앞 배치 commit 수량을 보존합니다.
- **YouTube 발송 집계:** 부모를 정렬된 순서로 잠근 다음 READ COMMITTED statement에서 자식을 집계하여 대기 전 snapshot이 최신 SENT 결과를 덮지 않게 합니다. 기존 상태 우선순위·시각·ledger·no-op write 억제는 유지합니다.
- **RSS 부분 실패 복구:** 최신 게시 시각 대신 현재 feed ID의 bounded 존재 조회로 미등록 항목을 선택합니다. 저장 실패를 `FailedEvents`와 반환 오류에 보존하므로 기존 scheduler 실패 경로가 동작하고, 다음 feed에도 남은 오래된 누락 항목을 다시 저장합니다.
- **RSS 예약 복구 기회 보존:** 저장 실패로 예약된 retry에서 실패 feed가 HTTP/조회 오류를 내고 다른 feed만 성공하면 남은 예약까지 취소하던 경로를 수정합니다. 기존 잔여 retry만 보존하고 성공 시 취소·소진 시 종료하며, 횟수·시각·timeout과 regular 부분 성공 정책은 바꾸지 않습니다.
- **수집 due 관측:** IDLE 지표가 scheduler와 같은 현재 cadence 단축식을 사용합니다. eligibility·ACTIVE·DEFERRED·만료 projection의 의미는 유지하며 scheduler 자체를 변경한 것은 아닙니다.
- **alarm 발송 lease 시계:** send-unit와 전체 member 잠금을 얻은 뒤의 단일 DB 시각으로 leased fence·sending 시각·연장 기한을 계산합니다. 대기 중 만료된 묶음은 외부 발송 전에 거절하며, standalone MarkSending의 부분 전이와 sending 상태의 적법한 재발급은 유지합니다.
- **YouTube 발송 후 상태 저장:** provider 호출의 취소는 유지하고, 발송 결과의 상태 전이·기존 read-back은 호출자 취소와 분리된 하나의 5초 예산으로 처리합니다. 불명 결과의 추가 발송·재발급 횟수 증가는 없습니다.
- **원천 publish 원인 보존:** 정상 stream 종료의 DB 오류를 결과 누락 검사보다 먼저 반환하여 SQLSTATE·취소 원인을 보존합니다. 앞선 Scan·행 불변식 실패와 transaction 원자성은 유지합니다.
- **payload 저장 크기 일치:** raw/canonical과 PostgreSQL JSONB-text의 기존 1MiB 한도를 모두 검사하고 초과는 기존 `response_too_large/RESOURCE_LIMIT`로 분류합니다. 검증에서 얻은 canonical payload를 소유한 복사본에 사용하여 저장 표현·digest를 일치시키며 caller 입력과 중복·충돌 계약은 보존합니다.
- **RSS 실패 계상:** 복구된 panic을 포함해 예약 source마다 attempted/failed 결과를 남기고 취소·deadline·DB 원인을 보존합니다. 일반 실행의 부분 성공에는 새 retry를 만들지 않습니다.
- **방 카탈로그 중복 갱신:** 같은 인스턴스의 겹친 전체 조회·저장 순회를 한 번으로 합칩니다. owner는 기존 5초 예산, waiter는 개별 취소를 사용하며, 저장 중 만료돼도 확보한 방 정보는 유지합니다. negative cache나 DB 오류 시 HTTP fallback은 추가하지 않습니다.
- **alarm metric 정합성:** retention의 기존 7개 삭제 label은 commit 확인 뒤에만 계상하고, immutable MarkSending 실패도 기존 실패 counter에 한 번 반영합니다. rollback·lock 미획득·commit 결과 불명에는 확정 삭제를 계상하지 않습니다.
- **관리자 통계 중복 수집 수정:** 캐시 miss 뒤 다른 수집이 완료되면 늦게 시작한 singleflight leader가 유효한 snapshot을 다시 사용합니다. TTL 안의 CPU·메모리·원격 health 재수집을 막고, 호출자별 복사본·취소·만료 후 재수집 계약은 유지합니다.
- **deunhealth 이미지 빌드:** 의존성 일괄 갱신(#604)이 `gosplash`를 upstream이 쓰지 않는 v0.2.0으로 올려 `BuildDate` 필드가 없어 빌드가 실패했습니다. upstream 고정값 v0.1.0으로 되돌립니다. 실행 중인 deunhealth에는 변경이 없습니다.
- **수명 동기 읽기 모델:** migration 312가 활성 session 또는 head의 좁은 상태와 현재 일치하는 최신 검토 시각을 `youtube_live_runtime_state`에 유지합니다. observability·live-check는 이 표를 직접 읽으며 시간 경과·신선도·미래 일정 필터는 조회 시 계산합니다. 원본·영수증 의미 사실과 불일치 진단은 보존합니다.
- **쓰기 직렬화:** API 관측 확정·종료 finalizer·증거 보존 삭제·수동 영수증·head 복구가 claim/원본 행보다 먼저 공통 MVCC guard를 잡습니다. 문장 trigger는 원본·영수증 변경과 같은 트랜잭션에서 모델을 갱신하고 삭제·FK·롤백을 반영합니다. 오래된 REPEATABLE READ/SERIALIZABLE snapshot은 직렬화 오류로 종료하며 새 재시도를 추가하지 않습니다. 모든 관측 확정이 직렬화되므로 쓰기 처리량과 잠금 대기 비용이 늘 수 있습니다.
- **312 적용 경계:** API writer 정지 → 신규 이미지의 `db-migrate` → 신규 API 시작 순서입니다. migration 동안 source 관측 INSERT도 일시 대기할 수 있습니다. worker·collector 바이너리는 변경하지 않습니다. 이전 API는 guard보다 source 행을 먼저 잠글 수 있으므로 **이미지만 되돌리는 롤백은 지원하지 않습니다**. 원본을 보존하고 guard-first 규약의 수정 이미지로 전진 복구합니다. 별도 기록 역할을 신설할 때 기존 기록 권한 외에 `lock_youtube_live_runtime_state()`와 `youtube_live_review_receipt_value(jsonb)` 실행 권한이 필요합니다. 원본 조회·행 잠금, 영수증과 facts INSERT 및 기존 기록·snapshot·semantic-facts 함수 권한도 유지해야 합니다.
- **Target bundle 동기 집계:** migration 313은 CURRENT/STAGING의 enabled target만 `youtube_collection_runtime_bundles`로 묶고, 8개 observation-to-job mapping을 한 DB view가 소유합니다. observability는 RPC 수·최소 cadence/생성/eligibility 시각을 직접 읽되 세대 만료와 lease/현재 시각은 계속 조회 시 판정합니다. target 변경은 영향받은 bundle만 갱신하며 RETIRED 이력과 기술적 heartbeat를 재집계하지 않습니다.
- **313 잠금·권한:** 기존 projection guard의 실제 MVCC 쓰기로 고정 snapshot 충돌을 드러내고, bounded retention도 guard부터 획득합니다. guard 누락은 자동 복구하지 않고 실패합니다. 기존 collector의 잠금과 `guard_key` 열 권한은 유지하며 새 `runtime_writer` 직접 쓰기는 허용하지 않습니다. API 정지 후 migration과 신규 API를 적용합니다. 312를 포함한 `14e102dca` API의 기존 projection 잠금 SQL은 호환되지만, 313을 모르는 이전 migration runner는 새 ledger에 실행하지 않습니다.
- **지연된 LIVE 승격의 부재 재평가:** 일반 `PARTIAL`도 저장된 현재 slot의 COMPLETE 증거를 조회합니다. metadata-only 영상이 늦은 `video_live_check`로 LIVE가 된 뒤 같은 slot의 Holodex PARTIAL을 받으면 기존 부재를 빠뜨리지 않습니다. 시간 인덱스 조회를 사용하며 slot이 없거나 payload에 포함된 영상의 ignored 배열은 읽지 않습니다. PARTIAL 자체는 부재를 만들지 않고, 중복 계산·기존 TOAST 보존·grace 조건을 유지합니다.
- **의존성·CI 핀 일괄 갱신:** openai-go v3.78.0, miniredis v2.40.0, grpc-gateway v2.32.0, x/crypto v0.58.0·x/net v0.61.0·x/sys v0.49.0 등 Go 모듈과 deunhealth·gosu 재빌드 graph(x/sys v0.49.0, gosu source `40506998e34a`)를 갱신합니다. deunhealth `gosplash`는 upstream 고정값 v0.1.0을 유지합니다. Dockerfile frontend 1.28.0, node 24.22.0 digest, nginx 1.31.7, `@types/node` 24.19.2와 po-sandbox lockfile 전이 의존성을 갱신합니다. CI는 Node 24.22.0, uv 0.13.1, staticcheck 2026.2.2, Trivy 0.76.0을 사용합니다. 실행 동작 변경은 없습니다.
- **별도 검토 기록 역할 복구:** migration 314는 이미 `record_youtube_live_review` 실행 권한을 가진 역할에 generated-column decoder 실행 권한만 추가합니다. 기록·표·PUBLIC·역할 membership 권한은 넓히지 않으며 SERIALIZABLE·CAS·append-only 계약을 유지합니다. 배포된 310·312·313은 수정하지 않습니다. 314 이후 신설하는 역할에는 위 의존 권한을 별도로 부여해야 합니다.

## v10.1.0 - 2026-10-14

- **검토 SQL 원본 공유:** observability·live-check가 활성 session/head/availability의 scalar 원본을 함께 쓰고, live-check는 미래 UPCOMING을 보조 원본 조회 전에 제외합니다. 판정 결과·신선도·한도·불일치 진단은 유지합니다.
- **불변 영수증 타입 투영:** migration 310이 기존 영수증 `facts`에서 비교용 `semantic_value`를 저장 생성 열로 만듭니다. 현재 원본은 캐시하지 않습니다. JSON의 누락/추가 키·다른 scalar 타입은 정확한 왕복 검사로 비일치 처리하며, 숫자 정밀도·NULL object·최신 일치 영수증 의미를 유지합니다. 타입/열 충돌은 migration을 중단합니다.
- **적용 경계:** 310은 작은 영수증 사실 표를 `ACCESS EXCLUSIVE`로 다시 쓰며 `lock_timeout=3s`를 유지합니다. 새 API보다 먼저 적용하고 원본 영수증은 수정·삭제하지 않습니다. collector·worker에는 변경이 없습니다.
- **알림 시각 수정(alarm-worker):** tier 스케줄러가 채널 점검 판정과 상태 갱신에 서로 다른 시각을 써서 Tier1 채널을 격번 tick마다 건너뛰었습니다. 그래서 5분 전 알림의 절반 가까이가 약 60초 늦거나 빠졌습니다. 이제 iteration 시각 하나를 씁니다.
- **생일 방송 중복 발행 제거(alarm-worker):** 30분마다 재평가할 때 이미 `sent`로 받은 방은 다시 발행하지 않습니다. 이전에는 publish의 69%가 dedupe로 버려지는 중복이었습니다.
- **`alarm_upcoming_checkpoints` 삭제:** worker는 더 이상 이 표에 쓰지 않고, migration 311이 표를 지웁니다. 읽는 곳은 없었습니다. 이 표에 쓰는 10.0.0 이하 worker가 실행 중이면 upcoming 후보 저장이 실패하므로, 새 worker로 바꾼 뒤 311을 적용합니다. 적용 뒤 worker를 10.0.0 이하로 되돌리려면 표를 먼저 다시 만들어야 합니다.
- **bot durable worker(hololive-api):** 유휴 worker 중 하나만 polling하고 나머지는 wake를 기다립니다. 유휴 claim이 약 16/s에서 줄어듭니다. 행을 잡으면 처리 전에 다음 worker를 깨워 polling 안전망이 끊기지 않습니다.
- **설정(alarm-worker):** `ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH`, `CELEBRATION_CHECK_HOUR_KST`, `CELEBRATION_RUN_INTERVAL_MS`, `BIRTHDAY_STREAM_*_MS`는 키가 있는데 비어 있거나 공백뿐이면 기본값 대신 기동에 실패합니다. 배포 전 alarm-worker env에 `KEY=` 형태의 빈 값이 없는지 확인합니다.
- **YouTube plane DB 시간 예산(hololive-api):** claim·live end·projection refresh·replay도 `TransactionTimeout`(기본 10초) 안에서 끝나야 하며, 넘으면 슬롯을 돌려주고 오류로 드러냅니다. 기동 중 첫 projection refresh가 이 시한을 넘으면 YouTube plane 생성이 실패합니다.
- **관측:** `hololive_db_pool_*`, `hololive_http_client_*`, `hololive_youtube_plane_db_slot_wait_seconds`·`_hold_seconds`를 추가합니다. `hololive_youtube_plane_retention_tick_seconds`에 `phase`(source, projection) 라벨이 붙습니다. YouTube plane consume·retention 카운터의 알려진 라벨을 0으로 미리 만들어 첫 사건도 `increase()`에 잡힙니다. `hololive_alarm_subscriber_db_fallback_total`은 cache가 있는데 확정하지 못한 DB 조회만 셉니다.
- **collector 로그·분류:** 수송 타임아웃과 DB 연결 실패(연결 거부, SQLSTATE 08·57P01~03)를 내부 불변식 위반이 아닌 timeout·일시 장애로 분류합니다. DB 장애 때 1초마다 반복되던 후보 탐색 WARN을 묶어 남기고, 종료 중 스스로 취소한 잡은 실패로 기록하지 않습니다.
- **성능:** calendar 명령이 멤버 사진을 4개씩 병렬로 받습니다. 첫 렌더가 6.7 s였습니다. admin 통계 수집은 singleflight로 합칩니다.
- **정리:** 오류 문구를 `action: context` 형식으로, slog error 속성을 한 형태로 맞춥니다. 관리자 activity 로그를 포함해 room_id를 가명화합니다. shared `pkg/util`을 해체하고, 한 모듈만 쓰던 shared 패키지·상수(`apperrors`, `imagehost`, `cleanupctx`, `sendoutcome`, alarm tier 시각 등)를 소유 모듈로 옮기며 중복 구현을 공용 facility로 바꿉니다.

## v10.0.0 - 2026-10-14

- **YouTube SQL hot path:** 활성 방송 집합을 과거 종료 이력과 분리하고, 검토 영수증은 typed timestamp의 의미 사실을 집합으로 비교합니다. Projection heartbeat는 현재 세대 범위에서 바뀐 `not_before`만 갱신하며, 커밋된 세대·header 버전·reasons 해시가 같으면 reasons 재조회를 생략합니다.
- **Live 관측:** 일반 `PARTIAL`은 쓰지 않는 ignored-absence 배열과 absence slot을 읽지 않습니다. 최초 positive replay와 scoped negative는 필요한 이력을 계속 읽고, 종료 head 저장은 배열을 비웁니다. 현재 slot만 필요한 조회는 시간 인덱스를 사용하는 별도 SQL입니다.
- **Collector lease:** 기존 job의 획득은 projection guard와 통합 admission/acquire SQL로 처리하며 무의미한 INSERT를 없앱니다. 최초 생성에 성공한 경우에만 추가 UPDATE로 충돌 대기 이후의 새 TTL을 부여합니다. 잠금·membership·fence·오류 판정은 유지합니다.
- **감사·퇴역 데이터 계약 변경:** application은 원본 삭제 시 `ON DELETE CASCADE`로 즉시 삭제하며 고아 3일 유예·중복 5열·별도 고아 보존 경로를 제거합니다. 퇴역 viewer 소비·재처리 계약과 viewer 표본 3종·구 통계 2종을 제거합니다. 살아 있는 관측·재처리·projection·lease 의무가 남으면 삭제 migration이 중단됩니다.
- **저장 공간:** source 관측과 live/content absence의 두 SHA-256 열은 `bytea(32 bytes)`로 저장하고 Go·공개 해시 표현은 소문자 hex를 유지합니다. 종료 배열·동결 pending 잔재를 배치 정리하고, 주요 TOAST 열 및 DB 기본 압축을 `lz4`로 지정합니다. 기존 TOAST 값이 설정 변경만으로 재압축되는 것은 아닙니다. 퇴역 Projection 보존은 승인된 **2일**이며 활성 보호 조건은 유지합니다.
- **배포 경계:** migration 순서는 `304 → 305 → 306 → 309 → 307 → 308`입니다. `309`는 고아 인덱스를 제거하는 단일 concurrent 문장입니다. 구 publisher/consumer를 모두 중지한 점검 창에서 스키마를 변경하고 새 API·worker·collector/issuer fleet를 시작해야 합니다. 삭제 데이터와 부분 완료된 binary-type cutover는 구 이미지로 되돌릴 수 없습니다. 이번 작업은 사용자가 새 DB 백업 없이 적용하도록 승인했습니다.

## v9.3.2 - 2026-10-14

- **발송 backlog 관측 주기 분리(alarm-worker):** 발송 backlog·격리 집계 조회를 retention runner에서 떼어 별도 runner가 60초마다(조회 시한 10초) 실행합니다. 이전에는 retention 주기(운영 기본 1시간)에만 갱신되어 `HololiveOutboxStale` 같은 경보가 최대 1시간 묵은 값을 평가했습니다. retention 주기·설정은 바꾸지 않으며, 관측 실패는 runner를 멈추지 않고 실패 counter·`alarm_dispatch_pg_backlog_snapshot_success=0`으로 드러납니다.
- **관측 최신성 지표:** `alarm_dispatch_pg_backlog_snapshot_last_success_timestamp_seconds`(alarm-worker), `bot_reply_outbox_manual_review_snapshot_success`·`_last_success_timestamp_seconds`, `hololive_youtube_plane_queue_snapshot_success`·`_last_success_timestamp_seconds`(hololive-api)를 추가합니다. 조회가 실패하면 건수·경과는 마지막 성공 값을 유지하고 success만 0으로 내립니다. 이전에는 manual review와 YouTube plane queue 조회 실패가 이전 값을 현재 값처럼 남겼습니다.
- **stdout 사본 비동기화:** alarm-worker와 youtube-collector도 hololive-api처럼 stdout 사본을 bounded queue로 씁니다. stdout sink가 막혀도 로깅 호출이 멈추지 않고, 큐가 가득 차면 stdout 사본 줄만 버립니다. 파일 로그는 그대로 동기 기록하며, 유실은 기존 `iris_stack_logging_stdout_dropped_total`/`_truncated_total`로 드러납니다.
- **의존성·관측:** API·alarm-worker·shared·collector·DB 테스트 모듈을 게시된 `shared-go/v2 v2.13.0`으로 올리고, 공용 metrics server가 파일 로그 쓰기 실패 counter `iris_stack_logging_file_write_failed_total`을 노출합니다.

## v9.3.1 - 2026-10-13

- **관측:** 공용 metrics server/router에서 stdout 사본 drop/truncate counter를 노출합니다. 기존 인증·worker 진단은 유지하며 API·alarm-worker·shared·collector·DB 테스트 모듈을 게시된 `shared-go/v2 v2.12.0`으로 고정합니다.
- **CI:** 일회용 DB·Valkey·이미지/ingress 검증 컨테이너에 `io.iris.observability.logs=exclude`를 명시해 중앙 stream churn을 줄입니다. 백업·운영 컨테이너와 로컬 진단 출력은 유지합니다.
- **릴리스 범위:** 루트·API·alarm-worker 버전을 `9.3.1`로 맞추고 API·alarm-worker·collector a·b·c·d를 같은 source snapshot으로 릴리스합니다. 이번 변경에 DB migration은 없으며 실제 배포·수용 검증과는 구분합니다.

## v9.3.0 - 2026-10-13

- **최초공개는 공개 5분 전에만 알림:** 예정 최초공개는 발견 시 `NEW_VIDEO` "N분 후 공개 예정"을 만들지 않고(hololive-api), 공개 전환 뒤에도 영상 알림을 만들지 않습니다. 구독자는 일반 방송과 같은 분 전 target(`선행공개 N분 전`)과 일정 변경 알림을 받고, 분 전 target을 모두 지난 뒤 발견된 경우 분 전 알림을 받지 못한 방에만 시작 알림(`선행공개 시작`)을 받습니다(alarm-worker). v9.2.0의 공개 예정 안내 중복 생략과 그 조회는 쓸모가 없어져 지웠습니다.
- **native AP 로그 회전 정정:** v9.2.2의 `maxsize 10M`·`daily` 설정을 `size 10M`만 남기도록 바로잡습니다. collector 로그는 앱(lumberjack)이 기본 5 MB마다 회전하고 압축 백업을 archive로 옮기므로, 호스트 logrotate는 앱 회전이 멈췄을 때만 10M에서 회전하는 안전망입니다. `daily`나 `maxsize`가 있으면 앱 회전과 별도로 매일 copytruncate 회전이 일어납니다. Osaka·Osaka2의 설정 파일은 v9.2.2 설정의 첫 일일 실행(2026-10-13 00:00 UTC) 전에 이 내용으로 바꿨습니다.
- **배포:** migration은 없습니다. hololive-api를 먼저 교체한 뒤 alarm-worker를 교체합니다. native AP 로그 회전 정정은 Osaka·Osaka2의 logrotate 설정 파일에 이미 반영했고 이미지를 교체하지 않습니다. api만 v9.2.1로 되돌리면 발견 시 알림이 다시 나가고, worker만 9.2.2로 되돌리면 분 전 target 뒤에 발견된 최초공개의 시작 알림이 빠집니다.

## v9.2.2 - 2026-10-13

- **생일 방송 시간대 창:** 멤버 생일은 현지 날짜이므로 생일 방송 판정 창을 KST 생일 전날 21:00부터 다음 날 17:00까지로 넓힙니다. ID·EN처럼 KST보다 늦은 지역의 생일 저녁 방송도 생일 방송으로 알리고, 생일 다음 날 17시까지는 전날 생일도 계속 평가합니다. 전날 밤에 시작한 카운트다운은 생일축하 발송 뒤에도 예정·진행 상태일 때만 알립니다. 생일 날짜와 다른 KST 날짜에 시작하는 방송은 `⏰ 01/02 15:04`처럼 날짜를 붙여 표시합니다.
- **생일 방송 판정:** 판정 창 안의 생일 멤버 채널 와꾸 중 제목에 생일 키워드(`birthday`·`bday`·`b-day`·`誕生日`·`誕生祭`·`誕生会`·`生誕`·`たんじょうび`·`バースデー`·`バースデイ`·`バースディ`·`생일`·`ulang tahun`·`ultah`, NFKC·대소문자 무시)가 있는 것만 생일 방송으로 알립니다. 이전에는 최초공개 VLOG·스폰서 방송도 생일 방송으로 보냈고, 이들이 멤버·일자 상한 3건을 먼저 차지하면 같은 날의 실제 생일 방송 알림이 막혔습니다. 상한과 제목 변경 뒤 재평가는 저장 제목이 생일 키워드를 포함하는 기발행 event만 세므로, 배포 전에 잘못 발행된 event는 실제 생일 방송을 막지 않고 제목이 바뀌어도 다시 나가지 않습니다. 키워드가 없는 생일 방송은 알리지 않습니다.
- **native AP 로그 회전:** Osaka·Osaka2 host-native 배포가 설치하는 `/etc/logrotate.d/hololive-bot`에서 `daily`와 함께 쓰던 `size 10M`을 `maxsize 10M`으로 바꿉니다. `size`가 `daily`를 덮어써 10M 전에는 회전하지 않았고, 두 호스트의 archive는 비어 있었습니다. 이제 매일 회전해 14개를 보관합니다. `copytruncate`에서 효과가 없던 `create`를 지웁니다.
- **배포:** migration은 없습니다. alarm-worker만 다시 릴리스하고 hololive-api는 9.2.1을 유지합니다. native AP 로그 회전은 Osaka·Osaka2의 logrotate 설정 파일만 바꾸며 이미지를 교체하지 않습니다. 9.2.0 worker로 되돌리면 제목 판정과 시간대 창이 사라지고, 그날 잘못 발행된 event가 다시 상한을 차지합니다.

## v9.2.1 - 2026-10-13

- **queue 보존 삭제 인덱스:** migration 302·303이 `source_observation_queue`에 만료 판정용 부분 인덱스 `(processed_at, observation_id) WHERE status = 'PROCESSED'`, `(dead_lettered_at, observation_id) WHERE status = 'DEAD_LETTER'`를 `CREATE INDEX CONCURRENTLY`로 만듭니다. 보존 삭제가 PROCESSED 이력 전체 대신 만료 행만 읽습니다(운영 2026-10-12: 약 22만 행, 평균 166 ms·최대 9.2초). SQL은 바꾸지 않습니다.
- **배포:** migration을 먼저 적용한 뒤 hololive-api를 교체합니다. youtube-collector와 PO issuer는 v8.0.0 이후의 collector 변경(오류 어휘 alias 정리, 쓰지 않는 공유 함수 삭제, Go builder 이미지 digest와 npm 갱신, 동작 변화 없음)을 반영하려고 같은 revision으로 a·b·c·d 모두 다시 배포합니다. alarm-worker는 9.2.0을 유지합니다.

## v9.2.0 - 2026-10-12

- **최초공개 분 전 알림:** 확정 최초공개도 일반 방송과 같은 분 전 target에서 `선행공개 N분 전` 알림을 보내고, 그 알림을 받은 방에는 일정 변경 알림도 보냅니다. 발견 시 나가는 `NEW_VIDEO` `N분 후 공개 예정` 안내가 가장 이른 target 시각 이후에 생성됐다면 그 안내가 공개 직전 알림을 대신하므로 분 전 알림을 보내지 않습니다. 대기 후보는 선정 뒤에 실패하지 않은 안내가 생겼을 때만 `stream_ended`로 종료합니다. 최초공개의 live catchup 시작 알림은 계속 보내지 않습니다.
- **배포:** migration은 없습니다. alarm-worker만 다시 릴리스하고 hololive-api는 9.1.2를 유지합니다. 9.0.0으로 되돌리면 최초공개 분 전 알림이 다시 빠집니다.

## v9.1.2 - 2026-10-12

- **send unit 없는 legacy delivery 정리:** migration 301이 send unit 도입(141) 전에 만든 종료 delivery(`sent`·`cancelled`)를 지우고 `alarm_dispatch_deliveries.send_unit_id`를 NOT NULL로 바꿉니다. 처리되지 않은 dlq·quarantined legacy 행이 있으면 지우지 않고 실패합니다. 같은 조건을 포함하게 된 활성 상태 CHECK(224)는 지웁니다.
- **관리자 발송 조회:** send unit 없는 행을 단일 항목으로 보여 주던 묶음 조회 분기를 지웠습니다. `sendUnitId`는 항상 값이 있습니다.
- **배포 순서:** migration을 먼저 적용한 뒤 hololive-api를 교체합니다. alarm-worker는 9.0.0을 유지합니다. 지운 legacy 행은 rollback으로 돌아오지 않습니다.

## v9.1.1 - 2026-10-12

- **방송 이력 다음 페이지·채널 조회:** since·커서 조건을 행 비교 범위로 바꾸고 전체·채널 목록 문장을 나눴습니다. 다음 페이지와 prepared statement의 generic plan에서도 정렬 인덱스에서 한 페이지만 읽습니다. 결과와 페이지 순서는 같고 migration은 없습니다. hololive-api만 다시 릴리스하고 alarm-worker는 9.0.0을 유지합니다.

## v9.1.0 - 2026-10-12

- **쓰지 않는 인덱스 삭제:** 런타임 조회가 쓰지 않는 인덱스 8개(`youtube_content_alarm_tracking` 3개, `youtube_community_shorts_alarm_states` 2개, `youtube_community_shorts_source_posts`·`youtube_videos`·`alarm_dispatch_deliveries` 각 1개)를 migration 283~290으로 지웁니다.
- **live 세션 HOT 갱신:** 방송 이력 정렬값을 `youtube_live_sessions.history_sort_at` 저장 생성 열로 두고 fillfactor를 90으로 낮춥니다(291). 정렬·채널·상태 인덱스를 `last_seen_at` 없이 새로 만들고(292~295) 기존 인덱스 5개를 지웁니다(296~300). 같은 상태의 반복 관측이 `last_seen_at`만 바꿀 때 인덱스를 다시 쓰지 않습니다. 방송 이력의 결과와 페이지 순서는 같습니다.
- **배포 순서:** migration을 먼저 적용한 뒤 hololive-api를 교체합니다. 291은 ACCESS EXCLUSIVE로 테이블을 다시 씁니다(운영 약 1만 행). alarm-worker는 9.0.0을 유지합니다. v9.0.1 api로 되돌려도 동작하지만, 그 이미지의 방송 이력 조회는 정렬 인덱스 없이 실행됩니다.
- CI Python을 CPython 3.15.0과 uv 0.13.0(runner 최소 버전·GitHub bootstrap)으로, govulncheck를 v1.9.0으로, `actions/setup-node`를 v7.1.0으로, collector helper npm을 11.21.0으로 갱신하고 Go 빌더 이미지 `golang:1.27.2-alpine3.24` digest를 현재 게시본으로 맞췄습니다.

## v9.0.1 - 2026-10-12

- **오류 문구 시드 정리:** v9.0.0부터 읽지 않는 `external_api_call_failed`·`cache_connection_failed` 시드 행을 migration 282로 지웁니다. v9.0.0 운영 배포 뒤 적용하며, 이후 v8.0.0으로 되돌리려면 두 행을 다시 넣어야 합니다. hololive-api만 다시 릴리스하고 alarm-worker는 9.0.0을 유지합니다.

## v9.0.0 - 2026-10-12

- **계약 변경 — 관리자 비밀번호 재설정 제거:** 2026-10-02부터 503만 반환하던 `POST /api/auth/password/reset-request`·`POST /api/auth/password/reset`과 재설정 서비스를 지웠습니다. 두 경로는 이제 NoRoute 응답(API key 없음 401, 맞으면 404)을 받습니다. migration 281이 빈 `auth_password_reset_tokens`를 지우고, 행이 남아 있으면 지우지 않고 실패합니다.
- **계약 변경 — Stream JSON의 Chzzk·Twitch 필드 제거:** 채우는 생산자가 없던 10개 키가 Stream HTTP 응답과 alarm dispatch event payload에서 빠집니다. event payload hash가 바뀌므로 alarm-worker는 처리 중인 dispatch event가 없을 때 교체합니다.
- **notification delivery 재적재:** 고정 요청 없이 FAILED가 된 행은 발송된 적이 없으므로 시도 횟수와 무관하게 생산자가 새 payload로 다시 적재합니다. 쓰지 않게 된 `known_unsent` 표시를 지웠습니다. api와 alarm-worker는 같은 배포에서 교체합니다.
- **alarm dispatch:** 빈 `alarm_type`을 Live로 간주하던 숨은 기본값을 지우고 오류로 거부합니다. 운영 생산자는 모두 종류를 채웁니다.
- **SQL:** 상태 상수를 부분 인덱스 조건과 맞추고, dispatch retention 삭제를 상태별 문장으로 나눠 만료 행 전체를 읽지 않게 했습니다. 결과는 같습니다.
- **오류 문구:** 운영에서 쓰이지 않던 `external_api_call_failed`·`cache_connection_failed`를 코드와 기동 검증에서 뺐습니다. 시드 행은 rollback 호환을 위해 다음 배포 뒤 별도 migration으로 지웁니다.

## v8.0.0 - 2026-10-11

- **공유 payload:** producer·worker가 notification delivery의 저장 payload/request 계약을 한 곳에서 사용합니다. 기존 JSON 바이트·고정 request·재무장 CAS·exhaustion fence와 runtime/ledger 책임 분리는 유지합니다.
- **trigger 계약 변경:** weekly/monthly/membernews 결과를 `queued`·`skipped`·`partial` 및 건수로 반환하고 admin client·run-now 소비자가 본문을 보존합니다. enqueue를 delivered로 표현하거나 partial 전체를 자동 재전송하지 않습니다. 구형 결과 본문 호환 분기는 두지 않습니다.
- **digest 수명:** 15분 lock TTL 안에 14분 협조적 실행 예산을 두되 기존 HTTP 5분처럼 더 짧은 deadline은 유지합니다. Stop은 취소 후 실제 join하며, 비협조적 외부 호출의 TTL 초과를 막는 lease 갱신/fencing을 추가한 것은 아닙니다.
- **SDK 통합 검증:** `iris-client-go/v3 v3.1.0`·`shared-go/v2 v2.10.0`을 pin하고 남아 있던 합성 오류 fixture를 실제 SDK HTTP 응답으로 이관했습니다. transport의 안전 재발급·종단 거절·이미지 경로가 새 오류 출처 계약을 검증합니다.
- **검증:** 관련 HTTP·scheduler·저장소 회귀와 격리 PostgreSQL 검사를 통과했습니다. 별도 실행 파일로 실제 handler/admin client의 결과·단일 호출, 14분/기존 5분 deadline, cancel-then-join, 실제 migration/enqueue·고정 request 재무장·exhaustion fence를 확인했습니다. 운영 배포·실제 알림 발송은 수행하지 않았습니다.

- **보안 CI:** SDK·shared-go 체크아웃을 스택에서 검증·게시한 commit SHA로 맞췄습니다. 과거 release tag의
  단독 모듈 manifest가 `x/net v0.59.0`을 선택하던 문제를 고칩니다. 모든 workspace 모듈의 취약점 검사와
  실패 조건은 유지하며, runtime `go.mod`·릴리스 태그·운영 배포는 바꾸지 않습니다.
- **의존성:** 모든 Go 모듈·빌더·분석기 빌드를 Go 1.27.2로 맞추고 `golang.org/x/net` v0.60.0,
  OpenAI v3.74.0, Prometheus client v1.25.0과 사용 중인 간접 의존성을 갱신했습니다.
  PostgreSQL의 gosu·deunhealth 빌드 의존성, npm 11.19.1·uv 0.12.24와 lockfile도 갱신하며 운영 배포는 포함하지 않습니다.
- staticcheck의 x/tools를 v0.51.0으로 올려 Go 1.27.2 export data v5를 읽도록 했습니다.
  generic method의 source/export 순서 보정과 다운로드·바이너리 무결성 검증은 유지합니다.
- 테스트 전용 PostgreSQL을 tmpfs에서 실행하고 내구성 쓰기를 꺼 빌드 호스트의 디스크 쓰기를 줄였습니다.
  운영 DB와 외부 테스트 DB의 설정은 유지하며, 임시 DB는 crash durability 검증 대상으로 사용하지 않습니다.
- 중앙 collector paired cutover는 issuer 이미지가 있는데 기준 영수증(`po-current-c`)이 없으면, 백업·rollback 태그를 만들기
  전에 원인을 출력하고 거절합니다. 이전에는 rollback 태그와 부분 백업을 남긴 뒤 메시지 없이 끝났습니다. 호스트 교체 때
  영수증을 옮기는 규칙과 유실 시 검토 영수증으로 복원하는 절차를 collector runbook에 추가했습니다.

## v7.2.10 - 2026-10-08

- v7.2.8·v7.2.9 검토 후속입니다. collector는 IDLE lease의 실효 due를 `LEAST(next_due_at, scheduled_for + 현재 target 주기)`로
  판정해, target 주기가 줄면 이전 긴 주기(최대 60분)의 남은 대기 없이 현재 주기의 다음 slot에서 재개합니다. 늦춰진 UPCOMING
  영상 확인이 positive 뒤 공개 방송으로 바뀌어도 종료 확인이 최대 60분 늦어지지 않습니다.
- 해소 불가 추적은 추적 시작 이후에 관측된 positive만 지웁니다. 늦게 도착한 이른 관측이 추적을 초기화해 종료 하한이 밀리던
  문제를 고쳤습니다. 모든 positive보다 이른 추적값(7.2.7 롤백 기간 잔여값)은 reducer·consumer·projection이 추적 없음으로
  보고 다시 추적하므로, 롤백 뒤 재전진에서 지속 시간 없이 끝나지 않습니다.
- head가 없는 `metadata_only`·`legacy_unknown` UPCOMING은 추적을 저장할 수 없어 `UNRESOLVABLE_TRACKED`만 반복 기록하던 것을
  기존 `IDENTITY_UNCONFIRMED`·`IDENTITY_MISMATCH` 결정으로 되돌렸습니다.
- 회귀 시험: 관측 이후 positive 보존, 채널 `/live` 음성 술어의 부정 사례, 주기 단축·유지 시 lease 재개. 계약 v2·migration
  규약(같은 트랜잭션 안 VALIDATE 잠금)·계획 문서 수치를 정정했습니다.
- `hololive-api`와 `youtube-collector` 7.2.10을 릴리스합니다. DB migration과 `hololive-alarm-worker`는 바뀌지 않습니다.

## v7.2.9 - 2026-10-08

- 비공개·삭제된 예약 영상처럼 identity를 확인할 수 없는 UPCOMING 세션의 영상 확인이 2분마다 같은 UNKNOWN만 반복하던
  부담을 줄입니다. `identity_missing`·`identity_mismatch` 확인은 head의 `unresolvable_since`로 추적만 하고(끝내지 않음),
  projection이 추적 기간에 따라 그 영상 확인 target의 재확인 주기를 10분·1시간·24시간 경계에서 10분·30분·1시간으로
  늦춥니다. positive가 오면 다음 projection에서 2분으로 돌아옵니다. 시작 감지는 채널 스냅샷 positive가 맡으므로 시작
  알림은 늦어지지 않으며, LIVE 영상 확인 주기와 검토 영수증 흐름은 그대로입니다.
- `hololive-api` 7.2.9만 릴리스하며 DB migration, `hololive-alarm-worker`, collector는 바뀌지 않습니다.

## v7.2.8 - 2026-10-08

- 종료 전에 비공개·삭제로 바뀐 방송의 LIVE 세션이 영구히 LIVE로 남아 영상 확인을 계속 받던 문제를 끝냅니다. 시작을
  관측한 LIVE 세션의 영상 확인이 `identity_missing`으로만 `YOUTUBE_PLANE_LIVE_UNRESOLVABLE_GRACE_SECONDS`(기본 600초)
  이상 이어지고, 같은 채널의 신선한 `/live` identity 확인 음성이 있으며, 신선한 positive가 없으면 `UNRESOLVABLE_VIDEO`
  사유로 종료합니다. 종료 시각은 첫 `identity_missing` 관측 시각이며 시작 시각·알림은 바꾸지 않습니다. UPCOMING 세션과
  `identity_mismatch` 등 다른 UNKNOWN 사유는 대상이 아닙니다.
- migration 280은 `youtube_live_reconciliation_heads`에 `unresolvable_since` 열을 더하고 `end_reason` 어휘 CHECK에
  `UNRESOLVABLE_VIDEO`를 추가합니다(`chk_youtube_live_reconciliation_heads_end_reason_vocab`로 개명).
- `hololive-api` 7.2.8과 migration 280을 릴리스하며 `hololive-alarm-worker`와 collector는 바뀌지 않습니다.

## v7.2.7 - 2026-10-08

- `!라이브`가 방송 중인 멤버가 없을 때 "현재 방송 상태를 확인할 수 없습니다."로만 답하던 원인 두 가지를 고쳤습니다.
  종료 전에 비공개·삭제로 바뀐 방송은 익명 영상 확인에서 `identity_missing` UNKNOWN만 남겨 영구히 stale LIVE였으므로,
  LiveQuery는 신선한 `identity_missing`을 기존 공개 불가 사실과 같은 시각 경계에서 stale LIVE 제외 근거로 씁니다. 다른
  UNKNOWN 사유와 만료는 계속 차단하며 세션의 수명 상태는 바꾸지 않습니다.
- 시작을 관측하지 못한 세션의 검증된 종료는 `ended_at` 이후의 positive가 grace(기본 2분) 안에서 관측된 동안만 미루고,
  그 뒤에는 공급자 지연으로 보고 upstream 종료 시각으로 끝냅니다. 1초 송출 뒤 공급자가 잠시 UPCOMING을 유지한 영상이
  `INVALID_END_TIMELINE`으로 영구히 남아 채널을 `confirming_end`로 막던 문제를 해소합니다. 시작·알림은 만들지 않습니다.
- `hololive-api` 7.2.7만 릴리스하며 `hololive-alarm-worker`와 collector, DB migration은 바뀌지 않습니다.

## v7.2.6 - 2026-10-08

- v7.2.5 배포 뒤 운영 재측정에서 남은 비용 두 가지를 줄였습니다. live 적재는 종료 후보가 없는 저장된 `ENDED` 세션의
  `youtube_live_pending_ends` 행을 읽거나 `FOR UPDATE`로 잠그지 않습니다(운영 초당 약 25행 잠금, 값이 같은 upsert가
  호출의 84%). 동결된 D2 행은 어떤 판정도 읽거나 바꾸지 않으며 삭제 대상도 아니므로 결정은 같습니다.
- migration 279는 검토 영수증의 의미 사실을 기록 시 `youtube_live_review_receipt_facts`에 저장하고,
  `youtube_live_review_current_receipt`가 영수증 원본을 다시 계산하는 대신 저장값과 비교합니다. projection guard 아래에서
  5초마다 도는 `live_check_videos.sql`이 운영 평균 25.7 ms(앱 DB CPU 8.5%)였고, 시험 DB에서 영수증 53건 기준 중앙값이
  24.5 ms에서 4.0 ms로 줄었으며 판정은 같습니다. 기존 영수증은 같은 migration에서 채우고, runtime은 새 표를 조회만 합니다.
- `hololive-api` 7.2.6만 릴리스하며 `hololive-alarm-worker`와 collector는 바뀌지 않습니다.

## v7.2.5 - 2026-10-08

- 운영 PostgreSQL과 fresh 재생 사이의 스키마 drift를 맞추는 migration 271–276을 추가했습니다. 적용하면
  `members.created_at`·`updated_at`(135행의 값 포함)과 `youtube_notification_outbox.dispatched_at` 열, 빈
  `streams`·`alarm_dispatch_outbox` 테이블과 각 시퀀스, `check_aliases_structure`·`check_status`·
  `youtube_notification_outbox_status_check` 제약, `idx_source_application_live_origin` 인덱스가 영구히 지워집니다.
  마지막 전체 DB 복원 검증(2026-10-07) 뒤 백업이 중단됐으므로 적용 전에 `members`의 시각 값을 추출해 보관합니다.
  `dispatched_at`에 값이 생겼거나 운영 전용 테이블에 행이 있으면 지우지 않고 멈춥니다. 함께
  `idle_in_transaction_session_timeout=5min`을 다시 선언하고 별명 형식 CHECK·NOT NULL과 template revision FK를 적용하며,
  별명 없이 만든 멤버는 빈 ko·ja 별명으로 저장합니다. 이름 열 폭 변경 뒤 기존 연결의 캐시된 member 조회가 한 번씩
  `cached plan must not change result type`로 실패할 수 있으므로 적용 직후 `hololive-api`와 `hololive-alarm-worker`를
  순차 재기동합니다.
- 운영: 중앙 API·alarm-worker·collector `c`와 PostgreSQL을 50GB 서울 VM으로 이전하고 기존 중앙 주소를 승계했습니다. AP `a/b/d`와 애플리케이션·DB 계약은 유지하며, 오사카 중앙 VM은 삭제하고 100GB 부트 볼륨만 이전 시점 복구 자료로 보존합니다. 운영 토폴로지·복구·시크릿 소유 문서를 갱신했습니다.
- 중앙·서울 PO issuer가 공통 `json-file` 5 MiB × 3 로그 회전 설정을 상속하도록 수정했습니다.
- 운영 중인 PostgreSQL CPU·대기 계측 소스를 main에 통합했습니다. 고정 버전의 `pg_stat_kcache`·`pg_wait_sampling`, 선택적 preload와 `hololive_observability`의 조회 전용 권한을 유지합니다. 서울 마스터 경로·운영 runbook을 현행화했으며, 이번 소스 통합으로 운영 이미지나 DB를 재배포하지 않습니다.
  통합 뒤 실패하던 아키텍처 게이트 두 건을 복구했습니다. 실험용 SQL은 허용 위치인 `testqueries/`로 옮기고, PostgreSQL 18 런타임 검사는 postgres 이미지를 참조하는 모든 `FROM`이 같은 digest 고정 이미지일 때 multi-stage `AS` stage를 인정합니다.
- non-race 테스트 선별은 locale과 무관한 바이트 순서로 비교하며, 목록 수집·정렬·비교 오류를 게이트 실패로 전달합니다.
- CI에서 최소 지원 버전 이상의 설치된 Go 검사 도구를 재사용하고 collector helper 의존성은 한 번만 설치합니다.
  운영 Go 테스트의 JSON 검사는 유지하며 성공 로그의 중복 출력을 줄이고 실패 때 진단을 출력합니다.
- CI의 Go 1.27 강제와 toolchain 스탬프를 제거하고 Go가 manifest 요구사항으로 버전을 선택하게 합니다.
  `GOTOOLCHAIN` 기본값은 `auto`이며 명시한 환경값을 유지합니다.
- YouTube live 상태 적재를 줄였습니다. live snapshot은 채널 범위에서 `UPCOMING`·`LIVE` 세션과 종료 후보(`next_end_check_at`)가
  남은 `ENDED` 세션만 잠그고, 모든 적재 경로가 session 행이 있는 `ENDED` 세션의 `ignored_absence_scheduled_for` 배열을 읽지 않습니다.
  배열을 읽지 않은 세션을 저장하면 기존 배열을 유지하고, reducer가 읽지 않은 이력이 필요하면 결정 없이 오류를 반환합니다.
  코드만 바꾸며 migration은 없습니다.
- YouTube live reducer가 session 행이 있는 `ENDED` 세션에 대한 반복 종료·취소 사실을 `youtube_live_pending_ends`에
  다시 기록하지 않습니다. 운영에서 초당 약 24행이던 HOT 없는 갱신의 84%가 이 재기록이었습니다. 세션·적용 기록·부재
  판정은 그대로이며, 기존 D2 진단 행은 지우거나 갱신하지 않고 남깁니다. session 행이 없는 영상과 head만 남은 영상의
  종료는 지금처럼 보관합니다. head에 아직 due가 아닌 종료 후보가 남은 `ENDED` 세션에서 반복 종료가 pending 관측 ID를
  바꿔 commit 때 head 후보 FK를 깨던 실패도 사라집니다.
- 검증된 영상 종료가 거부되어 LIVE 세션이 고착되던 문제를 고쳤습니다. 운영 LIVE 세션 22개 중 21개가 6시간–7일 동안
  남아 있었습니다. Holodex positive 시각은 수집 예정 시각이라 실제 종료보다 늦을 수 있는데, video live check consumer가
  이를 YouTube가 확인한 `ended_at`과 직접 비교해 하루 9,475번 `INVALID_END_TIMELINE`으로 거부했기 때문입니다. 이 비교는
  시작 미관측 terminal 경로에만 두고, LIVE positive clock이 있는 세션은 관측 시각 기준 positive 비교와 grace를 쓰는 일반
  명시적 종료 계약을 따릅니다. 고착된 세션은 배포 뒤 다음 영상 확인에서 실제 종료 시각으로 끝나며 알림은 만들지 않습니다.
- migration 277은 source observation·application retention 함수에 `jit = off`를 둡니다. 두 함수 내부 문장의 운영 실행
  시간 중 61–96%가 JIT 컴파일이었습니다. migration 278은 술어에 쓰이지 않는 heads `ignored_absence_scheduled_for` 배열의
  통계 수집을 꺼 평균 384 ms이던 autoanalyze 비용을 줄입니다. 질의 결과·계획·잠금은 바뀌지 않습니다.
- `hololive-api` 7.2.5와 `hololive-alarm-worker` 6.1.2를 함께 릴리스합니다.

## v7.2.4 - 2026-10-06

- CI Python 런타임을 uv 0.12.23과 CPython 3.14.8로 고정했습니다.
- **CI 중복 제거:** pre-push `local-ci.sh`와 PR CI matrix는 race 테스트를 실행하고 Go가 선택한 테스트 파일을
  비교해 race 빌드에서 빠지는 테스트가 있는 패키지만 일반 테스트로 보완합니다. golangci govet와 겹치던
  workspace `go vet`과 결과를 쓰지 않던 `go list -m -u`를 지웠습니다. security workflow의 NilAway도 로컬과
  같은 `go vet -p 1 -vettool`의 unitchecker로 의존성 분석을 재사용합니다.
- **CI 정리:** Docker Compose 설치를 `.github/actions/docker-compose` composite action 하나로 합치고 v5.6.0으로,
  Trivy를 0.75.0으로 올렸습니다. security workflow의 sibling checkout을 go.mod와 같은 shared-go v2.9.3·iris-client-go
  v3.0.4로 맞췄습니다.
- **테스트 정리:** 호출되지 않던 셸 테스트를 삭제하고, 스크립트 본문만 grep하던 단언을
  지웠습니다. 패키지명·문서 경로·퇴역 기한·toolchain 파일 형태·DB 프레임워크 이름 검사도 삭제했습니다.
  전체 모듈 테스트를 재귀 실행하던 workspace 테스트도 삭제했습니다. 권한 상승 방지와 compose 보안 설정 검사는 유지합니다.
- **퇴역 제공자 드레인 가드 삭제:** authoritative DB에 Twitch·Chzzk 단독 dispatch event가 0건임을 확인하고
  alarm-worker의 드레인 종단 가드를 지웠습니다. `domain.Stream`의 Twitch·Chzzk 필드는 Stream HTTP 응답 계약 확인 전까지
  유지합니다.
- YouTube.js helper의 `@types/node`를 24.19.1로 올렸습니다.

- 공용 Holodex 클라이언트가 429의 `Retry-After`를 초 또는 HTTP 날짜로 읽고 같은
  클라이언트의 후속 요청에도 대기를 유지합니다. 요청 취소로 서버 대기 시각을 지우지 않습니다.
  `HOLODEX_MAX_RETRY_ATTEMPTS`는 최초 요청 이후 0~9회 재시도에, `HOLODEX_REQUEST_DELAY_MS`는
  로컬 요청 간격에 반영합니다(0은 로컬 간격 제한 해제). 동시 요청 수가 0 이하이면 설정 오류로 거부합니다.
- collector는 명시적인 COOLDOWN 재시도 시각을 일반 오류의 `RetryMax`로 단축하지 않습니다.
  일반 defer와 부분 발행 후 defer 모두 DB의 최소 대기를 적용하고 같은 슬롯·fence를 유지합니다.
  일반 오류 backoff의 최소·최대 범위와 충돌 처리 계약은 유지합니다.
- 개인세·전체 조직의 예정 조회에도 요청 기간 상한을 적용하고, primary·fallback·캐시 결과를
  같은 시간 창으로 필터합니다. 시간 미정 허용, 비양수 기간의 기존 무상한 의미, 일반 조직 API의
  168시간 제한과 기존 50개 상한은 유지합니다.
- 대형 부재 이력이 있는 검토 영상의 지표 조회가 1초 예산을 넘던 문제를 수정합니다(migration 263).
  지속 판정은 필요한 수명 사실만 읽고, 전체 배열 직렬화·hash는 기존 기록 CAS에만 남깁니다.
  영수증·원본·보존 정책을 바꾸지 않으며 17,006개 slot을 가진 53개 head로 예산 회귀를 검증합니다.
- alarm-worker가 보존 격리 총량·경과는 그대로 두고 `alarm_dispatch_pg_unreviewed_quarantined_rows`와
  `alarm_dispatch_pg_oldest_unreviewed_quarantined_age_seconds`를 추가합니다. 행의 현재 revision·상태와
  정확히 일치하는 closeout receipt만 검토 완료로 보며, 바뀐 행이나 재격리된 행은 다시 셉니다.
- 라이브 검토 영수증의 면제를 수명 사실 기준으로 판정합니다(migration 262). 기록 CAS는 계속 전체
  원본 digest이고, 저장 snapshot은 무시한 부재 slot 배열을 개수·digest로 요약해 상한을 지킵니다.
  기존 영수증은 다시 쓰지 않고 같은 사실 추출로 인정합니다. 미래 일정 `legacy_unknown` UPCOMING은
  `legacy_not_due`로 분리하고 `unresolved_unreviewed`에서 뺍니다. 운영 collector의 RPC 간격은
  코드 기본값 2초를 유지한 채 운영 env에서 1초로 override합니다.
- projection 유효기간 설정을 줄여도 이전의 긴 만료가 남던 회귀를 수정합니다. 마지막으로
  수락한 refresh 시각을 header에 기록하여 최신 호출의 TTL 변경을 반영하고, 과거 호출의
  역순 갱신은 막습니다. migration 이전의 refresh 시각은 추정하지 않습니다.
- projection heartbeat의 유효기간을 header 한 행에서 관리하고, 실제로 바뀐 `not_before`만
  갱신합니다. collector는 전송량 회귀가 확인된 전체 target/lease 캐시를 채택하지 않고
  기존 관계형 후보 조회와 `LIMIT+1`을 유지합니다. LIVE 확인의 신선도 계산은 API로
  옮기고 같은 canonical head의 재저장을 생략합니다. 261은 새 API·collector를 함께
  전환해야 하는 계약이며 운영 적용·보존 단축·삭제·물리 회수는 포함하지 않습니다.
- 수집기 내부 계약·snapshot·membership 검증의 소유자를 `internal/runtime/collection`으로
  통합하고, PARTIAL 충돌은 오류를 보존한 terminal completion으로 정산합니다. 동시 실행의
  RPC 예약은 전체 호출 수와 inflight를 반영하며, content 근거 조회도 기존 limiter를 따릅니다.
- 일정 항목의 관측 시각(migration 258), ACL 변경 직렬화, 발송 단위 복구와 최신 delivery tuple
  검증을 합류합니다. 취소된 미발송 작업은 재큐잉하고 이미 발송된 작업의 결과 불명은 유지합니다.
- API·alarm-worker·shared의 `iris-client-go/v3`를 검증된 v3.0.4로 올려 webhook body timeout의
  `408` 재시도 계약을 반영합니다.
- replay epoch의 실제 단일행 제약을 claim planner에도 명시하여 빈 epoch 테이블의 과대 추정과
  불필요한 JIT 최적화를 제거합니다. 5만 행 backlog 실측은 custom 1,253→161ms,
  generic 697→160ms이며, 1초 제한·방문 행 예산·JIT 설정은 유지합니다. 실패한 EXPLAIN의
  원래 오류를 보존하고 rollback 뒤 prepared statement를 정리하여 다음 조회를 오염시키지 않습니다.

- 수집 projection의 구조적 membership과 `not_before` eligibility를 분리합니다. migration 259의 CURRENT guard·job scope·연속 membership fence로 무관한 세대 교체는 진행 중인 수집을 취소하지 않고, 자기 대상 변경·제거/재추가와 이전 owner는 계속 거부합니다. 대상 상한 초과는 잘라서 발행하지 않고 마지막 정상 projection을 유지한 채 오류로 드러냅니다.
- 일반 영상 목록 generation 2와 migration 260의 부분 목록 기준·항목별 pending 증거를 도입합니다. 목록 두 RPC 뒤 기존 limiter 안에서 player RPC 최대 두 번으로 게시 시각/최초공개를 확인합니다. 첫 기준 목록과 과거 재등장은 조용히 저장하고, 근거가 나중에 확인된 신규 영상·새 최초공개만 한 번 알립니다. 기존 영상·관측을 자동 backfill하지 않습니다.
- 신규성 근거 조회의 예약 시한과 오류가 겹쳐도 설정·취소·소유권·내부 오류를 정상 목록으로 바꾸지 않습니다. 허용된 요청 실패·timeout은 근거 미확정으로 보존하며, 비강등 오류가 조회 시한 뒤 반환되는 회귀를 추가합니다.
- 영상/Shorts 목록을 채널·kind별 선행 관측 순서로 소비하고 활성 backlog와 claim 후보 조회 비용을 구분하여 제한합니다. collector는 실제 checkpoint 전진의 수락 간격과 마지막 수락 시각을 기록하고, superseded·empty terminal 결과를 실제 durable 상태와 맞춥니다.
- 수락 간격 집계가 늘어난 publish 경로에서 단일행 잠금 함수의 기본 cardinality 추정이 반복 JIT를 유발하던 성능 회귀를 고칩니다. migration 259가 UNIQUE 키로 최대 한 행을 반환하는 두 함수에 `ROWS 1`을 지정하며, JIT·성능 예산·잠금/충돌 판정은 유지합니다.
- migration 259/260과 새 API·collector/helper는 승인된 coordinated cutover가 필요합니다. 기존 lock 함수와 video-list 세대가 바뀌므로 혼합 버전 실행이나 이전 image만의 롤백을 지원하지 않습니다. 이 미출시 변경은 운영 적용을 뜻하지 않습니다.
- YouTube 방송 제목·예정 시각을 필드별 관측 시각으로 갱신합니다. 늦게 도착한 과거 관측이나 같은 시각의 충돌은 정본 값을 덮지 않습니다. migration 257이 nullable `title_observed_at`을 추가하며 과거 시각은 추정하지 않습니다. 새 API 실행 전에 이 migration이 필요합니다.
- 알림 구독 set이 유실됐을 때 새 구독 하나로 부분 캐시를 만들지 않고 기존 DB 조회 경로를 유지합니다. 채널 registry 삭제는 DB의 잔여 구독으로 판단하며, registry 자체가 유실되면 전체 DB 구독으로 복구합니다.
- 방송 checker와 생일 방송 후보 조회가 `last_seen_at` 대신 LIVE의 `status_observed_at`, UPCOMING의 `schedule_observed_at`으로 최신성을 판정합니다. 관측 시각이 없거나 현재보다 미래이면 후보로 사용하지 않으며, 새 일정의 `last_seen_at`에는 예약 시각이 아닌 수신 시각을 기록합니다. LIVE guardrail 로그와 생일 알림 runbook도 실제 관측 시각에 맞춥니다.
- 구독 변경 전에 종류별 캐시를 무효화하고, DB commit 뒤 후처리를 요청 취소와 분리된 최대 5초 context로 수행합니다. commit 직후 취소·캐시 장애가 나도 오래된 수신자 집합이 남지 않으며, 무효화 실패 시에는 DB를 변경하지 않습니다.
- Holodex·생일 멤버 조회를 기다린 뒤 관측 최신성 기준 시각을 다시 측정합니다. 대기 중 들어온 정상 관측이 주기 시작 시각보다 늦다는 이유로 제외되던 회귀를 고칩니다.
- 7.2.0에서 hololive 사본만 바꿨던 NilAway 모델 빌드 입력(`scripts/ci/nilaway-models`)을 stack 정본 `tools/nilaway`와 다시 맞춥니다. stack 계약 검사가 두 사본의 차이로 메타 저장소 게시를 막았습니다. 같은 변경에서 지운 검사가 남긴, 쓰이지 않는 테스트 변수 하나도 지웁니다.
- YouTube 수집기 내부 상태와 계산을 줄입니다. 검증된 job 계약 집합과 고정 SQL 자산을 한 번만 만들고, 체크포인트는 수집 결과에 따로 저장하지 않고 발행 직전에 관측에서 만듭니다. discovery는 저장소가 이미 정렬·중복 제거하는 제외 키를 다시 정렬하지 않고, `PrepareEnvelope`는 방금 만든 canonical 값으로 최종 검증합니다. 공개 `Validate`, 발행 fence 검증, COMPLETE/PARTIAL 원자성과 식별자·해시 규칙은 그대로입니다.
- YouTube 수집기가 profile의 `collection.queue.max_age`를 넘긴 로컬 대기 항목을 lease 취득 전에 버리고 다음 항목을 계속 처리합니다. stale discard와 경고를 기록하며 DB terminal은 만들지 않습니다. `--check-worker-profile`은 TTL 30분 상한 등 runtime과 같은 profile 수치 정책을 운영 환경 변수 없이 검증합니다. collector 전용 설정과 profile 로더는 `hololive-youtube-collector/internal/config`가 소유합니다.
- 로컬 수집 pacing이 Valkey 구현을 가져오지 않도록 공용 rate-limit 판정과 Valkey backend를 분리하고, community RPC DTO를 collector로 옮겨 HTML scraper 의존을 없앱니다. helper raw 반환형에서 RPC 경계가 붙이는 `protocol_version`을 제외하고 소스 문자열 검사 대신 실제 타입·행동 검증을 유지합니다. 반복 exact-key schema와 날짜 판정 primitive를 공유하되 wire 형식과 provider별 실패 정책은 바꾸지 않습니다.
- 수집 job 계약·target snapshot·입력/결과·retry 값을 SQL에 의존하지 않는 collector `collection` core로 분리합니다. Scheduler가 실행 정책을 소유하고 defer는 typed 입력을 사용합니다. Provider 슬롯은 snapshot 조회·수집 뒤 반환하므로 DB publish의 잠금 대기가 다음 provider 수집을 막지 않습니다. Admission timeout의 durable 정책, 진단 마스킹과 DB clock clamp는 유지합니다.
- API만 사용하는 canonical 저장을 `internal/youtube/canonicalwrite`의 transaction 전용 함수로 옮깁니다. Shared batch repository·독립 transaction 경로·미사용 latency persister·post-commit 재조립·keyword 분기와 전달 wrapper를 지웁니다. Community는 canonical payload를 한 번 정규화해 조회·저장·알림에 쓰며 ID 병합 순서, NULL·시각·저장 JSON과 replay 중복 방지는 유지합니다.
- 호출자가 없던 YouTube HTML/RSS scraper·parser·admission·분산 limiter·생성자·설정 필드와 호환 경로를 모두 제거합니다. 로컬 pacing은 collector로 옮기고 사용하지 않는 goquery/gjson 계열 의존성을 정리합니다. 공식 일정·Holodex 경로는 유지합니다. 운영 env 변경이나 배포는 포함하지 않습니다.
- Collector의 tracing slot 정책과 profile fixture를 collector로 회수하고, shared tracking의 자체 transaction 경로는 기존 `dbx`를 사용합니다. 호출자 없는 transaction helper·cleanupctx와 퇴역 이름 grep·구현 개수 검사는 제거합니다. 공유 worker 계약과 실제 다중 소비자 primitive는 유지합니다.
- 다중 종류의 정상 발행 테스트가 수집 설정과 같은 운영 lease 예산을 사용합니다. 기존 50ms 갱신 제한은 DB 발행 지연을 갱신 실패로 바꿔 테스트를 불안정하게 만들었습니다. 운영 예산과 갱신 실패 테스트는 변경하지 않습니다.

## v7.2.3 - 2026-10-03

- RETIRED 수집 대상 이력을 정리할 때 120초당 한 배치만 실행하던 처리량 부족을 보완합니다. 배치당 1,000행과 기존 전체 DB 시한 안에서 최대 64개 배치를 독립 commit하고, 진척 없음·오류·취소 시 중단합니다. 7일 보존·CURRENT/lease 보호·수집 대상 선정은 유지하며, 뒤 배치 실패에도 앞서 완료한 삭제 계수를 보존합니다.

## v7.2.2 - 2026-10-02

- migration 256이 `youtube_notification_delivery.request_snapshot_allowed`를 지웁니다. 7.2.0부터 이 열을 읽는 코드가 없고, 표시가 `false`인 행은 모두 `SENT`·`FAILED`입니다. 끝나지 않은 행에 이 표시가 남아 있으면 열을 남기고 실패합니다.
- Seoul Compose 배포가 루트 `VERSION`도 보내 원격 tree가 실제 릴리스 버전을 가리킵니다. 이전에는 예전 전체 tree 배포가 남긴 `3.5.2`가 그대로 있었습니다.

## v7.2.1 - 2026-10-02

- 7.2.0 YouTube 수집기에서 일부 채널의 영상·shorts 목록 수집이 `parser_drift/DATA_CONTRACT`(`cursor start exceeds 512 bytes`)로 실패하던 회귀를 고칩니다. helper가 실제 InnerTube continuation token을 coverage cursor로 내보내기 시작했는데, 이 token은 수 KB까지 커지고 요청마다 달라질 수 있어 512 byte 상한을 넘고 observation scope도 흔들었습니다. 이제 token은 helper 안에서 cursor 반복 감지에만 쓰고, helper 응답과 coverage에는 cursor를 싣지 않습니다. 저장된 observation을 읽기 위해 계약의 cursor 필드는 남깁니다.
- Seoul Compose 배포(`ap-deploy.sh`)가 원격에 보내는 소스를 실행 checkout의 HEAD commit과 collector `go.mod`가 고정한 shared-go tag에서 만듭니다. 이전에는 `<저장소 상위>/hololive-bot`을 그대로 보내, 이름이 다른 worktree에서 실행하면 빌드 원본이 아닌 기본 checkout(미커밋 변경 포함)을 설치하려 했습니다.
- Seoul PO issuer의 active receipt를 같은 디렉터리의 rename으로 교체합니다. 소유자가 다른 기존 receipt를 `cp`로 덮어쓰지 못해 배포가 실패했고, rollback은 issuer를 지운 뒤 같은 단계에서 멈춰 issuer 없이 남았습니다. rollback은 이제 issuer를 멈추기 전에 receipt를 바꿉니다.

## v7.2.0 - 2026-10-02

- 봇 응답 저장이나 Iris 전송의 결과가 불명이면 추가 응답(대체 텍스트·공통 오류 문구)을 보내지 않고 결과 불명으로 남깁니다. Iris가 접수한 reply의 handoff 결과를 확정하지 못하면 자동 재발송하지 않는 `manual_review`로 정산합니다.
- 멤버 조회 backend 오류를 "찾을 수 없음" 응답으로 바꾸지 않고 오류로 드러냅니다.
- 관리자 비밀번호 재설정 두 경로(`POST /api/auth/password/reset-request`, `POST /api/auth/password/reset`)는 재설정 링크 전달 수단이 없어 HTTP 503으로 응답합니다. 허용 IP의 요청은 본문·계정과 관계없이 같은 응답을 받고 토큰 발급·소비나 비밀번호 변경을 하지 않습니다.
- LLM 요약의 `source_url`과 `member`는 입력 후보의 URL과 정본 멤버 이름(`MatchedMembers`)에 정확히 맞아야 합니다. 이름 안의 쉼표를 멤버 경계로 잘못 나누던 승인도 고칩니다. 인증 오류 로그의 키는 비식별화합니다.
- YouTube 영상 제목을 500바이트로 자를 때 UTF-8 문자 중간에서 자르지 않습니다. 같은 호스트의 limiter 표식이 충돌하던 문제와, 채널 metadata의 vanity URL 전체를 handle로 저장하던 문제를 고칩니다.
- YouTube 수집기는 helper가 인식하지 못한 일반 upstream 실패(탭 부재, 응답 본문 연결 오류, 400·403·404)를 내부 불변식 위반(fatal)으로 올리지 않고 기존 `collection_failed/TRANSIENT`로 분류합니다. 탭 부재는 원문 증거로 판정하고, 파싱 중 유실된 탭은 `parser_drift/DATA_CONTRACT`로 거부합니다. helper 종료 중 늦게 생성된 자원도 정리합니다. 실제 YouTube.js 객체 기준으로 profile·photo·attachment·continuation 매핑을 고칩니다.
- 묶음 YouTube 알림에서 payload를 읽지 못한 항목을 빈 줄로 보내지 않고, 알 수 없는 알림 종류를 영상 template으로 렌더링하지 않습니다. 둘 다 재시도 가능한 `format_message` 실패로 처리합니다.
- 멤버 표시명 예외의 중간 단계(최신 `alarms.member_name`, 알람 등록 때 사용자가 보낸 이름)를 지웁니다. 제거 조건(두 지표 30일 0회, 구독 채널 21개 모두 members 한국어 표시명 보유)을 확인했습니다. `hololive_alarm_member_name_fallback_channels`와 `hololive_alarm_member_name_caller_fallback_total`이 없어지고, 종단 문구(`misc/vtuber_fallback`) 사용 횟수를 `hololive_youtube_outbox_member_name_missing_total`로 셉니다.
- UNIT B 채널의 YouTube 알림 대상 조회를 진행자 조합마다 하지 않고 채널·알림 종류당 한 번만 합니다. 수신 집합과 조회 실패 의미는 같습니다.
- 알람 추가·삭제·초기화·캐시 warm의 lock 대기를 `hololive_alarm_service_mutation_lock_wait_seconds{operation}`로 기록합니다. `hololive_alarm_service_operation_duration_seconds`는 이제 lock 대기를 포함한 전체 응답 시간입니다.
- 수집 관측 저장소를 collector가 쓰는 발행(`sourceobservation`)과 API가 쓰는 소비(`sourceobservation/consume`)로 나누고, DB 자원 생성을 `providers/dbresource`로 분리합니다. collector가 링크하는 shared 패키지가 51개에서 36개로 줄었습니다. YouTube 알림 payload는 `pkg/contracts/youtubeoutbox` 타입으로 저장하며 JSON 형태는 그대로입니다. 동작 변경은 없습니다.
- 소스·스크립트 문자열만 확인하던 검사(collector 6개, batch 저장소 소유권 grep, QUIC UDP buffer 스크립트 문자열)를 stack 규칙에 따라 지웁니다.
- 행사·멤버 뉴스 알림 발송 배치가 방 순서와 동시 실행 슬롯을 기다리는 동안 60초 claim lease가 지나 전송 전에 건너뛰던 문제를 고칩니다. 실행 슬롯이 빈 방의 첫 항목만 claim하므로 대기 항목이 lease를 미리 쓰지 않고, 짧은 backoff로 다시 due가 된 항목도 같은 poll에서는 한 번만 처리합니다. 발송 attempt 시간은 worker profile의 `notification_delivery.executor.attempt_timeout`을 따르며, dispatcher 설정이 0 이하이면 기본값으로 바꾸지 않고 기동에 실패합니다.
- 달력 명령에서 같은 달을 함께 기다리던 요청이 먼저 온 요청의 취소 때문에 실패하거나, 취소된 요청이 공유 조회가 끝날 때까지 기다리던 문제를 고칩니다. 공유 조회는 요청 취소와 분리하되 기존 봇 명령 예산(10초) 안에서 끝납니다.
- 멤버 캐시의 epoch 재조회가 Valkey client를 닫은 뒤에도 15초마다 반복되던 문제를 고칩니다. 캐시의 `Close`가 구독·재조회 작업을 취소하고 끝날 때까지 기다리며, infra 모듈과 LLM plane은 Valkey·DB를 닫기 전에 이를 호출합니다.
- Holodex 장애 때 live check가 Holodex 재시도로 주기 예산(45초)을 모두 써서, 저장된 live session으로 계속하는 경로까지 기한 초과로 실패하던 문제를 고칩니다. 2026-09-30 장애에서 이 경로가 108회 쓰였고 그중 101회가 주기 실패였습니다. Holodex 조회는 이제 주기 안에서 최대 25초입니다.
- 제거 조건을 충족한 `YOUTUBE_PRODUCER_*` 퇴역 env 기동 거절 가드를 지웁니다. 2026-10-02에 중앙·AP env 파일, stack-secrets master 사본, 실행 중인 API·worker 프로세스에서 키가 0건이고, 남은 복구점도 키 정리 이후 상태임을 확인했습니다.
- YouTube 발송이 migration 249 이전 행을 구분하던 검사(`ErrLegacyRequestEvidence`)를 지웁니다. 해당 표시를 가진 67행은 모두 `SENT`이거나 revive 창 밖의 `FAILED`입니다. `request_snapshot_allowed` 열은 별도 migration으로 지울 때까지 남습니다.
- YouTube 알림의 멤버 표시명을 Valkey `alarm:member_names` 대신 PostgreSQL 정본(`alarm.Repository.GetMemberName`)에서 메시지마다 읽습니다. 조회 오류는 `misc/vtuber_fallback` 문구로 보내지 않고 재시도 가능한 `format_message` 실패로 처리합니다. 묶음 메시지를 만들지 못하면 개별 발송으로 바꾸지 않고 묶음 전체를 같은 실패로 재시도합니다.
- 남은 fallback 경로를 trigger·한도·종단·telemetry·owner·검토 조건을 갖춘 예외 계약으로 문서화합니다. 대상은 grouped 전송 permanent 실패의 개별 발송, Holodex 실패 때 저장된 live session 사용, live catchup 억제 marker 오류, membernews 결정적 digest, 공식 일정 fallback, 도움말·달력 이미지의 텍스트 대체, major event 링크의 HEAD→GET 재확인입니다. 새 metric은 `hololive_youtube_outbox_grouped_send_fallback_total{result}`, `hololive_bot_image_text_fallback_total{command,reason}`, `hololive_majorevent_link_get_fallback_total{result}`와 `hololive_alarm_youtube_persisted_live_sessions_total`의 `result="holodex_error_continued"`입니다.
- 도움말·달력 명령은 이미지 provider·renderer·전송 callback이 없으면 텍스트로 조용히 바꾸지 않고 명령 오류를 반환합니다. 운영 조립은 이 의존성을 항상 연결합니다.
- major event 링크 검사는 HEAD 오류를 문자열이 아니라 시간 제한·연결 재설정 타입으로만 판단해 GET으로 다시 확인합니다. `timeout`이나 `method not allowed` 문구만 들어간 다른 오류는 더 이상 GET으로 재확인하지 않습니다.
- membernews·major event 요약 prompt의 JSON 직렬화 실패를 고정 문자열로 대체하지 않고 요약 실패로 처리합니다. 최종 출력 검토 prompt를 만들지 못하면 Warn을 남기고 조립된 본문을 유지합니다.
- YouTube 발송 dispatcher는 0 이하 설정값을 기본값으로 바꾸지 않고 생성 때 거절합니다. alarm dispatch 보존 작업은 실행 중에 기본값을 채우지 않고, 설정 로더가 0 이하 값을 기동 때 거절합니다. 기본값은 설정 로더 한 곳에만 둡니다. YouTube 발송의 `delivery_send_timeout_ms`가 `youtube_delivery.executor.attempt_timeout`과 다르면 기동에 실패합니다.
- 공식 일정 fallback 하나만 쓰던 범용 실행기 `internal/service/fallback`을 Holodex provider 안으로 합칩니다. `hololive_fallback_primary_total`·`hololive_fallback_execution_total`의 이름과 label은 그대로입니다.
- `hololive_messagestrings_lookup_fallback_total`의 이름을 `hololive_messagestrings_lookup_miss_total`로 바꿉니다(label 동일). 호출자 대체 문구가 없어진 뒤로 이 metric은 "조회했지만 값이 없음"을 셉니다. 이전 이름의 시계열은 이어지지 않습니다.
- 중복 구현을 기존 공통 기능으로 바꿉니다. nil 판정은 shared-go `reflectutil.IsNil`, 이미지 body 상한 읽기는 `httputil.ReadAllLimited`, YouTube 발송 SQL helper는 `dbx`를 씁니다. 영상 필드 변경은 `dbx.ExecStatements`로 묶어 보냅니다. YouTube 발송의 MetricsRecorder는 claim 해제를 하지 않고 기록만 합니다.
- YouTube 알림 renderer(`format`)를 alarm-worker로 옮기고, 같은 기능을 다시 감싸던 worker 쪽 `MessageFormatter` wrapper를 지웁니다. 실제 seed template과 달라진 template 복사본 기반 테스트를 지우고, 실제 seed 본문을 렌더링하는 golden 테스트를 template 패키지에 둡니다.
- alarm-worker만 쓰는 shared 패키지 4개(`alarmservice`, `alarmcache`, `alarm/dedup`, `alarm/queue`)를 `hololive-alarm-worker/internal`로 옮깁니다. 동작 변경은 없습니다.
- 구형 호환 alias와 잔재를 지웁니다(scraper parser 재바인딩, reply handoff 오류 별칭, bot privacylog pass-through, 미사용 claim 구현, delivery format 별칭, `AlarmService.Close`). 삭제된 이름이나 퇴역 서비스의 재등장을 grep으로 확인하던 CI·deploy 검사와 테스트 단언도 함께 지웁니다.
- 채널 수치 통계 기능을 완전히 제거합니다. 구독자 수 명령·통계 템플릿·producer/consumer·공개 채널 통계 필드·도메인과 통계 전용 DB 객체를 migration 234로 함께 제거하며, 채널 profile/photo·방송·일정·알림 구독은 유지합니다. 미확정 통계 발송이나 예상 밖 durable MILESTONE 이력이 있으면 migration을 거절합니다.
- 통계 contract 삭제의 FK 확인이 큰 application 이력을 전체 스캔하여 timeout 나던 문제를 고칩니다. manifest에서 244의 임시 참조 인덱스를 234보다 먼저 동시 생성하고, 이행 끝에 243으로 동시 삭제합니다. timeout·무결성 검사를 완화하지 않으며 정상 적재에 추가 인덱스를 남기지 않습니다.
- 대용량 인덱스의 승인된 점검 창에 `db-migrate --statement-timeout`을 명시할 수 있습니다. 기본 4분, 허용 상한 10분, 전체 명령 15분과 기존 lock 제한은 유지하며 음수·상한 초과는 실행 전에 거절합니다.
- 관측 저장량을 줄입니다. migration 238–242는 JSONB(LZ4) payload를 kind/schema/canonical profile+전체 32바이트 digest로 공유하고 슬롯별 관측은 독립 보존합니다. bounded backfill, 참조 보호 GC, 손상 시 오류, 동시 insert 가시성 검증을 포함합니다. 구 payload 열·backfill 함수와 임시 index는 cutover에서 제거합니다. API와 collector fleet의 coordinated cutover가 필요하며 구 이미지 단독 rollback은 불가능합니다.
- 보관 기본값을 일반 원본 7일·schedule/absence 14일·profile/photo 30일·live-check 2일로 조정합니다. PROCESSED queue 1일·DLQ 14일·collision/replay 30일·application 추가 유예 3일·checkpoint 2일·RETIRED projection 7일입니다. 보호된 active/pending/head 조건은 유지하고 운영 master 값은 자동 변경하지 않습니다.
- migration 235–237은 application의 orphan 제외 부분 UNIQUE, reasons+targets 실제 행 수로 제한한 projection 정리와 table-local vacuum 설정을 적용합니다. application 결과는 한 INSERT로 묶고 발행 fence/contract 검증은 한 pipeline으로 보냅니다. 새 fallback·재수집·dual writer·런타임 의존성은 없습니다.
- 상세 측정·미달 목표·운영 승인과 적용 결과는 [실행 기록](docs/current/plans/2026-09-29-youtube-storage-reduction.md#실행-기록--2026-09-29), 이행·복구는 [API runbook](docs/current/runbooks/hololive-api.md#youtube-관측-저장-구조-전환)에 기록합니다. 백업 삭제·물리 공간 회수는 전환과 분리하여 승인받습니다.

## v7.1.1 - 2026-10-01

- '전체보기' 접기를 글자 수(250자) 대신 기능과 표시 건수로 정합니다. 라이브·예정 방송·채널 일정·방송 이력·알람 목록·멤버 목록·캘린더 텍스트는 0건이면 안내문, 1건이면 펼치고 2건 이상이면 접습니다. 뉴스는 `TopItems`에 추가 요약 문단을 한 블록으로 더한 수로 판정하며, 프로필·전체 도움말 텍스트는 본문이 있으면 접습니다. `BOT_SEE_MORE_FOLD=false` 차단 스위치는 유지합니다.
- 방송 예정·시작·선행공개, 영상·쇼츠·커뮤니티, 축하·생일 방송·X 스페이스 알림에는 단일·묶음 모두 자동 패딩을 넣지 않습니다. 사용자 template에 직접 넣은 패딩과 저장된 예약 본문·pinned request는 그대로 보존합니다.
- 예약 뉴스·행사 요약이 공백으로 렌더되거나 formatter가 없으면 코드 대체 본문을 보내지 않고 실패로 집계합니다. 행사 요약은 빈 본문을 enqueue하거나 알림 완료로 표시하지 않고 다음 주기에 다시 시도합니다.
- 접기 함수를 `hololive-api/internal/templateview`로 옮기고 bot/LLM의 뉴스 표시 데이터 조립을 공통화했으며, 호출되지 않던 bot 주간·월간 행사 요약 formatter를 제거했습니다.
- 루트·API 산출물 버전은 `7.1.1`, alarm-worker는 `6.0.2`입니다. 이번 배포는 API와 alarm-worker를 교체하며 migration은 없습니다.

## v7.1.0 - 2026-10-01

- 관리자 발송 원장 목록에 `alarmType`·`errorCode` 정확 일치 필터를 추가합니다. `/api/holo/dispatch/failures`는 실패 보관·격리 항목의 전체 건수와 오류 코드·알림 유형·채널·채팅방별 상위 10개 분포를 단일 스냅샷으로 반환하며 오류 원문·본문은 공개하지 않습니다.
- 실패 분포의 건수를 숫자로 내림차순 정렬합니다. PostgreSQL 출력 alias가 문자열 건수 정렬을 선택하던 문제를 수정하고 네 분류의 10건·2건 순서를 실제 격리 DB에서 검증합니다.
- 루트와 API 산출물 버전은 `7.1.0`이며 이번 배포는 API만 교체합니다. worker·collector·PO와 migration은 변경하지 않습니다.

## v7.0.1 - 2026-09-29

- 이미지 빌드가 checkout의 umask에 따라 파일 모드가 달라지던 결함을 고칩니다. v7.0.0 배포에서 umask 077 checkout으로 빌드한 PO issuer 이미지의 `/app/po-sandbox` 파일이 600이라 uid 65532가 `worker.mjs`를 열지 못했고(EACCES) 중앙 쌍이 자동 rollback됐습니다. alarm-worker 이미지에도 600 파일이 들어갔습니다. PO issuer·collector·alarm-worker Dockerfile은 이미지에 실리는 Node 트리(`package.json`·`package-lock.json`·`src`)를 복사한 뒤 Node build stage에서 `chmod u=rwX,go=rX`로 파일 644·디렉터리 755로 맞춥니다. 기호 `COPY --chmod`는 BuildKit 전용이라 `CONTAINER_CLI=podman`(buildah)에서도 같은 결과가 나오도록 `RUN chmod`를 씁니다. 소유자와 umask 022 checkout의 산출물(모드·소유자·크기·내용)은 그대로입니다. `scripts/build/image-runtime-tree-permissions_test.sh`가 umask 077 checkout을 재현해 세 build stage를 빌드하고 runtime uid로 트리 전체를 읽는지 확인하며, pre-push 게이트는 이 Dockerfile들이 바뀔 때 이를 실행합니다(docker 필요, kapu 전용).
- 정기 보안 workflow의 NilAway가 v7.0.0 LIVE 구독 복구 테스트 stub의 `QueryRow`가 `nil` row를 돌려주는 것을 잠재 nil panic으로 거절하던 문제를 고칩니다. stub은 이제 `Scan`에서 같은 조회 실패를 돌려주는 row를 반환합니다. 운영 코드와 산출물은 바뀌지 않습니다.
- 정상 SIGTERM 정지가 exit 1로 끝나던 결함을 고칩니다. v7.0.0 cutover에서 hololive-api와 alarm-worker를 함께 멈추자 먼저 끝난 API의 HTTP/3 연결이 CONNECTION_CLOSE 없이 남았고, quic-go `Shutdown`은 GOAWAY 뒤 client가 연결을 닫기를 기다려 QUIC idle timeout(60s)이 종료 시한(10s)보다 길어 `HTTP/3 server shutdown failed: context deadline exceeded`로 실패했습니다. 발송 중인 작업은 없었습니다. `RuntimeHTTPServers`(alarm-worker·admin·llm·collector)는 H3 handler에서 실행 중인 요청을 세고, 종료 시한에 실행 중인 요청이 없으면 남은 idle 연결을 닫고 성공합니다. 시한에 요청이 실행 중이면 지금처럼 종료 실패입니다. 시한 전에는 client가 응답을 받고 연결을 닫을 때까지 그대로 기다립니다.
- 관리 알림 목록의 방 이름이 Kakao 방 이름 대신 방 ID로 보이던 결함을 고칩니다. Iris webhook은 방 제목을 싣지 않아 bot은 방 ID를 `RoomName`으로 넘기고, 재등록 upsert가 저장된 Kakao 방 이름과 `room_name_updated_at`을 방 ID로 덮어써 대표값이 됐습니다. 알림 저장은 방 ID와 같은 이름을 빈 이름으로 저장해 기존 Kakao 이름을 보존하고, 대표값 선택은 `room_name = room_id` 행을 뺍니다. 표시 순서는 관리자 이름 → Kakao 방 이름 → 방 ID 그대로입니다. 운영 DB의 기존 행은 바꾸지 않습니다.
- host-native collector cutover의 실패 복원이 한 번의 실패에 두 번 돌던 결함을 고칩니다. `set -E`로 ERR trap이 명령 치환에도 상속돼 `ready="$(collector_readiness_poll …)"` 같은 치환 안의 실패가 subshell과 부모에서 각각 `restore_native_after_failed_cutover`를 불렀습니다. 복원은 trap을 건 shell에서만 실행하고 subshell은 원래 상태로 끝납니다.
- `hololive_alarm_subscriber_db_fallback_total`의 `result`(`hit`·`miss`·`error`) series를 기동 때 0으로 만들어 첫 관측 전에도 보이게 합니다.
- `scripts/ci/python-runner.sh`를 iris-bridge 정본과 맞춥니다. 인자를 해석기 조회 전에 검사해 `--`만 주거나 `--print-interpreter` 뒤에 인자가 있거나 모드가 없으면 usage와 exit 2로 끝나고, `.python-version`은 symlink가 아닌 정규 파일에 정확히 한 줄(`3.14.7` + 개행)만 허용합니다.
- `scripts/ci/check-recurring-security-scan-contract.sh`의 production bake 검사가 target 하나만 attestation을 요청해도 통과하던 것을, PO issuer(rootfs tar로 export해 attestation을 실을 수 없음)를 뺀 모든 target이 최대 provenance와 SBOM을 요청하는지로 좁힙니다. Compose 5.5.1 `build --print`가 항상 내는 `No services to build` 경고(출력 전용 경로라 빌드한 이미지가 없음)만 거르고 나머지 stderr는 보입니다.
- 모든 Go 모듈의 `github.com/park285/iris-client-go/v3`를 v3.0.3(`d79fccefd`)으로 올립니다. webhook in-memory scheduler가 dispatcher 스케줄링 지연을 queue full 503으로 오거절하던 SDK 결함 수정이 들어옵니다. 운영 소비자의 durable admitter 경로는 영향이 없습니다. 보안 workflow의 sibling checkout도 같은 커밋을 가리킵니다.
- hololive-api가 종료 때 내부 HTTP/3 client를 닫습니다. alarm-worker·llm-scheduler·bot 내부 API·health 조회용 client는 그동안 닫는 곳이 없어(`internalhttp.CloseClient` 호출처 0곳) API가 끝나면 연결이 CONNECTION_CLOSE 없이 사라졌고, peer의 graceful shutdown은 그 연결을 QUIC idle timeout까지 기다렸습니다. bot·관리 plane의 Close가 infra를 닫기 전에 이 client를 닫습니다. plane Close는 모든 plane의 `Shutdown`(요청 drain)이 끝난 뒤 불리므로 진행 중인 요청을 끊지 않습니다. 같은 조건에서 worker 쪽 H3 정지는 종료 시한 10s를 기다리던 것이 곧바로 끝납니다.
- bot이 모르는 방 이름을 방 ID로 채우지 않습니다. Iris webhook에는 방 제목이 없어 ingress가 방 ID를 방 이름으로 넘겼고, 알림 등록과 major event·member news 구독 upsert가 저장된 이름을 방 ID로 덮어썼습니다. 이제 이름은 빈 값(모름)입니다. major event·member news 구독은 빈/공백 이름을 NULL로 저장하고 재구독에서 저장된 이름을 지우지 않습니다(그동안은 빈 문자열이 이름을 덮어썼습니다). 알림 저장은 방 ID와 같은 이름도 계속 빈 이름으로 받습니다. worker를 API보다 먼저 배포하면 옛 API가 방 ID를 보내기 때문입니다. 표시 순서(관리자 이름 → Kakao 이름 → 방 ID)는 그대로입니다.
- H3 정지 판정의 남은 틈을 코드 주석에 적습니다. 요청 수는 handler 진입부터 셉니다. quic-go v0.63 http3에는 stream을 받은 시점의 hook이 없어서, HEADERS가 덜 도착한 요청은 세지 않습니다. 이 틈은 종료 시한에 그런 stream만 남았을 때 실패 대신 성공을 돌려줄 수만 있습니다. 그 요청은 handler에 닿지 않았고 어느 판정에서든 연결 종료로 끊기므로 서버 쪽 부수효과는 없습니다.
- `RuntimeHTTPServers.Shutdown`이 Metrics·Pprof 서버를 H3와 동시에 같은 ctx로 멈춥니다. 그동안은 H3 정지가 끝난 뒤에 멈춰, H3가 idle 연결만 남아 종료 시한까지 기다렸다 성공하면 Metrics·Pprof는 이미 끝난 ctx를 받았습니다. 그 사이 열려 있던 metrics listener로 붙은 scrape 연결이 남아 있으면 `metrics server shutdown failed: context deadline exceeded`로 정상 SIGTERM이 exit 1이 될 수 있었습니다. 이제 세 listener가 정지 시작과 함께 닫힙니다.
- 루트·API 산출물 버전은 `7.0.1`, alarm-worker는 `6.0.1`입니다. collector·PO image도 `hololive/hololive-api/VERSION`(`7.0.1`)을 씁니다. migration과 공개 API 변경은 없습니다. 배포는 API·alarm-worker 동시 교체 뒤 collector·PO 네 쌍(중앙 c, AP b·a·d)을 같은 SHA의 7.0.1로 교체합니다.

## v7.0.0 - 2026-09-29

- 중복 Valkey 기능 두 가지를 한 패치에서 제거합니다(빅뱅 fadeout, `docs/current/plans/2026-09-28-valkey-dependency-reduction.md`). ① 멤버 뉴스 구독 방 미러(`membernews:rooms`·`membernews:room_names`): repository의 cache 의존·write-through·`WarmupCacheFromDB`·`Service.WarmupSubscriptionCache`를 지우고 `NewRepository(postgres)`로 줄였습니다. 기동 시 `ListSubscribedRooms`를 1회 조회해 실패하면 경고만 남깁니다. ② 멤버 hash(`hololive:members`): `cache.MemberCache`와 `InitializeMemberDatabase`·`GetAllMembers`·`GetMemberChannelIDWithOrg`·`GetMemberChannelIDs`, mock field, 기동 초기화, matcher의 동적 hash 후보 경로를 지웠습니다. `NewMatcher`·`ProvideMatcher`는 cache 인자를 받지 않습니다. hash field 형식 오류나 DEL/HSET 실패로 기동이 실패하던 경로도 함께 사라집니다.
- 공유 채널 대표 규칙(최소 영속 ID)을 `member.ChannelRepresentatives`로 승격했습니다. production 호출자가 없던 YouTube `apiservice`(채널 통계·`youtube:channel_stats:*` 캐시)와 `BuildYouTubeAPIStack`·`BuildYouTubeStack`, StreamHandler의 쓰이지 않는 YouTube·Valkey 필드는 삭제했습니다. 이 서비스 전용 `YOUTUBE_CACHE_SAVE_TIMEOUT_SECONDS`·`YOUTUBE_SCRAPER_PHASE_TIMEOUT_SECONDS`는 빈 값이어도 존재만으로 기동을 거절합니다(remove_after 2026-12-31).
- `hololive-api/scripts/bot.sh`에서 멤버 hash 대기·상태 출력과 `--no-ready-wait`, `CORE_MEMBER_HASH_SOFT_MIN_COUNT`, `CORE_MEMBER_HASH_SOFT_TIMEOUT_SECONDS`를 지웠습니다. 제거한 옵션은 알 수 없는 인자로 거절합니다. 운영에 남은 폐기 key는 자동 만료되지 않으며 회수는 별도 승인 작업입니다.
- Valkey 책임을 추가로 줄입니다(2차). **정확성·보안**: alarm-worker checker가 TTL 없는 LIVE 구독 set이 evict되면 그 채널 알림을 조용히 건너뛰던 결함을 고쳐, empty marker가 없으면 한 번의 batch DB 조회로 구독 방을 확정합니다(set은 다시 채우지 않아 구독 해지 경합 없음). 비밀번호 reset 뒤 기존 세션이 인덱스 eviction·폐기 실패·동시 Refresh로 살아남던 결함은 `auth_users.session_generation`(migration 231)으로 막습니다. reset이 같은 문장에서 세대를 올리고 Me·Refresh가 PG 세대와 비교하며, `auth:user_sessions` 인덱스는 삭제했습니다.
- 관리자 지정 방 이름은 Valkey에만 있어 rebuild·재등록 때 사라졌습니다. 이제 `alarm_room_display_names`(migration 232)에 저장하고 Kakao 방 이름보다 우선합니다. 공백 이름은 지정 해제입니다. Kakao 대표 이름은 upsert로 실제 바뀐 시각(`alarms.room_name_updated_at`) 기준으로 고릅니다. 관리 목록·통계는 PG에서 (방, 채널) 단위로 만들고, `alarm:{room}`·`alarm:registry`·`alarm:room_names`·`alarm:user_names`·`alarm:channel_registry:version`과 사용자 이름 API(`POST /api/holo/names/user`, `PUT /internal/alarm/user-name`)를 삭제했습니다. Iris Console의 해당 호출은 별도 저장소 변경이 필요합니다.
- 방 이름 설정은 `room_id`·이름의 앞뒤 공백을 제거하고, 공백뿐인 `room_id`, 100자를 넘는 `room_id`, 255자를 넘는 이름을 PG 저장 전에 400으로 거절합니다(worker `invalid_request_body`, 관리자 API `invalid request body`). 그동안은 PG `VARCHAR` 폭 초과 오류가 500 `set_room_name_failed`로 드러났습니다.
- writer 없이 reader만 남아 있던 `alarm:next_stream:*`를 읽는 경로를 모두 지웠습니다: worker `GET /internal/alarm/next-stream/:id`와 `get_next_stream_info_failed`, 알람 목록의 batch HGETALL, `domain.NextStreamInfo`, 다음 방송 표시 view와 이 표시만 쓰던 `timefmt/relative_*` 필수 key. migration 233이 알람 추가·목록 표준 본문의 다음 방송 분기를 지우며, 출력은 이전과 같습니다(`NextStream`은 한 번도 채워진 적이 없음). 표준과 다른 본문이 `NextStream`을 참조하면 233은 아무것도 바꾸지 않고 거절합니다.
- ACL Valkey mirror(`acl:*`)와 `config:update` Pub/Sub을 삭제했습니다. ACL 변경은 같은 hololive-api 프로세스에서 bot plane이 직렬화된 PG reload로 즉시 따라가며, reload에 실패하면 관리자 요청이 500 `acl_bot_resync_failed`로 실패를 드러내고 같은 요청 재시도로 수렴합니다. 알림 사전 시간(`alarm_advance_minutes`)은 settings.json 1회 기록과 worker HTTP 적용 1회만 하며, 적용 실패는 `alarm_applied=false`로 응답합니다.
- member cache의 Valkey L2(`member-cache:v2:data:*`)를 삭제하고 in-process snapshot index만 씁니다. epoch는 값이 달라지면(작아져도) snapshot을 버리는 변경 신호가 되어, Valkey 재시작 뒤 장수 프로세스가 재기동 전까지 PG 직접 조회에 머물던 문제가 사라집니다.
- 효과 없는 캐시와 죽은 코드를 지웠습니다: Holodex `search_channels:*`·`channels_live_status_*`·`hololive_channels`, YouTube producer state store(production 미연결), `StreamCache`·`MGet`·`MSet`·`SetNXMulti`·`CompareAndExpire`, locker `ClaimRoom`, 쓰이지 않는 dedup claim 함수와 `AlarmDispatchState`. 뉴스·주요 이벤트 실행 잠금이 Valkey 오류 시 실행하지 않는(fail-closed) 현재 동작에 맞게 기존 감사·계획 문서를 정정했습니다. 운영에 남는 폐기 key와 회수 순서는 계획 문서에 적었고 회수는 별도 승인 작업입니다.
- CI 게이트를 슬림화합니다. 검사기 자기 테스트·fixture와 문서 토큰·삭제 코드 재도입 grep 가드를 지우고, 구조 예산은 golangci funlen/revive와 메타 저장소 검사로 옮겼습니다. 추적하던 `artifacts/architecture/go-workspace-import-graph.txt`와 그 생성 script, 소비자가 없는 `docs/architecture/release-governance-assets.txt`도 삭제했습니다. 최종 이미지 스캔 억제 금지 계약은 가짜 trivy로 실제 스캐너가 넘기는 인자를 확인하는 동작 검사로 바꿨습니다.
- 공개 사용자 이름 API(`POST /api/holo/names/user`)와 worker 내부 API(`PUT /internal/alarm/user-name`, `GET /internal/alarm/next-stream/:id`)를 삭제하고 migration 231~233을 더하므로 루트·API 산출물 버전은 `7.0.0`, alarm-worker는 `6.0.0`입니다. collector·PO image도 `hololive/hololive-api/VERSION`을 씁니다. 배포는 migration 231~233 적용 뒤 API·alarm-worker 동시 교체(계획 문서 C0~C5), 그 뒤 collector·PO 네 쌍(중앙 c, AP b·a·d)을 같은 SHA의 7.0.0으로 교체(C5b)합니다. Iris Console의 사용자 이름 호출 제거는 Hololive v7.0.0 수용(C5) 뒤에 배포하며(이전 Hololive는 빈 방 이름 해제를 400으로 거절), 방 이름 변경 동결은 Console 배포까지 유지합니다. 새 retry·fallback은 추가하지 않습니다. LIVE 구독 set 미스 때의 조회는 PG 정본 read-through이고, 대표 멤버 이름이 빌 때의 채널 제목 표시는 기존 동작입니다.

## v6.0.2 - 2026-09-28

- `po-broker` HTTP `IdleTimeout`을 2초에서 30초로 늘립니다. helper Agent는 유휴 socket을 계속 약 1초에 닫습니다. v6.0.1에서는 Node event loop가 1초 넘게 멈추면 서버가 이미 닫은 유휴 socket을 helper가 재사용해 EPIPE가 `broker_unavailable`로 끝나고 발급 세션을 버렸습니다(v6.0.0에는 없던 실패). 이제 수 초의 stall에도 client가 먼저 닫습니다. 비용은 유휴 연결 최대 1개이고, 퇴역 때의 `server.Close`는 유휴 연결도 즉시 닫습니다. 이전 2초를 넘긴 유휴 연결을 재사용하는 회귀 테스트를 추가했습니다.
- `po-broker --healthcheck`(Docker HEALTHCHECK)가 keep-alive를 끄지 않습니다. 이전에는 `Connection: close` 때문에 서버가 응답 직후 먼저 닫았습니다. 이제 client가 body를 읽고 먼저 닫으며 서버는 EOF를 본 뒤 닫습니다(strace로 순서 확인).
- `po-broker`는 기동 때 SIGINT를 상속 무시(SIG_IGN)했으면 SIGINT를 감시하지 않습니다(Go runtime은 상속 SIG_IGN을 SIGINT·SIGHUP에만 존중하므로 SIGTERM은 항상 감시·종료합니다). signal을 처리할 때는 SIGTERM·SIGINT를 모두 기본 처리로 되돌린 뒤 다시 보냅니다. 비대화형 shell의 background 실행처럼 SIGINT를 무시한 채 시작하면, v6.0.1은 SIGINT에 거짓 `reason=signal` 줄을 남긴 채 살아 있고 이후 SIGTERM을 삼켜 SIGKILL(137)로만 끝났습니다. 이제 SIGINT는 계속 무시되고, SIGTERM은 원인 줄 하나를 남기고 143으로 종료합니다. systemd·Docker 기동은 signal을 기본 처리로 두므로 운영 동작은 같습니다.
- native 실패 복원(`restore_native_after_failed_cutover`)이 `if ! ( set -e; … )` 안에서 set -e가 무시되어, 복원 단계가 실패해도 계속 진행하고 `could not be restored` 경고를 내지 않던 기존 결함을 고칩니다. 이제 첫 실패에서 멈추고 경고를 남기며, 종료 상태는 그대로 원래 cutover 실패 상태입니다. 배포 테스트는 cutover 최상위 정지 단계, 실패 복원 경로, 수동 rollback이 원격에 보내는 payload 각각에서 PO socket을 collector `disable --now`보다 먼저 멈추는지 가짜 systemctl로 검사합니다. 복원 단계 실패도 검사하며, 호출부를 바꾸는 변이는 모두 이 검사에서 실패합니다.
- 문서를 바로잡습니다. `worker_failed`는 요청이 직렬 슬롯을 쥔 동안의 종료이고, `worker_exited`는 직렬 슬롯이 비어 있을 때의 종료입니다. v6.0.1의 "worker IO 중 종료"보다 범위가 넓습니다. 종료 줄은 SIGTERM·SIGINT, 퇴역, listener·기동 실패에만 남고 SIGHUP·SIGQUIT·panic·SIGKILL에는 남지 않습니다. 커널 경쟁의 남은 창도 적었습니다. worker 사망 경로는 µs, `session_closed`·`lease_expired`·`worker_timeout`은 수 ms reap 간격이고, bootstrap 실패 뒤 `retireOwned` DELETE가 반복되는 대표 트리거입니다. native 산출물 version은 `HOLO_BOT_VERSION=<ver>`로 넘겨야 합니다(없으면 short SHA). API image를 재빌드하지 않아도 이후 중앙 전체 `compose up`은 API `APP_VERSION`을 repo 릴리스로 바꾸고, image label은 이전 build로 남습니다. v6.0.1의 "collector 기동도 `enable` 뒤 `restart` 한 번으로 줄였습니다"는 사실과 다릅니다. 이전 `enable --now`도 멈춘 collector를 한 번 기동했으므로, 이 변경은 기동을 `enable` + `restart`로 명시한 것일 뿐 횟수는 같습니다.
- PO를 먼저 멈춘 cutover 창에서 이전 collector의 mint·반납 실패는 helper `/health`의 `proof.last_error`에만 남고 로그 줄을 만들지 않습니다. 그래서 cutover journal 오류 검사가 거짓 실패하지 않으며, 검사 범위는 바꾸지 않았습니다.
- collector·PO 산출물 버전은 `6.0.2`입니다(루트 `VERSION`, `hololive/hololive-api/VERSION`). API는 코드 변경이 없어 재배포하지 않고, alarm-worker는 `5.0.0`을 유지합니다. retry·fallback은 추가하지 않았고 DB migration·운영 설정·공개 API는 바꾸지 않습니다.

## v6.0.1 - 2026-09-28

- `po-broker`는 종료할 때 stderr에 `po-broker exit reason=<reason> generation=<uuid>` 한 줄만 남깁니다. `reason`은 실제 퇴역 호출 지점에서 정한 고정 어휘입니다. `session_closed`(DELETE /v1/session), `lease_expired`(lease 타이머·요청 시점 만료), `worker_failed`(요청의 worker IO 중 종료 포함), `worker_timeout`, `request_aborted`(worker IO 중 요청 연결 끊김), `response_failed`, `worker_exited`(worker IO 요청이 없는 동안 기동을 마친 worker의 자체 종료), `startup_failed`, `listener_failed`, `signal`(퇴역 전 SIGTERM·SIGINT)이 있습니다. 여러 경로가 동시에 퇴역을 요청해도 처음 표시된 원인만 남고, 퇴역 중 받은 signal은 먼저 표시된 원인을 남깁니다. token·payload·요청 본문·worker 출력은 기록하지 않습니다. exit code는 그대로이고, signal 종료는 원인을 기록한 뒤 같은 signal을 기본 처리로 다시 보내 종료 상태(예: 143)를 유지합니다. 그동안 로그 없이 exit 0으로 끝나던 issuer 재시작도 이제 원인별로 구분됩니다.
- AppArmor af_unix 미디에이션 경쟁(upstream `b1aea2c19607`, 현재 Ubuntu linux-oracle 빌드 미포함)으로 생긴 커널 Oops(`unix_fs_perm+0xd0`)를 완화합니다. broker는 응답마다 keep-alive를 끄고 연결을 닫았고, 이 close가 youtubejs helper의 새 연결 첫 read와 겹치는 창에서 Oops가 났습니다. 이제 broker HTTP keep-alive를 켜서 정상 응답 뒤에는 서버가 연결을 닫지 않습니다. helper의 `ProofBrokerClient` Agent는 `timeout: 1000`으로 유휴 socket을 broker `IdleTimeout`(2초)보다 먼저 닫습니다. 그래서 유휴 연결은 client가 끝내고, 서버가 닫는 중인 socket에 요청을 보내는 재사용 경쟁도 생기지 않습니다. 퇴역 시에는 `server.Close`가 유휴·진행 중 연결을 즉시 닫으므로 프로세스도 바로 종료합니다. generation·lease·operationLimit·header/body 한도 검사는 요청마다 그대로 적용합니다. retry·fallback은 추가하지 않았습니다. 근본 수정은 커널 교체이며, 퇴역·오류 응답 직후의 새 연결 첫 read 창은 남아 있습니다.
- native AP(`collector-a`·`collector-d`) cutover·실패 복원·수동 rollback은 PO socket·service를 collector보다 먼저 멈춥니다. collector의 종료 generation 반납이 살아 있는 broker를 퇴역시켜 `Restart=always`가 곧 멈출 issuer를 다시 띄우던 재시작을 없앴고, collector 기동도 `enable` 뒤 `restart` 한 번으로 줄였습니다. 운영 runbook은 issuer의 Docker `RestartCount`·systemd `NRestarts`를 generation 교체 횟수로 정의하고 종료 코드·OOM·health·generation 일치로 합격을 판정합니다. Compose issuer-first cutover의 1회 issuer 재시작은 예상 동작으로 문서화했습니다.
- collector·PO 산출물 버전은 `6.0.1`입니다. collector·PO image는 `hololive/hololive-api/VERSION`을 씁니다. 같은 파일을 쓰는 API는 코드 변경이 없어 재배포하지 않고 `6.0.0` 산출물을 유지하며, alarm-worker도 `5.0.0`을 유지합니다. DB migration·운영 설정·공개 API는 바꾸지 않습니다.

## v6.0.0 - 2026-09-28

- `iris-client-go/v3 v3.0.2`으로 API·alarm-worker·shared·collector·DB 테스트 모듈을 함께 이관합니다. 웹훅 본문·방은 `Message.Msg`·`Message.Room`에서 읽고, 서명된 요청은 body `messageId`를 헤더와 일치시킵니다. API·alarm-worker 산출물 버전은 각각 `6.0.0`·`5.0.0`입니다.
- migration 228은 검토한 격리 send unit의 `closed_without_replay` 감사 영수증을 추가합니다. 정확한 전체 대상 ID·revision·상태 메타데이터와 원본 전체 SHA-256 digest를 저장하고, payload·본문·오류 원문과 원본 delivery 상태는 복사하거나 바꾸지 않습니다. maintenance 작업은 직렬화 트랜잭션에서 잠금·digest를 재검사하고 send unit당 한 건만 기록하며, 결과 불명은 receipt 조회로 판정합니다. 정상 retention은 그대로 적용됩니다.
- migration 229는 227 적용 기록과 현재 singleton의 완료·버전·cursor를 잠금 아래 다시 확인한 뒤 일회성 `youtube_notification_delivery_ledger_state`를 제거하고 229 적용 기록을 같은 transaction에 씁니다. 빈 DB는 원본 outbox·delivery·logical ledger가 모두 비어 있을 때만 허용하고, 기록 없는 table 부재는 거절합니다. 적용 전 운영 singleton을 복구용으로 보존하며 logical delivery ledger는 유지합니다.
- migration 230은 현재·보존·지원 rollback API 이미지가 terminal payload를 직접 비우는 writer를 포함함을 확인한 뒤, `bot_webhook_inbox`의 구 호환 scrub trigger와 함수만 원자적으로 제거합니다. 적용 전에 검증된 terminal payload CHECK와 정확한 trigger/function catalog 형태를 검사하고, CHECK는 남깁니다. 구 status-only writer는 CHECK에 거절되며 현행 complete·abandon·release·reclaim은 terminal scrub과 retry payload 보존을 계속 담당합니다. 의존성 오류는 전체 DROP을 롤백합니다. 수동 DDL 재설치는 230 적용 ledger와 어긋나므로 rollback이 아닙니다.
- 사용하지 않는 alarm-worker profile의 `notification_delivery.lock_timeout_ms`를 exact-key 설정과 fixture에서 제거합니다. 운영 profile에서도 같은 rollout 전에 키를 지워야 새 decoder가 기동하며, 실제 delivery lease·retry 동작은 기존 정본을 따릅니다.

- native PO 배포 fragment가 앞서 적재한 helper의 service/socket/unit 입력을 명시적으로 요구합니다. helper 없이 실행하면 payload·호스트 변경 전에 실패하며 값의 단일 소유자는 그대로입니다. readiness 관측 loop는 미사용 변수를 `_`로 명시하고 기존 30회·2초 상한을 유지합니다. meta ShellCheck 경고를 억제하지 않고 해소했으며 application binary·운영 설정·프로토콜은 바꾸지 않습니다.

## v5.0.1 - 2026-09-28

- DB pool이 `POSTGRES_SSLROOTCERT`를 pgxdb `Config.SSLRootCert`로 직접 넘기는 v5.0.0 배포 뒤의 lockstep 릴리스입니다. API·alarm-worker·shared·collector·DB 테스트 모듈과 정기 보안 검사의 sibling checkout을 `shared-go/v2 v2.8.0`(`36d47654f2c0`)과 `iris-client-go/v2 v2.8.0`(`b23933c3179c`)으로 함께 고정합니다. shared-go v2.8.0이 지운 pgxdb env 폴백과 호환 API는 v5.0.0에서 이미 대체 경로로 옮겨 이 고정에 필요한 코드 변경은 없고, 다른 의존성 버전도 바꾸지 않습니다.
- 운영 산출물은 API·collector `5.0.1`, alarm worker `4.0.1`로 식별합니다. DB migration·런타임 설정·공개 API는 변경하지 않습니다.
- T11의 Community/Shorts `post_id` 단일화를 마무리합니다. claim과 telemetry가 같은 canonical logical ID 검증을 사용하고, 누락·파싱 실패·불일치를 `content_id`·payload 리소스 ID로 대체하지 않습니다. 잘못된 claim ID는 오류로, 로그 ID는 원문을 포함하지 않는 `invalid:<kind>:<reason>`으로 드러냅니다. telemetry batch의 빈 post ID는 INSERT 전에 전체 batch를 거절하며, NEW_VIDEO·LIVE·MILESTONE의 content ID 계약은 유지합니다. 운영 canonical ID 존재 확인과 실제 PostgreSQL의 수정 전 실패·수정 후 거절 회귀, canonical consumer smoke를 거쳤습니다. 새 retry·fallback은 추가하지 않습니다.
- telemetry의 빈 `delivery_path`도 enrichment·INSERT 전에 전체 batch를 거절합니다. 빈 값을 `youtube_outbox_dispatcher`로 보충하던 경로를 없애고, 전이 기록자는 정본 상수를 직접 쓰며 방출 로그는 저장된 경로를 trim할 뿐 대체하지 않습니다.

## v5.0.0 - 2026-09-28

- API·alarm-worker·shared·collector·DB 테스트 모듈의 `iris-client-go/v2`를 게시된 `v2.7.0`으로 고정합니다. 정본 Karing wire 이름과 결과 불명 오류 보존, 서버 퇴역 사전 이관 계약을 같은 SDK 버전으로 검증합니다.
- `shared-go/v2`를 검증된 `v2.7.2`로 고정해 inbox prune의 호출당 삭제 상한을 보장합니다. 이 패치에는 S19 공개 API·TLS env 폴백 제거가 없으며, 소비자 TLS 명시 배포 뒤의 lockstep 릴리스와 구분합니다.
- local CI의 standalone `staticcheck`를 `scripts/ci/staticcheck-facts/build.sh` 고정 profile로 바꿉니다. 원인: `staticcheck 2026.2.1`(`honnef.co/go/tools v0.8.1`)이 쓰는 x/tools `v0.44.1-0.20260420230617-19499e7caabc`의 objectpath는 source에서 선언 순서대로 보이는 generic method를 export data에서는 일반 method 뒤로 읽어, `iris-client-go/v2` `APIClient`의 `SendKaringHololive` 폐기 fact를 `SendReaction`에 붙이는 거짓 SA1019를 냈습니다(cold cache에서도 재현). profile은 두 module의 sumdb hash와 zip SHA-256을 검증한 archive에서 x/tools revision을 바꾸지 않고 method 순서 정규화 패치만 적용해 go1.27.1로 빌드하고, 정상 호출은 경고 없이·실제 폐기 호출은 SA1019 1건으로 끝나는 source/export 경계 CLI fixture를 통과한 binary만 cache합니다. 경고 억제나 app·SDK 우회는 없습니다. 제거 조건: upstream x/tools objectpath 수정이 staticcheck release에 반영되고 같은 fixture가 무패치 binary로 통과하면 profile을 지우고 pinned install로 돌아갑니다.
- 알림 metric 수집기를 `sync.OnceValue`가 완성된 묶음으로 반환하게 합니다. 등록 중 panic 후 복구되더라도 다음 호출이 미초기화 전역을 읽지 않고 원래 등록 오류를 유지합니다. 지표 이름·label·등록 시점은 그대로입니다.
- 최종 알림 payload 회귀 fixture도 실제 claim의 저장된 send-unit ID와 기동 시 message_strings 적재·검증을 사용합니다. 전체 문구를 고정하던 알람 목록 테스트는 제거하고 접힘 경계·본문 보존 검증은 유지합니다.
- PostgreSQL 18 전용 DB fixture는 `DROP DATABASE ... WITH (FORCE)` 오류를 그대로 반환합니다. 최초 오류를 버리고 만료된 context로 재시도하던 PG<13용 terminate/plain-drop 경로를 제거했으며, 30초 정리 기한과 병렬도는 유지합니다. integration 전용 6개 패키지에서 실제 생성·삭제를 검증했습니다.
- PO·W4 통합 뒤 Compose 첫 배포 실패가 중지된 candidate image·issuer receipt를 남겨 다음 전환을 막던 상태를 고칩니다. 소유 container·tag·active receipt만 제거하고 source snapshot을 복원하며 volume·복구 archive는 보존합니다. collector 기준점 없는 기존 issuer는 전환 전에 거절하고, rollback 기준점 조회도 source 교체 전에 끝냅니다. 실제 Docker 정지·정리·source 복원 smoke와 실패-before/passing-after 상태 회귀로 검증했습니다.
- 중앙 collector·issuer paired cutover는 Compose의 collector·migrator DB 경로가 같음을 확인하고 migrator의 접속·TLS·network로 migration 222의 적용 checksum을 읽습니다. 외부 DB override를 로컬 socket ledger로 대신 검증하지 않습니다. 실제 두 PostgreSQL 인스턴스에서 대상 DB만 정상인 성공 경계와 checksum·경로 불일치 거절을 확인했습니다. 조회용 container·volume은 검증 후 제거하며 서비스와 DB는 변경하지 않습니다. 실행 계획은 완료된 최초 PO 계획 대신 현재 승인된 활성 `PO_PLAN_ID`를 필수로 받아 같은 strict gate를 통과시킵니다.
- native 완료 검사 fixture는 issuer 도입 전 rollback 상태를 명시하고 예상 issuer가 빠진 release를 거절합니다. polling 횟수·운영 지연은 유지하고 fixture의 대기만 기록하여 실패 사례가 실제 시간 경과에 의존하지 않게 합니다. 소스 문자열·로그 문구 고정 검사는 상태·결과 검증으로 대체합니다.
- retention 관측 timeout 회귀는 관측이 폐기할 연결과 삭제가 사용할 연결을 미리 엽니다. 새 연결 준비 지연을 250ms SQL 예산에 섞지 않으며 실제 timeout·SQL 오류 뒤 삭제의 독립 context와 삭제 횟수 검증은 유지합니다. production의 timeout·pool 설정은 바꾸지 않습니다.
- alarm-worker 알림은 Karing template을 보내지 않습니다(`DEC-20260926-hololive-karing-egress-disposition`, `DEC-20260904-hololive-karing-regular-chat-egress` 대체, PLN-20260926-stack-audit-refactoring T19). Karing 선택 분기·chunk planner·Karing sender·`karing` message_strings 요구·`karing.kakaolink` 계약 문서와 CI의 Karing 필수 문자열 검사를 삭제하고, CI는 alarm-worker에 Karing SDK 호출이 다시 들어오면 실패합니다. Markdown handoff 분류는 `ErrReplyHandoffOutcomeUnknown`·`ErrReplyHandoffFailed`로 이름을 바꿨습니다. 방송 알림 그룹의 ambiguous 발송 실패는 방 기준 quarantine 대신 다른 source와 같이 저장된 send-unit ID로 재시도합니다.
- 계약 없는 원천·표시 폴백을 오류 반환 단일 경로로 바꿉니다(`DEC-20260926-hololive-source-fallbacks-retirement`, T19). Holodex `GetChannel`의 scraper 부분 Channel, `GetChannels`의 개별 조회 보충, `GetChannelSchedule`의 YouTube·공식 일정 보충과 5분 재캐시, `GetRecentVideos`의 RSS 폴백·빈 성공·RSS backoff, matcher의 Holodex 보강과 채널명 폴백 체인, major event 요약 실패의 이벤트 목록 대체, `/api/holo` rate limit fail-open을 삭제했습니다. `domain.MemberDataProvider`는 오류를 돌려주는 `LoadAllMembers` 하나로 바꿨습니다. 알림 멤버 표시명 폴백만 예외 계약(trigger·한도·telemetry·owner·제거 조건)으로 남고 `hololive_alarm_member_name_fallback_channels`·`hololive_alarm_member_name_caller_fallback_total`로 관측합니다. `YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS`와 channel schedule 보충 캐시 TTL이던 `OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS`는 빈 값이어도 존재 기준 퇴역 가드가 hololive-api·alarm-worker 기동을 거절하고(remove_after 2026-12-31), `hololive_admin_rate_limit_fail_open_total`은 `hololive_admin_rate_limit_check_failures_total`로 바뀝니다. major event 요약이 실패한 주·월의 알림은 자동으로 다시 보내지 않으므로 `/internal/trigger/majorevent-*` 수동 trigger로 복구합니다.
- 감사 계약 없는 fallback을 제거합니다(PLN-20260926-stack-audit-refactoring T09). live-status YouTube scraper 2차 경로와 `HOLODEX_LIVE_STATUS_FALLBACK_*` 설정을 삭제하고, 남은 키는 존재 기준 퇴역 가드가 기동을 거절합니다(`DEC-20260926-hololive-live-status-scraper-fallback-removal`).
- LLM 월 토큰 상한과 Valkey 월 카운터를 삭제하고 `hololive_llm_cost_tokens_total`을 항상 기록합니다. `LLM_MONTHLY_TOKEN_CEILING`은 퇴역 가드가 거절합니다(`DEC-20260926-hololive-llm-token-ceiling-retirement`).
- message_strings를 bot·llm plane과 alarm-worker 기동 때 한 번 적재해 필수 key를 검증하고, 실패하면 기동에 실패합니다. 코드 대체 문구·lazy 재적재·`FallbackSentinel`을 제거하고 시드 SQL 기반 format 인자 수 테스트를 둡니다. 템플릿 렌더 실패 응답은 DB의 `command_processing_failed` 문구이며, 예약 알림 렌더 실패는 발송하지 않고 오류로 남깁니다(`DEC-20260926-hololive-message-strings-startup-validation`).
- `org=all` 부분 결과는 `PartialStreamsError`로 알리고 캐시하지 않으며, 공식 일정 fallback의 빈 결과를 빈 성공으로 캐시하지 않습니다. 이에 따라 Stream HTTP API `/api/holo/streams/{live,upcoming}?org=all`은 일부 org만 실패해도 부분 목록 대신 500을 응답합니다(이전: 부분 목록 200). channel schedule 보충 경로는 T19에서 provider의 5분 재캐시와 함께 삭제했습니다(아래 원천 폴백 항목).
- 숫자·bool·기간 env의 잘못된 값을 경고 뒤 기본값으로 바꾸지 않고 기동 실패로 드러냅니다(PLN-20260926-stack-audit-refactoring T10, 감사 B4). hololive-api·alarm-worker·youtube-collector 설정 로더와 alarm-worker의 `CELEBRATION_RUNNER_ENABLED`·`BIRTHDAY_STREAM_RUNNER_ENABLED`가 shared-go 엄격 파서(`IntE`·`Int64E`·`BoolE`·`FloatE`)를 쓰고, 이관한 초·밀리초 단위 기간 키(`*_SECONDS`·`*_MS`)가 `time.Duration` 범위를 넘으면 거절합니다. 한 로더 구획(bot·admin 공통 구획, llm plane, collector 공통 구획 등) 안의 잘못된 키는 한 번의 실패에 함께 보고되지만, 구획 사이에서는 처음 실패한 구획에서 멈춥니다. 여러 구획이 잘못됐으면 앞 구획을 고친 뒤 다시 기동해야 다음 구획의 오류가 보입니다. 값이 없거나 비어 있으면 지금처럼 기본값입니다.
- env 이름 alias 체인을 지웁니다. `HOLODEX_API_KEY`와 `SERVICES_LLM_SCHEDULER_HEALTH_URL` 하나씩만 읽고, 퇴역한 `HOLODEX_API_KEY_1`·`SERVICES_LLM_SERVER_HEALTH_URL`은 빈 값이어도 존재만으로 기동을 거절합니다(`DEC-20260926-hololive-legacy-env-config-retirement`). prod compose는 `HOLODEX_API_KEY_1`을 더 주입하지 않고, host-native collector env 생성기는 소비자가 없는 `HOLOLIVE_H3_SERVER_NAME`을 쓰지 않습니다. 배포 전에 모든 hololive env에서 두 퇴역 키를 지워야 합니다.
- dedup 선점·ACL 캐시 동기화·member news 요약·내부 H3 client·DB 역할 비밀번호의 fallback을 오류 반환 또는 기동 실패로 바꿉니다. 활성 member news LLM client 초기화 실패, 설정한 X allowlist 로드 실패, 내부 H3 전용 env 누락, 역할 비밀번호 누락은 모두 실패로 드러납니다. 내부 JSON client(`internalhttp.NewJSONClient`)는 H3 구성 실패 때 경고 뒤 TCP client로 내려가지 않고 오류를 돌려주며, 설정된 LLM scheduler URL이나 bot 내부 URL로 client를 만들지 못하면 bot·admin plane 기동이 실패합니다.
- 퇴역 가드와 드레인 종단에 제거 조건을 둡니다(PLN-20260926-stack-audit-refactoring T17). `MEMBER_NEWS_CLIPROXY_MODEL`·`DB_SSLMODE`·`DB_QUERY_EXEC_MODE`·`OTEL_ENVIRONMENT`는 빈 값이어도 존재만으로 hololive-api·alarm-worker·youtube-collector 기동을 거절하고(이전: 비어 있지 않을 때만), `HOLOLIVE_X_SPACES_LOGIN_ENABLED`는 compose env 파일뿐 아니라 호출 프로세스 env에 있어도 `compose.sh`가 멈춥니다. 퇴역 가드마다 도입 리비전·제거 조건·`remove_after` 재검토 기한을 적었고, 표준 `OTEL_EXPORTER_OTLP_ENDPOINT`·`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 거부와 재도입 방지 CI 게이트는 제거 조건 없는 영구 계약으로 이름과 문서를 고쳤습니다.
- 중앙 배포(`build-all.sh`·`compose.sh`·`compose-redeploy-service.sh`)는 퇴역 runtime 컨테이너 12종을 stop/rm 하거나 부재를 단언하지 않습니다. T18(2026-09-26)에서 중앙·AP에 퇴역 runtime 컨테이너·이미지가 0개였고 그 상태로 release가 배포돼 `removed-runtimes.sh`와 호출·테스트·sync 목록을 지웠습니다. 재도입은 `test-three-runtime-topology.sh` 정적 gate가 막고, compose up 순서 검사는 `test-compose-up-flow.sh`로 옮겼습니다(`DEC-20260926-hololive-retired-rollback-tooling`, T17·T19).
- alarm dispatch claim이 send unit 없는 migration 141 이전 delivery를 읽던 `legacy_head`를 지우고, Text 경로는 저장된 send-unit `client_request_id`가 없으면 파생 ID로 보내지 않고 발송 전 실패로 드러냅니다. 관리 재처리는 send unit 없는 행을 `group_identity_mismatch`로 막고, migration 224의 `alarm_dispatch_deliveries_active_send_unit_check`가 send unit 없는 delivery의 pending·retry·leased·sending 기록을 거절합니다(수동 requeue 스크립트 포함). 224의 VALIDATE는 활성 NULL 행이 있으면 실패합니다. Twitch/Chzzk 단독 보관 envelope는 error 로그를 남기고 DLQ로 가며 렌더러의 같은 분기는 지웠습니다.
- migration runner는 manifest 밖 `schema_migrations` 행을 182 적용 여부와 관계없이 즉시 거부합니다. epoch-1 ledger 잔재와 checkpoint baseline 수용 창은 닫혔습니다.
- migration 222가 177/189의 `legacy_collector` 진단 backfill trigger와 함수를 지우고, collector release는 복원 단계 없이 `last_failure_*`를 건드리지 않습니다. 222는 새 collector보다 먼저 중앙 `db-migrate`로 적용해야 합니다. migration 223은 `bot_webhook_inbox` 호환 scrub trigger가 payload를 비우지 않는 writer를 만나면 PostgreSQL WARNING을 남기게 합니다. 222·223과 위의 224는 운영 적용된 live-evidence 218~220과 부재 증거 보존 221 다음 번호입니다.
- 구형 코드와 끝난 전환 경로를 지웁니다(PLN-20260926-stack-audit-refactoring T11). celebration은 전환일 legacy identity 읽기를 지우고 `MemberID`만으로 식별하며, `MemberID`가 없는 celebration 발행은 canonical 검증에서 거절됩니다(dispatch group·identity의 ChannelID 대체 삭제). alarm dispatch payload의 `notification.alarm_type` 누락은 Live로 채우지 않고 decode 오류이고, NEW_SHORT raw content_id 재사용과 tracking raw 후보 조회, member point 캐시의 채널·이름 식별 폴백도 지웠습니다(T18 잔존 0건).
- YouTube scraper에서 production 호출자가 없던 browser snapshot fetch·진단, proxy 연속 실패 시 direct 자동 전환 정책, `SCRAPER_FETCHER_ENGINE` 선택, producer active-active 설정 잔재를 지웁니다. `hololive_youtube_scraper_fetch_fallback_total` metric은 생산자가 없어 삭제했고 fetch metric의 `engine` 라벨은 `nethttp`로 유지합니다. `SCRAPER_FETCHER_ENGINE`·`SCRAPER_BROWSER_DIAGNOSTIC_*`·`YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT`·`IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS`는 빈 값이어도 존재만으로 hololive-api·alarm-worker 기동을 거절합니다(remove_after 2026-12-31). production은 `IRIS_BASE_URL_FILE` stat 검사를 항상 하고 live-compat overlay는 우회 플래그를 더 주입하지 않습니다. SOCKS5 proxy와 관리 설정 `scraperProxyEnabled`는 iris-console 소비자와 함께 T19에서 다룹니다.
- 관리 화면이 저장한 `settings.json`이 targetMinutes 없는 구형 형식이거나 읽을 수 없으면 alarm-worker가 설정값으로 대신하지 않고 기동에 실패합니다(파일이 없으면 설정값 사용). v2 delivery locker는 cache가 없을 때 dedup을 끈 noop locker로 내려가지 않고 생성 오류이며, digest scheduler는 locker 없이 실행하지 않습니다. bot 응답 formatter의 퇴역 Chzzk/Twitch 분기와 멤버 프로필의 치지직 링크를 지웠습니다(저장된 식별자는 보존).
- 운영 도구를 정리합니다. epoch-2에서 거부만 하던 `apply-all.sh`·`bootstrap-and-apply.sh`, send-unit 이전 rollback preflight, 퇴역 Redis dispatch queue용 `requeue-alarm-dlq.sh`와 그 CI 리터럴 고정을 삭제했습니다. AP 배포·rollback은 퇴역 producer 첫 cutover 상태 기록·복원과 repo 루트 compose 경로 폴백 없이 동작하며, 기록된 `rollback-image-tag`(Compose)나 `previous` release(host-native)가 없으면 rollback을 거절합니다. `rollback-image-tag`가 있는 Compose AP 배포가 cutover 뒤 실패하면 이전 collector와 issuer를 함께 자동 복원하고, 없는 첫 배포는 새 collector와 issuer 컨테이너를 멈추고 fix-forward를 안내합니다. 중앙 compose up/down은 live-compat overlay를 항상 넣고 `HOLOLIVE_ENABLE_LIVE_COMPAT` drop-in과 빈 main-ap overlay 2종·profile을 지웠습니다. AlarmService의 미사용 Holodex 의존성을 지우고, `youtube:producer` Valkey 키 접두사는 전환 계획 없이 바꾸지 않은 채 파일명만 정리했습니다.
- `IRIS_WEBHOOK_REQUIRE_HMAC` 주입(prod compose `x-iris-env`, `.env.example`)과 `Webhook.RequireHMAC` 필드·값 가드를 지우고, 키가 남아 있으면 빈 값이나 `true`여도 존재만으로 hololive-api·alarm-worker 기동을 거절합니다(remove_after 2026-12-31, 계획 T11 C8). 배포 전에 중앙 compose.env와 bot.env·alarm-worker.env, 그 stack-secrets master에서 키를 지워야 합니다.
- `live_snapshot` contract generation 1 decoder·supported contract 항목과 collector의 generation 1 payload 경로를 지웁니다(C6 runbook 조건은 T18에서 충족). API는 generation 1 관측을 unsupported contract로 처리하고, collector는 DB current generation이 2가 아니면 `configuration_error`로 수집을 끝냅니다. migration 225는 빈 DB의 `live_snapshot` seed를 generation 2로 맞추며 운영 DB에서는 갱신 대상이 없습니다(운영 적용된 live-evidence 218~220과 부재 증거 보존 221 다음 번호).
- hololive-api settings 서비스도 `settings.json` 해석을 `settings.ReadFile` 하나로 합쳐, 읽기·decode 실패나 구형 형식을 기본값으로 대신하지 않고 bot·admin plane 기동 실패로 드러내며, 정규화 결과를 파일에 다시 쓰지 않습니다.
- dispatch 발행은 `Version` 0을 V1으로 채우지 않고 거절하며(celebration·birthday stream·X Space 생산자가 명시), dispatch consumer는 claim key releaser가 필수라 없으면 해제 오류를 돌려줍니다. member cache는 분산 캐시가 있으면 epoch authority가 필수이고 epoch 없는 구형 `member:*` keyspace 분기를 지웠습니다. collector는 구 helper failure class를 기본 class로 고치지 않고, 계약 밖 tuple을 미분류 `collection_internal_invariant`로 닫아 지연 처리하며 `youtube_collection_invalid_failure_tuple_total`로 셉니다.
- 잔존 0건이 확인된 legacy 수용을 지웁니다. v2 delivery outbox의 lease 이전(`locked_by` NULL) lock 회수·fence와 `FetchAndLock`의 lockTimeout 인자(worker profile `notification_delivery.lock_timeout_ms`는 이제 읽지도 양수 검증하지도 않지만, exact-key profile 계약 때문에 stack-secrets profile에서 함께 지울 때까지 키는 남음), 빈 PARTIAL Shorts watermark 무시 규칙, 비정규 YouTube source identity의 해시 수용(이제 dispatch 원장 저장 전에 거절), YouTube dispatch payload의 `render_template_key`·`pre_rendered_message`, 퇴역 Chzzk/Twitch·Redis dispatch queue Valkey 키의 예약 항목과 상수(배포 전 잔존 키 3개를 1회 삭제)입니다.
- 호출자 없는 코드와 끝난 도구를 지웁니다. AlarmQueueEnvelope 미사용 payload accessor, 테스트 전용 LLM scheduler 비-runtime 로더, 폐기된 `ChannelTargetGroupStats` alias, settlement archive migration·수동 drop 스크립트·퇴역 runbook, Osaka legacy log rollup 설치 스크립트, ap-status의 AP에 없는 신호·split-host 볼륨 진단, migration manifest 검사의 epoch-1 예외 목록, 끝난 전환을 소스 텍스트로 고정하던 테스트입니다. `alarm.dispatch` 계약 문서(CONTRACT_MANIFEST·CONTRACT_MAP·contracts/alarm·QUEUE_AND_PUBSUB_CONTRACTS·dlq-replay runbook·README)는 퇴역 Redis queue와 version 0 수용 대신 PostgreSQL dispatch outbox 기준으로 고쳤습니다.
- alarm-worker의 `ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH`·`CELEBRATION_CHECK_HOUR_KST`·`CELEBRATION_RUN_INTERVAL_MS`·`BIRTHDAY_STREAM_POLL_INTERVAL_MS`·`BIRTHDAY_STREAM_SESSION_FRESHNESS_MS`가 정수가 아니거나 범위를 벗어나면 경고 없이 기본값으로 바꾸지 않고 기동에 실패합니다(PLN-20260926-stack-audit-refactoring T10 후속). `CELEBRATION_CHECK_HOUR_KST`는 0–23만 받고(이전: 24 이상도 받아 날짜 정규화로 설정과 다른 시각에 실행됨), celebration·birthday stream 키는 해당 runner가 켜졌을 때 infra 확인보다 먼저 읽습니다. 값이 없거나 비어 있으면 지금처럼 기본값입니다. 배포 전에 alarm-worker env에 이 키가 있으면 값이 파싱되는지 확인해야 합니다.
- 품질 항목을 정리합니다(PLN-20260926-stack-audit-refactoring T12). nil context를 `context.Background()`로 바꾸거나 건너뛰거나 따로 오류로 돌려주던 분기(hololive-api·alarm-worker·youtube-collector·hololive-shared 30여 곳)를 지웠습니다. context는 호출자가 항상 넘기는 계약이며 nil을 넘기면 호출 지점의 결함입니다. 생성형 결과 이름(`value0`·`ok1`·`result1` 등)을 정리했고, production 코드의 단일 인자 `errors.Join(x)` 감싸기(변수 인자와 같은 패키지 helper 호출 인자 모두)를 직접 반환으로 바꿨습니다. 오류 문자열과 `errors.Is`·`errors.As` 판정은 그대로입니다. 캘린더 사진 렌더러의 테스트 전용 래퍼 `fetchMemberPhoto`·`fetchImage`를 지우고 테스트가 context를 받는 함수를 직접 부릅니다. youtubedispatch 테스트의 `deliveryTestDB` 호환 alias와, T18에서 운영 collector env 0건을 확인한 collector 퇴역 alias(`YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`·`YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS`) 무시 고정 테스트를 지웠습니다(prod compose의 collector canonical 기본값 렌더 검사는 별도 테스트로 유지). Holodex cache-fill의 원천 장애 꼬리 지연은 측정 기록만 남겼습니다(`docs/current/services/hololive-api.md`).
- outbox v3 handoff를 삭제하고 v1 YouTube 알림과 v2 digest의 direct egress를 정본으로 둡니다(`DEC-20260926-hololive-outbox-v3-convergence`, PLN-20260926-stack-audit-refactoring T19). `off`·`shadow`·`cutover` 분기, `PublishShadowDispatchBatch`, `alarm/handoff` 패키지, handoff metric(`hololive_youtube_outbox_v3_handoff_total`·`hololive_delivery_outbox_v3_handoff_total`), shadowed retention을 지웠습니다. `YOUTUBE_OUTBOX_V3_HANDOFF_MODE`·`DELIVERY_OUTBOX_V3_HANDOFF_MODE`는 prod compose에서 주입하지 않고, 빈 값이어도 존재만으로 hololive-api·alarm-worker 기동을 거절합니다(remove_after 2026-12-31). migration 226이 `alarm_dispatch_deliveries` 상태 CHECK에서 `shadowed`를 빼고, 적재 SQL은 shadowed 승격 UPDATE 없이 `ON CONFLICT (dedupe_key) DO NOTHING`입니다. 관리 조회 필터 `status=shadowed`는 iris-console enum과 필터 제거 배포 뒤 삭제했으며, 다른 알 수 없는 상태처럼 400으로 거절합니다. 226은 225 다음 번호입니다.
- YouTube delivery telemetry를 lifecycle 전이 트랜잭션 안의 단일 기록으로 바꿉니다(`DEC-20260926-hololive-delivery-telemetry-single-path`). `TransitionStore`가 `CompleteSent`·실패 전이·stale SENDING 격리 트랜잭션에서 owner 시도 하나당 한 행을 쓰고, 호출자는 `per_room`·`grouped` 발송 방식만 넘깁니다. prepared failure(`message_missing`·`pre_send_claim` 포함)도 기록하고, 결과 불명 격리는 `outcome_unknown`·`stale_sweep`으로 남깁니다. `failure_reason`은 lifecycle Reason 코드이고 `post_id`는 검증된 logical key입니다. `attempt_ordinal`은 버퍼에 남은 delivery별 최대 순번 다음 값과 claim 시점 `attempt_count + 1` 중 큰 값이라 revive 뒤나 telemetry retention(기본 24h) 정리 뒤에도 되돌아가지 않습니다. 버퍼 행이 지워진 뒤 revive된 delivery만 1부터 다시 세고, 같은 시도 식별자는 조용히 건너뛰지 않고 오류로 전이를 rollback합니다. commit 뒤 metrics recorder의 직접 enqueue와 `direct_fallback` 로그, 분류 재저장, delivery 테이블 역산 backfill(`recovered`)과 profile 키 `youtube_delivery.telemetry_backfill_batch`를 삭제했습니다. exact-key profile 계약 때문에 운영 alarm-worker profile의 키 제거는 이 release 배포와 같은 유지보수 단계에서 해야 하고, 이 release 이전 alarm-worker image로 rollback할 때는 키를 다시 넣어야 합니다(`docs/current/runbooks/rollback.md`). DEC 순서 조항에 따라 이 변경은 alarm-worker Karing dead branch 삭제(`DEC-20260926-hololive-karing-egress-disposition`)와 같은 release로 묶어 내보냅니다.
- v2 delivery locker가 Valkey `SetNX`·`CompareAndDelete`·`DelMany` 오류를 경고만 남기고 lock 없이 진행하지 않고 오류로 돌려줍니다. digest 실행은 lock 획득 오류로 실패해 다음 주기에 다시 시도됩니다.
- scraper proxy와 퇴역 producer 시대 설정을 지웁니다(`DEC-20260926-hololive-legacy-env-config-retirement`, `DEC-20260926-hololive-youtube-producer-budget-retired`, PLN-20260926-stack-audit-refactoring T19). 공개 계약 변경: admin 설정 API(`GET`·`POST /api/holo/settings`)는 `scraperProxyEnabled`를 받지 않고, 응답 runtime의 scraper proxy 항목과 `config_publish_scraper_proxy*`를 내지 않으며, 설정 파일에도 쓰지 않습니다(소비자 iris-console이 먼저 필드를 지움). `scraper_proxy` 설정 pub/sub type·runtime proxy 토글·SOCKS5 proxy client·scheduler proxy 전환, `ScraperConfig`의 scheduler·poll·snapshot·channel health·backfill 필드와 loader·검증·`*OrDefault`, 퇴역 producer의 scheduler·budget 코드(`ProvideScraperScheduler`, `scraper_scheduler_options.go`, `poller/runtime/scheduler`)를 삭제했습니다. `SCRAPER_PROXY_ENABLED`·`SCRAPER_PROXY_URL`·`SCRAPER_SCHEDULER_*`·`SCRAPER_POLL_*`·`SCRAPER_SNAPSHOT_*`·`SCRAPER_CHANNEL_HEALTH_*`·`SCRAPER_BACKFILL_*`와 가드 없이 무시되던 `SCRAPER_*_SECONDS`·`SCRAPER_WORKER_COUNT`·`IRIS_SHARED_TOKEN`은 빈 값이어도 hololive-api·alarm-worker 기동을 거절하고, youtube-collector는 `SCRAPER_PROXY_*`와 HC-013 이전 alias(`YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES`·`YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS`)를 같은 기준으로 거절합니다(remove_after 2026-12-31). 배포 전에 모든 youtube-collector env와 stack-secrets master에서 `SCRAPER_PROXY_*`를 지워야 합니다.
- youtube-collector YouTube.js helper의 proxy 경로와 `undici` 의존성을 지웁니다(`DEC-20260926-hololive-legacy-env-config-retirement` 후속, PLN-20260926-stack-audit-refactoring T16 인계). `SCRAPER_PROXY_*` 퇴역 뒤 collector는 늘 proxy 없이 bootstrap해 production에서 도달할 수 없던 Go `youtubejs.ProxyConfig`·`Config.Proxy`와 proxy URL 검증, bootstrap 요청의 `proxy`, bootstrap·`/health` 응답의 `proxy_enabled`, Node `ProxyAgent` transport·`loadUndici` 주입·proxy URL redaction과 이를 닫던 transport close timeout(`--shutdown-timeout-ms`)을 matched pair로 삭제했습니다. helper는 Node 내장 `fetch` 한 경로만 쓰며 재시도·취소·오류 분류는 그대로입니다. helper protocol 변경: bootstrap은 `protocol_version`과 `limits`만 받고 `proxy`는 unknown field로 거절합니다. collector와 helper는 같은 image로 함께 배포되며 섞인 조합은 bootstrap protocol mismatch로 기동에 실패합니다. collector hardening contract의 `new ProxyAgent` 필수 규칙(HC-002)도 지웠습니다.
- rate limiter와 dispatch worker 식별자는 hostname 하나만 씁니다. hostname을 얻지 못하면 random·`local`·`unknown-host`로 바꾸지 않고 limiter 생성과 alarm-worker 기동이 실패하며, 퇴역한 `INSTANCE_ID`는 분산 limiter 활성 여부와 무관하게 hololive-api·alarm-worker 설정 로드에서 빈 값이어도 거절합니다(T18: 운영 env에 없음). 표준 OTel endpoint env 거부는 퇴역 가드가 아닌 영구 계약으로 이름과 문서를 고쳤습니다. 코드와 맞지 않던 `DEC-20260512-hololive-alarm-http-provider-ownership`·`DEC-20260625-hololive-runtime-role-boundary`는 정정 레코드(`-v2`)가 대체합니다.
- 끝난 전환·rollback 도구를 지웁니다(`DEC-20260926-hololive-retired-rollback-tooling`, T19). rollback runbook의 5→3 긴급 rollback 절, epoch-1 복구 도구(`preflight-114-restore.sh`, `repair_message_contract_074_082.sh`, epoch-1 소스 사본과 source contract 검사)와 `db-maintenance-exec.sh`의 rollback 출력 마운트, epoch-2 baseline 생성기(`normalize-epoch2-baseline.py`는 `--check-existing` 검사만 남김), Dockerfile이 빌드하지 않던 `warm_member_cache`·`test_db_integration` 보조 cmd, admin-dashboard 퇴역 경로 가드와 SQL·DB 접근 검사의 admin-dashboard 대상을 삭제했습니다. YouTube delivery ledger backfill 명령·패키지·Dockerfile 빌드 줄과 alarm-worker `TransitionStore`의 전이마다 완료 표식을 읽던 gate도 지웠고, 완료 전제는 migration 227(`227_youtube_delivery_ledger_backfill_closed.sql`)이 적용 시점에 확인합니다. 227은 미완료 state가 있거나 backfill 없이 delivery·outbox 행이 있으면 실패하고, 운영 DB(T18: `completed_at` 있음)와 빈 DB는 통과합니다. 번호는 226 다음입니다.
- 방 ACL은 chatID 하나로만 판정합니다(`DEC-20260926-stack-hololive-room-acl-and-console-contract`, PLN-20260926-stack-audit-refactoring T19). `acl.Service.IsRoomAllowed`가 방 이름 인자를 받지 않아, 방 이름으로 등록된 값은 같은 이름의 다른 방을 허용하거나 차단하지 않습니다(T18: 운영 `acl_rooms` 3행 모두 숫자 chatID). 새 등록값도 chatID만 받습니다. 공개 계약 변경: admin `POST /api/holo/rooms`는 정확한 signed i64 chatID 문자열(부호·앞자리 0·범위 검사, iris-console `roomRegistration`과 같은 규칙)이 아니면 400으로 거절하며, 이미 저장된 비-chatID 값은 조회·제거만 할 수 있습니다. ACL 첫 초기화 seed `KAKAO_ROOMS`는 방 이름 기본값 `홀로라이브 알림방`을 지웠고 항목이 chatID가 아니면 첫 초기화 여부와 관계없이 hololive-api 기동을 거절합니다. `KAKAO_ROOMS`가 없거나 비면 기존 필수값 검증이 hololive-api(bot·admin plane)와 alarm-worker 기동을 거절하므로, 배포 전에 중앙 compose 보간 env의 `KAKAO_ROOMS`가 chatID 목록이어야 합니다. 멤버 목록 API(`GET /api/holo/members`)가 별명 없는 멤버에도 `aliases: {"ko":[],"ja":[]}`를 항상 내는 계약을 테스트로 고정했으며, iris-console은 이 계약에 맞춰 부재·null 보정을 지웁니다.
- LLM 지시를 invariant/developer/user 계층으로만 보냅니다(`DEC-20260926-stack-llm-instruction-layering-sole-path`, `DEC-20260926-stack-shared-go-compat-api-retirement`, T19). 내부 `llm.Client.GenerateJSON`이 `openaipreset.PromptLayers`를 받습니다. major event 요약 생성의 신뢰 경계(web_search_context 안의 지시는 무시하고 데이터로만 다룸)는 `prompts/invariant_prompt.tmpl`로 분리해 invariant 계층으로 보내고, major event·member news의 요약·검토·판정 작업 절차와 출력 형식은 developer 계층, 사건·후보·검색 결과는 user 계층입니다. prompt 자산 버전이 바뀌어 major event 요약 캐시(24시간)는 새 키로 다시 채워집니다. OpenAI 호환 client는 shared-go `JSONRequest.SystemPrompt` 대신 `InvariantPrompt`·`DeveloperPrompt`를 쓰고(Responses는 라벨 붙은 developer 메시지, Chat Completions는 라벨 붙은 단일 system 메시지), preset client는 퇴역할 `openaipreset.GenerateJSON(systemPrompt)` 대신 `GenerateJSONAs`로 JSON 원문을 받으며 schema 이름이 비면 생성에 실패합니다. Gemini의 `system_instruction`도 같은 라벨의 단일 섹션으로 보냅니다. 출력 유출 검사는 invariant·developer 계층을 함께 보호합니다. preset provider가 JSON이 아닌 출력을 내면 client 오류로 드러나며, member news consensus는 이전과 같이 primary 요약을 씁니다(로그 문구만 parse 실패에서 호출 실패로 바뀜).
- DB pool은 적재한 `POSTGRES_SSLROOTCERT`를 pgxdb `Config.SSLRootCert`로 직접 넘기고 shared-go pgxdb의 env 폴백에 기대지 않습니다(`DEC-20260926-stack-shared-go-compat-api-retirement`). 설정 원천과 값은 그대로이며, shared-go가 env 폴백을 지우기 전에 이 release가 먼저 배포되어야 합니다. youtube scraper 테스트는 퇴역할 `jsonutil.ErrBodyTooLarge` 대신 `httputil.ErrResponseBodyTooLarge`를 씁니다.
- native 완료 검사도 기존 bounded issuer 준비 대기를 사용해 SDK 기동 중의 단발 health 실패를 피합니다. 실제 5초 worker 기동에서 단발 검사는 2초에 실패하고 준비 대기는 성공했습니다. SDK 기동 deadline과 세대 교체 준비 deadline의 종료·정리 회귀를 추가하며 발급·upstream 예산은 변경하지 않습니다.
- issuer의 신뢰된 SDK import를 서비스 준비 단계로 옮겨 첫 prepare의 8초 예산에서 분리합니다. worker의 `loaded` 전에는 health를 제공하지 않고 native 배포/복원도 준비를 기다립니다. SDK 기동은 30초, helper의 세대 교체 준비는 종료·재시작을 포함해 40초로 제한합니다. 준비 뒤 prepare부터 발급 15초를 적용하며 개별 요청 8초·시도 간격 300초·격리 상한은 유지합니다. CPU 제한 재현과 실제 세대 교체 smoke를 통과했으며 운영 자원 경합 전반의 해결을 보장하지는 않습니다.
- PO 발급 실패 뒤 generation 정리 오류가 최초 원인을 덮어쓰던 진단 결함을 수정합니다. `last_error`와 `cleanup_error`를 분리하고, 새 발급 cycle에서 이전 오류를 초기화합니다. 요청·시간 상한과 재발급 간격은 변경하지 않습니다.
- 구독 대상 동시 조회 회귀를 `testing/synctest`로 검증해 전체 빌드 부하에서 100ms goroutine 시작 제한이 실패하던 문제를 제거합니다. 두 조회의 동시 진행과 타입별 반환 대상 검증은 유지하고 DB fixture는 가상 시계 밖에서 관리합니다.
- YouTube 라이브 확인에 정상 PO Token 발급·갱신·영상별 첨부를 연결합니다. 외부 interpreter는 앱 비밀과 네트워크가 없는 별도 issuer에서 실행하며, challenge 요청 전에 동일 UA/JSDOM 준비를 완료합니다. 만료·취소·발급 실패에는 기존 단일 무토큰 조회와 UNKNOWN 판정을 보존합니다.
- native/Compose collector와 issuer를 같은 full SHA로 빌드·검증·교체하고 실패 시 함께 복원합니다. AP 소스는 staging 검증 뒤 승격하고 snapshot으로 중단 전 상태를 복원합니다. 중앙 개별 collector/issuer 재배포는 paired entrypoint로 통합하며 `compose-redeploy-service.sh all`은 지원하지 않습니다.
- issuer rootfs의 소켓 디렉터리 권한(0770)과 `/tmp` sticky bit(1777)를 이미지 복사·일반 사용자 압축 해제에서도 보존합니다. 빌드 호스트 umask에 따라 배포 검증이 실패하던 원인을 수정하며 기존 무결성 검사는 유지합니다.
- native 배포의 두 필수 스크립트를 각각 검사해 정상 payload의 잘못된 거부를 고치고, 어느 파일이 빠져도 운영 변경 전에 중단합니다.
- native 설치·재검증·복원에서 root 소유 issuer archive의 hash는 root 권한으로 읽고 0600 보호 권한을 유지합니다. `systemd-analyze verify`에는 RootDirectory 안의 실제 실행 경로를 해석한 검사용 사본을 제공하며, 설치 unit의 격리 설정과 실행 파일 부재 거부는 보존합니다.
- systemd가 사용하는 빈 mountpoint를 issuer 이미지에 포함해 기동 전후 rootfs manifest를 동일하게 유지합니다. native 최초 설치의 rollback은 기록된 issuer 부재를 명시적으로 검증하고, 기존 bounded readiness 대기를 재사용해 재기동 직후 503을 완료로 오판하지 않습니다.
- Docker classic/containerd 저장소가 같은 이미지에 서로 다른 ID를 보고하는 차이를 archive의 manifest/config digest로 검증합니다. 등록된 두 표현 외의 이미지, revision·architecture·archive hash 불일치는 계속 거부합니다.
- 배포 검사에서 스크립트 문자열·등장 횟수·옛 함수 위치와 main 호출 배선만 비교하던 스냅샷을 제거하고, 버전 불일치·구성 실패·아티팩트 무결성·잘못된 배포 대상의 거부 검증은 유지합니다. 실제 broker CLI의 인수 거부·live socket 보존·강제 종료 뒤 stale socket 복구를 확인합니다.
- `youtube_live_absence_slots`를 `scheduled_for` 기준 30일(`YOUTUBE_PLANE_RETENTION_LIVE_ABSENCE_SLOTS_DAYS`, 기본 30, production은 양수 필수)이 지나면 retention tick당 최대 1000건 지웁니다. 삭제는 migration `221_live_absence_slot_retention.sql`의 제한된 함수가 수행하며, 30일보다 오래된 `live_snapshot`이 queue에서 대기·처리 중이거나 replay가 pending이면 보류합니다. 이미 session/head와 pending end에 반영된 사실은 대상이 아니지만, 30일 밖 slot은 과거 positive 재처리로 복원할 수 없습니다. 221은 2026-09-28 운영 ledger에 순번 082로 적용된 파일이라 bytes와 checksum을 바꾸지 않습니다.
- X가 로그아웃 상태의 `/home`을 로그인 화면으로 redirect하여 X 스페이스 수집이 요청 ID 초기화 단계에서 `collector_failed`로 반복 실패하던 문제를 수정합니다. `x-client-transaction-id`를 `0.3.2`로 올리고, helper 허용 목록을 인증 없는 앱 셸 `https://x.com/i/jf/`로 바꿉니다. 쿠키 전송 경로·오류 계약·런타임 설정은 변경하지 않습니다.
- X 스페이스 helper 실패 로그에 실패 단계(`input`·`library`·`app_shell`·`transaction`·`collect`)를 남기고, 분류되지 않은 `collector_failed`에는 내장 오류 종류와 Node 오류 코드만 덧붙입니다. helper가 결과 문서를 끝내지 못하면 worker가 관측한 종료 상태를 `helper_output`으로 기록합니다. 예외 원문·stderr는 계속 버리며 세션 상태의 오류 코드·재시도 간격은 바꾸지 않습니다. helper와 worker의 결과 문서 형식이 함께 바뀌므로 alarm worker 이미지 단위로 배포합니다.
- live consume의 현재 absence slot 재조회에 `scheduled_for` 인덱스(migration 212)를 추가합니다. 채널 GIN이 해당 채널의 무기한 보존 이력 전체를 heap에서 다시 검사하던 비용을 slot 한 행으로 줄입니다. absence slot 보존 계약과 조회 결과는 바꾸지 않습니다.
- 운영 조회가 선택하지 않는 source observation·queue 인덱스 네 개를 `DROP INDEX CONCURRENTLY`로 제거합니다(migration 213–216). 보존 기간과 보존 삭제·claim 경로는 유지합니다.
- 로컬 통합 검사가 PostgreSQL·Valkey 일회용 컨테이너를 지울 때 이미지가 선언한 익명 volume도 함께 지웁니다.
- 중앙 compose wrapper의 release version export 검사를 gate에 연결된 `ap-deploy-version_test.sh`로 옮깁니다. `compose-version-contract_test.sh`는 어떤 gate에도 연결되지 않은 채 2026-09-14 admin 통합 이후 Compose 문구 개수 불일치로 실패해 왔으므로 삭제합니다. 두 VERSION 읽기와 호출자 버전 불일치의 fail-closed는 계속 검사합니다.
- YouTube 발송 정리에서 FAILED 자식에 존재하지 않는 ledger 증거를 요구해 terminal outbox가 영구 보류되던 문제를 수정합니다. 같은 논리 키의 PENDING/SENDING 자식은 계속 보류하고, SENT/QUARANTINED 자식의 ledger 검증도 유지합니다.
- alarm dispatch 보존 삭제가 행 잠금을 기다리는 동안 DLQ 행이 retry로 재등록되면 삭제하지 않도록 상태·보존 시각을 다시 검사합니다. 수동 보존 삭제 스크립트에도 같은 조건을 적용합니다.
- YouTube PENDING claim·stale SENDING 조회에 상태 리터럴을 명시해 generic plan에서도 부분 인덱스 조건을 증명하고, aggregate sync 후보는 EXISTS/NOT EXISTS로 조회합니다. 후보 집합·순서·배치 상한은 유지합니다.
- 운영 호출자가 없는 `alarmread.Reader`, `ProvideAlarmReader`, `alarm.Repository.GetAllChannelIDs`와 대응 SQL을 제거합니다. YouTube plane의 projection transaction과 llm plane membernews의 직접 구독 조회는 유지합니다.
- source observation claim 후보를 PENDING과 만료 PROCESSING 가지로 나눕니다. PENDING 가지는 partial index 순서로 LIMIT까지만 읽고, 만료 PROCESSING 가지는 만료 행만 정렬합니다. 활성 backlog 5만 행 fixture에서 후보 선택이 읽는 행이 약 9.8만에서 10행 안팎으로 줄고, claim 순서·`SKIP LOCKED`·replay epoch·shorts 순서 계약은 유지합니다. 한 claim은 최대 2×LIMIT행을 claim transaction이 끝날 때까지 잠급니다.
- 발행 검증(fence·projection·target) 조회 세 개를 pgx 파이프라인 한 번으로 보냅니다. 잠금 순서와 판정 우선순위는 그대로이며 발행당 왕복이 두 번 줄어듭니다.
- content 관측은 로드 값과 달라진 evidence clock만 한 번의 배치로 저장하고, clock upsert에 값 열 14개의 `IS DISTINCT FROM` 가드를 둡니다. 상태 로드는 관측된 영상·clock 보유 영상과 현재·이후 absence slot만 잠그며 slot을 `scheduled_for` 순으로 적용합니다. 역순으로 저장된 slot을 sequential scan으로 읽어도 첫째·둘째 부재와 철회 시각이 유지되는지 PostgreSQL 회귀 테스트로 검증합니다.
- schedule 관측이 읽지 않던 누적 schedule item 전체 `FOR UPDATE` 조회를 제거하고, claim 예산은 남은 lease가 예산보다 짧을 때만 연장합니다. live 종료 finalizer는 due 조회에서 DB 시각을 함께 읽고, content 충돌 기록은 공용 reconcile 충돌 SQL을 씁니다. migration은 없습니다.
- 커뮤니티 게시물을 다시 관측했을 때 좋아요·댓글 수와 `published_at` 보강값이 그대로면 행을 다시 쓰지 않습니다. 따라서 `youtube_community_posts.last_seen_at`은 마지막 관측 시각이 아니라 마지막 값 변화 시각을 뜻하며, 이 열을 읽는 곳은 없습니다.
- 발송 telemetry의 기록 대상 선택과 lease 획득을 `FOR UPDATE SKIP LOCKED` 한 문장으로 합칩니다. 다른 인스턴스가 잡은 행은 기다리지 않고 건너뛰므로 한 번에 배치 한도보다 적게 반환할 수 있습니다. 반환 순서는 `(event_at, id)`입니다.
- bot 원장 보존 삭제의 cutoff를 문장 시작 시각(`statement_timestamp()`) 기준으로 계산하고 outbox 조건을 부분 인덱스 술어별로 나눠, 보존 기간 안의 이력을 훑지 않고 만료 행만 terminal 부분 인덱스로 읽습니다. 보존 기간, manual_review 분리 보존, 배치 한도와 `SKIP LOCKED`는 그대로입니다.
- Kakao 방 정보가 이미 저장값과 같으면 명령마다 하던 upsert 쓰기 트랜잭션을 만들지 않습니다. collector job lease 획득은 claim UPDATE가 돌려준 식별자로 job identity를 검증해 같은 행을 다시 읽는 왕복 한 번을 없애며, 불일치는 계속 `ErrInvalidJob`으로 롤백합니다.
- 호출자가 없는 hololive-shared·API 코드를 삭제합니다: `ViewerSampleCleaner`와 viewer 표본 보존 삭제 SQL, `dbx.WithSessionAdvisoryLock`, 발송 telemetry의 로그·경로 사용량·채널 게시물 요약·지연 기간 요약 조회, 게시물 타임라인의 기간 조회 두 개와 발송 수의 게시 구간 조회, ACL `CountRooms`. viewer 표본 데이터와 스키마는 DEC-20260925에 따라 유지하며 migration은 없습니다.
- `POSTGRES_POOL_MIN_CONNS=0`을 idle 연결 없음으로 그대로 적용합니다. 이전에는 plane 검증이 허용한 0을 풀 생성 시 2로 바꿔 `MIN=0·MAX=1` 조합이 기동에 실패했습니다. 음수는 연결 전에 거부합니다. 운영 compose 기본값(MIN 1·2)에는 영향이 없습니다.
- PostgreSQL 용량 gate의 reserve를 superuser 예약 3(`@superuser-reserved`)을 뺀 비슈퍼유저 여유로 계산하고 하한을 2로 둡니다. 현재 할당 55에서 통과·거부 판정은 이전과 같으며, compose가 `superuser_reserved_connections`를 바꾸면 policy 불일치로 거부합니다. collector 기본 max(8)는 바꾸지 않습니다.
- `holo-postgres`에 `log_autovacuum_min_duration=10s`를 추가해 10초 이상 걸린 autovacuum의 WAL/FPI·소요시간을 로그로 남깁니다. compose command 값이라 `holo-postgres` 재생성 뒤에 적용되며, 재생성은 별도 운영 승인으로 수행합니다. 전체 compose `up`뿐 아니라 서비스별 배포 wrapper의 선행 `run --rm hololive-db-migrate`도 의존 DB를 재생성할 수 있으므로, 최종 앱 `up --no-deps`만 보고 DB 무중단 배포로 판단하지 않습니다.
- 참조가 없거나 실행하면 해로운 수동 SQL(`seed_member_celebration_dates.sql`, `audit_message_contract_087_090.sql`, 주석뿐인 `pg18_db_usage_optional_concurrent_indexes.sql`)과 PK 때문에 항상 0행인 점검 쿼리, 호출자가 없는 `dbx` 배치 삭제 helper를 삭제합니다. dbtest 하니스는 러너와 같은 `dbmigrate.Manifest`로 manifest를 해석합니다.
- `.env.example`의 YouTube plane 보존 주기 예시값을 코드 기본값·DEC-20260824와 같은 120초로 맞춥니다. 300초에서는 보존 삭제 상한(1회 1,000행)이 하루 28.8만 행이라 2026-09-27에 실측한 application 유입(하루 약 42만 행)을 따라가지 못합니다. 운영 `compose.env`는 같은 날 120초로 바꿨습니다.

## v4.0.1 - 2026-09-25

- 공유 모듈을 `shared-go v2.7.1`, gRPC를 수정 안정판 `v1.83.2`로 고정하여 `CVE-2026-84445` / `GO-2026-6443`을 해소합니다. 정기 보안 검사의 shared-go SHA도 같은 릴리즈에 맞춥니다.
- ARM64 이미지 검사에서 `grpc v1.84.0`의 `affected` finding을 통과시키던 예외를 제거하고, 정확한 패키지 부재 증명만 허용합니다.
- GitHub Release 제목·본문을 `v3.5.0`의 자동 생성 형식으로 통일하고, 운영 검증 기록과 공개 릴리즈 본문의 작성 절차를 분리합니다.
- 운영 산출물은 API·collector `4.0.1`, alarm worker `3.2.6`으로 식별합니다. DB migration·런타임 설정·공개 API는 변경하지 않습니다.

## v4.0.0 - 2026-09-25

- YouTube.js 시청자 수 전용 RPC와 Holodex의 viewer 표본 발행을 중단합니다. 방송 상태·일정·알림 및 과거 관측의 소비·재처리는 유지합니다.
- 라이브 명령과 템플릿 미리보기에서 `ViewerCount`를 제거하고 미지원 변수는 명시적으로 거절합니다. 방송 개수·제목·링크와 외부 Stream API 필드는 보존합니다.
- Holodex 작업별로 필요한 payload만 계산하여 무관한 메타데이터 충돌이 방송·일정 수집을 막는 결함과 중복 순회·복사를 제거합니다.
- 수집 수요를 네 YouTube.js 작업으로 집계하고 방송 상태 진단을 viewer 대상에서 분리합니다. migration 210의 활성 head 부분 인덱스로 종료 이력 전체 조회를 피합니다.
- `ViewerCount` 템플릿 변수와 viewer 운영 지표의 계약 퇴역을 포함하므로 root/API artifact를 4.0.0으로 올립니다. alarm-worker 3.2.5와 외부 Stream API는 유지합니다.
- 관리자 기능 조회의 취소 신호를 HTTP까지 전달하고, 사전 로딩 실패와 화면 코드 복구를 정리했습니다.
- 이름·채널 ID 저장 중 추가 입력을 잠가 저장 응답이 새 초안을 닫아 버리지 않도록 했습니다.
- 멤버와 통계 전용 UI를 각 기능 디렉터리로 옮겨 소유 경계를 일치시켰습니다.

## v3.5.6 - 2026-09-24

- 템플릿 표시·복사 가능한 명령과 DB row_version 기반 캐시 일관성을 적용합니다(migration 203–208).
- 수집 대상·진행·RPC 지표를 추가하고 unknown을 보존하는 발송 전이를 필수화합니다.
- 퇴역 X Spaces 자동 로그인 실행 경로를 제거하고 수동 쿠키와 읽기 전용 이력은 유지합니다.
- API 3.2.4, alarm worker 3.2.5와 shared-go v2.7.0을 사용합니다.

## v3.5.5 - 2026-09-24

### 수정

- 게시된 shared-go v2.6.4와 iris-client-go v2.6.2를 모든 Go 소비 모듈에 고정하고 QUIC 0.63.0 및 OpenAI 클라이언트 패치를 적용했습니다.
- YouTube.js 18.1.0과 undici 8.11.0을 고정하고 업스트림에서 해결된 attachment-run 보정 코드를 제거했습니다.
- 앞서 main에 병합된 YouTube 429 cooldown 수정을 중앙 collector 이미지에 포함합니다.
- 중앙 Valkey 9.1.2의 Alpine 3.24.2 이미지를 검증된 digest로 고정하고, CI Python 실행기와 uv 부트스트랩을 0.12.18로 정렬했습니다.
- X Spaces 로그인 이미지는 Chromium headless shell만 보존하도록 재구성하고 Node 24.21.0을 사용합니다.
- 이번 릴리즈의 API artifact 버전은 3.2.3, alarm-worker artifact 버전은 3.2.4입니다.

## v3.5.4 - 2026-09-12

### 수정

- iris-client-go v2.6.0을 소비 모듈에 고정해 API webhook 생성 시 HandlerOption을 한 번만
  적용하며 기존 인증·nonce 저장소·durable admission 설정을 보존합니다.
- YouTube delivery 전이가 요청한 방·알림 종류·identity tuple만 묶어 다른 요청의 교차 조합을
  선택하지 않습니다. Raw·canonical identity 후보를 한 번씩 join하고 direct ID와 합쳐 중복을
  제거하며 기존 정렬·조회 상한·행 잠금 계약을 유지합니다.
- 실제 prepared custom/generic 계획에서 tuple-only sibling과 direct-only 행의 전체 ID·순서,
  요청 관계의 반복 실행 상한을 회귀 검사합니다.
- 배포 전 PG hot-path 검사가 YouTube claim의 `FOR UPDATE OF outbox SKIP LOCKED` 구문을
  포함하고, EXPLAIN·관측기·혼합 fingerprint를 제외하도록 SQL·결과 분류를 일치시킵니다.
- 이번 릴리즈의 API artifact 버전은 3.2.2, alarm-worker artifact 버전은 3.2.3입니다.

## v3.5.3 - 2026-09-11

### 수정

- alarm-worker 3.2.2의 LIVE 발송 누락 검사가 알림 생성과 동일하게 확인된 Premiere를 제외합니다.
  병합 후 분류도 반영하며 일반 LIVE의 발송·전달 누락 감지는 유지합니다.

## v3.5.2 - 2026-09-07

### 수정

- 알림 전송을 일반 텍스트 경로로 맞추고 shared-go v2.5.2로 URL·코드 원문을 보존합니다.
- fanout claim의 pending partial index 조건을 명시하고 generic/custom plan·잠금 행 회귀를 검증합니다.
- API와 alarm-worker artifact 버전을 3.2.1로 올리고 동일 source revision으로 빌드합니다.
- YouTube.js helper는 소켓 파일 생성과 listen 준비를 구분해 bootstrap 전송 전 기동 경합을 막습니다.

## v3.5.1 - 2026-09-04

### 수정

- Host-native AP 배포가 호출자의 restrictive umask와 무관하게 root 소유 YouTube.js helper와
  정적 데이터에 서비스 계정의 읽기·순회 권한을 부여합니다.

## v3.5.0 - 2026-09-04

### 추가

- YouTube delivery ledger lifecycle과 source-observation replay epoch 기반을 추가합니다.
  이번 릴리스는 epoch activation과 historical backfill을 실행하지 않습니다.
- 확인된 일반채팅의 YouTube-addressable 알림을 Karing content-list로 보내고 오픈채팅은 기존
  Markdown lane을 유지합니다.

### 수정

- Alarm-worker는 Iris `202 Accepted` 뒤 exact reply status의 `handoff_completed`를 확인한
  경우에만 Karing 성공으로 처리합니다. Egress path를 envelope당 한 번 결정해 split group 전체에
  고정하고 결과 불명확 상태는 재발송하지 않습니다.
- YouTube collector가 방송 예정 시각 metadata를 복구하고 lifecycle 전환을 한 owner에 결속합니다.
- `x/crypto`와 `fast-uri` 보안 권고를 해소하고 Node 24.20.0 production image와 npm dependency
  provenance를 갱신합니다.

### 변경

- migration 190·191이 delivery table constraint와 ledger/replay 기반을 추가합니다. Migration
  one-shot은 종료 코드 0을 직접 전파하며 central cutover는 build host와 runtime no-build 단계를
  분리합니다.
- Go toolchain을 `1.27.1`로 올리고 여섯 Go module 및 두 npm application dependency graph를
  갱신합니다. 모든 consumer는 `iris-client-go v2.4.2`, `shared-go v2.5.0`에 고정합니다.
- 전체 source release는 `3.5.0`, hololive-api·alarm-worker·collector·admin·migrator artifact는
  `3.2.0`입니다.

## v3.4.0 - 2026-08-29

### 추가

- celebration과 birthday-stream event identity에 stable `MemberID`를 포함하고, rollout 기간에는
  legacy payload가 member name·kind·channel·date·video와 일치할 때만 audience, payload와
  daily cap을 이관합니다. 같은 channel을 공유하는 다른 member는 legacy event를 재사용하지
  않습니다.
- YouTube collection lease에 bounded failure code/class/detail/timestamp를 추가하고 migration 189가
  legacy deferred row를 5,000행씩 reconcile합니다. `shutdown_release`는 failure로 기록하지 않으며
  constraint validation과 checksum reconciliation은 재실행에 안전합니다.
- admin dashboard public ingress를 dedicated nftables/systemd owner와 rendered nginx allowlist에
  결속합니다.

### 수정

- alarm dispatch event-hash collision은 collision ledger를 기록한 뒤 canonical winner delivery를
  계속 처리하며, claim release와 terminal error persistence가 ownership token과 cleanup context를
  보존합니다.
- YouTube collector, outbox, member cache, notification cache와 template renderer의 bounded
  cleanup·privacy key·fallback 경계를 정렬하고 invalid/ambiguous 상태를 success로 강등하지
  않습니다.
- bot ingress durable admission과 RSS/major-event parsing이 malformed row를 격리하면서 정상 row와
  이미 commit된 결과를 보존합니다.

### 운영

- PostgreSQL TLS/HBA와 route failover rollback, central/Seoul Compose, Osaka/Osaka2 host-native AP
  배포 계약을 정렬했습니다. AP deploy는 실제 rsync input manifest를 cutover 전에 검증하고
  실패 시 이전 route와 artifact를 복구합니다.
- CI ownership gate가 app workflow 전체를 module-specific SHA-256 snapshot으로 고정해 early
  exit, folded/flow YAML, custom shell, environment injection과 필수 gate 삭제를 거부합니다.
- GitHub Actions는 공식 `setup-python`과 고정된 `uv 0.12.7` 설치 경로를 사용하며,
  `shared-go v2.1.0`과 `iris-client-go v2.3.1`을 채택합니다.
- 이번 릴리스는 root app `3.4.0`, `hololive-api`·`youtube-collector`·`admin-dashboard` artifact
  `3.1.0`, `hololive-alarm-worker` artifact `3.1.0`입니다.

## v3.3.4 - 2026-08-26

### 변경

- Go 1.27 관용구 정렬과 병렬 게이트 안정화, 명령 파서 benchmark를 전 runtime 모듈의
  검증된 source snapshot에 포함했습니다.
- 모든 production Go 모듈의 `shared-go`·`iris-client-go` pin을 각각 `v2.0.4`·`v2.2.3`으로
  정렬하고 security workflow도 같은 exact commit을 사용하도록 갱신했습니다.
- 이번 릴리스는 root app `3.3.4`, `hololive-api`·`youtube-collector`·
  `admin-dashboard` artifact `3.0.12`, `hololive-alarm-worker` artifact `3.0.8`입니다.

## v3.3.3 - 2026-08-26

### 변경

- `admin-dashboard`, `hololive-alarm-worker`, `hololive-dbtest`, `hololive-shared`,
  `youtube-collector`의 production `shared-go` pin을 `v2.0.3`으로 정렬하고,
  security workflow의 `shared-go`·`iris-client-go` checkout도 각각 `v2.0.3`·`v2.2.2`
  exact commit으로 맞춰 로컬과 CI의 production dependency 경계를 일치시켰습니다.
- 이번 릴리스는 root app `3.3.3`, `hololive-api`·`youtube-collector`·
  `admin-dashboard` artifact `3.0.11`, `hololive-alarm-worker` artifact `3.0.7`입니다.

## v3.3.2 - 2026-08-26

### 수정

- YouTube.js가 알려진 transient network 오류와 안전한 Innertube read-only POST의
  `500`·`502`·`503`·`504`만 정확히 한 번 재시도합니다. `429`, 다른 endpoint·method,
  재생 불가능한 body, parser·protocol 오류와 두 번째 실패는 재시도하지 않습니다.
- shared H3 인증서 reloader와 `shared-go v2.0.3`, `iris-client-go v2.2.2`를 채택해 인증서
  재적재와 취소된 H3 resolve 경계를 최신 fail-closed 계약으로 정렬했습니다.

### 변경

- 이번 릴리스는 root app `3.3.2`, `hololive-api`와 `youtube-collector` artifact `3.0.10`이며
  변경되지 않은 alarm-worker version은 유지합니다.

## v3.3.1 - 2026-08-26

### 수정

- YouTube 커뮤니티 관측 창의 정렬이나 고정 글 변화가 기존 글 전체를 신규 글로 오인해 알림을
  폭주시키던 문제를 고쳤습니다. canonical post ID로 신규성을 판정하고 최초 관측은 기준선만
  생성합니다.

### 변경

- `hololive-api`의 production lifecycle 소유권을 Fx `v1.24.0`으로 옮겼습니다. 기존 build·start·
  shutdown 순서, fatal 우선순위, drain timeout과 정확히 한 번의 cleanup 계약은 유지합니다.
- Structure Gate v2를 legacy hard baseline 없이 적용했습니다. 이번 릴리스는 root app `3.3.1`,
  `hololive-api`와 `youtube-collector` artifact `3.0.9`이며 alarm worker version은 유지합니다.

## v3.3.0 - 2026-08-25

### 변경

- Go toolchain과 builder 기준을 `1.27.0`으로, `golangci-lint`를 `v2.13.1`로, `staticcheck`를
  `2026.2.1`로 올렸습니다. Go 1.27 `go fix` modernizer 재작성(`errors.AsType`, `sync/atomic` 타입,
  `slices.Backward`, 내장 필드 리터럴)을 적용했고, `apperrors.ServiceError`는 생성자·소비자와 같은
  포인터 리시버로 통일해 `%w` 포장이 Go 1.27 vet 검사를 통과합니다. request id·delivery lock
  토큰·admin 사용자 id 생성은 `github.com/google/uuid` 대신 Go 1.27 표준 `uuid`를 사용합니다.
- YouTube 최초공개 메타데이터를 source observation, reconciliation, outbox까지 보존하고 발송 시점의
  일정으로 남은 분을 다시 계산합니다. 예정된 최초공개는 `N분 후 공개 예정`, 시작했거나 일정이 없는
  최초공개는 `최초공개`, 일반 업로드는 `새 영상`으로 렌더링합니다. Karing 문자열과 기본 텍스트
  템플릿은 migration `188_youtube_premiere_notification_labels.sql`로 관리합니다.
- Holodex API 클라이언트가 주입된 표준 HTTP transport에도 HTTP/2 전용 정책을 적용하도록 고쳐,
  상류가 HTTP/2 SETTINGS frame을 보낼 때 HTTP/1.x malformed response로 오인하던 운영 장애를
  제거했습니다. 커스텀 RoundTripper 주입 계약은 변경하지 않았습니다.
- immutable epoch2 baseline의 checksum을 운영 ledger와 다시 일치시키고, 비동기 YouTube.js 페이지
  매퍼의 선언 타입을 실제 `paginate` 계약과 정렬했습니다.
- 호환 범위 의존성을 갱신했습니다. Go 직접 의존성은 `testify 1.12.1`, `gofeed 1.4.2`,
  `openai-go/v3 3.52.0`으로 올렸고, tidy 결과에는 MongoDB driver `2.8.1`, logrus `1.10.1`,
  gRPC `1.83.1`과 같은 transitive 갱신이 반영됩니다. 관리자 frontend는 TanStack Query
  `5.102.3`, React Virtual `3.14.10`, lucide-react `1.34.0`, Node types `26.3.0`, React DOM
  types `19.2.5`, Vite React plugin `6.1.0`, ESLint `10.9.1`, typescript-eslint `8.68.0`,
  Vite `8.2.2`로 올렸고 YouTube.js helper도 Node types `26.3.0`으로 정렬했습니다.
- 이번 릴리스는 root app `3.3.0`, `hololive-api` `3.0.8`, `hololive-alarm-worker` `3.0.6`으로
  올립니다. `youtube-collector` artifact version은 `hololive-api` VERSION을 따릅니다.

## v3.2.1 - 2026-08-21

### 수정

- `scripts/deploy/materialize-admin-dashboard-secrets.sh`에 실행 권한을 부여했습니다.
  v3.2.0에서 이 스크립트가 `100644`로 커밋되는 바람에, `systemd-compose-up.sh`가 직접
  실행하는 지점에서 `Permission denied`(exit 126)로 기동이 멈췄습니다. 중앙 호스트
  배포에서 재현했으며, 실행 중인 컨테이너는 영향을 받지 않고 기동만 실패합니다.

## v3.2.0 - 2026-08-21

### 변경

- 관리자 대시보드 세션을 안정적인 family 단위로 다시 묶었습니다. Valkey에 family lease를
  두고 회전을 family 인식·수렴형으로 바꿔, 동시 회전에서도 승자가 하나로 정해지고 이미
  업그레이드된 WebSocket까지 family 단위로 취소됩니다. 세션당 스트림 상한은 토큰 회전을
  건너도 유지됩니다.
- 로그인 실패 예산을 프로세스 로컬에서 Valkey 기반 분산 카운터로 옮기고 IP·계정·전역 세
  축으로 나눴습니다(15분 창, 각각 10·30·200회). Valkey 클라이언트는 `DisableCache`·
  `ForceSingleClient`로 고정해 카운터가 클라이언트 캐시나 다중 연결로 흩어지지 않습니다.
- 세션 서명 키와 CSRF 서명 키에 도메인 분리를 적용해, 한쪽 토큰을 다른 쪽 검증에 재사용할
  수 없게 했습니다.
- 운영 시크릿을 `*_FILE` 경로로만 읽도록 바꾸고 `SESSION_SECRET`에 32바이트 하한을
  강제합니다. 읽기 경로는 `Lstat` → `Open` → `os.SameFile` 대조로 TOCTOU를 막고,
  환경 변수 복원 실패를 삼키지 않고 오류로 전파해 시크릿이 남은 채로 기동하지 않습니다.
  Docker `Config.Env`에는 경로만 남습니다.
- CSP와 브라우저 권한 정책을 강화한 미들웨어를 필수 경로로 만들고, Docker 소켓 접근을
  최소 권한 프록시로 격리했습니다. 배포는 `deploy/compose/docker-compose.admin-security.yml`
  오버레이를 기동 시 필수로 요구하며, `scripts/deploy/materialize-admin-dashboard-secrets.sh`가
  `*_FILE` 시크릿을 안전한 권한으로 준비합니다.

## v3.1.0 - 2026-08-21

### 변경

- 적대적 감사에서 확정된 결함 6건을 수정했습니다.
  - 분류되지 않은 상류 오류가 fatal로 승격되어 수집기 프로세스 전체를 정지시키던 문제를
    고쳤습니다. `FromContext`의 catch-all이 만들어낸 typed 오류와 진짜 분류를 구분하는
    미분류 표식을 도입하고, fatal 승격에서 미분류를 제외합니다.
  - 템플릿 저장의 UPSERT와 이전 본문 revision 기록을 한 트랜잭션으로 원자화했습니다.
    이전에는 본문만 교체되고 revision이 남지 않아 이 API로는 롤백할 수단이 없었습니다.
    동시 저장 시 revision 순서가 뒤집히지 않도록 기록 시각을 잠금 획득 시점에 맞춥니다.
  - 알람 발송에서 만료된 attempt 컨텍스트로 상태를 기록하려다 실패해, 드레인된 행이
    `sending`으로 남아 재시도 대신 terminal quarantine으로 굳던 문제를 고쳤습니다.
    상태 기록과 실패 라우팅은 발송 attempt와 분리된 컨텍스트에서 완료됩니다.
  - 요청 ID의 봉투 수 상한이 재시도 허가 판정과 어긋나 같은 알람이 두 번 발화할 수 있던
    중복 구현을 제거했습니다.
  - Holodex 응답에서 row 하나가 어긋나면 정상 row 전체가 폐기되고 같은 파서를 공유하는
    live·viewer·channel_stats·photo·schedule 작업이 동시에 멈추던 문제를 고쳤습니다.
    같은 저장소 `officialcollector`와 동일하게 불량 row는 건너뛰고 전부 불량일 때만
    실패합니다.
  - lease 획득이 in-flight 완료 트랜잭션 뒤로 직렬화되던 잠금 순서를 교정했습니다.
    SQL 변경 없이 호출 순서만 바꿔, 획득 불가 상태에서 완료 커밋을 기다리지 않습니다.
- `shared-go`를 `v1.54.0`으로, `iris-client-go`를 `v2.1.3`으로 올렸습니다.

## v3.0.7 - 2026-08-21

### 변경

- YouTube plane의 `queue_observability.sql`이 `status IN (...)` 대신 `OR` 조건으로 `source_observation_queue`의
  partial index 경로(BitmapOr)를 타고, `observePendingQueue`의 DB 조회는 5초 간격으로 제한됩니다(utilization
  게이지는 매 호출 갱신). 운영 EXPLAIN에서 Seq Scan이 Bitmap Index Scan으로 바뀌는 것을 확인했습니다. (#402)
- youtubejs helper가 youtubei.js 18.0.0 `Text.fromAttributed`의 `length` 없는 attachment run 미매칭을
  상류 PR LuanRT/YouTube.js#1241과 같은 정규화 shim(`youtubei-attachment-run-fix.mjs`)으로 선적용해, 채널 페이지마다
  남던 `[YOUTUBEJS][Text]` 경고와 객체 덤프의 원인을 제거합니다. canary 테스트가 상류 수정이 반영되면 실패해
  shim 제거 시점을 알립니다. (#402)
- 이번 릴리스는 `hololive-api`(3.0.7)와 `youtube-collector` artifact를 재빌드합니다. `hololive-alarm-worker`(3.0.5)는
  변경이 없어 재빌드하지 않습니다. collector 이미지 version은 `hololive-api` VERSION을 따릅니다.

## v3.0.6 - 2026-08-21

### 변경

- 마이그레이션 `183_postgres_idle_transaction_timeout.sql`(manifest 순번 `044`)이 데이터베이스 기본값
  `idle_in_transaction_session_timeout = 5min`을 설정해, 오래 열린 idle transaction이 VACUUM horizon을
  붙잡아 `source_observation_queue`·`youtube_collection_job_leases`의 dead tuple 회수를 지연시키는 경로를
  막습니다. MVCC 운영 증적 문서를 함께 확장했습니다. (#376)
- 모든 Go 모듈의 `iris-client-go`를 `v2.1.2`로 갱신했습니다(내부 `randomhex` 도달 불가 분기 제거, 동작 동일).
- 이번 릴리스는 `hololive-api`(3.0.6)·`hololive-alarm-worker`(3.0.5)·`youtube-collector` artifact를 함께
  재빌드합니다. collector 이미지 version은 `hololive-api` VERSION을 따릅니다.

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

- 초기 배포 구성과 운영 문서를 기반으로 hololive-bot repository를
  초기화했습니다.
