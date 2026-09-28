# Hololive Valkey 선택적 축소 빅뱅 fadeout 계획

**Decisions:** `DEC-20260928-hololive-valkey-bigbang-fadeout` (governing), `DEC-20260731-legacy-fade-out-no-dual-path` (constraint)

## Execution capsule

**Goal:** 필요한 Valkey 기능을 유지하고, 두 중복 기능의 구 코드·연결·설정 소비를 하나의 완결된 패치에서 제거한다.
**Context:** 제거 집합은 membernews room mirror와 hololive:members이다. 이전 B1/B2 중간 writer 릴리스와 exported 구 API 보존안은 폐기한다.
**Constraints:** 현재는 재감사·플랜만 작성한다. 새 코드에 dual path·호환 shim·no-op API를 남기지 않는다. 보호용 Valkey와 PG durable state는 보존한다.
**Evidence:** `docs/review/2026-09-28-valkey-bigbang-fadeout-audit.md`, `docs/review/2026-09-28-alarm-membernews-valkey-audit.md`, 앞선 리뷰와 코드·테스트·runbook. 실행·운영 검증은 미수행이다.
**Success:** 제거 기능의 활성 생산·소비·선택 경로가 0이고, 유지 기능 회귀와 전체 모듈 검증이 통과한다. runtime 전환·data residue 회수는 별도 증거로 기록한다.
**Output:** 빅뱅 패치 명세·감사 보고·proposed DEC. 후속 구현은 한 revision과 대응하는 검증 근거로 제출한다.

## 이번 개정과 범위

사용자의 두 요구를 함께 적용합니다. 필요한 공유 임시 상태와 유의미한 캐시는 Valkey에 유지합니다. 제거하기로 정한 기능은 새로운 설계를 도입하는 지점에서 완전히 종료합니다.

[빅뱅 감사](../../review/2026-09-28-valkey-bigbang-fadeout-audit.md)는 직전 플랜에서 F01~F10을 확인했습니다. [선택적 유지 리뷰](../../review/2026-09-28-valkey-selective-retention-review.md)와 [실행 준비 리뷰](../../review/2026-09-28-valkey-implementation-readiness-review.md)의 정적 사실은 참고하되, 전환 방식과 완료 기준은 이 문서를 따릅니다.

[알람·멤버 뉴스 심층 감사](../../review/2026-09-28-alarm-membernews-valkey-audit.md)의 N01~N09를 반영했습니다. D1/D2 범위는 같으며 뉴스 실행 잠금·PG 발송 경로·구독 간 연결을 유지 명세와 B19~B27 검증으로 구체화합니다.

**패치는 하나입니다.** reader만 바꿔 배포하고 writer는 다음 릴리스에 지우는 B1/B2는 사용하지 않습니다. 구현 중 의존 순서에 따른 편집·테스트는 가능하지만 최종 merge/build 대상에는 아래 제거 집합이 모두 포함되어야 합니다. 일부만 완료하고 빅뱅 fadeout 완료라고 표시하지 않습니다. 범위를 줄여야 한다면 사유와 제거 집합부터 다시 확정합니다.

2026-09-28 HEAD `3be7229b060b`와 dirty 작업 트리를 읽었습니다. 운영 artifact는 이후 clean reviewed revision으로 준비해야 합니다. 이전 T11~T17/AC11~AC18/V11~V16은 단계적 패킷 의미였으므로 새 작업에는 T31 이후의 marker를 사용합니다. 기록·코드 수정 이력과 실제 완료를 혼동하지 않습니다.

## 제거 집합과 유지 집합

| 집합 | 포함 기능 | 패치 종료 상태 |
|---|---|---|
| D1 | `membernews:rooms`, `membernews:room_names` mirror | repository writer·warmup·전용 cache 의존·전용 API/테스트·상수가 없음 |
| D2 | `hololive:members` 중복 hash와 이를 읽는 readiness/조회 기능 | runtime reader/writer·backend API/interface/mock·동적 matcher helper·전용 CLI/env가 없음 |
| K1 | 세션·reset·임시 계정·서명 nonce·공유 rate limit | 기존 Valkey와 TTL·원자성·실패 계약 유지 |
| K2 | member epoch/L2, 설정·ACL Pub/Sub, alarm wakeup, 뉴스 주간·월간 실행 잠금 | 기존 조율 및 복구 계약 유지. `membernews:lock:weekly:*`·`monthly:*` 보존 |
| K3 | API/LLM 결과 cache·월간 경고 누계·알림 index/사전 claim | 현재 역할 유지. 측정 없이 메모리/PG로 전환하지 않음 |
| K4 | ACL 값 mirror, `alarm:member_names` | 이번 제거 집합에서 제외하고 그대로 유지 |
| K5 | Valkey server/client/config/auth/socket/readiness, PG ledger | 계속 필요한 공용 인프라와 영속 정본 보존 |

K4는 이름만 바꾼 임시 호환 경로가 아닙니다. ACL mirror 제거는 지속적인 Valkey 장애에서 rollback되던 권한 변경을 PG 성공+통지 실패로 바꾸고, 알림 이름은 `ShortKoreanName → NameKo → Name`과 구독 fallback 등 별도 의미를 가집니다. 이 설계·검증이 해결되지 않은 상태에서 빅뱅이라는 이유로 포함하지 않습니다.

PG는 dispatch pending/retry/lease/sending/terminal의 정본입니다. 이 패치는 outbox identity·retry·quarantine·unknown-send·schema를 변경하지 않습니다. Valkey 자체가 없어도 홀로봇 전체가 실행된다는 목표는 두지 않습니다.

### 알람과 뉴스의 연결 경계

뉴스 정기 수신 방은 `member_news_subscriptions`, 관심 멤버는 `alarms LEFT JOIN members`, 소식 후보는 `major_events`의 PG 조회입니다. 수동 뉴스 생성은 정기 구독 여부를 선행 검사하지 않습니다. 관심 멤버 SQL은 LIVE 타입만 고르지 않으며 이름 우선순위·이름 기준 DISTINCT를 보존합니다. 뉴스 구독 해지는 알람 등록이나 이미 enqueue된 메시지를 취소하지 않습니다.

뉴스 잠금은 15분 TTL과 token 비교 해제이며 Valkey 오류 시 실행을 계속합니다. 알람 사전 claim은 Valkey 오류 시 확보 실패로 해당 준비 건을 건너뜁니다. 서로 다른 실패 의미를 통일하거나 cache=nil로 잠금을 비활성화하지 않습니다. `llm:cost:tokens:*`는 경고용 누계이며 호출을 차단하는 예산 한도가 아닙니다.

뉴스 enqueue는 기존 handoff 설정에 따라 `notification_delivery_outbox` 또는 alarm dispatch ledger를 사용합니다. 실제 운영값은 미확인이며 이번에 mode/default/executor를 바꾸지 않습니다. 같은 기간이라도 dispatch digest의 본문 hash가 바뀌면 identity가 달라질 수 있으므로 잠금 제거를 PG dedup만으로 정당화하지 않습니다. 스케줄러의 `Sent`는 enqueue 성공이며 실제 발송 결과는 worker에서 검증합니다.

알람 표시 이름·index·wakeup·토큰 누계는 현재 유지하되 영구적인 Valkey 필수성으로 판정하지 않습니다. 추후 축소에는 이름 의미·모든 reader·DB 부하·polling 지연·공유 관측 대체 근거가 필요합니다. 상세 근거와 실패 행렬은 N01~N09를 따릅니다.

## 완전 제거 명세

아래 `shared`, `api`, `worker`는 `hololive/hololive-shared`, `hololive/hololive-api`, `hololive/hololive-alarm-worker`입니다. 모든 행이 한 변경 집합에 속합니다.

| 항목 | 정확한 제거·교체 대상 | 남겨야 할 경계 |
|---|---|---|
| D1 repository | `api/.../membernews/repository.go`의 mirror 상수·cache 필드/constructor 인자, `repository_cache.go`의 clear/load/write-through, `repository_mutation.go` 호출 | PG query·정렬·idempotency·오류 |
| D1 startup API | `Service.WarmupSubscriptionCache`, 해당 repository `WarmupCacheFromDB`, bootstrap의 구 호출과 API guard 테스트 | bootstrap에서 기존 `ListSubscribedRooms` 1회와 warning-only 확인을 직접 사용. 같은 함수의 LLM cost tracker cache는 유지 |
| D2 producer | `shared/pkg/providers/member_providers.go`의 `initializeMemberDatabaseFromSnapshot`·`initializeMemberDatabase`와 호출·전용 snapshot interface | 실제 `member.Cache` 생성·epoch/L2 warmup·cleanup |
| D2 backend | `shared/pkg/service/cache/member.go`, `member_cache.go`의 `cache.MemberCache`, `Service.InitializeMemberDatabase/GetAllMembers/GetMemberChannelIDWithOrg/GetMemberChannelIDs`와 전용 helper | 정상 멤버 domain/repository·adapter API는 유지 |
| D2 interface | `cache.DomainCache`의 `MemberCache` embedding | `StreamCache`와 공용 `cache.Client`의 KV/hash/set/CAS/connection/low-level 기능 |
| D2 mock | `cache/mocks/client.go`의 member 전용 function field/assertion과 `client_domain.go`의 해당 methods | stream·CAS 등 유지 기능 mock. 파일 전체를 지우지 않음 |
| D2 matcher | `dynamicLoadErr`, `storeDynamicSnapshotMembers`, `snapshotEntryFromDynamic`, `splitMemberKey`, `tryExactValkeyMatch`, `tryPartialValkeyMatch`, `loadDynamicMembers`, `candidateFromDynamic`, `preferHololiveCandidate`와 전용 branch | 정상 snapshot·별칭·조직·후보 우선순위·1분 TTL·별도 표시 이름 fallback |
| D2 YouTube | `apiservice.loadChannelNameMap/storeChannelNameMap/memberNameFromCacheKey`의 구 hash 해석을 기존 멤버 source 기반 초기화로 교체 | 통계 결과 cache·shared rate limiter와 오류 시 service 유지 |
| D2 wiring | `YouTubeAPIStackParams`, `YouTubeStackParams`, 두 builder, bot/admin bootstrap의 멤버 source 연결 | 두 builder 간 전달 누락 방지. 공용 cache 인자를 일괄 제거하지 않음 |
| D2 tests | cache `member_cache_test.go`, `service_test.go`의 member 부분, mock tests, provider 초기화 test, matcher additional/failure/benchmark의 구 field 사용 | 실제 별칭·조직·오류·할당량 회귀는 새 fixture에서 보존 |
| 교차 테스트 | `alarm_service_durability_test.go`의 두 member mock 연결 | 알림 durability 회귀 자체는 유지 |
| D2 script | `api/scripts/bot.sh`의 hash/ready-sentinel 읽기·개수 대기·출력·관련 변수·help, `CORE_MEMBER_HASH_SOFT_MIN_COUNT`, `CORE_MEMBER_HASH_SOFT_TIMEOUT_SECONDS`, `--no-ready-wait` | 일반 process-start/stop와 Valkey dependency 확인 등 유지 역할 |
| script fixture | `api/scripts/test-bot-env-loader.sh`의 구 옵션 invocation·필요한 fixture | command substitution 거부의 원래 보안 assertion 유지 |

helper는 실제 호출 관계를 다시 확인해 해당 제거 기능만 소유할 때 삭제합니다. `GetAllMembers` 같은 일반 이름을 저장소 전체에서 삭제하지 않습니다. `member.Cache`, `domain.MemberDataProvider`, 정상 repository/matcher/member mock의 같은 이름은 K 집합입니다.

실제 `api/internal/planes/bot/cmd/warm_member_cache`와 `bootstrap_core_tools.go`는 member epoch/L2를 다루므로 유지하고 compile을 검증합니다. deprecated 이름을 빈 구현으로 남기거나 새 이름 wrapper가 구 구현에 다시 접근하는 구조는 허용하지 않습니다.

## source·CLI 계약 변경

이 제안은 D2의 backend-specific exported Go API와 hash 전용 CLI/env를 폐기합니다. source 호환성이 자동으로 보존된다고 주장하지 않습니다. 알려진 consumer는 동일 patch에 모두 갱신하고, 아직 확인되지 않은 외부 Go consumer·script 자동화가 실제로 발견되면 source publication/cutover를 차단합니다. 호환 wrapper를 추가하는 것으로 해결하지 않습니다.

`--no-ready-wait`는 제거된 member-hash 대기만 제어하므로 폐기하며, 새 script는 일반 unknown-option 경로에서 명시적으로 실패합니다. 구 env를 새 env alias로 읽지 않습니다. `test-bot-env-loader.sh`는 새 `start` invocation으로 literal env 검사까지 도달해야 합니다.

hash 개수 기반 readiness는 제거합니다. 기존 process-start 확인을 멤버 초기화 완료로 표현하지 않습니다. 실제 runtime `/ready`는 PG/Valkey 의존 검사이며 기존 runbook의 healthcheck로 확인합니다. 새로운 endpoint·poller·환경변수·고정 host 주소를 이 정리 때문에 도입하지 않습니다.

위 source/CLI 변경은 이번 감사의 명시적 제안입니다. 코드 구현이나 공개 계약 폐기 권한을 감사 요청만으로 확대하지 않습니다. 후속 구현 범위를 승인할 때 이 변경도 함께 검토할 수 있도록 diff와 영향 목록을 제시합니다.

## 데이터와 실패 처리

| 경로 | 새 패치의 단일 경로 | 보존·명시할 변화 |
|---|---|---|
| membernews mutation/list | 기존 PG repository | SQL·오류·정렬·idempotency 동일. 성공 뒤 mirror 명령 없음 |
| membernews startup | 기존 Service의 `ListSubscribedRooms` 1회 | 실패하면 경고 후 service 반환. fatal startup으로 승격하지 않음 |
| matcher snapshot | 기존 error-aware `domain.LoadAllMembers` | provider 실패는 오류. hash-only 후보·구 hash 오류 branch는 존재하지 않음 |
| matcher query | 현재 snapshot index와 match-result cache | alias/org/partial 규칙과 TTL 유지. provider 성공+구 hash 장애는 provider 결과 사용 |
| YouTube 이름 초기화 | 같은 shared 멤버 source의 snapshot | 오류는 경고 후 기존 fallback 유지. 반복적인 채널별 PG 조회로 바꾸지 않음 |
| 공유 channel 표시 이름 | 최소 영속 member ID의 `Name` | 기존 shared representative와 정렬. 기존 map 덮어쓰기는 비결정적이므로 의도된 정규화로 검증 |
| scripts | hash와 무관한 기존 process/dependency 상태 | 가짜 멤버 개수 0·항상-ready 출력·구 flag alias 없음 |

shared channel의 대표 이름 정규화는 `Name`에 한정합니다. 알림의 한국어 단축명 정책이나 matcher의 첫 후보 선택 규칙을 동시에 바꾸지 않습니다. 영속 ID가 있는 실제 데이터 형상으로 fixture를 만들고 동명 동조직·다른 조직·colon·nil/빈 channel도 검증합니다.

provider에 없는 hash-only 행을 새 설계에 fallback으로 합치지 않습니다. 실제로 지원해야 하는 외부 writer가 확인되면 canonical source 계약을 먼저 확정하고 패치 승인을 보류합니다. 정상 PG source가 같은 경우, 폐기 hash가 없든 오래됐든 임의 값이 있든 새 결과가 같아야 합니다.

멤버 provider는 여전히 epoch/L2를 사용할 수 있고 matcher의 별도 이름 fallback, YouTube 통계·LLM cost tracker도 Valkey에 남습니다. 검증 double은 제거 key/capability만 차단해야 합니다. source 변경으로 인해 retention·rate-limit·auth failure mode를 바꾸지 않습니다.

## 잔재 판정과 허용되는 literal

완전 fadeout은 **실행 가능한 구 기능 경로 0개**를 뜻합니다. 모든 텍스트에서 과거 이름을 지우는 기준을 사용하지 않습니다.

| 위치 | 허용 여부 | 근거 |
|---|---|---|
| production 구현·constructor·구 interface·deprecated shim·unused old method | 금지 | 다시 구 기능을 실행할 수 있거나 의미 없는 계약을 남김 |
| runtime mock·구 backend 동작을 성공시키는 fixture | 금지 | 구 기능을 계속 구현하며 재도입을 숨길 수 있음 |
| 정상 domain/provider의 동명 method | 유지 | 별개의 실제 기능. 타입·package·receiver로 판정 |
| `pkg/privacylog/cachekey.go`의 `membernews:room_names` field 마스킹과 해당 보안 test | 유지 | 과거 key나 generic 로그 입력의 식별자 보호. 값 저장·조회 기능 아님 |
| 삭제 key 접근을 감지하는 negative regression fixture | 허용 | 실행 금지의 증거. 구 동작의 성공 테스트가 아님 |
| 이 plan·감사·과거 기록·폐기 key manifest | 허용 | 변경 기록 및 통제된 운영 정리에 필요 |
| 현재 운영 문서의 구 사용 방법 | 수정 | 새 지원 절차로 고치되 과거 이력과 구분 |

예외는 정확한 파일·용도별로 판정합니다. tests/docs/디렉터리 전체를 검사에서 제외하지 않습니다. symbol 검색은 선언·호출·interface 구현을 확인하고, literal 검색은 활성 consumer와 위 기록을 나눠 검토합니다. 유지 목적 없이 남는 구 key·env·flag는 사유 없는 예외로 승인하지 않습니다.

## 빅뱅 runtime 전환안

한 source revision과 여러 runtime의 전환은 다른 층위입니다. 최종 source에는 호환 경로가 없으므로 운영은 **관련 구버전 runtime을 종료한 뒤 새 release set을 시작하는 coordinated cutover**를 기본 제안으로 합니다. 부분 신버전·구버전 동시 운영을 정상 완료로 인정하지 않습니다.

이 절차는 승인 후 실행할 설계입니다. 실제 downtime·host·replica·quiesce 수단·backlog 상태는 아직 조사하지 않았으며 운영값을 임의로 지정하지 않습니다. `hololive-bot-ops`의 kapu 검증/no-build remote 원칙과 기존 API/worker runbook이 우선합니다.

| 순서 | 준비·판정 | 중단 조건 |
|---|---|---|
| C0 범위 고정 | API·alarm-worker·설치된 관련 CLI/script·구 one-shot writer 목록, host별 artifact ID, config/profile·이전 release set을 확정. 실제 delivery handoff mode·양쪽 executor 상태 포함 | consumer/writer·복구 target·source/CLI 영향이 미해결 |
| C1 사전 준비 | clean reviewed full SHA 하나로 kapu에서 API/worker와 필요한 관리 파일 build/검증. remote에 준비만 하고 시작하지 않음 | wrong arch, SHA 혼합, 누락 image, unrelated dirty work 포함 |
| C2 quiesce/drain | 승인된 traffic/producer를 잠시 제어하고 현재 runtime의 inbox/outbox·active lease·sending 상태를 해당 runbook 기준으로 정리. 뉴스 생성 작업과 `notification_delivery_outbox`·dispatch ledger 잔여 backlog 포함 | drain 실패, unknown-send 처리 미정, 유효한 rollback preflight 미확보 |
| C3 구 runtime 종료 | 관련 구 API/worker·one-shot writer가 종료됐고 supervisor/autoheal이 구 artifact를 재시작하지 않음을 확인 | 구 프로세스·writer 재출현 또는 확인 불가 |
| C4 일괄 release 적용 | 동일 SHA의 API·worker와 관련 script/config를 준비된 artifact로 순서 있게 시작. 외부 traffic은 아직 개방하지 않음 | 부분 시작 실패, 기존 release와 혼합, readiness 실패 |
| C5 수용 | image SHA+ID, PG/Valkey readiness, 검색·통계·구독·유지 기능 smoke와 제거 key I/O 부재를 확인한 뒤 승인 범위로 traffic 재개 | health만 정상이고 revision 또는 기능 검증이 불일치 |
| C6 데이터 회수 | 아래 별도 조건을 충족한 폐기 key만 정리하고 재생성 여부 관측 | 활성 reader/writer 또는 소유 미확인, cleanup 승인 없음 |

Compose 명령 하나를 원자적 transaction으로 취급하지 않습니다. cutover 실패 시 남은 구 runtime을 임의로 켜 mixed 운영하지 않습니다. collector는 이미 Valkey를 요구하지 않으므로 이 기능 제거만으로 AP fleet의 배포·중단까지 자동 확장하지 않습니다. 기존 API drain을 위해 upstream 제어가 실제 필요하면 그 범위만 별도 승인·runbook으로 정합니다.

quiesce·supervisor 제어·traffic 재개·실제 메시지 smoke·key 삭제는 모두 운영 영향이 있으므로 현재 요청으로 실행하지 않습니다. 배포 승인과 Git publication은 별개입니다.

## 복구와 data residue

기본은 새 단일 경로의 forward fix입니다. source 내부에 `legacy=true`, 구 hash loader, fallback writer, 새 CLI가 구 CLI를 재호출하는 분기를 만들지 않습니다.

이전 release 복원은 사전 검증한 API·worker·config/profile·script를 하나의 세트로 수행합니다. 새 runtime을 quiesce/종료하고, 기존 runbook의 durable backlog·lease·unknown-send·schema/profile compatibility 검사를 통과한 경우에만 진행합니다. schema migration이 없다는 사실만으로 rollback이 안전하다고 판단하지 않습니다. 이전 startup의 PG→hash 재생성이 필요하면 성공 여부도 확인합니다.

rollback하면 runtime은 구 설계로 돌아간 것이므로 이번 fadeout의 운영 완료 판정을 철회합니다. 새 source에는 여전히 구 경로를 넣지 않습니다. 이전 artifact는 운영 수용 기간 동안 복구용으로 보존하며, 보존 자체가 새 artifact의 실행 경로 잔재는 아닙니다.

폐기 대상은 `membernews:rooms`, `membernews:room_names`, `hololive:members`, 그리고 소유가 확인된 구 `hololive:members:ready` sentinel입니다. 이들은 TTL 자동 회수를 보장하지 않습니다. namespace 전체 삭제나 `FLUSH*`를 사용하지 않습니다. 세션·nonce·member epoch/L2·alarm 이름/index는 유지합니다.

특히 `membernews:*` 전체 삭제는 유지할 주간·월간 실행 잠금까지 지우므로 금지합니다. 정리 명세와 key별 I/O 검증은 폐기 대상의 정확한 이름으로 제한합니다.

소스 fadeout(S), runtime cutover(R), 저장된 폐기 key 회수(D)를 각각 보고합니다. S만 통과했으면 patch 검증 완료일 뿐 운영 적용 완료가 아닙니다. D가 미승인/미실행이면 data residue를 명시합니다. S/R/D의 미완료를 숨기거나 이를 이유로 새 코드에 구 경로를 부활시키지 않습니다.

## 구현 task

T31~T36은 하나의 patch를 만드는 내부 순서입니다. task별 별도 legacy-compatible 배포를 하지 않습니다. T37은 운영 전환안을 준비하는 task이며 live activation은 포함하지 않습니다.

### T31 제거 집합과 consumer 명세 확정

소유: shared providers/cache, API membernews/matcher/bootstrap, worker bootstrap, scripts. D1/D2의 모든 declaration·caller·mock·literal 소비를 갱신하고 경계 밖 Go/script 사용 여부를 확인합니다. source/CLI 변경과 이름 정규화안을 명시하고 범위의 미결정을 닫습니다. privacy/negative-test literal 예외도 정확히 기록합니다. AC31, V31에 연결합니다.

### T32 membernews mirror 기능 완전 종료

의존: T31. repository cache 의존·mirror helper·구 warmup service/repository method·호출·전용 테스트를 같은 패치에서 제거합니다. bootstrap은 기존 ListSubscribedRooms로 warning-only 확인을 보존합니다. LLM cost tracker의 cache 전달은 남깁니다. AC32, V32에 연결합니다.

### T33 멤버 hash 기능과 공용 API 완전 종료

의존: T31. reader를 기존 멤버 source로 연결하고 initializer·backend interface·embedding·method·mock·동적 matcher branch를 모두 제거합니다. 두 YouTube stack builder와 bot/admin source 전달을 함께 수정합니다. API/worker 및 warm_member_cache 도구의 정상 epoch/L2 기능을 유지합니다. AC33, V33에 연결합니다.

### T34 script와 관련 테스트 정리

의존: T31; T33의 새 데이터 경로와 일치해야 합니다. hash poll·상태 표기·전용 flag/env·사용 안내를 삭제하고 env-loader fixture와 CLI 실패 테스트를 갱신합니다. 구 flag를 no-op으로 수용하지 않습니다. process-start/실제 runtime readiness를 구분합니다. AC34, V34에 연결합니다.

### T35 behavior 및 잔재 회귀 검증

의존: T32~T34. 제거된 포맷 자체의 테스트만 정리하고 조직·별칭·오류·캐시 변이·epoch·allocation·보안 regression을 유지합니다. 구 key를 독성 값/부재로 바꿔도 새 결과가 같고 대상 I/O가 0인지 검증합니다. 같은 패치에서 누락 mock·interface consumer를 수정합니다. AC35, V31~V35에 연결합니다.

### T36 한 revision의 통합 검증과 산출물 고정

의존: T35. 전체 영향 Go module과 standalone 계약, applicable NilAway/race·Stage 3/prerequisites를 통과시킵니다. source diff·제거 manifest·검증 명령·허용 literal·유지 기능 연결을 기록합니다. 배포용 artifact 준비는 이후 승인 범위와 clean reviewed source를 따릅니다. AC36, V35~V36에 연결합니다.

### T37 전환과 복구 절차 준비

의존: T31의 범위; 최종 후보는 T36. 소유: 각 runtime 담당과 owning ops. 실제 설치 topology·image/config pair·drain/traffic/supervisor 제어·수용·rollback·cleanup 권한을 검토 가능한 문서로 정리합니다. C0~C6 명세를 만족시키는 적용 계획을 만들며 이 task 완료만으로 R/D를 완료 표시하지 않습니다. AC37, V36에 연결합니다.

## 수용 기준

### AC31 제거 범위의 완결성

D1/D2의 소유자·producer/consumer·exported source API·CLI·예외 literal이 확인됩니다. 발견한 외부 consumer를 같은 범위에 포함하지 못하면 완료를 선언하지 않습니다. K 집합을 제거 대상으로 혼동하지 않습니다.

### AC32 구독 동작과 startup 경계

PG SQL·idempotency·목록 순서·오류 의미가 유지되고 D1 key I/O는 0입니다. 구 warmup method와 repository cache 주입은 남지 않습니다. startup DB 오류는 warning-only이며 LLM cost tracking은 유지됩니다.

뉴스 정기 구독과 알람 멤버의 네 조합, 수동 조회, 빈 digest·멤버 없음 구분을 유지합니다. SQL 의미와 다른 기존 fake를 근거로 동일성을 주장하지 않습니다.

### AC33 멤버의 단일 조회 경로

새 runtime은 D2 key를 읽거나 쓰지 않습니다. backend-specific cache API/interface/mock 및 동적 matcher 경로는 없어야 합니다. provider 오류를 정상 빈 목록으로 저장하지 않습니다. YouTube 공유 channel 이름은 명시한 최소 영속 ID 대표 규칙을 따르고, 별도의 alarm 표시 이름·matcher 우선순위는 보존합니다.

### AC34 CLI와 설정 소비의 종료

구 hash env lookup·대기·표시·지원 flag가 남지 않습니다. 구 옵션은 명시적으로 거부되며 보안 env-loader test는 실제 env 검증 실패를 확인합니다. 새 readiness·fallback protocol을 만들지 않습니다.

### AC35 보호 기능과 오류 의미 보존

세션·nonce·limiter·member epoch·Pub/Sub·wakeup·API/LLM cache·ACL/alarm 이름·PG terminal ownership은 기존 경로를 유지합니다. 구 코드 테스트를 지운 것을 회귀 검증으로 대신하지 않습니다. 제거 key만 차단한 fixture에서도 새 기능과 유지 기능이 함께 작동해야 합니다.

뉴스 주간·월간 실행 잠금과 알람 사전 claim의 상이한 장애 동작, 기존 delivery mode별 enqueue와 worker 소유권도 보존합니다. 관측용 토큰 누계를 엄격한 호출 제한으로 바꾸지 않습니다.

### AC36 단일 패치 검증 완료

구 기능의 declaration/call/selection 경로 0개, 전체 영향 module compile/test와 required gate 통과, source/CLI 변경 명세 및 동작 검증 근거가 한 source revision에 대응해야 합니다. unused parameter·no-op shim·버전 switch로 임시 통과시키지 않습니다.

### AC37 운영 완료 단계의 구분

S/R/D를 구분하고 C0~C6 결과를 해당 권한과 증거로 기록합니다. 정상 운영으로 개방할 때 release set에 구/new active writer가 섞여 있지 않아야 합니다. rollback하면 R 완료 판정을 철회합니다. 실제 운영을 실행하지 않은 문서를 배포 완료로 표시하지 않습니다.

## 회귀 명세

아래는 향후 구현 검증입니다. 이번 감사에서 테스트를 실행한 증거가 아닙니다.

| ID | 시나리오 | 기대 결과 |
|---|---|---|
| B01 | 구독·재구독·해지·없는 방 | 기존 PG 결과·정렬·idempotency, D1 명령 0 |
| B02 | PG Exec/Query/Scan 오류·caller 취소 | 기존 오류 전파, 성공/빈 목록으로 대체 없음 |
| B03 | startup 빈 목록/여러 목록/DB 실패 | List 조회 1회, cache warmup 없음, 오류는 warning-only |
| B04 | membernews bootstrap | repository는 cache 불필요, cost tracker는 기존 cache 사용 |
| B05 | exact/partial·별칭·동명 다른 조직 | matcher 결과·후보 규칙 보존, D2 조회 없음 |
| B06 | provider 최초 실패 뒤 성공 | 실패 결과를 negative match/snapshot으로 고정하지 않음 |
| B07 | 폐기 hash 부재/오래된 값/잘못된 값/의도적 접근 실패 | 같은 PG source이면 새 결과 동일, 해당 key 명령 0 |
| B08 | 공유 channel 복수 ID·colon·동명 동조직·빈 channel | 명시한 대표 규칙·필터 검증, map 순서 의존 없음 |
| B09 | YouTube 이름 로드 실패 | service nonfatal·기존 fallback, 새 retry/N+1 없음 |
| B10 | member epoch 변경·TTL·reload 경합 | 기존 epoch 회귀 통과, TTL 임의 변경 없음 |
| B11 | 구 CLI 옵션과 제거 env | flag는 unknown으로 실패, 구 env alias/readiness poll 없음 |
| B12 | literal env의 command substitution 입력 | 새 invocation이 env 검증에서 거부, 구 flag 오류를 성공 근거로 삼지 않음 |
| B13 | stats cache·alarm-name fallback·LLM cost tracker | 필요한 Valkey 호출은 유지, cache nil 일괄 주입 없음 |
| B14 | 공용 cache mock·alarm durability 연결 | compile 및 기존 영속 상태 회귀 통과, member backend field 없음 |
| B15 | key별 privacy log 입력 | 유지된 민감 field 마스킹 작동. literal 잔존을 구 기능으로 오인하지 않음 |
| B16 | release set 일부 시작 실패/old writer 재등장 | traffic 개방 중지, 완료 판정 실패, 정해진 복구만 수행 |
| B17 | 새 release 중단 후 이전 세트 복원 모의 | 새 source의 구 branch 없이 복구 절차 수행 가능, runtime 완료 철회 |
| B18 | cleanup 승인 없음·TTL 없음 | S/R와 D 분리, 자동 만료·메모리 회수 완료를 주장하지 않음 |
| B19 | 뉴스 정기 구독 유무 × 알람 멤버 유무 | N01 네 조합 보존, 수동 조회에 뉴스 구독 조건 추가 없음 |
| B20 | alarms/members join·알람 타입·이름 중복·빈 이름 재구독 | 실제 PG SQL 기준 결과 보존, fake와 SQL의 빈 문자열 차이 해소 |
| B21 | 뉴스 주간·월간 잠금 성공/경쟁/Valkey 오류 | 기존 key·TTL·해제·skip/진행 의미 보존, nil로 비활성화 없음 |
| B22 | 알람 사전 claim의 Valkey 오류 | 준비 건 skip 유지, 뉴스 locker의 fail-open과 혼동 없음 |
| B23 | delivery off/shadow/cutover와 같은 기간의 다른 본문 | 기존 enqueue 대상·identity·terminal 상태 보존, 양쪽 backlog 확인 |
| B24 | 알람 추가 PG 성공 뒤 cache 실패 | PG 저장과 반환 오류·재구성 결과를 구분, 뉴스 mirror 실패와 동일시하지 않음 |
| B25 | 알람 이름의 등록/warmup·host 구독·provider 실패 | 기존 producer·한국어 표시·fallback 보존, D2 제거를 이름 통일로 확대하지 않음 |
| B26 | 월간 토큰 누계 초과/Valkey 오류 | 기존 경고·비차단 동작, 필요한 cache 연결 보존 |
| B27 | 뉴스 해지 전후 대상 수집·이미 enqueue된 메시지 | 다음 수집에서 제외, 기존 backlog 자동 취소 기능을 추가하지 않음 |

B16~B18은 먼저 격리 또는 fake process/artifact로 검증하고 실제 운영 결과는 승인된 cutover에서만 기록합니다. 구 runtime의 `sending`을 단순 retry로 되돌리거나 실제 메시지를 임의 발송하는 테스트를 하지 않습니다.

## 검증 명령과 근거

### V31 잔재의 정적·의미 검증

repo root에서 아래 검색과 Go type/compile 결과를 함께 사용합니다.

```bash
rg -n 'InitializeMemberDatabase|GetMemberChannelIDWithOrg|GetMemberChannelIDs|WarmupSubscriptionCache|initializeMemberDatabaseFromSnapshot|tryExactValkeyMatch|tryPartialValkeyMatch|loadDynamicMembers' hololive scripts deploy
rg -n 'hololive:members|membernews:rooms|membernews:room_names|CORE_MEMBER_HASH_SOFT_' hololive scripts deploy
rg -n -- '--no-ready-wait' hololive scripts deploy
```

`GetAllMembers`와 `MemberCache`는 package/receiver를 확인합니다. raw substring 0건으로 대신하지 않습니다. cache-specific declaration·embedding·function field·assignment까지 점검하며 aliases로 숨긴 호출은 Go compile과 참조 분석으로 확인합니다. 역사·negative-test·privacy literal은 정확한 위치·역할만 허용합니다. broad grep exclusion을 추가하지 않습니다.

### V32 구독과 bootstrap 검증

`go test ./hololive/hololive-api/internal/planes/llm/internal/service/membernews/... ./hololive/hololive-api/internal/planes/llm/runtime/...`

B01~B04/B19~B21/B26/B27을 기존 fake pool과 대상 key만 거부하는 cache spy로 검증합니다. repository_pgx_test의 이름만으로 실제 PG 통합을 실행했다고 주장하지 않습니다. N08에서 join 검증 공백과 fake의 빈 문자열 처리 차이를 확인했으므로 B20은 기존 dbtest harness에 해당 SQL을 실행하는 좁은 격리 PG 회귀를 추가합니다. 정본 SQL을 임의 수정하지 않고 fake를 실제 의미에 맞춥니다. LLM/Exa/Iris 외부 호출은 fake를 사용합니다.

`go test ./hololive/hololive-api/internal/planes/llm/internal/schedulerkit/... ./hololive/hololive-api/internal/planes/llm/internal/llm/... ./hololive/hololive-shared/pkg/service/delivery/... ./hololive/hololive-shared/pkg/service/alarm/...`

위 기존 suite에서 뉴스 잠금·관측·B22/B23의 claim 및 identity 근거를 확인합니다. 중복 방지·해지 semantics를 이 패치에서 재설계하지 않습니다. 실제 worker 발송은 enqueue 성공과 구분하며 격리 검증 뒤 승인된 운영 수용에서만 확인합니다.

### V33 cache API와 멤버 소비자 검증

`go test ./hololive/hololive-shared/pkg/service/cache/... ./hololive/hololive-shared/pkg/providers/... ./hololive/hololive-shared/internal/service/youtube/apiservice/... ./hololive/hololive-shared/pkg/service/notification/alarmservice/... ./hololive/hololive-api/internal/planes/bot/internal/service/matcher/... ./hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/... ./hololive/hololive-api/internal/planes/admin/app/... ./hololive/hololive-alarm-worker/internal/app/workerapp/...`

`go test -race ./hololive/hololive-shared/pkg/service/member/... ./hololive/hololive-api/internal/planes/bot/internal/service/matcher/... ./hololive/hololive-shared/internal/service/youtube/apiservice/...`

B05~B10/B13/B14/B24/B25를 검증합니다. 기존 matcher allocation budget과 alias/org/provider 오류 회귀를 유지하며 helper 삭제를 이유로 기능 회귀까지 삭제하지 않습니다.

### V34 script와 개인정보 보호 검증

`bash -n hololive/hololive-api/scripts/bot.sh`

`bash hololive/hololive-api/scripts/test-bot-env-loader.sh`

`go test ./hololive/hololive-shared/pkg/privacylog/...`

추가 CLI fixture는 임시 repo/PID/log/가짜 container·process 도구에서 B11/B12를 검사합니다. 보안 fixture의 기존 command substitution rejection assertion을 보존합니다. 실운영 start/stop/restart를 호출하지 않습니다. B15에서는 과거 key 입력에도 마스킹이 유지되는지 확인합니다.

### V35 전체 영향 모듈과 필수 게이트

`go test ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/... ./hololive/hololive-dbtest/...`

`go test . -run TestRuntimeSplitStandaloneModulesContract`

`./scripts/ci/local-ci.sh`

공용 interface 삭제이므로 직접 consumer만 테스트하고 종료하지 않습니다. 모든 compile/test/build는 kapu에서 실행합니다. applicable NilAway/race·Stage 3/prerequisites와 기존 boundary/standalone 규칙을 유지하며, 실패하면 원인과 영향 범위를 기록하고 억제로 통과시키지 않습니다. 신규 production dependency나 toolchain/lockfile 일괄 업그레이드는 포함하지 않습니다.

### V36 변경·artifact·전환 검토

`git diff --check`, `./scripts/architecture/ci-boundary-gate.sh`와 실제 변경한 계약/서비스 문서의 project-map·contract-map·runbook coverage 검사를 수행합니다. DEC/plan은 stack root의 `bash tools/checks/check-decision-catalog.sh check --submodules`로 검증합니다.

필요한 이미지 build는 `./build-all.sh --build-only --no-bump` 등 기존 build-only 경로로 준비하며 운영 artifact는 clean reviewed full SHA·대상 architecture·image ID를 고정합니다. 출판 승인 시 `scripts/ci/pre-push-gate.sh`가 별도 필수입니다. cutover는 owning ops의 필요한 정적 배포 계약과 no-build 절차를 적용하며, 이 문서 때문에 전체 stack/runtime을 재배포하지 않습니다.

증거에는 source SHA+관련 diff, D1/D2 명세, 실행한 명령/exit, scenario별 결과, 유지 literal 사유, startup/PG/cache 호출 수, 미실행 검증, S/R/D 상태를 남깁니다. 절감 목표는 구독·해지의 target-key 명령 각 2→0, warmup target-key 명령 2~4→0, matcher rebuild·YouTube 초기화의 중복 hash 조회 1→0입니다. 이는 정적 호출 수이며 운영 latency/메모리 개선 수치가 아닙니다.

## 실행 전 미결 조건과 인계

1. source/CLI 계약 삭제와 이름 정규화, 외부 consumer 확인을 포함한 **전체 D1/D2 패치 범위**의 검토가 필요합니다. 발견한 consumer를 조용히 예외 처리하거나 no-op을 남기지 않습니다.
2. C0~C5의 실제 host/process 목록·downtime·quiesce·supervisor·artifact set·rollback preflight는 운영 준비에서 확정합니다. 준비가 없으면 mixed fleet를 허용하는 식으로 전환 방식을 약화하지 않습니다.
3. cleanup 권한·정확한 key 소유·수용 기간은 별도입니다. 운영 데이터 회수 미실행을 코드/런타임 구현 실패와 혼동하지 않습니다.
4. 현재는 감사·문서 작성만 승인됐습니다. 신규 DEC는 proposed이며 등록된 실행 PLN은 없습니다. accepted governing DEC 확정 뒤 lifecycle을 만들고 실행 직전 gate와 사용자 권한을 각각 확인합니다.

이전 selective-retention 제안의 필요한 Valkey 유지 방향은 보존하되 단계적 전달안은 철회했습니다. 후속 실행자는 이 문서의 새로운 marker와 빅뱅 감사의 제거 명세를 사용합니다. 이전 패킷 일부의 테스트 결과를 새 전체 patch의 완료 근거로 승계하지 않습니다.
