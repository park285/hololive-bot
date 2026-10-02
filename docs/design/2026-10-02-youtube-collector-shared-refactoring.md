# YouTube 컬렉터와 공유 코드 분석 및 리팩토링안

YouTube 컬렉터의 구조, 성능, 컨벤션, 중복 구현과 불필요한 추상화를 분석하고 공유 코드의 개선 및 이관 방향을 제안합니다. 우선순위는 **upstream 오류 분류의 계약 복원 → 재시도 표현과 테스트 guard 정리 → 불필요한 복사와 중복 구현 정리 → 공유 코드의 소유권 분리**입니다.

가장 먼저 해결할 대상은 helper가 인식하지 못한 upstream 오류를 모두 내부 불변식 위반(`INTERNAL`)으로 선언하는 기본 분기입니다. 이 분기 때문에 탭이 없는 채널, 응답 본문 연결 오류, YouTube의 400·403·404 응답처럼 일반적인 provider 실패가 컬렉터 전체 중단 경로로 연결됩니다. 이는 [youtube-collector 서비스 문서](../current/services/youtube-collector.md)의 "Ordinary provider failure … fatal이 아닙니다" 계약과 충돌하므로, 수정은 새 정책이 아니라 계약 복원입니다.

이 문서는 2026-10-02 코드 분석과 같은 날의 적대적 리뷰 결과를 문서화한 설계와 실행 기록입니다. 아래 분석은 수정 전 상태를 설명하며, 사용자 요청에 따른 구현·검증 진행은 **구현 진행 기록** 절에 구분합니다. 현재 런타임 소유권은 [PROJECT_MAP](../current/PROJECT_MAP.md), [SERVICE_OWNERSHIP](../current/SERVICE_OWNERSHIP.md), [CONTRACT_MAP](../current/CONTRACT_MAP.md)을 따릅니다. `sourceobservation`·설정 loader·`providers` 이관의 실행 계획은 [알람 워커와 API 공유 모듈 리팩토링안](2026-10-02-alarm-worker-api-shared-refactoring.md)이 소유하며, 이 문서는 해당 이관에서 collector가 보존해야 할 조건만 기록합니다.

## 구현 진행 기록

2026-10-02 사용자 요청으로 독립 구현 범위(1·2·4단계와 helper 수명주기)를 완료했습니다. `executing-plans`, `modern-go-guidelines:use-modern-go`, `modern-javascript-typescript`를 적용했으며, 추가 요청에 따라 병렬 구현·검토 작업자는 `gpt-6-astra`를 사용했습니다. 조건부 작업과 다른 계획 소유의 이관은 아래에 별도로 남깁니다.

| 작업 | 진행 상태 |
|---|---|
| upstream 오류 분류·탭 판정·본문 오류 | 원문 증거로 탭 부재를 판정하고 정상 조회 호환성 복원. 후속 보완을 포함한 helper 358개 테스트·타입 검사와 독립 재현 152개 통과 |
| helper bootstrap/drain 수명주기 | 종료 의도와 늦게 생성된 자원 정리 구현 완료. 전용 테스트 19개 통과 |
| 재시도 표현 | DELAY·RetryDecision 및 SQL 분기 제거. AT 저장 스케줄 직접 전달, UTC 정밀도·DB min/max·complete/defer rollback 테스트 통과 |
| HTTP 본문 처리·오류 wrapper | shared-go 기존 API 재사용, 입력/출력 오류 식별 보존, 정확한 읽기·drain 상한과 마지막 읽기 중 취소 회귀 검증 완료 |
| 불변 결과·metrics·중복 검증 | 생성 시 소유권 확보 후 불변 결과 공유, 검증·metrics의 payload 복사 제거. 외부 getter snapshot·저장소 검증 유지. 소유권·metrics 회귀와 할당량 비교 완료 |
| 미사용 설정·문자열 guard·주석 | OrDefault/MaxProviderTimeout 전용 경로와 문자열 경계 테스트 삭제, 채널 주석 수정. 설정 및 youtubejscollector 테스트 통과 |
| 운영 근거 | a/b/c/d의 아래 24시간 구간에서 해당 fatal 0건. YouTube Retry-After 헤더 송신 근거 미확인 |

결정과 범위는 다음과 같습니다.

- 일반 upstream 실패에는 기존 `collection_failed/TRANSIENT` 계약을 사용합니다. 취소, 명시적 내부 불변식, 응답 계약 위반의 구분은 유지합니다.
- 앱은 upstream 힌트로 요청 시각을 계산하고 DB는 실제 저장 시각의 지연 범위를 보장합니다. 두 경계의 clamp는 시계·transaction 지연 차이에 대한 기존 동작을 보존하며, 이번 정리에서 정책을 변경하지 않습니다.
- Retry-After 전달과 Holodex 요청 병합·큰 queue 최적화는 문서에 명시한 운영/측정 근거가 확보된 경우에만 진행합니다.
- publish/consume·설정 loader·providers 이관은 연관 문서의 소유 범위이며 여기서 별도로 실행하지 않습니다. 이관에 필요한 collector 보존 조건은 위 문서와 이 문서의 소유권 절을 따릅니다.
- 배포, 운영 상태/데이터 변경과 Git 게시는 이 작업에 포함하지 않습니다.
- 이 구현은 2026-10-02 [알람 워커와 API 공유 모듈 리팩토링안](2026-10-02-alarm-worker-api-shared-refactoring.md)의 branch로 인계해 함께 커밋·게시합니다. 5단계 중 publish/consume 분리와 `providers` DB 초기화 분리는 그 계획이 실행했습니다(`sourceobservation`·`sourceobservation/consume`, `providers/dbresource`). 설정 loader 이동은 `internal/load` 전체 공개가 필요해 하지 않았습니다. 이관 뒤 collector가 링크하는 shared 패키지는 51개에서 36개로 줄었습니다.

운영 확인 구간은 `2026-10-01T01:56:59Z ≤ time < 2026-10-02T01:56:59Z`(KST 10/1 10:56:59~10/2 10:56:59)입니다. 4개 collector의 최신 동기화 로그가 원격 원본 파일의 크기·수정 시각과 일치함을 확인하고, native journal(a/d)과 Docker logs(b/c)를 같은 구간으로 조회했습니다. `helper_internal_invariant`, `youtube collector scheduler fatal`, upstream 429 및 Retry-After 언급은 각 0건입니다. 이는 해당 로그 구간의 관찰이며 일반적인 발생 부재를 증명하지 않습니다. 활성 파일의 과거 `retry_after` 언급은 Holodex 오류 detail의 JSON 필드였으며 YouTube 응답 헤더 근거가 아닙니다. 따라서 Retry-After 전달의 착수 조건은 충족하지 않았습니다. 운영 변경·DB 조회·새 YouTube 요청은 수행하지 않았습니다.

### 할당량 비교

동일 benchmark를 구현 전후 kapu에서 실행했습니다. `BenchmarkCollectResult`는 생성→complete 결과→Output→publish용 방어적 getter까지 측정하며 DB 저장소 snapshot은 포함하지 않습니다. `BenchmarkObservePublished`는 metrics label을 준비한 뒤 성공 집계 호출을 측정합니다. 단일 envelope 기준이며 운영 처리량이나 지연 개선율을 뜻하지 않습니다.

| 경로 | payload | 이전 B/op → 이후 B/op | 이전 allocs/op → 이후 allocs/op |
|---|---:|---:|---:|
| complete 결과 생성·조회 | 1 KiB | 6,272 → 3,136 | 16 → 8 |
| complete 결과 생성·조회 | 64 KiB | 264,322 → 132,161 | 16 → 8 |
| complete 결과 생성·조회 | 1 MiB | 4,196,499 → 2,098,266 | 16 → 8 |
| partial 결과 생성·조회 | 1 MiB | 4,196,788 → 2,098,540 | 20 → 12 |
| 성공 metrics | 1 KiB | 1,376 → 0 | 2 → 0 |
| 성공 metrics | 64 KiB | 65,888 → 0 | 2 → 0 |
| 성공 metrics | 1 MiB | 1,048,930 → 0 | 2 → 0 |

Fallback delta: 새 fallback·추가 retry 경로는 없습니다. 일반 provider 실패의 분류를 기존 typed 실패/defer 계약으로 복원하며 물리 요청 횟수, 지연 상한, lease/fence와 원자적 publish 정책은 유지합니다. 저장소의 입력 snapshot과 외부로 반환하는 payload/cursor의 방어적 복사는 남겨 aliasing을 막습니다.

### 1차 구현 후 검증

모든 빌드·테스트는 kapu에서 실행했습니다. DB 테스트는 `TEST_DATABASE_URL`, `TEST_DATABASE_OWNER_TOKEN`, `ALLOW_EXTERNAL_TEST_DB`를 제거해 테스트 전용 provision을 사용했습니다.

| 검증 | 실제 결과 |
|---|---|
| collector 전체, shared 관측 계약·저장소·collector 설정, API YouTube 전체 `go test -race -p 2 -count=1` | 통과 |
| 후속 보완한 마지막 본문 읽기 중 취소 및 결과 소유권·검증·metrics 회귀 `-race` | 통과 |
| `npm --prefix hololive/hololive-youtube-collector/youtubejs test` | 260개 통과, 실패·skip 없음 |
| 같은 helper의 `npm run typecheck` | tsconfig.json·jsconfig.json 모두 통과 |
| `scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector test-prod` | 최종 코드의 GOWORK=off·CGO_ENABLED=0 테스트 및 JSON 결과 검사 통과 |
| 같은 gate의 `build-prod` | 최종 코드의 linux/amd64/v1 production Go build·artifact 검사 통과 |
| collector 전체 및 수정한 shared 두 패키지 golangci-lint | 최종 0 issues |
| 같은 Go 범위의 pinned NilAway·staticcheck | 모두 통과 |
| `scripts/architecture/ci-boundary-gate.sh` | 통과 |
| stack `check-stack-retry-contract.sh`, `check-stack-db-access-policy.sh` | 모두 통과 |
| 1차 diff 및 독립 리뷰 | diff-check 통과. 실제 getter·Text 오분류를 발견해 수정하고 독립 재현으로 확인. 이후 적대적 리뷰에서 발견한 탭 파싱 실패 P2는 아래 후속 수정 기록을 따릅니다. |

초기 lint·NilAway 지적은 테스트의 길이 확인·변이 검증·구조·서식을 수정해 해결했습니다. 검사 설정 변경이나 새 억제는 없습니다. Docker image build, 배포, 운영 트래픽 부하 측정은 수행하지 않았습니다. Retry-After(3단계)는 헤더 송신 근거 미확인, discovery/Holodex 최적화(6단계)는 큰 queue·요청 중첩 측정 근거 미확인으로 조건부 후속입니다. publish/consume·설정·providers 이관(5단계)은 연관 계획에서 수행할 범위입니다.

### 적대적 리뷰 P2 후속 수정

후속 리뷰에서 실제 YouTube.js가 탭 파싱 예외를 삼키고 탭을 제거하면 `has_* === false`가 되어 빈 성공으로 변하는 회귀를 확인했습니다. 로컬 HTTP 응답의 `apiUrl: 17`을 실제 `Innertube → Actions → Parser → Channel`에 전달했을 때 HTTP 200과 `missing_tab: true`가 반환됐습니다. Go는 해당 관측을 생략하고 수집을 정상 완료할 수 있으므로 실패 진단과 defer를 잃습니다.

사용자 수정 요청에 따라 라이브러리의 파서 오류 콜백을 한 번 설치하고 `AsyncLocalStorage`로 `runUpstream`·`readUpstream` 호출별 실패를 보존했습니다. 콜백에서 즉시 예외를 던지면 라이브러리 memo 정리를 건너뛰므로, 파싱과 정리가 끝난 뒤 `parser_drift/DATA_CONTRACT`로 거부합니다. JIT 생성·갱신 성공 진단은 실패로 바꾸지 않고, 원시 노드 데이터를 저장하거나 로그로 출력하지 않습니다. 명시적 INTERNAL/PROTOCOL/CANCELED와 부모 취소는 기존 구분을 유지합니다.

이 단계에서는 최초 채널과 탭 로더가 반환한 채널의 `contents_memo`에서 Tab 목록과 각 URL을 검사했습니다. 유효한 featured-only 채널의 특정 탭 부재와 존재하는 탭의 빈 콘텐츠를 정상 성공으로 유지하고, 별도 형태인 continuation Feed에는 탭 목록을 요구하지 않았습니다. 후속 리뷰에서 이 검사가 파싱 중 조용히 유실된 원문 항목을 판별하지 못하고, 무관한 탭의 선택적 URL 누락까지 거부한다는 두 결함을 확인했습니다. 이 검사는 아래 원문 증거 기반 수정으로 대체합니다.

Go의 새 `TestContentTabRPCDistinguishesMissingTabsFromParserDrift`는 HTTP 응답부터 실제 RPC·runner·scheduler·테스트 DB까지 연결합니다. 정상 부재는 성공 완료, videos/shorts의 parser drift는 관측 0건·DEFERRED·typed 실패 기록·fatal 0건임을 검증했습니다. 이 테스트는 `-race`와 `GOWORK=off CGO_ENABLED=0` 모두 통과했습니다. 기존 content 테스트, 해당 Go 패키지의 lint·NilAway·staticcheck도 통과했습니다.

이 단계의 검증 결과:

- 실제 라이브러리의 parser·Tab·중첩 행·continuation·JIT 진단·취소·메모 정리·동시성 회귀 38개 추가, helper 전체 **298개 통과**, typecheck 통과.
- 독립 Astra 리뷰에서 네 탭의 손상 응답 **80개**(최초 채널/탭 로더), 정상 응답 **12개**, 교차 실행 **30개**를 재현하여 오류 격리와 기존 요청 횟수를 확인했습니다. 정상 continuation 및 손상 continuation, 명시적 fatal·취소, 원문 비노출, 메모 정리도 확인했습니다. 이 검증에서는 아래 후속 리뷰의 두 결함을 발견하지 못했습니다.
- 최종 코드에서 `youtubejs`, `youtubejscollector`, `collectorruntime` 세 Go 패키지 전체를 외부 DB 환경변수 없이 `go test -race -p 2 -count=1`로 실행해 통과했습니다.
- 최초 `apiUrl: 17` 재현을 다시 실행하여 현재 응답이 HTTP 422 `parser_drift/DATA_CONTRACT`임을 확인했습니다. diff 검사도 통과했습니다.

이 검증은 kapu의 로컬 합성 HTTP 응답과 테스트 DB를 사용했습니다. 실제 YouTube 호출·배포·Git 게시는 수행하지 않았습니다. Fallback delta: 없음. 추가 요청·자동 재시도·새 의존성 없이 잘못된 성공을 기존 typed 실패/defer 경로로 복원했습니다.

### 원문 탭 증거와 정상 조회 호환성 보완

다음 리뷰에서 두 결함을 실제 `Innertube → Actions → HTTPClient → Parser`로 재현했습니다. 첫째, `[정상 featured 탭, null]` 또는 `[정상 featured 탭, {}]`는 파서 진단 없이 불완전한 항목이 제거되어 요청 탭 부재로 잘못 성공했습니다. 둘째, 정상 요청 탭과 본문이 있어도 무관한 featured 탭의 선택적 URL이 없으면 첫 번째 검사에서 실패했습니다. URL의 query·후행 slash 때문에 존재하는 탭을 찾지 못하는 경우도 첫 번째 부재 판정의 문제에 포함됩니다.

수집 전용 어댑터가 SDK endpoint와 Actions 호출을 그대로 사용하면서 원문 응답을 해당 채널 객체의 수명 동안 보존합니다. 부재 판정은 손실된 parser memo 대신 원문 전체 목록을 근거로 하며, 요청 탭을 찾은 성공 경로와 부재 확정 경로의 검증을 분리합니다. 정상 요청 탭이 있으면 무관한 탭의 선택적 URL 누락을 허용합니다. 반환된 요청 탭의 선택 상태·본문·채널 식별자를 검사한 뒤 해당 본문만 파싱하여 다른 탭·header·sidebar의 영상이나 continuation이 수집 결과에 섞이지 않게 합니다. 투영 전 원본 전체의 parser 진단도 보존합니다. Metadata 작업은 기존 전체 채널과 about 경로를 유지합니다.

SDK가 수행하던 초기 browse 이동은 체인 전체를 같은 endpoint 호출로 유지하고, 일반 탭 로드에는 새 이동이나 재시도를 추가하지 않습니다. 정상 빈 본문과 확인된 탭 부재는 계속 성공입니다. 손상되거나 불명확한 증거는 기존 `parser_drift/DATA_CONTRACT` 경로로 전달합니다.

추가 경계 검증에서는 채널 경로가 아닌 URL, 제공된 채널 ID 불일치, 중복 선택과 boolean이 아닌 선택 표시도 부재나 정상 콘텐츠의 증거로 쓰지 않도록 보완했습니다. 선택 본문의 `sectionListRenderer`·`richGridRenderer`와 그 안의 `itemSectionRenderer.contents`에서 null·빈 객체가 사라지는 경우도 거부합니다. 일반 renderer 객체 전체를 재귀 검증하는 별도 파서는 추가하지 않았습니다. 정상 빈 행 목록, 검색용 ExpandableTab, 채널 root·handle·legacy URL과 HTTP/HTTPS URL 호환성을 유지합니다.

최종 검증은 다음과 같습니다.

| 검증 | 실제 결과 |
|---|---|
| `npm --prefix hololive/hololive-youtube-collector/youtubejs test` | **358/358 통과**, 실패·취소·skip 없음 |
| 같은 helper의 `npm run typecheck` | tsconfig.json·jsconfig.json 모두 통과, 검사 설정 변경 없음 |
| 새 `collection-client.test.mjs` | 46개 통과. 네 탭의 원문 증거·정상 조회·선택·본문·identity·리디렉션·취소·동시성·metadata와 player 바인딩을 검증 |
| 독립 Astra 재현 | **152/152 통과**. 실제 native SDK와 production adapter를 사용 |
| HTTP 요청 동등성 | 기존 SDK 대비 직접 조회 2회 및 초기 2단계 이동을 포함한 4회의 URL·메서드·헤더·JSON 본문이 동일. 응답 직후 취소도 추가 요청 없이 보존 |
| Go 소비자·오류 분류·스케줄러 | `youtubejs`, `youtubejscollector`, `collecterr`, `collectorruntime`의 관련 기존 테스트 통과. `undefined` 컴파일 오류 없음 |
| RPC → 로컬 PostgreSQL | `TestContentTabRPCDistinguishesMissingTabsFromParserDrift` 세 하위 테스트 통과, skip 없음. 정상 부재는 IDLE, videos/shorts drift는 관측 0건·DEFERRED·fatal 0건 |
| 최종 diff 검토 | 공백 오류 없음. 추가 수정이 필요한 지적 없음 |

이번 Go 검증은 외부 테스트 DB 환경변수 세 개를 제거하고 실제 로컬 테스트 PostgreSQL을 생성했습니다. Community/live는 실행기의 정상·부재·실패 테스트와 오류 전파 소스를 확인했으며, 두 종류의 RPC→DB 전체 경로를 새로 실행한 것은 아닙니다. 이 보완에서 운영 YouTube·원격·운영 DB 호출, 배포와 Git 게시는 수행하지 않았습니다. Fallback delta: 없음. 신규 의존성·자동 재시도·추가 HTTP 요청 경로는 없습니다.

## 분석 기준과 근거 수준

| 항목 | 기준 |
|---|---|
| Hololive revision | `5c12d2c503915caf63f21aa86e1aeb346da94e63` |
| shared-go revision | workspace의 `1e5c725342e9c3a5c563e2df239720a429e4760a` |
| 실행 환경 | kapu, Go 1.27.1, Node 24.21.0, `hololive-bot/go.work` 로컬 workspace |
| helper 의존성 | 설치된 `youtubei.js@18.1.0` 소스와 해당 helper 테스트 |
| 주 대상 | `hololive-youtube-collector`의 Go 런타임과 `youtubejs/src` |
| 대조 대상 | 관련 `hololive-shared`, `shared-go`, API 및 alarm-worker의 실제 소비자, 현재 계약 문서 |
| 조사 방법 | 소스와 테스트 대조, production import 및 `go list -deps` 관계, 로컬 대체 응답·루프백 서버·초기화 지연을 이용한 재현 |
| 범위 밖 | 모든 함수의 전수 검증, 운영 데이터와 트래픽 조사, 운영 장애 빈도 확인, 부하 프로파일, race·전체 lint·NilAway·이미지 build |

production build는 `GOWORK=off`와 collector `go.mod`의 `shared-go/v2 v2.8.0`을 사용합니다. 재사용 후보로 인용한 `shared-go/pkg/httputil/body.go`는 로컬 `v2.8.0` tag와 분석 workspace 사이에 diff가 없음을 확인했습니다. 이 문서의 workspace 테스트 통과가 production build 검증을 대신하지는 않습니다.

근거 수준은 항목별로 구분합니다.

- **로컬 재현**: 실제 helper 함수를 대체 응답, 로컬 루프백 서버 또는 지연된 초기화로 호출해 결과를 확인했습니다. 실제 YouTube 요청이나 운영 프로세스 장애 재현은 아닙니다.
- **코드 확인**: 호출, 복사, 의존성 또는 검증 조건을 소스에서 확인했습니다. 운영 발생 빈도와 지연 증가량은 측정하지 않았습니다.
- **추론**: 확인한 코드 사실을 조합해 예상한 동작이며, 실행이나 운영 관측으로 확인하지 않았습니다.
- **최적화 후보**: 구조상 비용은 존재하지만 변경의 실익과 안전성을 추가 측정해야 합니다.
- **설계 제안**: 목적지와 변경 방법을 제안한 것으로 현재 계약이나 승인된 구현을 뜻하지 않습니다.

AI 작성 여부는 코드만으로 판정하지 않습니다. 아래의 AI slop 성격은 의미 없는 간접화, 도달 불가능한 분기, 미사용 코드, 실제 의존성의 동작을 반영하지 않는 테스트처럼 검증 가능한 유지보수 문제를 뜻합니다.

## 우선순위

P1은 서비스 중단으로 확대될 수 있고 현재 계약과 충돌하는 오류 분류 문제, P2는 구조·정합성·운영 진단 문제, P3는 영향 범위가 제한된 수명주기·성능·정리 작업입니다. 성능 항목의 우선순위는 운영 병목을 측정한 순위가 아닙니다.

| 항목 | 우선순위 | 근거 | 핵심 조치 |
|---|---|---|---|
| 미인식 upstream 오류의 INTERNAL 분류 | P1 | 로컬 재현, Go 호출 경로와 계약 문서 확인 | helper 기본 분기를 fatal 계약에 맞게 복원 |
| 탭 없음 판정의 fetcher별 불일치 | P1 하위 사례 | 로컬 재현 | 세 fetcher가 공유하는 탭 판정 하나로 통합 |
| 본문 읽기 네트워크 오류의 INTERNAL 분류 | P1 하위 사례 | 로컬 재현(실제 undici 오류 형태 포함) | transport·pagination·RPC 오류 분류 통합 |
| Retry-After 손실 | P2 | 로컬 재현, upstream 헤더 송신 여부 미확인 | 헤더 송신 확인 후 기존 retry wire 계약으로 전달 |
| 재시도 표현의 죽은 경로와 clamp 중복 | P2 | 코드 확인 | DELAY 경로 삭제, clamp 위치 결정 |
| 문자열 기반 경계 테스트 | P2 | 코드 및 stack 규칙 확인 | 즉시 삭제 |
| 공유 패키지의 publish와 consume 혼재 | P2 | 의존 그래프 확인 | 연관 문서의 이관 계획에 collector 보존 조건 제공 |
| 범용 HTTP 본문 처리 재구현 | P2 | 코드 확인 | shared-go의 기존 기능 재사용 |
| 결과 payload 반복 복사 | P3 | 코드 확인, 기본 상한으로 크기 추정 | 불변 결과의 소유권과 조회 API 정리 |
| discovery 제외 집합 반복 정렬 | P3 | 최적화 후보 | 정렬 유지 삽입으로 단순화 |
| bootstrap 완료 후 종료 상태 복원 | P3 | 로컬 재현, 운영 영향 제한 | 종료 의도와 초기화 자원 소유권 보존 |
| 의미 없는 wrapper, 도달 불가능한 분기, 중복 검증, 미사용 코드 | P3 | 코드 확인 | 책임 없는 계층과 잔재 삭제 |

## 오류 분류와 수명주기

### 미인식 upstream 오류를 내부 불변식 위반으로 선언

[rpcErrorResultFor의 기본 분기](../../hololive/hololive-youtube-collector/youtubejs/src/rpc-validation.mjs#L138)는 취소, 응답 계약 위반, `status` 401/403/429, 알려진 `code`에 해당하지 않는 모든 오류를 `HTTP 500 / helper_internal_invariant / INTERNAL`로 반환합니다. Go의 [helperStatusError](../../hololive/hololive-youtube-collector/internal/runtime/youtubejs/rpc_failure.go#L13)는 이 결과를 `collecterr.New`로 명시 분류합니다. 그래서 Go가 미인식 오류에 붙이는 `unclassified` 표시가 없고, [fatalCollectionError](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler_run.go#L207)가 true를 반환해 [discovery와 worker 취소](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler.go#L315)로 이어집니다. Go 쪽은 미인식 오류를 fatal에서 제외하는데 helper 쪽은 미인식 오류를 내부 결함으로 선언하므로, 두 경계의 의미가 어긋나 있습니다.

설치된 YouTube.js의 `HTTPClient.fetch`(`youtubejs/node_modules/youtubei.js/dist/src/utils/HTTPClient.js`)는 2xx가 아닌 응답에서 `status` 필드가 없는 `InnertubeError`를 던집니다. [fetch-transport의 응답 분류](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-transport.mjs#L224)는 429와 5xx만 직접 처리하고 나머지 4xx는 라이브러리로 통과시킵니다. 로컬에서 400·403·404 메시지의 `InnertubeError`를 helper 오류 분류에 전달하면 모두 `helper_internal_invariant`가 됩니다. 따라서 삭제·정지된 채널, 봇 차단이나 권한 거부 같은 일반 provider 실패도 컬렉터 전체 중단 경로로 연결됩니다. 아래의 탭 없음과 본문 연결 오류는 이 문제의 하위 사례입니다.

[rpcErrorResultFor의 401/403 분기](../../hololive/hololive-youtube-collector/youtubejs/src/rpc-validation.mjs#L96)는 라이브러리 경로에서는 도달하지 않습니다. 이 분기를 검증하는 [PAG-004 테스트](../../hololive/hololive-youtube-collector/youtubejs/src/rpc-validation.test.mjs#L62)는 라이브러리가 만들지 않는 `.status = 403` 오류를 사용하므로 실제 의존성의 동작을 반영하지 못합니다.

**반복성(추론)**: `fatalCollectionError`가 true여도 실행 흐름은 return 없이 [deferFailedRun](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler_run.go#L261)으로 진행하며, 이 함수는 `context.WithoutCancel`을 사용하므로 job defer가 성공합니다. collector 컨테이너는 [x-app-service](../../deploy/compose/docker-compose.prod.yml#L34)의 `restart: unless-stopped`를 따릅니다. 따라서 같은 job이 기본 지연 뒤 다시 lease되면 같은 오류로 다시 fatal이 되고, 프로세스 재시작이 주기적으로 반복되며 그때마다 진행 중인 다른 job도 취소될 것으로 예상합니다. 운영에서 실제로 반복되는지는 확인하지 않았습니다. 구현 착수 전에 운영 로그에서 `helper_internal_invariant`와 scheduler fatal의 발생 빈도를 해당 ops 스킬로 한 번 조회해 긴급도를 확정합니다.

**기존 선례**: [live-check의 requestFailure](../../hololive/hololive-youtube-collector/youtubejs/src/live-check.mjs#L474)는 이미 취소와 transport가 분류한 오류를 보존하고, youtubei.js HTTP 오류와 미분류 네트워크 오류를 `collection_failed/TRANSIENT`로 묶습니다. helper 오류 분류의 단일 소유자는 이 구현에서 출발합니다.

리팩토링은 helper 내부에 upstream 오류 분류 소유자를 하나 두고 content, channel, community fetcher와 RPC 경계가 함께 사용하도록 진행합니다. 라이브러리 HTTP 오류는 메시지의 상태 코드를 근거로 기존 failure tuple에 매핑하고, 401/403의 `configuration_error` 계약을 실제로 도달 가능한 경로로 만듭니다. helper 자체 코드의 불변식 위반만 `helper_internal_invariant`로 남깁니다. 미인식 오류에 어떤 class를 줄지는 아래 결정 사항에서 정하며, 프로그래밍 결함을 TRANSIENT 뒤에 숨기지 않아야 합니다. 회귀 검증에는 실제 `InnertubeError` 형태의 400·401·403·404, 기존 cooldown과 취소, helper 자체 불변식 위반을 포함하고, PAG-004 fixture를 실제 라이브러리 형태로 교체합니다.

### 탭 없음 판정이 fetcher마다 다름

설치된 YouTube.js의 `Channel`에는 탭이 없어도 `getVideos/getShorts/getCommunity` 메서드가 있으며, 호출하면 `getTabByURL`이 `InnertubeError: Tab "<name>" not found`를 던집니다. 근거 위치는 `youtubejs/node_modules/youtubei.js/dist/src/parser/youtube/Channel.js`의 메서드와 `has_videos/has_shorts/has_community`, `dist/src/core/mixins/TabbedFeed.js`의 `getTabByURL`입니다. 현재 세 fetcher의 판정은 다음과 같습니다.

| fetcher | 판정 방식 | 상태 |
|---|---|---|
| [fetch-channel](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-channel.mjs#L188) | `InnertubeError`이며 메시지가 `Tab "streams" not found`인지 확인 | 올바르게 동작합니다. |
| [fetch-community](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-community.mjs#L63) | `has_community === false`로 먼저 판정한 뒤 [isMissingCommunity](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-community.mjs#L27)로 예외 판정 | 정상 경로는 안전합니다. 다만 `/tab not found/i`는 실제 메시지 `Tab "posts" not found`와 일치하지 않고, `err?.status === 404`는 `InnertubeError`에 status가 없어 도달하지 않습니다. |
| [fetch-content](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-content.mjs#L38) | `getVideos/getShorts` 메서드 존재 여부 | 결함입니다. 메서드가 항상 있으므로 탭이 없으면 예외가 그대로 올라갑니다. |

현재 라이브러리의 실제 `Channel.prototype.getShorts`를 사용하고 탭 조회 실패를 주입한 결과, content helper는 `HTTP 500 / helper_internal_invariant / INTERNAL`을 반환했습니다. 기존 [PAG-011 테스트](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-content.test.mjs#L98)는 채널을 `{}`로 만들어 메서드가 없는 경우만 검증하므로 실제 라이브러리의 탭 없음 동작을 반영하지 못합니다.

리팩토링은 `has_*` getter와 `InnertubeError`·정확한 메시지 판정을 묶은 탭 판정 하나를 세 fetcher가 공유하도록 진행하고, 기존 `missing_tab` 결과를 보존합니다. community의 도달하지 않는 정규식과 status 조건은 함께 정리합니다. 일반 파싱·네트워크 실패를 탭 없음이나 빈 성공으로 변환하지 않습니다. 회귀 검증에는 메서드는 존재하되 탭 조회가 실패하는 경우, 정상 탭, 다른 메시지의 InnertubeError를 세 fetcher 모두에 포함합니다.

### 응답 본문 연결 오류가 내부 결함으로 확대

[fetch-transport.mjs](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-transport.mjs#L77)의 오류 분류는 `fetch()` 호출만 감쌉니다. 헤더를 받은 뒤 라이브러리가 `Actions.execute`의 `response.json()`에서 겪는 스트림 오류는 이 범위 밖입니다. content fetcher의 첫 페이지는 `paginate` 바깥에서 읽히므로, 첫 페이지의 본문 오류는 기본 분기로 들어가 fatal 경로에 연결됩니다. 반면 [pagination의 continuation 오류 분류](../../hololive/hololive-youtube-collector/youtubejs/src/pagination.mjs#L286)는 네트워크 오류를 `continuation_transient`로 인식하므로, 같은 오류가 최초 페이지와 후속 페이지에서 다르게 분류됩니다.

두 가지 형태로 재현했습니다. HTTP 200 응답 스트림에 `code: "ECONNRESET"` 오류를 주입한 경우와, 로컬 루프백 서버가 헤더를 보낸 뒤 소켓을 끊은 경우입니다. 실제 undici 오류는 `TypeError: terminated`이고 `cause`가 `SocketError`(`code: "UND_ERR_SOCKET"`)였습니다. 두 경우 모두 `HTTP 500 / helper_internal_invariant / INTERNAL`이었습니다.

네트워크 오류 코드 집합은 [pagination.mjs](../../hololive/hololive-youtube-collector/youtubejs/src/pagination.mjs#L18)와 [fetch-transport.mjs](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-transport.mjs#L10)에 따로 정의되어 있습니다. 현재 내용은 같지만 소유자가 둘입니다. 위 항목의 단일 오류 분류 소유자가 이 집합과 `code/cause.code` 판정을 함께 소유하고 transport, pagination, RPC 경계에서 사용하도록 합니다. 요청 취소 여부와 기존 class를 보존해야 합니다. 본문 오류를 TRANSIENT로 바로잡는 작업이 새로운 자동 재시도를 추가하는 근거가 되어서는 안 됩니다. 특히 live-check의 단일 물리 요청 계약은 유지합니다. 회귀 fixture는 인공 오류가 아니라 실제 undici 형태(`cause` 한 단계)를 사용합니다.

### Retry-After 전달 누락

[classifyUpstreamResponse](../../hololive/hololive-youtube-collector/youtubejs/src/fetch-transport.mjs#L224)는 429 본문을 정리한 뒤 COOLDOWN 오류를 만들지만 `Retry-After`를 읽지 않습니다. `Retry-After: 600`을 준 로컬 응답도 `retry: {kind: "default"}`로 변환됐습니다.

RPC에는 이미 `after`와 `at` 표현이 있습니다. 현재는 정보를 잃어 [Go 재시도 계산](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler_observe.go#L177)이 기본값을 선택합니다. [기본 설정](../../hololive/hololive-shared/pkg/config/settings/collector/config.go#L63)의 RetryMin 30초와 RetryMax 5분을 적용하면 기본 지연은 165초입니다. 운영 profile에 다른 값이 있으면 이 계산 결과도 달라집니다.

YouTube innertube의 429 응답이 실제로 `Retry-After`를 보내는지는 확인하지 않았습니다. 헤더를 보내지 않는다면 이 수정은 효과가 없으므로, 운영 로그나 응답 헤더로 송신 여부를 확인하는 것을 착수 조건으로 둡니다. 헤더가 확인되면 초 단위와 HTTP 날짜 형식을 검증한 뒤 기존 retry 계약으로 전달합니다. 긴 힌트는 Go의 `RetryMax`와 DB clamp에서도 잘리므로(아래 재시도 항목 참조) 헤더 전달 수정과 최대 cooldown 정책을 별도로 다룹니다. 기존 최대값을 조용히 확대하지 않습니다. malformed·누락·음수·범위 초과 헤더와 두 유효 형식을 검증합니다.

### 초기화가 종료 상태를 되돌림

[initializeBootstrap](../../hololive/hololive-youtube-collector/youtubejs/src/helper-runtime.mjs#L171)은 비동기 초기화 이후 현재 상태를 확인하지 않고 READY를 설정합니다. 초기화를 지연시킨 뒤 `beginDrain()`을 호출한 로컬 재현에서 상태는 `STOPPED → READY`로 바뀌었고 bootstrap은 200을 반환했습니다. 늦게 생성된 자원은 명시적으로 정리하기 전까지 close되지 않았습니다.

운영 [listenUnix의 manageProcess 경로](../../hololive/hololive-youtube-collector/youtubejs/src/server.mjs)는 `onStopped`에서 즉시 `process.exit()`를 호출합니다. 따라서 이 결과를 곧바로 운영 재활성화 장애라고 판단하지 않습니다. helper 수명주기 자체의 결함이며, 비동기 초기화나 프로세스를 직접 종료하지 않는 소비자에서 의미가 있습니다.

초기화 중 종료 의도를 보존하고, 늦게 생성된 자원은 닫은 뒤 bootstrap을 거부하도록 정리합니다. 구현 시 bootstrap 완료와 drain의 순서, 중복 drain, 초기화 실패 후 정리를 검증합니다.

## 성능과 중복 구현

### 결과 조회가 payload를 반복 복사

[NewRunOutput과 Observations](../../hololive/hololive-youtube-collector/internal/runtime/collectutil/runner.go#L277), [CollectResult](../../hololive/hololive-youtube-collector/internal/runtime/collectutil/result.go#L29), [publish 입력 복사](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_publish_prepare.go#L11)를 따라가면 일반적인 complete 경로에서 다음 복사가 발생합니다.

| 순서 | 호출 지점 | 깊은 복사 |
|---|---|---|
| 1 | `NewRunOutput` | envelope payload 및 checkpoint cursor |
| 2 | `NewCompleteResult` | `cloneRunOutput` |
| 3 | 결과 검증의 `result.Output()` | `cloneRunOutput` |
| 4 | 결과 검증의 `output.Observations()` | envelope payload |
| 5 | commit의 `result.Output()` | `cloneRunOutput` |
| 6 | `PublishComplete`의 `output.Observations()` | envelope payload |
| 7 | 저장소의 `clonePublishBatchInput()` | envelope payload 및 checkpoint cursor |
| 8 | 성공 metrics의 `output.Observations()` | envelope payload |

payload 구성 이후에도 8회의 깊은 복사 경로가 있고, checkpoint cursor는 이보다 더 자주 복사됩니다. 기본 [MaxSuccessResponseBytes](../../hololive/hololive-shared/pkg/config/settings/collector/config.go#L18)가 1 MiB이므로 helper 기반 작업 하나의 일시 할당은 대략 8 MiB 수준으로 제한될 것으로 추정합니다. 이는 정적 호출 분석과 설정 상한에 근거한 추정이며, 상주 메모리나 지연 증가율을 측정한 결과가 아닙니다. 이득에 비해 aliasing 위험이 있으므로 우선순위는 P3입니다. partial과 오류 경로는 별도로 추적해야 합니다.

생성 경계에서 입력 소유권을 확보한 불변 결과를 이후 단계가 공유하고, `Output()` 같은 조회에는 전체 복사를 숨기지 않는 방향을 제안합니다. metrics에는 provider, kind, completeness, continuity만 전달합니다. 저장소 경계의 방어적 snapshot은 외부 입력 변이와 transaction 안전성을 검토한 뒤 줄입니다. 복사를 일괄 삭제해 slice aliasing을 만들면 안 됩니다.

검증은 생성 후 원본을 바꿔도 저장 입력이 변하지 않는 계약, partial 결과 보존, metrics 값 유지와 payload 크기별 `B/op`, `allocs/op` 비교를 포함합니다. 성능 benchmark는 조사용이며 머신별 지연 수치를 blocking gate로 만들지 않습니다.

### 범용 HTTP 본문 처리를 다시 구현

[providerhttp/response.go](../../hololive/hololive-youtube-collector/internal/runtime/providerhttp/response.go#L184)는 제한 읽기, 초과 판정, drain, close를 직접 구현합니다. [shared-go/httputil/body.go](../../../shared-go/pkg/httputil/body.go)는 이미 `ReadAllLimited`, `ReadAllAndDrain`, `ReadAllAndCloseWithDrainLimit`, `DrainAndClose`를 제공합니다. collector reader는 자체 `io.ReadAll`이 소유한 버퍼를 반환하면서 `bytes.Clone(body)`로 추가 복사도 수행합니다.

일반 I/O 동작은 공통 구현을 사용하고, provider 상태 코드, 오류 class, 비밀 마스킹은 collector에 남깁니다. 성공 본문 제한 읽기부터 재사용하고, 다음 차이는 명시적으로 대조합니다.

- 오류 본문은 상한까지 잘라 보존해야 하지만 `ReadAllLimited`는 초과 시 데이터 없이 오류를 반환합니다.
- collector의 `ctxReader`는 읽을 때마다 ctx를 확인하지만 shared-go에는 대응 기능이 없습니다. `net/http` 요청 ctx 취소만으로 충분한지 확인한 뒤 제거 여부를 정합니다.
- collector는 ctx가 취소되면 drain을 생략합니다.

공통 API에 부족한 기능이 있다면 실제 소비자에게 필요한 작은 기능만 보완합니다. 기존 gzip 해제 후 크기 제한, trailing JSON 거부, 본문 상한, 취소 시 close, 오류 마스킹과 연결 재사용 테스트를 유지합니다. 제한 숫자나 cleanup 순서를 바꾸면서 단순 중복 제거라고 설명하지 않습니다.

### discovery 제외 집합의 반복 정렬

[addExcludedKey](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler_discovery_cycle.go#L237)는 항목마다 `slices.Contains`와 `slices.Sort`를 실행합니다. [normalizeExcludedJobKeys](../../hololive/hololive-youtube-collector/internal/runtime/joblease/repository_candidates.go#L322)는 각 조회에서 다시 복사, 정렬, 중복 제거합니다.

기본 [QueueCapacity](../../hololive/hololive-shared/pkg/config/settings/collector/config.go#L65)는 `workers * 4`이고 10,000은 검증 상한일 뿐이므로 기본 설정에서의 비용은 작습니다. 우선순위는 P3입니다. `excluded`는 이미 정렬 상태로 유지되므로 map을 새로 두지 않고 `slices.BinarySearch`와 `slices.Insert`로 membership 확인과 삽입을 처리하는 것으로 충분합니다. SQL의 제외 의미와 runner 간 공정성, rotation, queue capacity, 입력 slice 비변이 계약은 유지합니다. 큰 queue를 운영 profile에 실제로 쓰는 경우에만 비용을 측정합니다.

### Holodex 요청 병합 후보

[live와 metadata와 schedule runner](../../hololive/hololive-youtube-collector/internal/runtime/holodexcollector/runner.go)는 같은 [Client.Fetch](../../hololive/hololive-youtube-collector/internal/runtime/holodexcollector/client.go)로 같은 `/live?org=Hololive&status=live,upcoming` 요청을 보냅니다. 각각의 lease와 수집 주기는 독립적이므로 항상 세 번 중복 요청한다고 단정할 수는 없지만, 요청이 겹칠 때는 동일 응답을 별도로 읽고 파싱할 수 있습니다.

먼저 실제 요청 겹침과 호출량을 측정합니다. 이득이 있을 때만 동시에 진행 중인 동일 요청의 병합을 검토하고, freshness, 관측 시각, 선행 호출자 취소가 대기자에게 미치는 영향과 세대 경계를 정합니다. 측정 없이 장기 캐시나 새 fallback을 추가하지 않습니다.

## 공유 코드의 소유권과 이관

이 절의 이관 실행 계획과 파일 이동 담당은 [알람 워커와 API 공유 모듈 리팩토링안](2026-10-02-alarm-worker-api-shared-refactoring.md)의 2.1절과 이동 표가 소유합니다. 아래 내용은 그 계획을 실행할 때 collector 관점에서 확인한 의존 관계와 보존 조건이며, 별도의 이동 계획으로 실행하지 않습니다.

### 역할별 wrapper가 패키지 의존성을 분리하지 못함

분석 기준에서 `hololive-shared/pkg/service/youtube/sourceobservation`은 비테스트 Go 파일 46개, 7,989줄입니다. [PublishRepository와 ConsumeRepository](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_roles.go#L18)는 같은 Repository를 감쌉니다. 호출 가능한 메서드는 좁히지만, publish와 API 소유 consume, canonical 저장, reconcile, replay, retention의 패키지 의존성은 그대로입니다.

`go list -deps`로 확인한 경로는 다음과 같습니다.

```text
collector → collectorruntime → sourceobservation → reconcile/live
collector → collectorruntime → sourceobservation → poller/runtime/batchrepo
collector → collectorruntime → pkg/providers → 기존 scraper/scraping
```

실제 publish 생성자는 collector에서, consume 및 consumer 생성자는 API YouTube plane에서 사용합니다. 이 그래프는 컬렉터가 canonical 쓰기를 실제 호출한다는 증거가 아니며, 바이너리 크기 증가를 측정한 결과도 아닙니다. 문제는 빌드와 변경의 영향 범위가 런타임 소유권보다 넓다는 점입니다.

### collector 관점의 보존 조건

아래 목적지 이름은 제안입니다. 패키지를 옮기기 전에 실제 외부 소비자와 DB 계약을 다시 확인하고, 다른 서비스의 `internal`을 import하는 구조로 해결하지 않습니다.

| 현재 코드 | 권장 위치와 조치 | 이유와 보존 조건 |
|---|---|---|
| `pkg/contracts/sourceobservation` | hololive-shared 유지 | envelope, payload, identity, 세대, failure 계약은 양쪽 런타임이 사용합니다. hash와 JSON 의미를 보존합니다. |
| service의 `job_contract*.go` | 공유 계약 영역으로 이동 | 순수 JobID와 JobContract를 쓰기 위해 저장소와 consumer를 의존할 필요가 없습니다. |
| `repository_publish*`, 발행 전용 checkpoint 및 terminal SQL | collector의 `internal/observationpublish` | 운영 publish 소유자는 collector입니다. fence, observation, checkpoint, complete/defer의 원자성을 유지합니다. |
| consumer, canonical persist, replay, retention, live finalizer | API YouTube plane 내부 | 운영 소비와 정합성 처리의 소유자는 API입니다. canonical, notification intent, finalize의 transaction 경계를 유지합니다. |
| `internal/service/youtube/reconcile/*` | 소비 구현과 함께 API로 이동 | 현재 소비 구현의 내부 의존성입니다. 순수 reducer라는 이유만으로 shared에 남기지 않습니다. |
| `poller/runtime/batchrepo` 등 연결 구현 | 소비자별로 분리 후 이관 | 관련 타입과 간접 소비자를 함께 추적합니다. 다른 런타임이 필요한 부분까지 통째로 옮기지 않습니다. |
| `settings/collector` | collector의 `internal/config` | 직접 운영 소비자가 collector뿐입니다. env 이름, 기본값, worker profile 계약을 보존합니다. |
| `providers`의 DB 초기화 | 작은 DB 전용 패키지 또는 기존 DB 생성 API | DB 초기화가 scraper 의존성을 함께 끌어오지 않도록 합니다. |
| parser의 `CommunityPost`를 쓰는 helper RPC DTO | collector RPC 타입으로 분리 검토 | RPC wire 계약이 기존 HTML parser의 모델 변경에 종속됩니다. 실제 공통 도메인 필드만 공유합니다. |
| bounded HTTP read와 drain과 close | shared-go의 기존 httputil 재사용 및 필요한 부분 보완 | 도메인과 무관하며 실제 공통 소비자가 있는 I/O 기능입니다. |
| provider 오류, helper 프로토콜, collection 정책 | collector 내부 유지 | YouTube와 collector의 실행 정책입니다. shared-go로 올리지 않습니다. |

`poller/runtime/batchrepo` 이관은 [SERVICE_OWNERSHIP의 Shared Package Retention](../current/SERVICE_OWNERSHIP.md#shared-package-retention)이 `service/youtube/{admission,batchrepo,poller/runtime,tracking/observation}`을 "여러 runtime에 걸치는 기반 패키지"로 잔류 분류한 것을 바꾸는 제안입니다. 이관 단계에서 이 분류를 실제 소비자에 맞게 함께 갱신합니다. 같은 문서가 poller 구현 소유자로 적은 `hololive-youtube-collector/internal/runtime/pollers`는 분석 revision에 존재하지 않고, youtube-collector 행의 `stats` observation도 현재 observation kind에 없습니다. 이 두 낡은 서술도 같은 갱신에서 정리합니다.

`settings/collector`는 [settings/internal/load](../../hololive/hololive-shared/pkg/config/settings/collector/runtime.go)를 사용합니다. collector로 옮기면 Go internal 접근 제한에 걸리므로, 공통 설정 로딩 기능의 경계를 먼저 분리해야 합니다. 기존 internal/load 전체를 공개하거나 그대로 복제하지 않습니다.

공유 service의 `types.go`도 사용처별로 나눕니다. 여러 런타임이 실제 교환하는 타입은 계약에 두고, publish 전용 입력과 API 내부 claim 처리 타입은 소유 구현에 둡니다. 파일 전체를 contracts로 옮겨 새로운 잡다한 공통 패키지를 만들지 않습니다.

### 공유 코드 자체의 개선

- 계약 패키지는 SQL, provider client, runtime 구현을 의존하지 않도록 유지합니다.
- publish 입력의 preflight, snapshot, 검증, encoding 책임을 명확히 하고, 검증에서 만든 binding 인덱스의 후속 재사용을 검토합니다. preflight의 복사 전 크기 제한은 유지합니다.
- publish 결과 검증은 저장소가 소유합니다. 현재 [저장소 내부](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_publish.go#L350)와 collector [publisher](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/publisher.go#L177)([partial 경로](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/publisher.go#L302))가 같은 결과를 두 번 검증합니다.
- retry의 저장 타입과 밀리초 단위 검증을 한 곳에서 소유합니다.
- 공통 HTTP API는 취소, 제한 읽기, drain, close 오류 보존 계약을 유지하면서 소비자에게 부족한 기능만 보완합니다.
- 실행 구현 이관 뒤 역할 wrapper와 소비자가 없는 export를 제거합니다. 다른 런타임의 실제 사용처와 공개 계약 여부를 먼저 확인합니다.

### 강제로 공유하지 않을 구현

collector scheduler 전체를 [shared-go ManagedPool](../../../shared-go/pkg/workerpool/managed_pool.go)로 바꾸는 것은 현재 근거로 권하지 않습니다. projection discovery, job-key 중복 방지, lease 갱신, 부분 성공의 원자적 defer는 collector 고유 책임입니다. ManagedPool의 취소 및 finalizer 소유권 계약도 다릅니다. 단순히 worker와 queue가 있다는 이유로 통합하면 adapter와 예외가 늘어날 수 있습니다.

마찬가지로 canonical JSON과 hash 규칙을 일반 JSON 직렬화로 대체하거나, helper의 물리 요청 상한을 일반 retry 유틸리티에 맡기는 변경은 중복 제거 범위가 아닙니다.

## 컨벤션과 불필요한 추상화

### 재시도 표현의 죽은 경로와 clamp 중복

현재는 `collecterr.RetryHint → joblease.RetryDecision → sourceobservation.RetrySchedule`의 표현과 변환이 있고, [RetryDecision](../../hololive/hololive-youtube-collector/internal/runtime/joblease/types.go#L204)과 [RetrySchedule](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/retry_schedule.go)는 모두 DELAY와 AT를 표현합니다. 그러나 운영 코드에서 `RetryDecision`을 만드는 곳은 [commitCollectResult의 NewRetryAt](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler_run.go#L437) 하나뿐입니다. `NewRetryDelay`에는 운영 호출자가 없고, [publisher의 retryDelaySchedule](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/publisher.go#L248) 분기와 `NewRetryDelaySchedule`도 운영 경로에서 도달하지 않습니다. DELAY의 양수 duration과 밀리초 정렬 검증이 서로 다르지만, 이 불일치는 운영에서 발생하지 않습니다. 정리할 대상은 DELAY 경로 자체입니다.

정책 적용도 한 번이 아닙니다. Go의 [retryAt](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler_observe.go#L171)이 앱 시계로 `[MinRetryDelay, MaxRetryDelay]` 범위에 맞춘 뒤, [lease defer SQL](../../hololive/hololive-youtube-collector/internal/runtime/joblease/queries/repository_lease_defer_0144_12.sql)의 `LEAST/GREATEST`와 [publish defer](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_publish_terminal.go#L119)가 DB 시각 기준으로 같은 범위를 다시 적용합니다.

DELAY 생성자와 변환 분기를 삭제하고, AT 하나를 upstream 힌트에서 저장 스케줄까지 전달하는 경로로 줄입니다. 그 뒤 중간 `RetryDecision` 제거를 검토합니다. clamp를 한 곳으로 줄일지, 앱 시계와 DB 시계 중 어느 쪽을 기준으로 둘지는 결정 사항입니다. 실패 분류, UTC 처리, 최소·최대 지연, DB 시간 단위는 보존합니다.

### 의미 없는 wrapper와 불가능한 분기

[result_validation.go](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/result_validation.go#L67)에는 다음 패턴이 13회 반복됩니다.

```go
if invalid {
    if err := invariantError("..."); err != nil {
        return fmt.Errorf("invariant error: %w", err)
    }
    return nil
}
```

`invariantError`는 항상 오류를 반환하므로 안쪽 조건과 성공 반환은 불필요합니다. 같은 파일에 이미 같은 일을 하는 [wrappedInvariantError](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/result_validation.go#L333)가 있습니다. [golangci 설정](../../.golangci.yml#L230)의 `report-internal-errors: false` 때문에 같은 패키지 helper의 오류를 다시 감쌀 필요가 없으므로, 이 패턴을 삭제해도 lint 제약에 걸리지 않습니다.

같은 모양은 다른 곳에도 있습니다.

- [helperStatusError](../../hololive/hololive-youtube-collector/internal/runtime/youtubejs/rpc_failure.go#L13)는 항상 오류를 반환하는 `applyHelperRetry`의 결과를 `if err != nil`로 감싸고 `return nil`로 끝납니다. `applyDefaultRetryResult → applyDefaultRetry`, `protocolMismatchError → protocolMismatch`처럼 의미를 추가하지 않는 계층도 있습니다.
- [readProviderError](../../hololive/hololive-youtube-collector/internal/runtime/providerhttp/response.go#L107)와 [readProviderSuccess](../../hololive/hololive-youtube-collector/internal/runtime/providerhttp/response.go#L136)는 읽기 실패 후 `collecterr.FromContext`가 nil이면 `return nil, nil`로 빠집니다. [FromContext](../../hololive/hololive-youtube-collector/internal/runtime/collecterr/errors.go#L213)는 nil이 아닌 오류에 대해 nil을 반환하지 않으므로 현재는 도달하지 않습니다. 그러나 이 분기에 도달하면 읽기 실패나 비정상 상태 코드가 nil 본문의 성공으로 바뀌는 모양이므로, result_validation.go의 사례보다 위험도가 높고 먼저 정리합니다.

이 계층을 접고 오류 값과 발생 문맥을 직접 반환합니다. `%w`로 오류 식별성을 유지하되 함수명을 기계적으로 누적한 메시지를 만들지 않습니다.

다음 항목도 정리 대상입니다.

| 코드 | 확인한 문제 | 제안 |
|---|---|---|
| [ValidatePublishBatchResult](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_publish_prepare.go#L146)의 `seen[i]` | 인덱스를 한 번씩 순회하고 Ordinal도 i와 비교하므로 이 배열은 중복 검출 효과가 없습니다. | 불필요한 할당과 분기를 제거하고 유효성 검증을 유지합니다. |
| [Config.OrDefault와 MaxProviderTimeout](../../hololive/hololive-shared/pkg/config/settings/collector/config.go#L100) | 조사한 Hololive Go 소스에서 테스트를 포함해 호출자가 없습니다. | 미사용 공개 함수와 전용 default 보조 경로를 함께 제거 검토합니다. |
| 당시 `joblease/durable_failure_sql.go` | SQL 정규식 해석 함수 두 개가 테스트에서만 사용되지만 일반 소스에 있습니다. | 필요한 도우미를 [테스트 파일](../../hololive/hololive-youtube-collector/internal/runtime/joblease/durable_failure_sql_test.go)로 이관했습니다. |
| [ChannelRunner 주석](../../hololive/hololive-youtube-collector/internal/runtime/youtubejscollector/channel.go) | 현재 observation kind에 없는 채널 통계도 수집한다고 설명합니다. | 현재 profile과 photo 수집 계약에 맞춥니다. |
| exported 타입과 함수 | 계약 주석이 없는 부분과 단순 호출을 되풀이하는 설명이 혼재합니다. | 입력, 반환, 소유권, 부수효과를 한국어로 설명합니다. |

오류 래핑 정리는 원인 오류를 숨기거나 class를 낮추는 작업이 아닙니다. `errors.Is/As`, 마스킹, retry 힌트와 fatal 판정은 기존 동작 테스트로 보존합니다. 변경 코드는 해당 모듈의 Go 1.27 가이드를 적용하며, 이와 무관한 파일의 일괄 문법 변경은 하지 않습니다.

### 문자열 기반 경계 테스트 삭제

분석 당시 `youtubejscollector/import_boundary_test.go`는 소스에서 함수명과 테이블명 문자열을 검색했습니다(이번 구현에서 삭제). 금지 목록 중 `CollectNewPosts`와 `internal/runtime/pollers`는 분석 revision의 코드베이스에 존재하지 않으므로, 이 테스트는 제거된 이름의 재등장을 막는 grep guard에 해당합니다. 또한 `youtubejscollector`는 `sourceobservation`을 거쳐 `poller/runtime/batchrepo`에 간접 의존하지만(`go list -deps`로 확인) 테스트는 통과합니다. 즉 실제 보호 효과가 없습니다.

[stack AGENTS.md](../../../AGENTS.md)는 이런 grep guard를 금지하고 "delete the code instead"로 규정하며, 이 규칙은 이전 DEC나 감사의 "permanent contract" 표시보다 우선합니다. 따라서 이 테스트는 패키지 이관을 기다리지 않고 즉시 삭제합니다. 경계는 이관 후 Go internal 접근 제한으로 보장합니다. DB 권한, fence, publish 원자성, source observation 결과를 검증하는 제품 테스트는 유지합니다. 별도 검사기 self-test나 새 구조 budget을 만들지 않습니다.

## 권장 구현 순서와 완료 조건

| 순서 | 작업 | 의존성과 완료 조건 |
|---|---|---|
| 0 | 운영 로그에서 `helper_internal_invariant`와 scheduler fatal 빈도, 429 응답의 `Retry-After` 송신 여부 확인 | 해당 ops 스킬로 조회하며 코드 변경은 하지 않습니다. 결과로 1번의 긴급도와 Retry-After 착수 여부를 확정합니다. |
| 1 | helper 오류 분류 단일 소유자 도입, 기본 분기 복원, 세 fetcher의 탭 판정 통합, 본문 오류 분류 | 실제 `InnertubeError`·undici 형태의 fixture로 4xx, 탭 없음, 본문 오류, 취소, helper 자체 불변식 위반을 검증하고 PAG-004·PAG-011 fixture를 교체합니다. 일반 실패를 빈 성공으로 바꾸지 않고 live-check 물리 요청 상한을 유지합니다. |
| 2 | 문자열 경계 테스트 삭제, 재시도 DELAY 경로 삭제, providerhttp의 nil 성공 모양 분기 정리 | 오류 식별, retry 시각, fatal 판정 테스트가 유지되어야 합니다. |
| 3 | Retry-After 전달 | 0번에서 헤더 송신이 확인된 경우에만 진행합니다. 최대 cooldown 정책은 별도 결정에 따릅니다. |
| 4 | 결과 복사, wrapper, 중복 검증, 미사용 코드 정리 | 소유권과 aliasing 테스트, 오류 식별 및 metrics 계약이 유지되고 할당량을 비교할 수 있어야 합니다. |
| 5 | 공통 계약과 설정 로딩 경계 분리, publish와 consume 이관 | 연관 문서의 이관 계획과 담당 범위를 따릅니다. 이 문서의 보존 조건과 SERVICE_OWNERSHIP 분류 갱신을 함께 처리합니다. |
| 6 | discovery 및 요청 중복 최적화 | 큰 queue profile과 Holodex 요청 겹침이 실제로 확인된 경우에만 측정 후 적용합니다. |

같은 계약 변경을 이 문서와 연관 문서의 두 경로에서 각각 구현하지 않습니다. collector와 API 실행 구현을 분리한 뒤에 모듈 수 축소 여부를 검토하며, 현재 go.work나 모듈 경로를 이 작업의 선행 조건으로 바꾸지 않습니다.

구현 시에는 affected collector·shared·API 테스트와 해당 변경에 필요한 race·NilAway·아키텍처 검증을 실행합니다. runtime 또는 retry 정책 변경은 [workspace Failure paths](../../../.agents/workflows.md)의 단일 경로, 실패 결과 보존 및 예외 근거 규칙을 따릅니다. 이 문서 자체는 새로운 retry, fallback, DB 상태 변경, 운영 조회, 배포 또는 Git 게시의 승인이 아닙니다.

결정이 필요한 사항은 다음과 같습니다.

- helper가 인식하지 못한 upstream 오류에 줄 class. live-check처럼 `collection_failed/TRANSIENT`로 묶을지, fatal이 아닌 별도 표시를 둘지 정하며, helper 자체 프로그래밍 결함이 TRANSIENT 뒤에 숨지 않아야 합니다.
- 재시도 clamp를 앱 시계와 DB 시계 중 어디서 소유할지.
- 긴 Retry-After와 최대 cooldown의 관계.
- helper 종료 중 초기화 결과의 소유권.
- 필요성이 측정된 경우의 Holodex 요청 병합 취소·신선도 정책.

일반적인 파일 배치와 wrapper 삭제는 기존 계약 안에서 처리할 수 있습니다.

## 실제 검증 결과

아래 명령은 앞선 코드 분석 중 kapu의 Hololive 루트에서 실행했습니다. DB 관련 검증에서는 외부 test DB 환경 변수를 제거해 테스트 전용 provision 경로를 사용했습니다.

```sh
env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  go test ./hololive/hololive-youtube-collector/...

env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  go test ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation

npm --prefix hololive/hololive-youtube-collector/youtubejs test
npm --prefix hololive/hololive-youtube-collector/youtubejs run typecheck
```

| 검증 | 결과 |
|---|---|
| collector 전체 Go 테스트 | 통과, 일부 결과는 Go test cache 사용 |
| 공유 관측 계약과 저장소 테스트 | 통과, 계약 패키지는 cache 사용, 저장소 패키지는 실행 |
| YouTube.js 테스트 | 226개 통과, 실패·skip 없음(적대적 리뷰에서 다시 실행해 같은 결과 확인) |
| helper 타입 검사 | tsconfig.json 및 jsconfig.json 검사 통과 |
| 탭 없음 로컬 재현 | INTERNAL 500 반환 확인 |
| 본문 ECONNRESET 로컬 재현 | INTERNAL 500 반환 확인 |
| Retry-After 600 로컬 재현 | default 힌트로 손실 확인 |
| bootstrap 지연 후 drain 로컬 재현 | STOPPED에서 READY로 복원되고 200 반환 확인 |
| 실제 undici 본문 종료 로컬 재현 | `TypeError: terminated`, `cause.code=UND_ERR_SOCKET`, INTERNAL 500 확인 |
| 라이브러리 400·403·404 오류 분류 | 모두 `helper_internal_invariant` 확인 |
| `isMissingCommunity`의 실제 탭 메시지 판정 | `Tab "posts" not found`에 false 확인 |

적대적 리뷰에서는 위 재현 스크립트를 문서에서 그대로 추출해 다시 실행했고, 네 결과가 모두 같았습니다. Go 테스트는 리뷰에서 다시 실행하지 않았습니다. 기존 테스트 통과는 재현한 결함이 없다는 뜻이 아닙니다. 테스트 fixture의 실제 의존성 대응과 실패 경계의 누락을 함께 수정해야 합니다. 운영 트래픽, 장애 빈도, 부하 프로파일과 production build는 확인하지 않았으므로 실제 성능 개선율이나 운영 발생률은 제시하지 않습니다.

## 로컬 재현 방법

아래 코드는 Hololive 루트에서 실행합니다. 설치된 helper 의존성을 사용하며 실제 YouTube 요청, DB 접근 또는 파일 수정을 하지 않습니다. 실행 후 fetch 전역과 생성한 helper 자원을 정리합니다. 결과는 위 분석 revision의 동작을 기록한 것이며 수정 후에는 달라져야 합니다.

```sh
node --input-type=module <<'JS'
import { Utils } from './hololive/hololive-youtube-collector/youtubejs/node_modules/youtubei.js/dist/src/platform/node.js';
import Channel from './hololive/hololive-youtube-collector/youtubejs/node_modules/youtubei.js/dist/src/parser/youtube/Channel.js';
import { fetchContentFeed } from './hololive/hololive-youtube-collector/youtubejs/src/fetch-content.mjs';
import { handleContentRequest } from './hololive/hololive-youtube-collector/youtubejs/src/rpc-boundary.mjs';
import { createFetchTransport } from './hololive/hololive-youtube-collector/youtubejs/src/fetch-transport.mjs';
import { rpcErrorResultFor } from './hololive/hololive-youtube-collector/youtubejs/src/rpc-validation.mjs';
import { createHelperRuntime } from './hololive/hololive-youtube-collector/youtubejs/src/helper-runtime.mjs';

const channel = {
  getShorts: Channel.prototype.getShorts,
  getTabByURL: async () => { throw new Utils.InnertubeError('Tab "shorts" not found'); },
};
const result = await handleContentRequest(JSON.stringify({
  protocol_version: 1, channel_id: 'UC_TEST', kind: 'shorts',
  max_results: 10, max_pages: 1, max_success_response_bytes: 1048576,
}), options => fetchContentFeed({
  ...options, innertube: { getChannel: async () => channel },
}));
console.log('missing_tab', JSON.stringify(result));

const originalFetch = globalThis.fetch;
try {
  const transport = createFetchTransport({
    currentSignal: () => undefined, retryDelayMs: 0, observeRetry: () => {},
  });
  globalThis.fetch = async () => new Response('', {
    status: 429, headers: { 'Retry-After': '600' },
  });
  try {
    await transport.singleAttemptFetch('https://www.youtube.com/youtubei/v1/player', { method: 'POST' });
  } catch (error) {
    console.log('retry_after', JSON.stringify(rpcErrorResultFor(error)));
  }

  globalThis.fetch = async () => new Response(new ReadableStream({
    start(controller) {
      controller.error(Object.assign(new Error('body socket reset'), { code: 'ECONNRESET' }));
    },
  }), { status: 200 });
  try {
    const response = await transport.singleAttemptFetch(
      'https://www.youtube.com/youtubei/v1/browse', { method: 'POST' },
    );
    await response.text();
  } catch (error) {
    console.log('body_reset', JSON.stringify(rpcErrorResultFor(error)));
  }
} finally {
  globalThis.fetch = originalFetch;
}

let unblock;
let closed = 0;
const gate = new Promise(resolve => { unblock = resolve; });
const runtime = createHelperRuntime({
  createTransport: async () => {
    await gate;
    return { fetch: async () => new Response(), singleAttemptFetch: async () => new Response() };
  },
  createFetchers: () => ({ close: async () => { closed++; } }),
});
try {
  const pending = runtime.handleBootstrap(JSON.stringify({
    protocol_version: 1,
    limits: { request_body_bytes: 65536, response_body_bytes: 1048576, max_inflight: 1 },
  }));
  runtime.beginDrain();
  console.log('after_drain', runtime.state);
  unblock();
  const bootstrap = await pending;
  console.log('after_bootstrap', JSON.stringify({ state: runtime.state, status: bootstrap.status, closed }));
} finally {
  await runtime.closeResources();
}
JS
```

실제 undici 본문 종료와 라이브러리 4xx 오류 분류는 아래 코드로 재현합니다. 본문 종료 재현은 `127.0.0.1`의 임시 포트에 루프백 서버를 열고 실행 후 닫습니다.

```sh
cd hololive/hololive-youtube-collector/youtubejs && node --input-type=module <<'JS'
import http from 'node:http';
import { Utils } from 'youtubei.js';
import { rpcErrorResultFor } from './src/rpc-validation.mjs';
import { isMissingCommunity } from './src/fetch-community.mjs';

const server = http.createServer((req, res) => {
  res.writeHead(200, { 'content-type': 'application/json', 'content-length': '100000' });
  res.write('{"a":');
  setTimeout(() => req.socket.destroy(), 20);
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
try {
  const response = await fetch(`http://127.0.0.1:${server.address().port}/`);
  await response.text();
} catch (error) {
  console.log('undici_body', error.name, error.message, error.cause?.code,
    JSON.stringify(rpcErrorResultFor(error).body.error.code));
} finally {
  server.close();
}

for (const status of [400, 403, 404]) {
  const error = new Utils.InnertubeError(
    `Request to https://www.youtube.com/youtubei/v1/browse failed with status code ${status}`, '{}');
  console.log('library_status', status, JSON.stringify(rpcErrorResultFor(error).body.error.code));
}
console.log('community_tab_message', isMissingCommunity(new Error('Tab "posts" not found')));
JS
```
