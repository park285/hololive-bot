# 홀로봇 상세 리뷰와 리팩토링안

> 2026-10-15에 수행한 코드 리뷰와 후속 로컬 구현·검증의 기록입니다. 최초 리뷰 결과와 후속 실행 결과를 구분하며 운영 적용 완료를 뜻하지 않습니다. 런타임 소유권은 [PROJECT_MAP](../../current/PROJECT_MAP.md), 구현 경계는 [SERVICE_OWNERSHIP](../../current/SERVICE_OWNERSHIP.md), 외부·내부 계약은 [CONTRACT_MAP](../../current/CONTRACT_MAP.md)을 따릅니다.

## 결론과 범위

현재의 3개 런타임 구조를 유지하면서 관리자 쓰기 경계를 먼저 정리하는 것이 우선입니다. 수집·발송 경로에는 영속 큐, 소유권 검증, 결과 불명 처리와 관련 테스트가 갖춰져 있습니다. 관리자 API에서는 저장 후 실패 응답, 빈 멤버 생성, 로그 마스킹 누락, 폐기된 설정 필드 수용의 네 가지 문제를 재현했습니다.

검토 대상은 API의 bot/admin/LLM/YouTube plane, alarm-worker의 발송 경로, collector의 lease·실행·YouTube.js helper, 직접 연결된 shared 코드입니다. 핵심 처리 흐름 중심의 검토이며 전체 코드의 전수 감사는 아닙니다.

- 최초 리뷰 상태: 리뷰·문서화 완료, 당시 제품 코드 수정은 미착수였습니다. 이후 요청에 따른 구현과 검증 제한은 [후속 실행 기록](#후속-실행-기록)에 기록합니다.
- 검증 환경: kapu, Go 1.27.2, 로컬 Docker의 임시 PostgreSQL, miniredis 및 기존 테스트 대역입니다.
- 기준: 기존 미커밋 변경을 포함한 2026-10-15 작업 트리입니다. 문서화 중에도 다른 변경이 추가되어, 아래 결과를 최신 트리 전체 또는 단일 커밋의 검증으로 해석하지 않습니다.
- 최초 리뷰 문서화에서는 제품 테스트를 다시 실행하지 않았습니다. [리뷰 당시 실제 검증 결과](#실제-검증-결과)의 수치는 직전 리뷰에서 실행한 결과입니다.
- 최초 리뷰에서는 운영 DB 발생 빈도, 실제 Iris/Kakao 전달, 운영 성능을 측정하지 않았고 전체 local-ci.sh와 NilAway도 실행하지 않았습니다.
- 최초 리뷰 단계에서는 제품 소스 수정, 커밋, 원격 게시, 배포를 수행하지 않았습니다.

## 재현한 결함

### 멤버 저장 후 캐시 실패가 등록 실패로 응답됩니다

**근거:** [api_member.go](../../../hololive/hololive-api/internal/planes/admin/internal/server/api/api_member.go)의 AddMember는 PostgreSQL 등록 다음에 memberCache.Refresh를 호출합니다. 캐시 갱신이 실패하면 HTTP 500을 반환하고 활동 기록에는 도달하지 않습니다. [api_member_field_update.go](../../../hololive/hololive-api/internal/planes/admin/internal/server/api/api_member_field_update.go)의 필드 변경에도 저장 후 캐시 실패를 처리하는 동일한 형태가 있습니다.

**재현:** TestReviewMemberCacheFailurePreservesMutationEvidence에서 miniredis에 오류를 주입한 뒤 정상 멤버 등록 요청을 보냈습니다. 첫 응답은 500이지만 PostgreSQL에는 한 행이 저장됐습니다. 캐시 오류를 해제하고 같은 요청을 다시 보내도 500이었습니다. 생성 SQL이 기존 slug와 충돌하므로 동일 등록 요청으로 캐시 동기화를 재수행할 수 없습니다.

**영향:** 호출자가 저장 여부를 명확히 판단하기 어렵고, 일반 재시도로 복구되지 않습니다. 이미 커밋된 변경의 활동 기록도 빠집니다. 활동 기록 누락은 실행 순서에서 확인했으며 이 진단의 직접 assertion 대상은 응답·저장 행·재시도 결과입니다.

**수정 방향:** 저장 결과와 캐시 동기화 결과를 구분하는 MemberMutationResult를 서비스 경계에 둡니다. 저장 완료 사실은 캐시 실패와 관계없이 기록하고, 등록 재시도가 기존 요청과 일치하는지 확인한 뒤 동기화만 수행할 수 있게 합니다. 오류를 무시하거나 충돌을 무조건 성공 처리하지 않습니다. 응답 계약의 구체적인 표현은 Iris Console 소비자와 함께 확정합니다.

### 빈 요청으로 이름 없는 멤버를 등록할 수 있습니다

**근거:** 생성 API가 [domain.Member](../../../hololive/hololive-shared/pkg/domain/org_member.go)를 요청 타입으로 사용합니다. 이 타입에는 필수 입력 검증이 없고, [repository_mutation.go](../../../hololive/hololive-shared/pkg/service/member/repository_mutation.go)의 CreateMember도 nil 여부만 확인합니다. Name은 slug와 english_name에 함께 저장됩니다.

**재현:** TestReviewEmptyMemberRejected에서 빈 객체를 전달하자 HTTP 201을 반환하고 slug와 english_name이 모두 빈 문자열인 행 한 개를 저장했습니다.

**영향:** 표시명과 검색 식별자가 없는 데이터가 정상 등록됩니다. 채널·이름 정규화와 저장 폭 검증의 책임도 불명확합니다.

**수정 방향:** CreateMemberRequest를 분리하고 필수 이름, 공백 정규화, 저장 길이를 검증합니다. 현재 slug는 100자, english_name은 200자이므로 이름을 slug로 사용하는 계약까지 함께 고려합니다. 기존 선택 정보의 허용 범위는 유지합니다. 저장소 수준의 불변식 보강은 모든 호출자를 확인한 뒤 적용합니다.

### 방 이름이 활동 로그의 요약 문자열에 남습니다

**근거:** [settings_handler.go](../../../hololive/hololive-api/internal/planes/admin/internal/server/api/settings_handler.go)의 SetRoomName은 room_name 구조화 필드 외에 활동 summary에도 원문 이름을 삽입합니다. [activity/logger.go](../../../hololive/hololive-api/internal/service/activity/logger.go)의 Log는 summary를 그대로 출력합니다. 구조화 필드의 마스킹으로 문자열 안의 이름까지 제거되지는 않습니다.

**재현:** TestReviewRoomNameAbsentFromProductionLogs에서 실제 sharedlogging.NewLogger와 stdout 전용 ActivityLogger를 사용했습니다. 합성 방 이름 REVIEW_PRIVATE_ROOM_CANARY가 출력에 남았습니다. 실제 사용자 정보나 운영 로그를 검사한 것은 아닙니다.

**영향:** 구조화 필드가 마스킹돼도 같은 정보가 로그에 남습니다. 기존 관련 테스트는 방 ID를 검사하지만 방 이름은 검사하지 않습니다.

**수정 방향:** 요약은 고정 문구로 만들고 식별 정보는 마스킹 가능한 구조화 필드로 전달합니다. 실제 로깅 경로의 최종 출력에 방 이름이 남지 않는 회귀 검사를 추가합니다. 활동 이벤트 타입을 도입하면 신규 호출자가 임의 문자열에 식별 정보를 삽입하는 일을 줄일 수 있습니다.

### 폐기된 설정 필드를 보내도 변경 성공으로 응답합니다

**근거:** [api_deps.go](../../../hololive/hololive-api/internal/planes/admin/internal/server/api/api_deps.go)의 bindJSON은 알 수 없는 JSON 필드를 거부하지 않습니다. 알려진 설정 필드가 없으면 변경 없는 요청으로 처리됩니다.

**재현:** TestReviewRetiredSettingRejected에서 scraperProxyEnabled=true만 전달하자 HTTP 200과 Settings updated를 반환했습니다. 실제 설정은 바뀌지 않았습니다. 폐기된 필드를 거부한다는 설정 코드의 설명과 동작이 다릅니다.

**영향:** 구형 클라이언트나 필드명 오타가 성공으로 처리되어 설정 변경 여부를 오인할 수 있습니다.

**수정 방향:** 우선 설정 요청 경계에 RejectUnknownMembers(true)를 적용합니다. 공통 bindJSON을 일괄 변경하면 다른 API의 기존 허용 동작까지 달라지므로 경로별 계약과 소비자를 확인하며 확대합니다.

## 구조 평가

아래 내용은 위의 재현 결함과 구분되는 개선 제안입니다. 파일 길이만으로 결함을 판정하거나 성능 개선을 입증한 것은 아닙니다.

| 영역 | 확인한 구조 | 개선 방향 |
|---|---|---|
| 런타임 소유권 | Collector가 수집·발행, API가 정본 저장·알림 의도, Worker가 발송을 소유합니다. | 현재 소유 경계를 유지합니다. |
| Bot 영속 처리 | inbox/outbox, claim token, 결과 확정과 종료 예산이 명시적입니다. | 복구 계약을 유지하면서 조립과 실행 정책의 경계를 정리합니다. |
| 관리자 API | 도메인별 핸들러가 큰 공통 Handler를 내장해 전체 의존성을 공유합니다. | 각 핸들러에 필요한 서비스만 생성자로 전달합니다. |
| YouTube 발송 | 행·outbox·claim token·요청이 여러 인자와 병렬 배열로 전달됩니다. | 한 발송 시도의 데이터를 묶어 관계를 타입으로 표현합니다. |
| Collector | lease 감독, 수집, 발행, 실패 판정, 지표 기록이 실행기에 모여 있습니다. | 실패 판정과 부수효과 실행을 분리합니다. |
| JavaScript helper | 엄격한 검증이 있지만 rpc-validation.mjs에 여러 프로토콜 책임이 집중됩니다. | 공통 RPC 처리와 요청·응답별 검증을 분리합니다. |
| LLM 뉴스 생산 | enqueue 성공을 내부 SendResult.Sent와 sent 로그로 표현합니다. | 내부 결과와 로그를 Queued 중심으로 명확히 합니다. 외부 trigger 결과가 실제 발송 완료를 주장한다는 뜻은 아닙니다. |

주요 검토 지점은 [관리자 핸들러 구성](../../../hololive/hololive-api/internal/planes/admin/internal/server/api/api_domains.go), [발송 실행](../../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/send_engine_send.go), [Collector 실행기](../../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler_run.go), [RPC 검증](../../../hololive/hololive-youtube-collector/youtubejs/src/rpc-validation.mjs), [뉴스 enqueue·로그](../../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/scheduler/digest_helper.go)입니다.

세 실행 파일의 go list -deps 결과에서 도달하는 hololive-shared 패키지는 68개였습니다. 이 중 62개는 둘 이상의 실행 파일에서 사용됐고, API에서만 도달한 나머지 6개는 irisrooms·majorevent·membernews·settings·subscription·trigger 계약 패키지였습니다. 외부 소비자까지 조사한 수치는 아니므로 단일 실행 파일에서만 도달한다는 이유로 계약을 이동하거나 삭제하지 않습니다.

## 후속 리팩토링안

목표는 관리자 쓰기 결과의 신뢰성을 높이고 발송·수집 코드에서 함께 유지해야 하는 상태를 명확히 하는 것입니다. 아래 작업은 제안이며 구현 완료나 실행·배포 승인을 뜻하지 않습니다. 착수할 때 현재 코드에서 재현 여부와 진행 중인 다른 수정의 범위를 다시 확인합니다.

| 순서 | 작업 | 의존성과 완료 기준 |
|---|---|---|
| 1 | 생성 요청 DTO, 설정 검증, 활동 로그 수정 | 각 항목은 독립적으로 적용할 수 있습니다. 빈 멤버·폐기 필드를 거부하고 실제 로깅 출력에 방 이름이 남지 않아야 합니다. |
| 2 | MemberAdminService로 저장·캐시 동기화·활동 기록의 책임 정리 | 재시도 식별과 소비자 응답 계약을 먼저 확정합니다. 저장 후 캐시 실패를 구분하고, 동일 요청의 복구에서 새 행이 생기지 않으며 이미 저장된 사실이 기록돼야 합니다. |
| 3 | 관리자 도메인 핸들러의 의존성 축소 | 2단계의 멤버 서비스 경계를 활용합니다. 멤버 핸들러가 ACL·Iris·LLM의 무관한 의존성 없이 구성·검증돼야 합니다. |
| 4 | 발송 시도 데이터와 결과 전달 단순화 | 관리자 변경과 독립적입니다. 단건·묶음 발송의 고정 요청, claim 검증, 결과 불명 처리를 보존해야 합니다. |
| 5 | Collector 실패 판정과 JS 검증 모듈 분리 | Go와 JS는 독립된 변경으로 나눌 수 있습니다. 취소·lease 상실·부분 결과·오류 분류·응답 크기 제한의 동작을 보존해야 합니다. |

발송 시도 객체는 row, outbox, claim 정보와 고정 요청의 관계를 나타내도록 하고, 실제 필요한 범위부터 적용합니다. 각 발송 원장의 상태전이, 영속 JSON, 멱등 키와 요청 generation은 그대로 유지합니다. 수집 실패 판정은 기존 오류·소유권 증거에서 행동을 결정하는 순수 함수로 추출하고, 새로운 retry/fallback 경로를 암묵적으로 추가하지 않습니다.

검증은 각 수정의 회귀 테스트와 기존 관련 패키지 테스트를 우선합니다. 저장 후 캐시 실패, 재시도, 응답 유실, stale claim, 단건·묶음 발송, 취소·lease 갱신 실패·callback join 경계를 포함합니다. 제품 코드 변경 시 적용되는 Stage 3·NilAway·race 등 기존 필수 검증은 유지합니다. JS 변경은 npm test와 typecheck로 검증합니다.

응답 계약 변경은 Iris Console 소비자와 함께 확인해야 하며 breaking change는 별도 승인이 필요합니다. DB 제약·마이그레이션이 필요하면 기존 데이터 영향을 먼저 검토합니다. 원격 게시·운영 적용·신규 런타임 의존성은 이번 문서화 범위에 포함되지 않습니다.

## 후속 실행 기록

### 적용 범위와 결정

후속 요청으로 1~5단계와 LLM 뉴스 결과 명명 개선을 작업 트리에 적용했습니다. 구현 4개 에이전트와 독립 리뷰 2개 에이전트를 사용했고 통합 소유자가 검사·실제 경로 smoke·문서 갱신을 수행했습니다. **제품 구현과 표적 검증은 완료했지만 전체 품질 게이트는 미통과입니다.** 아래 기존 작업 트리 진단을 남긴 채 배포·게시 가능 상태로 판정하지 않습니다.

| 작업 | 적용 내용과 확인한 결과 |
|---|---|
| 1. 생성·설정·로그 경계 | `member_request.go`의 DTO가 공백 이름과 저장 폭 초과를 거부합니다. 선택 입력은 보존하고 Unicode 문자 수로 검증합니다. 설정/LLM 설정에서만 unknown JSON 필드를 거부합니다. 방 이름은 고정 summary와 가명화된 구조화 필드로 기록하며 실제 운영 logger stdout·파일 경로의 회귀 검사를 통과했습니다. |
| 2. 멤버 서비스 | `member_admin_service.go`가 모든 멤버 변경의 저장·활동·캐시 순서를 소유합니다. 기존 생성 201/변경 200에 `persisted`, `cacheSynced`, `memberId`, `replayed`를 추가했습니다. slug 충돌 후 모든 저장 필드가 같을 때만 캐시 동기화를 복구하고, 불일치는 409로 거부합니다. |
| 3. 도메인 의존성 | 모든 도메인 핸들러에서 공통 `*Handler` 내장을 제거했습니다. 멤버 핸들러는 DB·멤버 서비스·logger만으로 구성됩니다. 통합 검사에서 발견한 X Spaces 메서드의 설정 핸들러 이관 누락도 수정했습니다. |
| 4. 발송 시도 | `delivery_attempt.go`의 `claimedDelivery`와 `deliveryAttempt`로 행·outbox·claim·고정 요청·시작 operation의 관계를 묶었습니다. 배치 결과가 mutex를 소유하며 단건·묶음·저장 요청 복구·기존 fallback 호출자를 함께 전환했습니다. 영속 JSON·generation·결과 불명·claim 해제 계약은 유지합니다. |
| 5. 수집·JS | `scheduler_decision.go`가 실패 행동과 감독 결과를 순수 판정하며 executor가 부수효과를 실행합니다. JS의 공통 실행·오류·검증 primitive·collection·live-check 검증을 5개 모듈로 나누고 기존 umbrella와 모든 import를 정리했습니다. |
| LLM 뉴스 | 멤버 뉴스 내부 결과를 `digestEnqueueResult.Queued`, 결과 로그를 `queued`로 바꿨습니다. 다른 runtime의 공용 `SendResult`와 외부 trigger 계약은 그대로입니다. |

### 실제 검증

모든 DB 검사는 외부 `TEST_DATABASE_URL`, `TEST_DATABASE_OWNER_TOKEN`, `ALLOW_EXTERNAL_TEST_DB`를 제거하고 `DOCKER_HOST=unix:///var/run/docker.sock`의 임시 PostgreSQL을 사용했습니다. 원문에 이미 보고된 네 결함은 재확인 목적으로 다시 실행하지 않았고, 수정된 계약을 회귀 검사와 smoke로 검증했습니다.

| 검사 | 후속 실행 결과 |
|---|---|
| admin 전체·API app·YouTube dispatch 하위 패키지 통합 테스트 | 15개 패키지 통과. shared member 테스트도 통과 |
| 실제 HTTP 멤버 생성 + PG + 캐시 장애 smoke | 빈 요청 400, 저장 후 캐시 실패 201/`persisted=true`/`cacheSynced=false`, 동일 요청 복구 201/`replayed=true`/동기화 성공, 불일치 409. 정본 1행·활동 1건 유지 |
| 실제 HTTP 생성 응답 유실 smoke | upstream의 저장 완료 201을 받은 중계 연결을 응답 전 닫았습니다. 호출자는 응답을 잃었지만 동일 요청 재전송이 기존 ID로 201을 반환하고 정본 1행·활동 1건을 유지했습니다. |
| Console 실제 `Contract.ProjectResponse` smoke | 생성 201 및 이름·졸업·별명 변경 200의 기존 envelope와 추가 결과 필드를 수락 |
| 뉴스 실제 영속 outbox smoke | 주간 동일 요청 2회와 월간 1회 뒤 `PENDING` 2행, `queued=1` 결과 로그 3건. 발송 완료를 주장하지 않음 |
| YouTube.js | 366개 테스트 통과, 실패·skip 0; typecheck 통과 |
| JS 실제 loopback HTTP helper smoke | bootstrap/READY 및 정상 수집 200, unknown 필드 400, 응답 byte 한도 422, 원천 실패 502 확인 |
| Collector production mode | `CGO_ENABLED=0` JSON 테스트와 `build-prod` 아티팩트 검증 통과 |
| 표적 race | collection·joblease·collectorruntime 통과. 아래 4개 모듈 전체 race에서도 변경 대상 admin·member·membernews scheduler·youtubedispatch·collectorruntime 패키지는 통과 |
| 4개 영향 모듈 전체 race | **실패**. shared Holodex의 `TestHandleServerError_CircuitOpenStopsRetry`가 요청 3회 기대에 5회를 관측했습니다(`api_client_test.go:389`). 해당 경로는 이번 변경 대상이 아니며 재실행으로 실패를 지우지 않았습니다. |
| admin·membernews scheduler·shared member golangci-lint | **0 issues** |
| 직접 변경 대상 NilAway | admin 전체·membernews scheduler·shared member·youtubedispatch 전체·collectorruntime 통과 |
| 독립 리뷰 | 최종 유효 지적 없음. Go 1.27 promoted-field literal을 구형 문법으로 판단한 후보는 실제 컴파일 결과와 버전 지침으로 반박했고 리뷰어가 철회했습니다. |

miniredis fixture는 구독과 일반 명령을 같은 RESP3 연결에서 처리할 때 발생한 `INCR` 거부를 확인하여 `AlwaysRESP2`로 구독 연결을 분리했습니다. 제품 캐시 정책은 바꾸지 않았습니다. 동시 생성의 epoch 경합으로 warmup이 무효화될 수 있으므로 각 응답의 `cacheSynced=false`를 보존하고, 경합 종료 후 동일 요청이 새 행 없이 동기화를 복구하는 것을 검사합니다.

### 전체 게이트 차단 사항과 한계

- `scripts/ci/local-ci.sh`는 architecture의 SQL ownership 단계에서 중단됐습니다. 기존 `closingprobe`·`closingclock`·`closing_probe.go`의 인라인 SQL 및 `notificationdelivery/queries/outbox_repository_0279_09.sql`의 `count(*)` 진단입니다. 검사 설정을 약화하거나 기존 probe를 삭제하지 않았습니다.
- 전체 대상 golangci-lint에는 기존 `send_engine_settlement_test.go`, `closing_probe.go`, `payload_size_rpc_test.go`, `provider_attempt_test.go`, `channel_live_slot_test.go`, `content_tab_rpc_test.go`, `scheduler_test.go`, `publisher_test.go`의 진단이 남습니다. 기존 사용자 테스트/probe의 private API 호출은 필요한 범위만 이관했고, 무관한 테스트 구조·probe 동작은 정리하지 않았습니다. 이번 구현 파일의 새 진단은 수정했습니다.
- 4개 모듈 전체 NilAway는 `hololive-alarm-worker/internal/closingclock/main.go:51`, `hololive-shared/cmd/closingprobe/main.go:64`, `hololive-youtube-collector/internal/runtime/sourceobservation/repository_publish_stream_test.go:38,43`의 기존 nil 흐름 진단으로 실패했습니다.
- 전체 pinned staticcheck는 `hololive-api/internal/planes/llm/closingprobe/main.go:25`의 `panicCause` 명명 진단(ST1012)으로 실패했습니다. 따라서 Stage 3/prerequisites·전체 local CI 완료로 간주하지 않습니다. 위 별도 작업 트리 진단의 정리 범위 확정과 해당 검사 재검증이 남습니다.
- Console은 추가 멤버 결과 필드를 projection에서 제거합니다. 기존 응답 수락은 검증했으나 캐시 상태를 화면에서 표시하는 변경·시각 검증은 하지 않았습니다.
- 활동 기록은 기존 logger sink이며 DB commit과 기록 사이의 프로세스 종료까지 원자성을 새로 보장하지 않습니다. 운영 DB·실제 Iris/Kakao 전달·운영 성능·운영 Valkey 장애는 검사하지 않았습니다.
- Fallback delta: 없음. 자동 재시도·대체 원천을 추가하지 않았습니다. 동일 멤버 생성 복구는 기존 slug의 유일성 증거와 전체 입력 일치에 한정한 호출자 재요청 처리입니다.
- 의존성·마이그레이션 추가, 커밋, 원격 게시, 운영 적용은 수행하지 않았습니다. 임시 smoke/overlay는 검증 후 제거했습니다.

주요 실행 명령은 다음과 같습니다. 임시 overlay smoke는 위 시나리오를 실제 HTTP·DB·queue 표면에서 실행한 일회성 검사이며 제품 suite에는 영구 회귀 검사만 남깁니다.

~~~sh
env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  DOCKER_HOST=unix:///var/run/docker.sock \
  go test -p 3 -count=1 -timeout 180s \
  ./hololive/hololive-api/internal/planes/admin/... ./hololive/hololive-api/internal/app \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/...

env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  DOCKER_HOST=unix:///var/run/docker.sock \
  go test -race -p 3 -count=1 -timeout 240s \
  ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-shared/... ./hololive/hololive-youtube-collector/...

npm --prefix hololive/hololive-youtube-collector/youtubejs test
npm --prefix hololive/hololive-youtube-collector/youtubejs run typecheck

env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  DOCKER_HOST=unix:///var/run/docker.sock \
  bash scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector test-prod
bash scripts/ci/public-pr-go-gate.sh hololive/hololive-youtube-collector build-prod

env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  DOCKER_HOST=unix:///var/run/docker.sock RACE_TEST_PARALLEL=3 \
  bash scripts/ci/local-ci.sh
~~~

## 실제 검증 결과

| 검사 | 리뷰 당시 결과 |
|---|---|
| 선정한 Go 23개 패키지의 기존 테스트 | 통과 |
| API app·Bot runtime·notificationdelivery·collectorruntime의 race 검사 | 4개 패키지 통과 |
| YouTube.js npm test | 362개 통과, 실패·skip 0 |
| X Spaces npm test | 10개 통과, 실패·skip 0 |
| YouTube.js typecheck | 통과 |
| 임시 overlay의 네 재현 검사 | 모두 기대 동작 assertion 실패로 위 결함을 검출 |

재현 검사의 실패는 기존 suite 전체가 실패했다는 뜻이 아닙니다. 생성·설정·로그·복구의 문제 동작을 드러내기 위해 별도로 실행했습니다.

아래 명령은 저장소 루트에서 실행했습니다. 외부 TEST_DATABASE_URL을 제거하고 로컬 Docker를 명시하여 테스트용 PostgreSQL만 사용했습니다.

~~~sh
env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  DOCKER_HOST=unix:///var/run/docker.sock \
  go test -p 3 -timeout 180s \
  ./hololive/hololive-api/internal/app \
  ./hololive/hololive-api/internal/config \
  ./hololive/hololive-api/internal/planes/bot/runtime \
  ./hololive/hololive-api/internal/planes/bot/internal/durability \
  ./hololive/hololive-api/internal/youtube/sourceobservation \
  ./hololive/hololive-alarm-worker/internal/egress/notificationdelivery \
  ./hololive/hololive-alarm-worker/internal/egress/sendoutcome \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch/... \
  ./hololive/hololive-youtube-collector/internal/runtime/collection \
  ./hololive/hololive-youtube-collector/internal/runtime/joblease \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime

env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  DOCKER_HOST=unix:///var/run/docker.sock \
  go test -p 3 -timeout 180s \
  ./hololive/hololive-api/internal/planes/admin/internal/server/api \
  ./hololive/hololive-api/internal/planes/llm/internal/schedulerkit \
  ./hololive/hololive-api/internal/planes/llm/internal/service/membernews/... \
  ./hololive/hololive-shared/pkg/service/member

env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  DOCKER_HOST=unix:///var/run/docker.sock \
  go test -race -p 2 -timeout 180s \
  ./hololive/hololive-api/internal/app \
  ./hololive/hololive-api/internal/planes/bot/runtime \
  ./hololive/hololive-alarm-worker/internal/egress/notificationdelivery \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime

npm --prefix hololive/hololive-youtube-collector/youtubejs test
npm --prefix hololive/hololive-alarm-worker/xspaces test
npm --prefix hololive/hololive-youtube-collector/youtubejs run typecheck
~~~

## 재현 코드 보존

원래 임시 파일은 /home/kapu/.cache/tmp/holobot-review-hm5_zc_9/review_test.go와 같은 디렉터리의 overlay.json입니다. 임시 파일의 존속에 의존하지 않도록 진단 코드를 아래에 보존합니다. 원본에서 gofmt에 의한 서식만 정리했습니다. 기존 패키지의 테스트 helper를 사용하므로 후속 구조 변경 시 현재 helper와 맞춰야 합니다.

이 코드는 진단 스냅샷입니다. 특히 저장 후 캐시 실패 검사는 당시의 문제 조합을 검출하는 검사이므로, 정식 회귀 테스트로 편입할 때는 합의된 응답 계약·기존 행 확인·동기화 복구·감사 기록을 명시적으로 검증하도록 보강해야 합니다. stdout 교체 검사에는 t.Parallel을 추가하지 않고 TestReview만 선택해 실행합니다.

다음 코드 블록을 임시 디렉터리의 review_test.go에 저장한 뒤, 아래 절차로 checkout 변경 없이 실행할 수 있습니다.

~~~go
package api

import (
	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-api/internal/service/activity"
	dbtest "github.com/kapu/hololive-dbtest"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/member"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/valkey-io/valkey-go"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewRetiredSettingRejected(t *testing.T) {
	store := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger())
	h := &SettingsHandler{Logger: newDiscardLogger(), Settings: store, SettingsApplier: testSettingsApplier{}}
	c, rec := newSettingsTestContext(t, []byte(`{"scraperProxyEnabled":true}`))
	h.UpdateSettings(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("retired setting HTTP=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReviewRoomNameAbsentFromProductionLogs(t *testing.T) {
	const canary = "REVIEW_PRIVATE_ROOM_CANARY"
	output, err := os.CreateTemp(t.TempDir(), "stdout-")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	prior := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = prior }()
	logger := sharedlogging.NewLogger()
	h := &SettingsHandler{Logger: logger, Activity: activity.NewActivityLogger("", logger), Alarm: &roomNameRecordingAlarm{}}
	c, rec := newSettingsTestContext(t, []byte(`{"roomId":"1234567","roomName":"`+canary+`"}`))
	h.SetRoomName(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected HTTP=%d", rec.Code)
	}
	raw, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), canary) {
		t.Fatal("production sanitizer retains room name in activity summary")
	}
}

func newReviewMemberHandler(t *testing.T, epoch *cachemocks.Client) (*MemberHandler, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repo := member.NewMemberRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)
	var cached *member.Cache
	var err error
	if epoch == nil {
		cached, err = member.NewMemberCache(t.Context(), repo, nil, logger, member.CacheConfig{})
	} else {
		cached, err = member.NewMemberCache(t.Context(), repo, epoch, logger, member.CacheConfig{EpochReconcileInterval: time.Hour})
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cached.Close)
	return &MemberHandler{Handler: &Handler{repository: repo, memberCache: cached, logger: logger}}, pool
}

func TestReviewEmptyMemberRejected(t *testing.T) {
	h, pool := newReviewMemberHandler(t, nil)
	c, rec := newAPITestContext(http.MethodPost, "/api/holo/members", []byte(`{}`))
	h.AddMember(c)
	var rows int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM members WHERE slug='' AND english_name=''").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest || rows != 0 {
		t.Fatalf("empty member HTTP=%d, persisted blank rows=%d; want 400,0", rec.Code, rows)
	}
}

func TestReviewMemberCacheFailurePreservesMutationEvidence(t *testing.T) {
	mini := miniredis.RunT(t)
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{mini.Addr()}, DisableCache: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	h, pool := newReviewMemberHandler(t, &cachemocks.Client{GetClientFunc: func() valkey.Client { return client }})
	mini.SetError("ERR review injected cache failure")
	body := []byte(`{"name":"Review Cache Commit Member"}`)
	c, rec := newAPITestContext(http.MethodPost, "/api/holo/members", body)
	h.AddMember(c)
	var rows int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM members WHERE slug='Review Cache Commit Member'").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	mini.SetError("")
	second, again := newAPITestContext(http.MethodPost, "/api/holo/members", body)
	h.AddMember(second)
	t.Logf("first HTTP=%d, committed rows=%d; same-request retry HTTP=%d", rec.Code, rows, again.Code)
	if rec.Code == http.StatusInternalServerError && rows == 1 && again.Code == http.StatusInternalServerError {
		t.Fatal("creation committed before cache failure; same-request retry cannot recover")
	}
}
~~~

임시 디렉터리 경로를 인자로 전달해 overlay를 만듭니다. 가상 대상 파일이 이미 있으면 다른 미사용 이름을 선택합니다.

~~~sh
python3 - /absolute/path/to/review-temp <<'PYTHON'
import json
import sys
from pathlib import Path

probe_dir = Path(sys.argv[1]).resolve()
virtual = Path.cwd() / 'hololive/hololive-api/internal/planes/admin/internal/server/api/review_overlay_test.go'
assert not virtual.exists(), 'choose an unused virtual test filename'
assert (probe_dir / 'review_test.go').is_file()
(probe_dir / 'overlay.json').write_text(json.dumps({
    'Replace': {str(virtual): str(probe_dir / 'review_test.go')}
}))
PYTHON

env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  DOCKER_HOST=unix:///var/run/docker.sock \
  go test -overlay=/absolute/path/to/review-temp/overlay.json \
  -count=1 -timeout 120s \
  ./hololive/hololive-api/internal/planes/admin/internal/server/api \
  -run '^TestReview' -v
~~~
