# Hololive 폴더와 파일 경로 개편 심화 분석

이 문서는 경로 개편의 조사·실험 근거입니다. API 책임·성능 개편과 합친 [Hololive 통합 리팩토링 계획](2026-10-02-hololive-api-refactoring.md)이 작업 순서·범위·완료 기준을 단독으로 소유합니다. 아래 후보와 이동표를 독립 실행계획으로 사용하지 않습니다.

현재 모듈 위치를 유지하면서 작은 패키지 결합부터 제거하는 안을 권고한다. 일부 경로 이동은 임시 사본의 실제 빌드와 제한된 테스트로 타당성을 확인했다. 반면 observation 전체 이동, dispatcher의 claim/send 분리, Node 하위 폴더 도입은 기존 제안대로 기계적으로 진행하면 접근 경계나 검증 범위를 깨뜨린다.

이 문서는 **설계 제안과 조사 결과**이며 현재 운영 절차나 구현 완료 기록이 아니다. 실제 애플리케이션 소스는 변경하지 않았다. 기존 미커밋 작업을 포함한 2026-10-02 작업 트리와 HEAD `5c12d2c503915caf63f21aa86e1aeb346da94e63`을 조사했다. 아래 수치는 조사 시점의 값이다.

## 이전 제안에서 수정한 판단

| 이전 제안 | 이번에 확인한 근거 | 수정한 권고 |
|---|---|---|
| observation 소비 구현을 API `planes/youtube/internal`로 이동 | `cmd/source-observation-replay-epoch`도 Repository를 호출한다. 그 cmd는 plane 전용 internal에 접근할 수 없다. | API `internal/youtube/sourceobservation`에 두고 cmd와 plane이 함께 사용한다. |
| worker formatter를 새 패키지로 추출 | `youtubedispatch`의 함수는 이미 있는 shared `outbox/format`을 감싸는 전달 함수다. | 기존 formatter를 직접 호출한다. 새 패키지를 추가하지 않는다. 오류 wrapping은 보존한다. |
| collector DB factory만 분리하면 shared 결합이 크게 줄어듦 | 실제 그래프에서 기존 shared 도달 수는 51→48이다. `httpserver→holodex` 경로가 남는다. | DB factory와 공통 HTTP 서버의 기능 handler 결합을 각각 처리한다. 실행 성능 개선율로 해석하지 않는다. |
| collector SQL 13개를 사용처 기준으로 이동 | 실제 filename literal 소비는 8개이고, 5개는 현재 참조를 찾지 못한 보존 대상이다. | 실제 소비와 제안 목적지를 다른 열로 기록한다. 미확정 SQL을 삭제하지 않는다. |
| helper 파일과 테스트를 역할별 하위 폴더로 옮김 | 현재 test glob과 strict TypeScript 파일 목록이 하위 파일을 놓치는 상황을 재현했다. | 이동과 test discovery·strict 검사 범위를 한 변경으로 이행한다. |
| 모든 helper 이미지에서 nested test가 유입될 수 있음 | collector Docker context는 이미 재귀적으로 test 파일을 제외한다. worker와 native artifact의 flat prune은 별도다. | 경로별 실제 context 필터를 적용한다. collector 이미지 유입 결함으로 일반화하지 않는다. |
| 최상위 디렉터리를 바꾸면 모든 `../../` 계산 수정 필요 | `hololive/hololive-api`와 `apps/api`는 같은 깊이다. | 상대 깊이가 유지되는 참조는 보존하고, 정확한 module 경로·replace·선택 조건만 수정한다. |

## 실제 검증 방법과 한계

Go 1.27.1, Linux amd64의 kapu에서 설치된 toolchain binary를 직접 사용했다. `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOFLAGS=-mod=readonly`로 새 dependency 다운로드와 원본 module metadata 수정을 막았다.

패키지 그래프는 정규식 import 추정 대신 `go list -deps -e -json`으로 읽었다. `-e`가 성공 코드로 부분 결과를 반환할 수 있으므로 모든 `Error`와 `DepsErrors`가 비어 있는지도 검사했다.

| 프로필 | 로딩된 패키지 및 테스트 variant | 오류 패키지 |
|---|---:|---:|
| workspace 기본 tag + tests | 1,449 | 0 |
| workspace `integration` tag + tests | 1,449 | 0 |
| workspace Linux arm64, `CGO_ENABLED=0`, 제품 패키지 | 907 | 0 |
| API command 집합, `GOWORK=off`, Linux arm64 | 855 | 0 |
| worker command 집합, `GOWORK=off`, Linux arm64 | 681 | 0 |
| collector command 집합, `GOWORK=off`, Linux arm64 | 649 | 0 |

이 표는 **패키지 로딩 결과**다. ARM64 바이너리 실행이나 전체 테스트 통과를 뜻하지 않는다. 실제 컴파일·테스트 실험은 뒤의 후보별 표에 별도로 기록한다. DB 통합·전체 race·NilAway·최종 이미지 빌드·원격 배포는 이번 조사에서 실행하지 않았다.

재실행 입력과 결과는 [graph_results.json](evidence/2026-10-02-repository-layout/graph_results.json), [collect_layout.py](evidence/2026-10-02-repository-layout/collect_layout.py)에 있다. 이 스크립트는 일회성 조사 도구이며 CI gate에 연결하지 않는다.

```bash
GO_BIN="$(go env GOROOT)/bin/go"
python3 docs/design/evidence/2026-10-02-repository-layout/collect_layout.py \
  --root "$PWD" --go "$GO_BIN" --output /tmp/hololive-layout-evidence
```

## 현재 그래프와 이동 자산

기본 tag에서 원본 패키지 206개를 확인했다. 생산 Go 파일이 있는 패키지는 203개이고 테스트만 있는 패키지는 3개다. 합성 테스트 variant를 제외한 저장소 내부 패키지 간 edge는 제품 791개, same-package test 580개, external test 62개다.

테스트만 있는 `worker/.../youtubedispatch/claim`, API bot의 `internal/adapter`, `internal/bot`은 그 사실만으로 잘못된 패키지가 아니다. 구현 대상과 테스트의 관계를 보고 배치한다. 파일 수를 줄이기 위해 테스트를 삭제하지 않는다.

| shared 소비 기준 | 0개 서비스 | 1개 서비스 | 2개 서비스 | 3개 서비스 |
|---|---:|---:|---:|---:|
| 서비스 모듈 제품 패키지의 직접 import | 31 | 36 | 24 | 7 |
| shared 내부를 포함한 전이적 도달 | 9 | 17 | 34 | 38 |

| 시작점 | 도달하는 shared 패키지 |
|---|---:|
| API 모듈의 모든 제품 패키지 | 80 |
| API `cmd/hololive-api` 실행 진입점 | 79 |
| API replay command | 44 |
| worker 실행 진입점 | 68 |
| collector 실행 진입점 | 51 |
| 별도 PO broker 실행 진입점 | 0 |

모듈 전체와 실제 binary entrypoint의 도달 범위는 서로 다르다. 직접 import 1곳이라는 사실만으로 runtime 소유권이나 table writer를 결정하지 않는다.

기본 tag에서 선택되는 embed 자산은 **557개**다. 기존에 셌던 52개 `go:embed` 선언보다 실제 이동해야 할 파일 범위를 정확히 보여 준다. API 238개, worker 64개, shared 225개, collector 19개, 테스트 embed 11개를 포함한다. 상세는 [패키지 목록](evidence/2026-10-02-repository-layout/packages.tsv), [의존 edge](evidence/2026-10-02-repository-layout/package_edges.tsv), [shared 소비자](evidence/2026-10-02-repository-layout/shared_consumers.tsv), [embed 자산](evidence/2026-10-02-repository-layout/embedded_assets.tsv)에 있다.

`integration` tag는 admin dispatchops, htmlscraper, dispatchoutbox, batchrepo, scraper의 테스트 파일 7개를 추가한다. 폴더 이동 후 기본 tag만 확인하면 이 테스트들을 놓친다. [프로필별 파일 차이](evidence/2026-10-02-repository-layout/profile_file_deltas.tsv)를 검증 대상에 반영한다.

## API에서 실제로 검증한 이동

현재 API 소스·테스트·embed 자산을 임시 사본에 복사해 아래 세 후보를 순서대로 적용했다. 실제 원본은 그대로 두었다. 이동 목록은 [API 이동표](evidence/2026-10-02-repository-layout/api-move-map.tsv)에 보존한다.

| 후보 | 변경 내용 | 구조적 효과 |
|---|---|---|
| A1 | bot `internal/app/runtime/http_server.go` → bot `runtime/http_server_helpers.go`, 단일 소비자 수정 | 생산 파일 하나뿐인 패키지 제거, 8개 helper를 package-local 함수로 축소 |
| A2 | admin `app` 직접 파일 → `runtime`, `app/http` → `internal/httpapi` | 다른 plane과 구성 진입점 통일, HTTP 구현을 admin 내부로 제한 |
| A3 | bot `internal/bot/orchestration` → `internal/orchestration`, 부모의 테스트를 `orchcmd` 외부 테스트로 이동 | 반복 namespace 제거, 테스트 대상과 위치 일치 |

A1은 경로만 단축하는 작업보다 효과가 명확하다. [runtime_http_server.go](../../hololive/hololive-api/internal/planes/bot/runtime/runtime_http_server.go)는 내부 helper package의 유일한 제품 소비자다. 기존 nil 처리·종료 오류·로그 문맥을 유지한 채 동일 runtime package 안으로 합칠 수 있다.

A2의 HTTP package에 sibling 경로에서 import하는 부정 실험은 compiler의 `use of internal package ... not allowed`로 거부됐다. 내부 가시성 제약이 유지됨을 확인한 것이며 실패한 제품 빌드가 아니다.

A3는 이름 중복을 줄이지만 orchestration의 책임이나 fan-out을 줄이지 않는다. 이동 54개 Go 파일과 부모 테스트 1개, import 변경 43개 파일이 필요하므로 A1보다 우선순위를 낮춘다. `messaging`, `command/handlers`, LLM `service` 경로까지 한꺼번에 평탄화하지 않는다.

누적 A1+A2+A3 적용 후 **API 전체 제품 `go build -buildvcs=false ./...`가 통과**했다. 선택한 상위 시험 19개와 하위 사례를 포함한 45개 시험이 통과했으며 실패는 0개였다. 선택한 9개 Go package 중 ingress/lifecycle 두 package는 시험 실행 없이 컴파일됐다. 원본 API 소스 변경은 0개였다. 전체 API 테스트 실행이나 DB 통합·race 통과를 뜻하지 않는다.

임시 사본에는 Git metadata가 없으므로 전체 제품 build에는 실제 image build와 같은 `-buildvcs=false`를 사용한다. VCS stamping 실패는 제품 결함으로 분류하지 않는다. [API 실험 결과](evidence/2026-10-02-repository-layout/api-probe-results.json), [시험 원출력](evidence/2026-10-02-repository-layout/api-focused-tests.jsonl), [API 재현 스크립트](evidence/2026-10-02-repository-layout/reproduce_api.py)가 실제 실행 범위를 소유한다.

## worker와 collector에서 실제로 검증한 이동

### worker alarm dispatch

`internal/service/dispatchrun`의 제품 13개·테스트 25개·SQL 6개를 함께 `internal/egress/alarmdispatch`로 옮겼다. module 내부 import를 갱신하고 alarm formatter 호출을 기존 shared 구현으로 직접 연결했다.

[message_format.go](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/message_format.go)의 전달 함수는 실제 formatter owner가 아니다. 실제 구현은 `shared/pkg/service/youtube/outbox/format`이다. 직접 호출로 바꿀 때 기존 두 겹 오류 wrapping 중 전달 계층이 더하던 문맥도 보존하는 실험을 사용했다. 로그 오류 문구 변경은 별도 판단 사항이다.

이동한 alarmdispatch와 workerapp의 제품 build, alarmdispatch의 `go test -c`가 통과했다. 기존 DB 테스트는 실행하지 않았다. SQL 6개가 새 package의 EmbedFiles에 포함되는 것도 확인했다.

반면 claim과 send를 별도 package로 옮기는 안은 준비되지 않았다. [DeliveryExecutor](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/claim_manager.go)의 비공개 `dispatchDeliveryRows` 메서드는 다른 package에서 같은 이름으로 선언해도 구현할 수 없다. 실제 compiler도 `unexported method dispatchDeliveryRows`로 거부했다. 단순 export 확대보다 manager·engine의 상호 보관을 끊고 호출 흐름을 먼저 재설계해야 한다.

### collector pagination 해석

`collectutil.PaginationOf`는 공통 실행 계약이 아니라 YouTube.js RPC 표현을 해석한다. 이 함수와 기존 impossible-tuple 회귀를 `youtubejscollector/pagination.go`, `pagination_test.go`로 옮기고 channel/content/community의 세 caller를 수정했다.

두 package의 제품 build와 테스트 binary compile이 통과했고, 이동한 `TestPaginationOfRejectsImpossibleTupleAsProtocolFault`도 통과했다. `collectutil`에서 `youtubejs` 직접 import가 사라졌다. 그러나 shared observation과 joblease 의존은 여전히 남으므로 공통 실행 계약의 분리가 끝났다고 판단하지 않는다.

따라서 `collectutil`을 곧바로 일반적인 `types` 폴더로 이름만 바꾸기보다 이 transport 의존을 먼저 제거하고, app이 provider와 collection 계약을 조립하는 방향으로 진행한다.

### helper 디렉터리와 검사 범위

현재 평면 배치가 이미 실패하고 있다는 주장은 아니다. 제안했던 하위 디렉터리 이동이 만드는 회귀를 현재 설정으로 재현한 결과다.

| 실험 | 실제 결과 | 개편 시 요구 |
|---|---|---|
| 현재 npm test 명령 아래 top-level 성공 시험과 nested 실패 시험 배치 | 1개만 실행, 실패 시험 누락, exit 0 | 모든 이동된 테스트의 발견 범위 갱신 |
| 같은 자료를 recursive Node glob으로 실행 | 2개 실행, 실패 1개, exit 1 | 기존 순차 실행 설정 유지 |
| 실제 tsconfig/jsconfig 사본에 implicit-any 함수 추가 | 기존 strict 경로에서는 TS7006, exit 1 | 파일별 strict 계약 유지 |
| 그 함수를 `src/runtime/server.mjs`로 이동 | strict·relaxed 검사 모두 exit 0 | 새 경로를 strict 목록과 relaxed 제외 목록에 함께 반영 |
| 실제 helper source를 `helpers/youtubejs`로 이동 | 옛 Go helper·protocol fixture 상대 경로 두 개가 없어짐 | 테스트의 source/fixture 경로도 갱신 |
| 이동한 실제 `server.mjs`에 `node --check` | 통과 | syntax 확인일 뿐 RPC runtime 검증으로 해석하지 않음 |

collector Dockerfile의 context는 `**/*.test.mjs`를 이미 제외한다. 따라서 flat `rm`만 보고 collector 이미지에 테스트가 유입된다고 판정하면 안 된다. worker의 Docker context와 AP native artifact의 flat prune은 별도 보완이 필요하다. PO sandbox context도 좁은 allowlist이므로 helper source 경로를 바꾸면 whitelist를 함께 바꾼다.

저장소 source를 `helpers/*`로 옮겨도 `/app/youtubejs`, `/app/xspaces`, `/app/po-sandbox`와 native artifact의 `youtubejs` 설치 경로는 유지할 수 있다. source operand·test path·npm audit/cache 경로만 바꾸고 설치 경로까지 변경하는 별도 작업으로 확대하지 않는다.

원문 결과와 재실행 코드는 [worker·collector 실험 결과](evidence/2026-10-02-repository-layout/runtime-probe-results.txt), [재현 스크립트](evidence/2026-10-02-repository-layout/reproduce_runtime.py)에 있다. toy 검증은 일회성 회귀 재현이며 저장소 test suite나 CI에 추가하지 않았다.

## shared는 함수와 소비자 단위로 분해한다

### DB 구성과 HTTP 기능의 중복 의존 경로

DB factory의 최소 추출 대상은 `DatabaseResources`, `ProvideDatabaseResources`, `infra_providers_database_test.go`다. 기존 `*settings.PostgresConfig` 입력·DB service 결과·cleanup 반환을 그대로 유지하려면 `pkg/providers/database`가 자연스럽다. leaf `pkg/service/database`에 startup settings 의존을 역으로 추가하는 안보다 명확하다. 호출자가 설정을 직접 DB config로 변환하도록 하는 대안은 구성 코드 중복 비용을 별도로 비교해야 한다.

HTTP 공통 패키지의 StreamHandler·OAuth·WebSocket 기능은 admin 내부로, bot과 LLM이 함께 소비하는 TriggerHandler는 API 모듈 공통 internal로 옮길 수 있다. 공통 H3·health·ready·metrics·pprof는 실제 간접 소비를 따라 shared에 남긴다.

전체 파일 이동만으로는 다음 두 연결을 놓친다.

- `runtime_helpers.go`의 `NewTriggerRuntimeRouter`, `triggerRuntimeRouteRegistrar`도 `TriggerHandler`를 참조하므로 함께 API로 옮긴다.
- `response.go`의 private `respondJSON`은 shared health·ready도 사용한다. 응답 파일 전체를 API로 옮기지 않는다. 기존 공통 JSON 응답 primitive를 재사용한다.

`runtime_helpers_factories_test.go`는 health와 trigger 시험이 혼재하므로 함수 단위로 나눈다. 기능별 정확한 selector 소비자는 [HTTP symbol 소비자 목록](evidence/2026-10-02-repository-layout/httpserver-symbol-consumers.json)에 있다.

| 기존 그래프의 edge를 제거한 가설 | collector가 도달하는 기존 shared 패키지 |
|---|---:|
| 현재 | 51 |
| 포괄 providers 직접 의존만 제거 | 48 |
| HTTP 기능 의존만 제거 | 50 |
| 두 조건을 함께 적용 | 36 |

이 계산은 새로운 package를 포함하지 않는 **그래프 반사실 분석**이다. 실제 패치 컴파일이나 binary·메모리·빌드 시간 측정이 아니다. 두 경로가 같은 Holodex 하위 그래프를 유지하므로 단독 차이를 더해서 효과를 계산하지 않는다. [단일 edge 제거 근거](evidence/2026-10-02-repository-layout/collector_provider_cut.json), [동시 분리 가설](evidence/2026-10-02-repository-layout/shared-closure-hypotheses.json)

### alarmservice와 private cache

alarmservice 제품 18개·테스트 14개와 private alarmcache 제품 2개·테스트 1개를 한 소유권 변경으로 묶는다. 예시 목적지는 worker `internal/service/alarm/subscriptions`와 그 `internal/alarmcache`다. 이미 검증된 dispatchrun→egress 이동과 독립적으로 진행할 수 있다.

API `handler_alarm_test.go`의 InvalidAction 시험은 실제 `AlarmService{}`를 주입하지만 서비스 메서드를 호출하지 않는다. 같은 파일의 `alarmListViewerStub`을 사용할 수 있다. worker 내부 구현을 API에 공개하거나 테스트를 삭제할 필요가 없다. worker의 build_runtime와 scheduler 시험도 새 import를 사용하도록 함께 바꾼다.

### observation 파일과 혼합 테스트

기존 [API 심화 분석의 이동표](2026-10-02-hololive-api-deep-analysis.md#observation-이관의-파일과-테스트-배치를-확정합니다)를 재사용하되 이번 대조로 확인한 구분을 보강한다. 새 문서가 경쟁하는 두 번째 일괄 이동 명세가 되지 않도록 한다.

| 제품 Go 46개 분류 | 개수 | 이행 방식 |
|---|---:|---|
| publisher/job 계약 구현 | 8 | collector 내부로 이동 |
| consume/canonical/replay/retention 구현 | 34 | API `internal/youtube/sourceobservation`으로 이동 |
| `repository.go`, `repository_roles.go`, `types.go`, `sql.go` | 4 | receiver·필드·선언·embed를 양쪽으로 분해 |

API `cmd/source-observation-replay-epoch/main.go`가 동일 repository를 사용하므로 plane 전용 internal은 목적지로 부적합하다. shared private community 및 여섯 reconcile package도 함께 API로 옮겨야 한다. 이 7개 package의 제품 23개·테스트 21개가 추가 이행 범위다.

| SQL 85개 실제 소비 분류 | 개수 | 주의 |
|---|---:|---|
| collector 제품 literal 소비 | 8 | publisher 쿼리 |
| API 제품 literal 소비 | 71 | claim·canonical·finalize·replay·retention |
| API test-only | 1 | nullable title 시험의 live session 조회 |
| 현재 literal 소비 미확정 | 5 | 기존 표는 publisher 측 보존 대상으로 배치했음 |

미확정 5개는 identity advisory lock, collision insert, current contract, queue insert, checkpoint upsert query다. 이름만 보고 삭제하거나 소비가 증명된 쿼리로 세지 않는다. [전수 대조](evidence/2026-10-02-repository-layout/observation-move-audit.json)에 source 집합과 참조가 있다.

기존 46개 테스트 파일은 정적 검토상 **교차 시험 32개, collector 7개, API 5개, helper-only 미확정 2개**로 분류했다. 이 숫자는 이관 후 테스트 compile 증명이 아니다. `claim_backlog_plan_test.go`, `shorts_claim_plan_test.go`는 publisher로 seed하지만 API 소유 claim SQL을 검사하므로 collector-only로 이동하면 안 된다. `consts_test.go`, `content_consumer_helpers_test.go`는 선언 단위로 나눠야 한다. [테스트 분류 근거](evidence/2026-10-02-repository-layout/observation-test-categories.json)

양쪽 실제 구현을 호출하는 회귀는 기존 설계의 좁은 무태그 testkit 또는 동등한 방식으로 보존한다. 테스트를 build tag 뒤로 숨기거나 fake publisher로 바꿔 DB 동시성 검증을 잃어서는 안 된다. publisher의 fence·projection·contract 검증과 observation/checkpoint·complete/defer 기록, consumer의 canonical callback·receipt·offset 확정은 각각 기존 transaction 안에 남긴다.

## 경로 consumer를 수정 목록과 구분한다

2,978개 안전한 현존 파일을 대상으로 물리 경로·module identity·상대 깊이·package 자산·정확한 cmd 이름을 분류했다. 최상위 물리 이동의 lexical 검토 집합은 82파일·1,099행, 내부 후보의 검토 집합은 80파일·232행이다. 두 집합은 겹치며, 모든 행을 변경하라는 목록이 아니다. 역사 문서와 검토용 인용을 현재 소비자로 합산하지 않는다.

직접 검토한 계약 29개는 [경로 계약표](evidence/2026-10-02-repository-layout/reviewed-path-contracts.tsv)에 보존한다. [최상위 검토 파일](evidence/2026-10-02-repository-layout/outer-review-files.tsv), [내부 후보 검토 파일](evidence/2026-10-02-repository-layout/inner-review-files.tsv), [활성 경로 참조 자료](evidence/2026-10-02-repository-layout/consumer-references.tsv)는 재검토 자료다. 보존한 참조 TSV는 실행 코드·test/build·current 문서의 5,596행으로 한정했다. 과거 절차와 프롬프트 원문을 새 증거에 다시 복제하지 않았으며 전체 검색의 분류별 수치는 [검색 요약](evidence/2026-10-02-repository-layout/path-consumer-summary.json)에 남겼다.

| 경로 종류 | 실제 처리 |
|---|---|
| `go.work`에서 읽는 generic module loop | 기존 discovery 재사용. 새 registry 불필요 |
| 각 go.mod `module` | 물리 이동만 할 때 유지 |
| 각 go.mod sibling `replace` | 새 디스크 상대 위치로 변경 |
| module Makefile의 `../../.golangci.yml` | 두 단계 깊이를 유지하면 그대로 사용 |
| collector cmd 디렉터리 | Go build target뿐 아니라 artifact manifest와 verifier의 package identity도 동시 변경 |
| Dockerfile.dockerignore whitelist | COPY source가 바뀌면 허용 경로도 동시 변경 |
| AP rsync manifest | Go와 EmbedFiles, helper source가 누락되지 않도록 이행 |
| exact CI module 허용 목록·test allowlist | module·package 이동에 따라 해당 항목 갱신 |
| `.golangci.yml`의 collecterr import 예외 | collecterr package의 import path가 바뀔 때만 부모 `tools/lint/golangci.template.yml`에서 반영. 물리 모듈·cmd 경로만 옮기면 유지 |
| integration/race별 package 목록 | 이동 후에도 같은 제품 검증 범위를 유지 |
| npm cache/audit/typecheck/test 선택 | helper source 이동과 함께 갱신 |

collector production build는 `GOWORK=off`, `CGO_ENABLED=0`, `-pgo=off`, 빈 tag 집합을 사용한다. 경로 개편 때문에 실험 tag나 다른 codec으로 바꾸지 않는다. 바이너리 이름, 버전·revision 전달, build-id 형식은 유지하되 `go version -m`에서 확인하는 main package suffix는 cmd 이동에 맞춰 바꿔야 한다.

## 초기 목표와 후순위 선택

초기에는 `hololive/hololive-*`, `go.work`, runtime binary·service·port, `deploy/compose`, runtime-config, migration 위치를 유지한다. 검증된 작은 경계부터 다음처럼 정리한다.

```text
hololive/
  hololive-api/internal/
    planes/
      admin/{runtime,internal/httpapi}/
      bot/{runtime,internal/orchestration}/
      llm/{runtime,internal}/
      youtube/{runtime,targetprojection}/
    youtube/sourceobservation/       # observation 분해 후 cmd·plane 공동 사용
  hololive-alarm-worker/internal/
    egress/{alarmdispatch,youtubedispatch}/
    service/alarm/subscriptions/      # alarmservice와 private cache 이관 후보
  hololive-youtube-collector/
    cmd/{youtube-collector,healthcheck,po-broker}/
    internal/runtime/
      collectutil/                   # RPC 변환 제거부터 수행
      youtubejscollector/
      sourceobservation/             # observation publisher 이관 후보
  hololive-shared/pkg/
    contracts/sourceobservation/
    providers/database/              # DB 생성 책임만 분리
    server/httpserver/               # 공통 수명·health·ready 등 유지
```

이 트리는 모든 후보가 구현됐다는 뜻이 아니다. collector의 app/collection/providers 재배치는 조립 의존을 정리한 뒤 선택한다. 같은 구현을 observation 이관과 폴더 평탄화라는 이유로 연속해서 옮기는 비용도 고려한다.

최상위 `apps/{api,alarm-worker,youtube-collector}`, `libs/hololive`, `tests/dbtest`는 후순위 대안이다. 현재 [tree policy](../current/architecture/repo-tree-policy.md)의 모듈 위치 안정성 규칙을 개정하고 파일 이동만의 별도 변경으로 수행해야 한다. Go module 이름까지 동시에 바꾸지 않는다. 단일 go.mod 통합은 의존성 소유권·검증 비용을 재평가한 뒤 별도로 판단한다.

`SERVICE_OWNERSHIP.md`의 구체 retained 목록도 이행 시 정정해야 한다. format·queue·dispatchoutbox의 현재 제품 소비는 worker뿐이지만 deliverysql·timeline·checker는 API와 worker가 함께 쓴다. 현재 없는 `service/scraper`, `youtube/batchrepo`, outbox store·dispatchstate 경로를 이동의 근거로 사용하지 않는다. 현재 import와 별도로 DB writer·reader 계약은 계속 보존한다.

## 실행 순서와 완료 조건

기존 경로 개편 순서는 [통합 계획의 실행 순서와 완료 기준](2026-10-02-hololive-api-refactoring.md#실행-순서와-완료-기준)에 합쳤습니다. 설정·취소·종료 결함 수정, API 구성 및 shared 회수, 성능 변경과의 실제 의존성을 그곳에서 관리합니다. 이 문서에는 별도 순서나 체크리스트를 유지하지 않습니다.

A1/A2는 수명주기·구성 변경의 최종 경로와 조정하고 A3·Node·최상위 물리 이동은 조건부입니다. formatter 직접 호출과 기존 formatter 구현의 worker 이관은 서로 모순되지 않으며 새 formatter 구현을 추가하지 않습니다. 기존 Stage 3·NilAway·해당 race·DB 통합 검증과 승인 경계는 통합 계획에서도 유지합니다.

이번 실험은 작은 이동의 기술적 가능성과 잘못된 분할의 실패 조건을 확인했다. 전체 기능 회귀, 운영 rollout 가능성, 빌드 시간·바이너리 크기·메모리 절감은 증명하지 않았다. 실제 소스 이동·의존성 업그레이드·DB 변경·runtime 변경은 수행하지 않았다.
