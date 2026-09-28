# Hololive Valkey 선택적 축소 빅뱅 fadeout 계획

**Decisions:** `DEC-20260928-hololive-valkey-bigbang-fadeout` (governing), `DEC-20260731-legacy-fade-out-no-dual-path` (constraint)

## Execution capsule

**Goal:** 필요한 Valkey 기능을 유지하고, 두 중복 기능의 구 코드·연결·설정 소비를 하나의 완결된 패치에서 제거한다.
**Context:** 제거 집합은 membernews room mirror와 hololive:members이다. 이전 B1/B2 중간 writer 릴리스와 exported 구 API 보존안은 폐기한다.
**Constraints:** 사용자가 D1/D2 전체 구현, 공개 backend Go API·hash 전용 CLI/env 폐기, YouTube 공유 channel 대표 이름 정규화를 승인했다. 운영 전환·key 회수·Git publication은 승인 범위가 아니다. 패치는 dual path·호환 shim·no-op API 없이 보호용 Valkey와 PG durable state를 보존한다.
**Evidence:** `docs/review/2026-09-28-valkey-bigbang-fadeout-audit.md`, `docs/review/2026-09-28-alarm-membernews-valkey-audit.md`, 앞선 리뷰와 코드·테스트·runbook. 과거 감사는 정적 근거이며, 준비 확인과 구현 검증은 아래 기록에서 구분한다. 운영 검증은 미수행이다.
**Success:** 제거 기능의 활성 생산·소비·선택 경로가 0이고, 유지 기능 회귀와 전체 모듈 검증이 통과한다. runtime 전환·data residue 회수는 별도 증거로 기록한다.
**Output:** 한 worktree revision의 D1/D2 source fadeout(S)과 대응 검증 근거. R/D는 별도 승인 작업이다.

## 이번 개정과 범위

사용자의 두 요구를 함께 적용합니다. 필요한 공유 임시 상태와 유의미한 캐시는 Valkey에 유지합니다. 제거하기로 정한 기능은 새로운 설계를 도입하는 지점에서 완전히 종료합니다.

[빅뱅 감사](../../review/2026-09-28-valkey-bigbang-fadeout-audit.md)는 직전 플랜에서 F01~F10을 확인했습니다. [선택적 유지 리뷰](../../review/2026-09-28-valkey-selective-retention-review.md)와 [실행 준비 리뷰](../../review/2026-09-28-valkey-implementation-readiness-review.md)의 정적 사실은 참고하되, 전환 방식과 완료 기준은 이 문서를 따릅니다.

[알람·멤버 뉴스 심층 감사](../../review/2026-09-28-alarm-membernews-valkey-audit.md)의 N01~N09를 반영했습니다. D1/D2 범위는 같으며 뉴스 실행 잠금·PG 발송 경로·구독 간 연결을 유지 명세와 B19~B27 검증으로 구체화합니다.

**패치는 하나입니다.** reader만 바꿔 배포하고 writer는 다음 릴리스에 지우는 B1/B2는 사용하지 않습니다. 구현 중 의존 순서에 따른 편집·테스트는 가능하지만 최종 merge/build 대상에는 아래 제거 집합이 모두 포함되어야 합니다. 일부만 완료하고 빅뱅 fadeout 완료라고 표시하지 않습니다. 범위를 줄여야 한다면 사유와 제거 집합부터 다시 확정합니다.

과거 감사는 2026-09-28 HEAD `3be7229b060b`와 당시 dirty 작업 트리 기준이며, 이번 구현 준비는 `a03eb83f9bdf38a451c86aa9627d2df008bcffbf` 기준입니다. 아래 준비 기록의 현재 코드 교정을 우선합니다. 운영 artifact는 이후 clean reviewed revision으로 준비해야 합니다. 이전 T11~T17/AC11~AC18/V11~V16은 단계적 패킷 의미였으므로 새 작업에는 T31 이후의 marker를 사용합니다. 기록·코드 수정 이력과 실제 완료를 혼동하지 않습니다.

## 제거 집합과 유지 집합

| 집합 | 포함 기능 | 패치 종료 상태 |
|---|---|---|
| D1 | `membernews:rooms`, `membernews:room_names` mirror | repository writer·warmup·전용 cache 의존·전용 API/테스트·상수가 없음 |
| D2 | `hololive:members` 중복 hash와 이를 읽는 readiness/조회 기능 | runtime reader/writer·backend API/interface/mock·동적 matcher helper·전용 CLI/env가 없음 |
| K1 | 세션·reset·임시 계정·서명 nonce·공유 rate limit | 기존 Valkey와 TTL·원자성·실패 계약 유지 |
| K2 | member epoch(변경 신호), alarm wakeup, 뉴스 주간·월간 실행 잠금 | 기존 조율 및 복구 계약 유지. `membernews:lock:weekly:*`·`monthly:*` 보존. member L2 data와 설정·ACL Pub/Sub은 2차 패치에서 제거(아래 2차 축소 기록) |
| K3 | API/LLM 결과 cache·알림 채널 registry·구독 index/사전 claim | 현재 역할 유지. 측정 없이 메모리/PG로 전환하지 않음. 토큰 관측은 이미 Valkey와 분리된 현재 metrics 경로 유지. 방 index·이름 hash와 효과 없는 결과 cache는 2차 패치에서 제거 |
| K4 | `alarm:member_names` | 이번 제거 집합에서 제외하고 그대로 유지. ACL 값 mirror는 2차 패치에서 in-process 통지와 함께 제거 |
| K5 | Valkey server/client/config/auth/socket/readiness, PG ledger | 계속 필요한 공용 인프라와 영속 정본 보존 |

K4는 이름만 바꾼 임시 호환 경로가 아닙니다. 알림 이름은 `ShortKoreanName → NameKo → Name`과 구독 fallback 등 별도 의미를 가지므로 유지합니다. ACL mirror는 1차 fadeout 당시 "지속적인 Valkey 장애에서 rollback되던 권한 변경이 PG 성공+통지 실패로 바뀐다"는 이유로 보류했으나, 2차 패치에서 ACL 변경 통지를 같은 프로세스 in-process 전파로 바꿔 통지 유실을 없앤 뒤 제거했습니다. 남은 실패(봇 plane의 PG 재읽기 실패)는 PG 커밋 뒤 관리 응답 `500 acl_bot_resync_failed`로 드러나고, 같은 요청 재시도가 PG 쓰기 없이 재동기화합니다(`contracts/settings.md`).

PG는 dispatch pending/retry/lease/sending/terminal의 정본입니다. 이 패치는 outbox identity·retry·quarantine·unknown-send·schema를 변경하지 않습니다. Valkey 자체가 없어도 홀로봇 전체가 실행된다는 목표는 두지 않습니다.

### 알람과 뉴스의 연결 경계

뉴스 정기 수신 방은 `member_news_subscriptions`, 관심 멤버는 `alarms LEFT JOIN members`, 소식 후보는 `major_events`의 PG 조회입니다. 수동 뉴스 생성은 정기 구독 여부를 선행 검사하지 않습니다. 관심 멤버 SQL은 LIVE 타입만 고르지 않으며 이름 우선순위·이름 기준 DISTINCT를 보존합니다. 뉴스 구독 해지는 알람 등록이나 이미 enqueue된 메시지를 취소하지 않습니다.

뉴스 잠금은 15분 TTL과 token 비교 해제이며, Valkey 오류 시 오류를 돌려줘 실행하지 않습니다(fail-closed, 다음 주기 재시도). 알람 사전 claim도 Valkey 오류를 skip과 구분해 해당 준비 건을 실패로 기록합니다. 두 경로를 cache=nil로 비활성화하지 않습니다. 현재 HEAD의 `ProvideLLMCostTracker()`는 cache 인자 없는 `NewTokenMetricsRecorder()`이며, 월 상한·`llm:cost:tokens:*` 카운터는 이미 퇴역했습니다. 과거 감사 N07의 경고용 누계를 복원하지 않습니다.

뉴스 enqueue는 기존 handoff 설정에 따라 `notification_delivery_outbox` 또는 alarm dispatch ledger를 사용합니다. 실제 운영값은 미확인이며 이번에 mode/default/executor를 바꾸지 않습니다. 같은 기간이라도 dispatch digest의 본문 hash가 바뀌면 identity가 달라질 수 있으므로 잠금 제거를 PG dedup만으로 정당화하지 않습니다. 스케줄러의 `Sent`는 enqueue 성공이며 실제 발송 결과는 worker에서 검증합니다.

알람 표시 이름·index·wakeup은 현재 유지하되 영구적인 Valkey 필수성으로 판정하지 않습니다. 추후 축소에는 이름 의미·모든 reader·DB 부하·polling 지연·공유 관측 대체 근거가 필요합니다. 상세 근거와 실패 행렬은 N01~N09를 참고하되, 토큰 관측은 위 현재 코드 계약을 따릅니다.

## 완전 제거 명세

아래 `shared`, `api`, `worker`는 `hololive/hololive-shared`, `hololive/hololive-api`, `hololive/hololive-alarm-worker`입니다. 모든 행이 한 변경 집합에 속합니다.

| 항목 | 정확한 제거·교체 대상 | 남겨야 할 경계 |
|---|---|---|
| D1 repository | `api/.../membernews/repository.go`의 mirror 상수·cache 및 mirror 전용 log 필드/constructor 인자, `repository_cache.go`의 clear/load/write-through, `repository_mutation.go` 호출 | PG query·정렬·idempotency·오류. 생성자는 `NewRepository(postgres)`로 축소 |
| D1 startup API | `Service.WarmupSubscriptionCache`, 해당 repository `WarmupCacheFromDB`, bootstrap의 구 호출과 API guard 테스트. `initMemberNewsService`의 mirror 전용 cache 인자와 caller 4곳 | 기존 `ListSubscribedRooms` 1회와 warning-only 확인. cache 비의존 토큰 metrics 유지. scheduler 상위의 epoch/L2·뉴스 잠금·결과 cache·readiness 연결은 보존 |
| D2 producer | `shared/pkg/providers/member_providers.go`의 `initializeMemberDatabaseFromSnapshot`·`initializeMemberDatabase`와 호출·전용 snapshot interface | 실제 `member.Cache` 생성·epoch/L2 warmup·cleanup |
| D2 backend | `shared/pkg/service/cache/member.go`, `member_cache.go`의 `cache.MemberCache`, `Service.InitializeMemberDatabase/GetAllMembers/GetMemberChannelIDWithOrg/GetMemberChannelIDs`와 전용 helper | 정상 멤버 domain/repository·adapter API는 유지 |
| D2 interface | `cache.DomainCache`의 `MemberCache` embedding | `StreamCache`와 공용 `cache.Client`의 KV/hash/set/CAS/connection/low-level 기능 |
| D2 mock | `cache/mocks/client.go`의 member 전용 function field/assertion과 `client_domain.go`의 해당 methods | stream·CAS 등 유지 기능 mock. 파일 전체를 지우지 않음 |
| D2 matcher | `dynamicLoadErr`, `storeDynamicSnapshotMembers`, `snapshotEntryFromDynamic`, `splitMemberKey`, `tryExactValkeyMatch`, `tryPartialValkeyMatch`, `loadDynamicMembers`, `candidateFromDynamic`, `preferHololiveCandidate`와 전용 branch. `Matcher.cache`, `NewMatcher`/`ProvideMatcher`의 cache 인자와 모든 caller | 정상 snapshot·별칭·조직·후보 우선순위·1분 TTL·현재 roster 기반 표시 이름. 과거 Valkey 알림 이름 fallback은 이미 퇴역했으므로 복원하지 않음 |
| D2 YouTube | `apiservice.loadChannelNameMap/storeChannelNameMap/memberNameFromCacheKey`의 구 hash 해석을 기존 멤버 source 기반 초기화로 교체 | 통계 결과 cache·shared rate limiter와 오류 시 service 유지 |
| D2 wiring | `YouTubeAPIStackParams`, `YouTubeStackParams`, 두 builder, bot/admin bootstrap의 멤버 source 연결 | 두 builder 간 전달 누락 방지. 공용 cache 인자를 일괄 제거하지 않음 |
| D2 tests | cache `member_cache_test.go`, `service_test.go`의 member 부분, mock tests, provider 초기화 test, matcher additional/failure/benchmark의 구 field 사용 | 실제 별칭·조직·오류·할당량 회귀는 새 fixture에서 보존 |
| 교차 테스트 | `alarm_service_durability_test.go`의 두 member mock 연결 | 알림 durability 회귀 자체는 유지 |
| D2 script | `api/scripts/bot.sh`의 hash/ready-sentinel 읽기·개수 대기·출력·관련 변수·help, `CORE_MEMBER_HASH_SOFT_MIN_COUNT`, `CORE_MEMBER_HASH_SOFT_TIMEOUT_SECONDS`, `--no-ready-wait` | 일반 process-start/stop와 Valkey dependency 확인 등 유지 역할 |
| script fixture | `api/scripts/test-bot-env-loader.sh`의 구 옵션 invocation·필요한 fixture | command substitution 거부의 원래 보안 assertion 유지 |

helper는 실제 호출 관계를 다시 확인해 해당 제거 기능만 소유할 때 삭제합니다. `GetAllMembers` 같은 일반 이름을 저장소 전체에서 삭제하지 않습니다. `member.Cache`, `domain.MemberDataProvider`, 정상 repository/matcher/member mock의 같은 이름은 K 집합입니다.

과거 명세의 `api/internal/planes/bot/cmd/warm_member_cache`와 `bootstrap_core_tools.go`는 현재 checkout의 `hololive/` 아래에 없습니다. 존재하지 않는 도구의 복원·compile을 요구하지 않습니다. 실제 K2 검증 대상은 `member.Cache`와 API bot/admin/llm·worker의 provider/adapter 경로입니다. deprecated 이름을 빈 구현으로 남기거나 새 이름 wrapper가 구 구현에 다시 접근하는 구조는 허용하지 않습니다.

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
| member provider startup | 정상 member.Cache 생성·epoch/L2만 유지 | D2 hash field 형식 검사 및 DEL/HSET 실패가 provider 오류로 전파되던 전용 fatal 경로는 제거. 공용 Valkey 연결·readiness 장애 처리는 유지 |
| matcher snapshot | 기존 error-aware `domain.LoadAllMembers` | provider 실패는 오류. hash-only 후보·구 hash 오류 branch는 존재하지 않음 |
| matcher query | 현재 snapshot index와 match-result cache | alias/org/partial 규칙과 TTL 유지. provider 성공+구 hash 장애는 provider 결과 사용 |
| YouTube 이름 초기화 | 같은 shared 멤버 source의 snapshot | 오류는 경고 후 기존 fallback 유지. 반복적인 채널별 PG 조회로 바꾸지 않음 |
| 공유 channel 표시 이름 | 최소 영속 member ID의 `Name` | 기존 shared representative와 정렬. 기존 map 덮어쓰기는 비결정적이므로 의도된 정규화로 검증 |
| scripts | hash와 무관한 기존 process/dependency 상태 | 가짜 멤버 개수 0·항상-ready 출력·구 flag alias 없음 |

shared channel의 대표 이름 정규화는 `Name`에 한정합니다. 알림의 한국어 단축명 정책이나 matcher의 첫 후보 선택 규칙을 동시에 바꾸지 않습니다. 영속 ID가 있는 실제 데이터 형상으로 fixture를 만들고 동명 동조직·다른 조직·colon·nil/빈 channel도 검증합니다. 대표 계산은 기존 `member.channelRepresentatives`를 공개 helper `member.ChannelRepresentatives`로 승격하고 기존 내부 호출도 같은 함수로 옮기는 안으로 정합니다. `LoadAllMembers` 1회 결과에 이 규칙을 적용하며, `ORDER BY english_name`의 첫 행 선택·규칙 복제·채널별 N+1 조회를 하지 않습니다. 최소 ID 대표의 `Name`이 비면 차순위 멤버를 고르지 않고 기존 `resolveChannelTitle`의 fallbackTitle 동작을 유지합니다.

provider에 없는 hash-only 행을 새 설계에 fallback으로 합치지 않습니다. 실제로 지원해야 하는 외부 writer가 확인되면 canonical source 계약을 먼저 확정하고 패치 승인을 보류합니다. 정상 PG source가 같은 경우, 폐기 hash가 없든 오래됐든 임의 값이 있든 새 결과가 같아야 합니다.

멤버 provider의 epoch/L2와 YouTube 통계는 Valkey에 남습니다. matcher의 cache 인자는 D2 전용이므로 제거하되 alarm service의 별도 이름 cache와 혼동하지 않습니다. 토큰 관측은 현재 cache 비의존 metrics를 유지합니다. 검증 double은 제거 key/capability만 차단해야 합니다. source 변경으로 인해 retention·rate-limit·auth failure mode를 바꾸지 않습니다.

위 startup 실패 조건 축소는 `member_providers.go:57-59`와 `cache/member_cache.go:35-63`의 정적 호출 관계에서 확인한 의도된 변화이며, 아직 실패 주입으로 재현한 결과는 아닙니다. B07/B08 검증에 colon 이름과 제거 key 쓰기 실패가 정상 provider 초기화를 막지 않는 사례를 포함합니다. 이를 Valkey 전체 장애에서도 runtime이 기동한다는 의미로 확대하지 않습니다.

## 잔재 판정과 허용되는 literal

완전 fadeout은 **실행 가능한 구 기능 경로 0개**를 뜻합니다. 모든 텍스트에서 과거 이름을 지우는 기준을 사용하지 않습니다.

| 위치 | 허용 여부 | 근거 |
|---|---|---|
| production 구현·constructor·구 interface·deprecated shim·unused old method | 금지 | 다시 구 기능을 실행할 수 있거나 의미 없는 계약을 남김 |
| runtime mock·구 backend 동작을 성공시키는 fixture | 금지 | 구 기능을 계속 구현하며 재도입을 숨길 수 있음 |
| 정상 domain/provider의 동명 method | 유지 | 별개의 실제 기능. 타입·package·receiver로 판정 |
| `pkg/privacylog/cachekey.go`의 `membernews:room_names` field 마스킹과 해당 보안 test, `cachekey_test.go`의 `hololive:members` 입력 | 유지 | 과거 key나 generic 로그 입력의 식별자 보호. 값 저장·조회 기능 아님 |
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
| C1 사전 준비 | clean reviewed full SHA 하나로 kapu에서 API/worker와 필요한 관리 파일 build/검증. remote에 준비만 하고 시작하지 않음. 아래 migration 233 사전 점검 쿼리 0건 확인. 중앙 `compose.env`·`bot.env`·`alarm-worker.env`와 stack-secrets master 사본에 기동 거절 env key(`config_youtube_retired_env.go`의 `YOUTUBE_CACHE_EXPIRATION_SECONDS`·`YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS`·`YOUTUBE_CACHE_SAVE_TIMEOUT_SECONDS`·`YOUTUBE_SCRAPER_PHASE_TIMEOUT_SECONDS`, `config_youtube_producer_retired.go`의 `YOUTUBE_PRODUCER_*`)가 0건인지 hololive-bot-ops로 확인. key 이름만 세고 값은 출력하지 않음(예: `sudo grep -cE '^[[:space:]]*(export[[:space:]]+)?(YOUTUBE_CACHE_EXPIRATION_SECONDS\|YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS\|YOUTUBE_CACHE_SAVE_TIMEOUT_SECONDS\|YOUTUBE_SCRAPER_PHASE_TIMEOUT_SECONDS\|YOUTUBE_PRODUCER_[A-Z0-9_]+)=' <env 파일>`) | wrong arch, SHA 혼합, 누락 image, unrelated dirty work 포함, 233 사전 점검 결과가 0건이 아님, 거절 env key가 1건 이상(존재만으로 C4의 API·worker 기동이 실패하므로 제거 뒤 재확인) |
| C2 quiesce/drain | 승인된 traffic/producer를 잠시 제어하고 현재 runtime의 inbox/outbox·active lease·sending 상태를 해당 runbook 기준으로 정리. 뉴스 생성 작업과 `notification_delivery_outbox`·dispatch ledger 잔여 backlog 포함 | drain 실패, unknown-send 처리 미정, 유효한 rollback preflight 미확보 |
| C3 구 runtime 종료 | 관련 구 API/worker·one-shot writer가 종료됐고 supervisor/autoheal이 구 artifact를 재시작하지 않음을 확인 | 구 프로세스·writer 재출현 또는 확인 불가 |
| C4 일괄 release 적용 | 동일 SHA의 API·worker와 관련 script/config를 준비된 artifact로 순서 있게 시작. 외부 traffic은 아직 개방하지 않음 | 부분 시작 실패, 기존 release와 혼합, readiness 실패 |
| C5 수용 | image SHA+ID, PG/Valkey readiness, 검색·통계·구독·유지 기능 smoke와 제거 key I/O 부재를 확인한 뒤 승인 범위로 traffic 재개 | health만 정상이고 revision 또는 기능 검증이 불일치 |
| C5b collector·PO 교체 | C5 수용 뒤 같은 SHA의 7.0.0 collector·PO를 네 쌍(중앙 c, AP b·a·d)에 한 쌍씩 기존 paired 절차로 교체. c는 `po-central-cutover.sh deploy`(issuer-first), b는 `ap-deploy.sh seoul --apply`(issuer-first), a·d는 `HOLO_BOT_VERSION=7.0.0 ap-host-native-deploy.sh osaka\|osaka2 --apply`([youtube-collector runbook](../runbooks/youtube-collector.md#isolated-po-token-lifecycle)). 판정: c는 `po-central-cutover.sh check <이번 deploy의 po-c backup 경로>`와 compose `/ready`, AP는 `ap-completion-check.sh seoul\|osaka\|osaka2`가 exit 0이고 issuer·collector의 revision이 같은 SHA, version이 7.0.0이며 issuer 재시작은 runbook의 issuer 수용 기준 안 | 한 쌍이라도 판정 실패면 다음 쌍으로 넘어가지 않고 그 쌍을 해당 절차의 rollback으로 되돌림 |
| C6 데이터 회수 | 아래 별도 조건을 충족한 폐기 key만 정리하고 재생성 여부 관측 | 활성 reader/writer 또는 소유 미확인, cleanup 승인 없음 |

Compose 명령 하나를 원자적 transaction으로 취급하지 않습니다. cutover 실패 시 남은 구 runtime을 임의로 켜 mixed 운영하지 않습니다. 이 "mixed 불허" 규칙은 API·alarm-worker에 적용합니다. collector·PO는 Valkey를 요구하지 않고 6.0.2 collector가 7.0.0 API·worker와 섞여도 기능이 깨지지 않으므로 C4~C5에서는 6.0.2로 두고, C5b에서 네 쌍 모두를 같은 7.0.0 SHA로 올립니다. 7.0.0 collector는 scraping·member cache·holodex provider 변경을 담은 실제 산출물이고, `po-central-remote.sh check`와 `ap-completion-check.sh`는 image version을 트리의 `hololive-api/VERSION`(7.0.0)과 비교하므로 C5b 전에는 이 두 검사가 version 불일치로 실패합니다. 기존 API drain을 위해 upstream 제어가 실제 필요하면 그 범위만 별도 승인·runbook으로 정합니다.

quiesce·supervisor 제어·traffic 재개·실제 메시지 smoke·key 삭제는 모두 운영 영향이 있으므로 현재 요청으로 실행하지 않습니다. 배포 승인과 Git publication은 별개입니다.

## 복구와 data residue

기본은 새 단일 경로의 forward fix입니다. source 내부에 `legacy=true`, 구 hash loader, fallback writer, 새 CLI가 구 CLI를 재호출하는 분기를 만들지 않습니다.

이전 release 복원은 사전 검증한 API·worker·config/profile·script를 하나의 세트로 수행합니다. 새 runtime을 quiesce/종료하고, 기존 runbook의 durable backlog·lease·unknown-send·schema/profile compatibility 검사를 통과한 경우에만 진행합니다. schema migration이 없다는 사실만으로 rollback이 안전하다고 판단하지 않습니다. 이전 startup의 PG→hash 재생성이 필요하면 성공 여부도 확인합니다.

rollback하면 runtime은 구 설계로 돌아간 것이므로 이번 fadeout의 운영 완료 판정을 철회합니다. 새 source에는 여전히 구 경로를 넣지 않습니다. 이전 artifact는 운영 수용 기간 동안 복구용으로 보존하며, 보존 자체가 새 artifact의 실행 경로 잔재는 아닙니다.

1차 폐기 대상은 `membernews:rooms`, `membernews:room_names`, `hololive:members`, 그리고 소유가 확인된 구 `hololive:members:ready` sentinel입니다. 이들은 TTL 자동 회수를 보장하지 않습니다. namespace 전체 삭제나 `FLUSH*`를 사용하지 않습니다. 세션·nonce·member epoch·alarm 채널 registry·구독 index·`alarm:member_names`는 유지합니다.

### 2차 전환: migration 233 사전 점검

233은 `CMD_ALARM_ADDED`·`CMD_ALARM_LIST` 가운데 표준 본문(233의 `old_body`)과 바이트 단위로 다르면서 `NextStream`을 참조하는 행이 하나라도 있으면 파일 전체를 거절합니다. C4의 db-migrate에서 거절되면 231·232만 커밋된 채 구 runtime은 이미 멈춰 있고 새 API·worker는 migration 의존 때문에 뜨지 않습니다. 그래서 C1에서, 늦어도 C3 전에 아래 read-only 쿼리로 거절 조건을 미리 확인하고, 그때부터 C4까지 두 템플릿의 Console 편집을 동결합니다. md5 값은 233 파일의 두 `$old$` 본문에서 계산했습니다.

```sql
SELECT template_key, channel_id
FROM notification_templates
WHERE template_key IN ('CMD_ALARM_ADDED', 'CMD_ALARM_LIST')
  AND body LIKE '%NextStream%'
  AND (template_key, md5(body)) NOT IN (
      ('CMD_ALARM_ADDED', 'beb0b263948d956a24e6beef3872ca8f'),
      ('CMD_ALARM_LIST', 'ae8fd72e5db7f640b06d87300ecc8298'));
```

결과가 0건일 때만 C3로 진행합니다. 행이 나오면 C3 전에 그 본문에서 `NextStream` 참조를 지우거나 배포를 보류합니다.

### 2차 전환: 관리자 방 별칭 이관

base(`12d78df8a`)의 관리자 방 이름은 Valkey hash `alarm:room_names`에만 있었고 migration 232는 빈 `alarm_room_display_names`만 만듭니다. 구 worker는 기동·rebuild 때 이 hash를 PG `alarms.room_name`으로 다시 채웠으므로, hash에서 어떤 `alarms.room_name`과도 다른 값이 마지막 기동 이후 지정된 관리자 별칭입니다. 이 값을 이관하기 전에는 hash를 지우지 않습니다.

1. C0부터 Iris Console 배포까지 Console의 방 이름 변경(`POST /api/holo/names/room`)을 동결하고 관리자에게 공지합니다. Console은 Hololive v7.0.0의 C5 수용 뒤에 배포합니다(이전 Hololive는 빈 이름 해제를 400으로 거절). 전환 창에 구 worker로 들어온 rename은 Valkey에만 기록되어 사라집니다.
2. 구 worker를 정지하기 직전(C3 직전)에 `alarm:room_names`를 root 전용 경로의 권한 600 CSV로 export합니다. 값은 방 제목 평문이므로 stdout·로그·티켓에 남기지 않고 건수만 기록합니다. 아래 명령은 Valkey Lua `cjson`으로 hash를 JSON 배열로 받고 Python `csv.writer`가 쉼표·따옴표·줄바꿈을 quoting하므로 `\copy ... (FORMAT csv)`가 그대로 읽습니다. 출력은 건수 한 줄이고, Valkey가 오류를 돌려주면 JSON 해석이 실패해 파일을 만들지 않습니다.

```bash
# root shell(sudo -i)에서 실행합니다.
set -o pipefail
umask 077
mkdir -p /root/valkey-fadeout
docker exec valkey-cache sh -c 'REDISCLI_AUTH="$CACHE_PASSWORD" exec valkey-cli -s /var/run/valkey/valkey-cache.sock --raw EVAL "return cjson.encode(redis.call(\"HGETALL\", KEYS[1]))" 1 alarm:room_names' \
  | python3 -c '
import csv, json, sys
pairs = json.load(sys.stdin)
pairs = [] if isinstance(pairs, dict) else pairs  # cjson은 빈 hash를 {}로 씁니다
with open(sys.argv[1], "x", encoding="utf-8", newline="") as out:
    writer = csv.writer(out)
    for i in range(0, len(pairs), 2):
        writer.writerow(pairs[i:i + 2])
print(len(pairs) // 2)' /root/valkey-fadeout/room_names.csv
```

3. migration 232 적용(C4의 db-migrate) 뒤, 같은 psql 세션에서 export를 임시 테이블로 읽어 PG와 비교하고 후보를 만듭니다. 이 비교는 오탐이 있습니다. 마지막 worker 기동 이후 방의 최신 Kakao 이름을 가진 유일한 알람 행이 삭제되면 hash에는 그 Kakao 이름이 남고 PG에는 옛 이름만 남으므로, Kakao 이름이 관리자 별칭 후보로 잡힙니다. 그렇게 들어간 이름은 관리자 별칭으로 고정되어 이후 Kakao 이름 변경을 따라가지 않습니다. 그래서 후보를 넣기 전에 운영자가 화면에서 확인하고(값은 복사·기록하지 않음) 실제 Kakao 방 제목과 같은 후보는 지웁니다. `\copy` 경로는 psql 클라이언트가 읽는 위치여야 합니다.

```sql
CREATE TEMP TABLE room_name_export (room_id text PRIMARY KEY, room_name text NOT NULL);
-- \copy room_name_export FROM '<room_names.csv>' WITH (FORMAT csv)
CREATE TEMP TABLE room_name_candidates AS
SELECT e.room_id, btrim(e.room_name) AS display_name
FROM room_name_export AS e
WHERE btrim(e.room_name) <> ''
  AND char_length(e.room_id) <= 100
  AND char_length(btrim(e.room_name)) <= 255
  AND EXISTS (SELECT 1 FROM alarms AS a WHERE a.room_id = e.room_id)
  AND NOT EXISTS (
      SELECT 1 FROM alarms AS a
      WHERE a.room_id = e.room_id AND btrim(a.room_name) = btrim(e.room_name)
  );
SELECT count(*) FROM room_name_candidates;
-- 운영자 확인: 화면에서만 보고 복사·기록하지 않습니다.
SELECT c.room_id, c.display_name,
       (SELECT string_agg(DISTINCT a.room_name, ' | ') FROM alarms AS a WHERE a.room_id = c.room_id) AS pg_room_names
FROM room_name_candidates AS c
ORDER BY c.room_id;
-- Kakao 방 제목으로 판단한 후보를 지웁니다.
-- DELETE FROM room_name_candidates WHERE room_id IN ('<room_id>', ...);
INSERT INTO alarm_room_display_names (room_id, display_name)
SELECT room_id, display_name FROM room_name_candidates
ON CONFLICT (room_id) DO NOTHING;
```

4. 관리 목록(`GET /internal/alarm/keys`)에서 이관한 방 수를 확인한 뒤에만 아래 순서대로 `alarm:room_names`를 회수합니다. 별칭을 포기하기로 하면 그 결정을 기록하고 export 파일을 폐기한 뒤 회수합니다. export 파일은 수용이 끝나면 삭제합니다.

### 2차 rollback 규칙(migration 231~233 적용 뒤)

- 되돌리는 것은 image뿐입니다. 보존한 rollback tag를 `:prod`로 되돌리고 `up -d --no-build --no-deps hololive-alarm-worker hololive-api`로만 교체합니다.
- 231~233 적용 뒤에는 구 image의 `hololive-db-migrate`를 절대 실행하지 않습니다. 구 runner는 manifest 밖 ledger 행(092~094)을 이유로 거절하고, `--no-deps` 없는 `up`은 이 실패 때문에 API·worker·collector 기동까지 막습니다. schema는 되돌리지 않습니다. 구 SQL은 열을 명시하고 새 열은 DEFAULT가 있으며, 233의 템플릿 본문은 구 코드의 빈 `NextStream`과 같은 출력을 냅니다.
- `hololive-db-migrate`도 `hololive-api:prod` image를 쓰므로 retag 순간부터 migrate는 구 runner입니다. 재부팅 때 `hololive-compose.service`가 실행하는 `systemd-compose-up.sh`는 `up -d --no-build`를 `--no-deps` 없이 돌려 이 구 runner를 실행하고, API·worker·중앙 collector가 기동하지 못합니다. rollback 창에는 중앙 host를 재부팅하지 않고, retag 전에 현재 7.0.0 `:prod`를 `<service>:v7.0.0`으로 보존하고 `sudo systemctl disable hololive-compose.service`로 부팅 자동 실행을 끕니다. 이미 막혔다면 보존한 7.0.0 image를 `:prod`로 되돌리고 새 runner의 `run --rm hololive-db-migrate`(적용 없이 exit 0) 뒤 `enable --now`로 재전진합니다(명령은 `rollback.md`).
- image만 되돌린 동안 hololive-api는 트리 `VERSION`에서 온 `APP_VERSION=7.0.0`을 보고합니다. 판정은 image revision·version label로 합니다.
- rollback 직전에 `SCAN 0 MATCH auth:sess:* COUNT 1000` 반복과 `UNLINK`로 세션 key를 모두 지워 재로그인시킵니다. 새 코드가 발급한 세션은 `auth:user_sessions:*` 인덱스에 없어 구 reset이 폐기하지 못하고, 구 코드는 `session_generation`을 비교하지 않으므로 새 코드에서 reset으로 무효화된 세션이 되살아납니다. 세션 key는 사용자별로 걸러낼 수 없으므로 `session_generation > 0`인 사용자만 골라 지우는 대신 전체를 지웁니다.
- `auth:user_sessions:*`는 rollback 창이 닫힐 때까지 회수하지 않고 TTL(8일)로 만료시킵니다. 구 reset의 폐기 대상 목록이기 때문입니다.
- rollback 기간에도 방 이름 변경 동결을 유지합니다. 구 worker의 rename은 Valkey에만 남으므로, 동결을 풀었다면 재전진 전에 위 이관 절차를 다시 수행합니다.
- 구 image의 알람 upsert는 기존 행의 `alarms.room_name`을 바꿀 때 `room_name_updated_at`을 갱신하지 않습니다(새 행은 DEFAULT `now()`). 재전진 뒤 관리 목록의 Kakao 대표 이름은 그 방의 다음 알람 upsert 전까지 rollback 이전 기준으로 골라질 수 있습니다. 영향은 관리 화면 표시 이름에 한정됩니다.
- 폐기 key를 회수한 뒤 rollback하면 구 worker가 기동·rebuild 때 `alarm:registry`·`alarm:{roomID}`·`alarm:room_names` 등을 PG에서 다시 만듭니다. 재전진 뒤에는 아래 회수 표 1~8을 같은 순서로 다시 수행합니다.
- C4에서 233이 거절되면(사전 점검을 건너뛰었거나 그 뒤 본문이 바뀐 경우) 231·232만 적용된 상태입니다. 위 사전 점검 쿼리로 찾은 본문에서 `NextStream` 참조를 지운 뒤 새 image의 db-migrate를 다시 실행하거나, 위 첫 규칙대로 `--no-deps`로 구 image를 되돌립니다. 233은 한 transaction이라 거절 시 바뀐 행이 없습니다.

### 2차 폐기 key 회수

> **금지**: `alarm:*`, `membernews:*`, `notified:*`, `youtube:producer:*`, `ratelimit:*` 패턴 삭제와 `FLUSHDB`/`FLUSHALL`을 쓰지 않습니다. `alarm:*`에는 유지 key(`alarm:channel_registry`, `alarm:channel_subscribers*`, `alarm:member_names`, `alarm:subscriber_cache_empty`, `alarm:dispatch:wakeup*`)가, `membernews:*`에는 주간·월간 실행 잠금이, `notified:*`에는 활성 dedup claim이 있습니다. 활성 분산 rate-limit bucket은 `ratelimit:sliding:` 아래(`KeyPrefix` `ratelimit:sliding` + `BucketBase`, 예: `ratelimit:sliding:youtube:producer:*`)에 있으며 회수 대상이 아닙니다. `youtube:producer:*`는 표 7의 정확한 family로만 회수합니다. 회수 대상은 아래의 정확한 key 또는 family로만 지정하고, 회수 도구는 room·user 식별자와 값을 로그에 남기지 않습니다.

회수 순서는 표의 위에서 아래입니다. `alarm:user_names`(Kakao user ID→닉네임 평문)를 먼저 지우고, `alarm:room_names`는 별칭 이관(위 절차 4)이 끝난 뒤에만 지웁니다.

방 key `alarm:{roomID}`는 두 경로로 열거해 합칩니다. 첫째 `SMEMBERS alarm:registry`(크면 `SSCAN`), 둘째 읽기 전용 `SCAN 0 MATCH alarm:* COUNT 1000 TYPE set` 반복입니다. 두 결과 모두 key 이름이 `^alarm:-?[0-9]+$`와 맞고 `TYPE`이 `set`인 것만 대상으로 합니다. 유지 key 이름(`alarm:channel_registry`, `alarm:subscriber_cache_empty`, `alarm:member_names`, `alarm:registry`, `alarm:room_names`, `alarm:user_names`)이나 `:`를 포함하는 registry 원소는 건너뛰고 건수만 기록합니다. registry만 evict되고 방 key가 고아로 남은 경우를 SCAN이 잡습니다. 방 key를 `UNLINK`한 뒤 `alarm:registry`를 지우고, 같은 SCAN을 다시 돌려 대상 0건을 확인합니다.

| 순서 | key | TTL | 회수 방법·비고 |
|---|---|---|---|
| 1 | `alarm:user_names` | 없음 | 즉시 `DEL`. 평문 닉네임 |
| 2 | `alarm:{roomID}` → `alarm:registry` | 없음 | 위 두 경로 열거, 방 key 뒤 registry |
| 3 | `alarm:channel_registry:version` | 없음 | `DEL` |
| 4 | `acl:settings`, `acl:mode`, `acl:rooms:whitelist`, `acl:rooms:blacklist` | 없음 | ACL mirror, `DEL` |
| 5 | `{acl:rooms:whitelist}:tmp:*`, `{acl:rooms:blacklist}:tmp:*` | 없음 | 구 ACL 원자 교체의 임시 set. SADD와 RENAME 사이에 프로세스가 죽으면 남음. family별 `SCAN MATCH` 후 `UNLINK` |
| 6 | `alarm:next_stream:*` | 없음 | writer는 이관 시점부터 없었고 reader는 이번 브랜치에서 제거(migration 233). `SCAN MATCH` 건수 확인(예상 0) 후 `UNLINK` |
| 7 | `youtube:producer:community-missing:*`, `youtube:producer:channel-health:*`, `youtube:producer:snapshot-interval:*` | 24시간·24시간·간격 | 구 producer state store는 production에 주입된 적이 없어 예상 0. family별 정확한 `SCAN MATCH`만 사용 |
| 8 | `alarm:room_names` | 없음 | 별칭 export·backfill 확인 뒤 `DEL`. 평문 방 제목 |
| — | `auth:user_sessions:{userID}` | 8일 | rollback 창이 닫힌 뒤에만 회수, 그 전에는 TTL 만료 |
| — | `member-cache:v2:data:*` | 30분 | 자동 만료 |
| — | `youtube:channel_stats:*`, `search_channels:*`, `channels_live_status_*` | 5분·10분·30초 | 자동 만료 |
| — | `hololive_channels`, `channels_live_status`(접미사 없음) | 20분·5분 | 구 코드에 호출자 없는 setter만 있었음. 예상 0, 있어도 TTL로 소멸 |
| — | `admin:channel_stats`, `admin:channel_stats:refresh_lock` | 잠금 5분 | 존재하면 `DEL` |

특히 `membernews:*` 전체 삭제는 유지할 주간·월간 실행 잠금까지 지우므로 금지합니다. 정리 명세와 key별 I/O 검증은 폐기 대상의 정확한 이름으로 제한합니다.

소스 fadeout(S), runtime cutover(R), 저장된 폐기 key 회수(D)를 각각 보고합니다. S만 통과했으면 patch 검증 완료일 뿐 운영 적용 완료가 아닙니다. D가 미승인/미실행이면 data residue를 명시합니다. S/R/D의 미완료를 숨기거나 이를 이유로 새 코드에 구 경로를 부활시키지 않습니다.

## 구현 task

T31~T36은 하나의 patch를 만드는 내부 순서입니다. task별 별도 legacy-compatible 배포를 하지 않습니다. T37은 운영 전환안을 준비하는 task이며 live activation은 포함하지 않습니다.

### T31 제거 집합과 consumer 명세 확정

소유: shared providers/cache, API membernews/matcher/bootstrap, worker bootstrap, scripts. D1/D2의 모든 declaration·caller·mock·literal 소비를 갱신하고 경계 밖 Go/script 사용 여부를 확인합니다. source/CLI 변경과 이름 정규화안을 명시하고 범위의 미결정을 닫습니다. privacy/negative-test literal 예외도 정확히 기록합니다. AC31, V31에 연결합니다.

### T32 membernews mirror 기능 완전 종료

의존: T31. repository cache 의존·mirror 전용 log 필드/인자·helper·구 warmup service/repository method·호출·전용 테스트를 같은 패치에서 제거합니다. bootstrap은 기존 ListSubscribedRooms로 warning-only 확인을 보존합니다. `initMemberNewsService`의 불필요해지는 cache 인자와 runtime caller 1곳·test caller 3곳을 함께 제거하며, cache 비의존 `ProvideLLMCostTracker()`와 상위 scheduler의 필요한 cache 연결은 유지합니다. AC32, V32에 연결합니다.

### T33 멤버 hash 기능과 공용 API 완전 종료

의존: T31. reader를 기존 멤버 source로 연결하고 initializer·backend interface·embedding·method·mock·동적 matcher branch 및 matcher의 전용 cache 인자를 모두 제거합니다. `ProvideMemberCache`의 hash 초기화 전용 nil-cache 분기도 함께 종료합니다. 두 YouTube stack builder와 bot/admin source 전달을 함께 수정합니다. API bot/admin/llm·worker의 정상 epoch/L2 기능을 유지하며, 존재하지 않는 warm_member_cache 도구는 복원하지 않습니다. AC33, V33에 연결합니다.

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

PG SQL·idempotency·목록 순서·오류 의미가 유지되고 D1 key I/O는 0입니다. 구 warmup method와 repository cache 주입은 남지 않습니다. startup DB 오류는 warning-only이며 cache 비의존 토큰 metrics는 유지됩니다.

뉴스 정기 구독과 알람 멤버의 네 조합, 수동 조회, 빈 digest·멤버 없음 구분을 유지합니다. SQL 의미와 다른 기존 fake를 근거로 동일성을 주장하지 않습니다.

### AC33 멤버의 단일 조회 경로

새 runtime은 D2 key를 읽거나 쓰지 않습니다. backend-specific cache API/interface/mock 및 동적 matcher 경로는 없어야 합니다. provider 오류를 정상 빈 목록으로 저장하지 않습니다. YouTube 공유 channel 이름은 명시한 최소 영속 ID 대표 규칙을 따르고, 별도의 alarm 표시 이름·matcher 우선순위는 보존합니다.

### AC34 CLI와 설정 소비의 종료

구 hash env lookup·대기·표시·지원 flag가 남지 않습니다. 구 옵션은 명시적으로 거부되며 보안 env-loader test는 실제 env 검증 실패를 확인합니다. 새 readiness·fallback protocol을 만들지 않습니다.

### AC35 보호 기능과 오류 의미 보존

세션·nonce·limiter·member epoch·Pub/Sub·wakeup·API/LLM cache·ACL/alarm 이름·PG terminal ownership은 기존 경로를 유지합니다. 구 코드 테스트를 지운 것을 회귀 검증으로 대신하지 않습니다. 제거 key만 차단한 fixture에서도 새 기능과 유지 기능이 함께 작동해야 합니다.

뉴스 주간·월간 실행 잠금과 알람 사전 claim의 상이한 장애 동작, 기존 delivery mode별 enqueue와 worker 소유권도 보존합니다. 토큰 metrics를 엄격한 호출 제한으로 바꾸거나 퇴역한 Valkey 월 카운터를 복원하지 않습니다.

### AC36 단일 패치 검증 완료

구 기능의 declaration/call/selection 경로 0개, 전체 영향 module compile/test와 required gate 통과, source/CLI 변경 명세 및 동작 검증 근거가 한 source revision에 대응해야 합니다. unused parameter·no-op shim·버전 switch로 임시 통과시키지 않습니다.

### AC37 운영 완료 단계의 구분

S/R/D를 구분하고 C0~C6 결과를 해당 권한과 증거로 기록합니다. 정상 운영으로 개방할 때 release set에 구/new active writer가 섞여 있지 않아야 합니다. rollback하면 R 완료 판정을 철회합니다. 실제 운영을 실행하지 않은 문서를 배포 완료로 표시하지 않습니다.

## 회귀 명세

아래는 후속 구현 검증입니다. 준비 기록의 기존 코드 확인을 이 수용 기준의 통과 증거로 사용하지 않습니다.

| ID | 시나리오 | 기대 결과 |
|---|---|---|
| B01 | 구독·재구독·해지·없는 방 | 기존 PG 결과·정렬·idempotency, D1 명령 0 |
| B02 | PG Exec/Query/Scan 오류·caller 취소 | 기존 오류 전파, 성공/빈 목록으로 대체 없음 |
| B03 | startup 빈 목록/여러 목록/DB 실패 | List 조회 1회, cache warmup 없음, 오류는 warning-only |
| B04 | membernews bootstrap | repository·initMemberNewsService의 mirror 전용 cache 인자 없음. 토큰 metrics는 cache 없이 유지하고 상위 scheduler의 잠금·결과 cache는 보존 |
| B05 | exact/partial·별칭·동명 다른 조직 | matcher 결과·후보 규칙 보존, D2 조회 없음 |
| B06 | provider 최초 실패 뒤 성공 | 실패 결과를 negative match/snapshot으로 고정하지 않음 |
| B07 | 폐기 hash 부재/오래된 값/잘못된 값/의도적 접근 실패 | 같은 PG source이면 새 결과 동일, 해당 key 명령 0 |
| B08 | 공유 channel 복수 ID·colon·동명 동조직·빈 channel | 명시한 대표 규칙·필터 검증, map 순서 의존 없음 |
| B09 | YouTube 이름 로드 실패 | service nonfatal·기존 fallback, 새 retry/N+1 없음 |
| B10 | member epoch 변경·TTL·reload 경합 | 기존 epoch 회귀 통과, TTL 임의 변경 없음 |
| B11 | 구 CLI 옵션과 제거 env | flag는 unknown으로 실패, 구 env alias/readiness poll 없음 |
| B12 | literal env의 command substitution 입력 | 새 invocation이 env 검증에서 거부, 구 flag 오류를 성공 근거로 삼지 않음 |
| B13 | stats cache·alarm service의 이름 cache·토큰 metrics | 앞의 두 기능에 필요한 Valkey 호출과 cache 비의존 토큰 관측 유지. matcher의 퇴역한 이름 fallback 복원 및 cache nil 일괄 주입 없음 |
| B14 | 공용 cache mock·alarm durability 연결 | compile 및 기존 영속 상태 회귀 통과, member backend field 없음 |
| B15 | key별 privacy log 입력 | 유지된 민감 field 마스킹 작동. literal 잔존을 구 기능으로 오인하지 않음 |
| B16 | release set 일부 시작 실패/old writer 재등장 | traffic 개방 중지, 완료 판정 실패, 정해진 복구만 수행 |
| B17 | 새 release 중단 후 이전 세트 복원 모의 | 새 source의 구 branch 없이 복구 절차 수행 가능, runtime 완료 철회 |
| B18 | cleanup 승인 없음·TTL 없음 | S/R와 D 분리, 자동 만료·메모리 회수 완료를 주장하지 않음 |
| B19 | 뉴스 정기 구독 유무 × 알람 멤버 유무 | N01 네 조합 보존, 수동 조회에 뉴스 구독 조건 추가 없음 |
| B20 | alarms/members join·알람 타입·이름 중복·빈 이름 재구독 | 실제 PG SQL 기준 결과 보존, fake와 SQL의 빈 문자열 차이 해소 |
| B21 | 뉴스 주간·월간 잠금 성공/경쟁/Valkey 오류 | 기존 key·TTL·해제·skip/진행 의미 보존, nil로 비활성화 없음 |
| B22 | 알람 사전 claim의 Valkey 오류 | 해당 준비 건을 실패로 기록(skip과 구분). 뉴스 locker도 오류 시 실행하지 않는 fail-closed |
| B23 | delivery off/shadow/cutover와 같은 기간의 다른 본문 | 기존 enqueue 대상·identity·terminal 상태 보존, 양쪽 backlog 확인 |
| B24 | 알람 추가 PG 성공 뒤 cache 실패 | PG 저장과 반환 오류·재구성 결과를 구분, 뉴스 mirror 실패와 동일시하지 않음 |
| B25 | 알람 이름의 등록/warmup·host 구독·provider 실패 | 기존 producer·한국어 표시·fallback 보존, D2 제거를 이름 통일로 확대하지 않음 |
| B26 | LLM 사용량 기록과 퇴역 설정 경계 | 현재 토큰 metrics·비차단 동작 및 퇴역 env 거부 유지. 월 상한·Valkey 월 카운터를 다시 도입하지 않음 |
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

B01~B04/B19~B21/B27은 기존 fake pool과, cache를 유지하는 상위 경계에서 대상 key만 거부하는 spy로 검증합니다. repository와 initMemberNewsService에는 cache spy를 다시 주입하지 않습니다. B26은 기존 토큰 metrics·퇴역 설정 회귀로 검증합니다. repository_pgx_test의 이름만으로 실제 PG 통합을 실행했다고 주장하지 않습니다. N08의 join 검증 공백과 fake의 빈 문자열 처리 차이는 현재도 존재하므로, B20은 `membernews` 패키지의 `_test.go`에서 기존 `dbtest.NewPool(t)`로 실제 SQL을 실행합니다. 정본 SQL을 임의 수정하지 않고 fake를 실제 의미에 맞춥니다. LLM/Exa/Iris 외부 호출은 fake를 사용합니다.

`go test ./hololive/hololive-api/internal/planes/llm/internal/schedulerkit/... ./hololive/hololive-api/internal/planes/llm/internal/llm/... ./hololive/hololive-shared/pkg/service/delivery/... ./hololive/hololive-shared/pkg/service/alarm/...`

위 기존 suite에서 뉴스 잠금·관측·B22/B23의 claim 및 identity 근거를 확인합니다. 중복 방지·해지 semantics를 이 패치에서 재설계하지 않습니다. 실제 worker 발송은 enqueue 성공과 구분하며 격리 검증 뒤 승인된 운영 수용에서만 확인합니다.

### V33 cache API와 멤버 소비자 검증

`go test ./hololive/hololive-shared/pkg/service/cache/... ./hololive/hololive-shared/pkg/providers/... ./hololive/hololive-shared/internal/service/youtube/apiservice/... ./hololive/hololive-shared/pkg/service/notification/alarmservice/... ./hololive/hololive-api/internal/planes/bot/internal/service/matcher/... ./hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/... ./hololive/hololive-api/internal/planes/bot/internal/command/handlers/... ./hololive/hololive-api/internal/planes/bot/runtime/... ./hololive/hololive-api/internal/planes/admin/app/... ./hololive/hololive-alarm-worker/internal/app/workerapp/...`

`go test -race ./hololive/hololive-shared/pkg/service/member/... ./hololive/hololive-api/internal/planes/bot/internal/service/matcher/... ./hololive/hololive-shared/internal/service/youtube/apiservice/...`

B05~B10/B13/B14/B24/B25를 검증합니다. 기존 matcher allocation budget과 alias/org/provider 오류 회귀를 유지하며 helper 삭제를 이유로 기능 회귀까지 삭제하지 않습니다.

### V34 script와 개인정보 보호 검증

`bash -n hololive/hololive-api/scripts/bot.sh`

`bash hololive/hololive-api/scripts/test-bot-env-loader.sh`

`go test ./hololive/hololive-shared/pkg/privacylog/...`

추가 CLI fixture는 임시 repo/PID/log/가짜 container·process 도구에서 B11/B12를 검사합니다. 보안 fixture의 기존 command substitution rejection assertion을 보존합니다. 실운영 start/stop/restart를 호출하지 않습니다. B15에서는 과거 key 입력에도 마스킹이 유지되는지 확인합니다.

### V35 전체 영향 모듈과 필수 게이트

`go test ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/... ./hololive/hololive-dbtest/...`

`./scripts/ci/local-ci.sh`

공용 interface 삭제이므로 직접 consumer만 테스트하고 종료하지 않습니다. 모든 compile/test/build는 kapu에서 실행합니다. applicable NilAway/race·Stage 3/prerequisites와 기존 boundary/standalone 규칙을 유지하며, 실패하면 원인과 영향 범위를 기록하고 억제로 통과시키지 않습니다. 신규 production dependency나 toolchain/lockfile 일괄 업그레이드는 포함하지 않습니다.

### V36 변경·artifact·전환 검토

`git diff --check`와 `./scripts/architecture/ci-boundary-gate.sh`를 수행합니다. [현재 계획 규칙](../../../../docs/agent-workflows/README.md)에 따라 DEC/PLN 등록·lifecycle·폐기된 카탈로그 검사기는 실행 조건이 아닙니다. 기존 애플리케이션 계약·필수 코드 게이트·사용자 승인 경계는 그대로 유지합니다.

필요한 이미지 build는 `./build-all.sh --build-only --no-bump` 등 기존 build-only 경로로 준비하며 운영 artifact는 clean reviewed full SHA·대상 architecture·image ID를 고정합니다. 출판 승인 시 `scripts/ci/pre-push-gate.sh`가 별도 필수입니다. cutover는 owning ops의 필요한 정적 배포 계약과 no-build 절차를 적용하며, 이 문서 때문에 전체 stack/runtime을 재배포하지 않습니다.

증거에는 source SHA+관련 diff, D1/D2 명세, 실행한 명령/exit, scenario별 결과, 유지 literal 사유, startup/PG/cache 호출 수, 미실행 검증, S/R/D 상태를 남깁니다. 절감 목표는 구독·해지의 target-key 명령 각 2→0, warmup target-key 명령 2~4→0, matcher rebuild·YouTube 초기화의 중복 hash 조회 1→0입니다. 이는 정적 호출 수이며 운영 latency/메모리 개선 수치가 아닙니다.

## 실행 전 미결 조건과 인계

1. source/CLI 계약 삭제와 이름 정규화, 외부 consumer 확인을 포함한 **전체 D1/D2 패치 범위**의 검토가 필요합니다. 발견한 consumer를 조용히 예외 처리하거나 no-op을 남기지 않습니다.
2. C0~C5의 실제 host/process 목록·downtime·quiesce·supervisor·artifact set·rollback preflight는 운영 준비에서 확정합니다. 준비가 없으면 API·alarm-worker의 mixed fleet를 허용하는 식으로 전환 방식을 약화하지 않습니다. collector·PO는 C5b에서 같은 SHA로 올립니다.
3. cleanup 권한·정확한 key 소유·수용 기간은 별도입니다. 운영 데이터 회수 미실행을 코드/런타임 구현 실패와 혼동하지 않습니다.
4. 공개 backend Go API·hash 전용 CLI/env 폐기와 공유 channel 이름 정규화를 포함한 D1/D2 구현은 사용자 승인에 따라 아래 구현 기록의 worktree에서 수행했습니다. DEC/PLN 상태나 lifecycle 등록을 승인 대용 또는 추가 실행 게이트로 사용하지 않습니다.

이전 selective-retention 제안의 필요한 Valkey 유지 방향은 보존하되 단계적 전달안은 철회했습니다. 후속 실행자는 이 문서의 새로운 marker와 빅뱅 감사의 제거 명세를 사용합니다. 이전 패킷 일부의 테스트 결과를 새 전체 patch의 완료 근거로 승계하지 않습니다.

## 2026-09-28 구현 준비 기록

### 기준과 현재 판정

- 기준 HEAD: `a03eb83f9bdf38a451c86aa9627d2df008bcffbf`. 앞선 감사의 `3be7229b060b`와 구분합니다. 이 절은 구현 전 준비 시점의 기록이며, 구현 결과는 아래 구현 기록을 따릅니다.
- 시작 시 기존 미커밋 파일 5개를 확인했습니다: `workerapp/worker_registry.go`, `settings/alarmworker/worker_profile.go`, `worker_profile_test.go`, `settings/stack_worker_profile_types.go`, `settings/testdata/stack-worker-profile-alarm-worker.json`. 모두 보존하며 현재 checkout을 clean release source로 간주하지 않습니다.
- 환경: kapu, Go `go1.27.1 linux/amd64`, Docker client/server `29.8.1`. 로컬 `go.work`의 shared-go·iris-client-go 및 다섯 hololive 모듈을 사용합니다. 의존성·toolchain·go.work 갱신은 하지 않았습니다.
- 준비 시점에는 T31 로컬 consumer 조사까지만 진행했습니다. T32~T35 구현과 검증은 아래 구현 기록에 있습니다. T37 운영 topology 조사는 미착수이며 **R/D는 미완료**입니다. key 존재·크기·실제 배포 상태는 조회하지 않았습니다.

### 현재 코드에 맞춘 구현 인계

아래 `api`, `shared`, `worker`는 앞의 모듈 약칭과 같습니다. 과거 감사는 덮어쓰지 않으며 현재 코드와 다른 전제는 이 계획의 명세·수용 기준에서 교정했습니다.

| 대상 | 현재 확인과 구현 인계 |
|---|---|
| D1 생성자·startup | `api/.../membernews/repository.go`, `repository_cache.go`, `repository_mutation.go`, `service.go` 및 `runtime/bootstrap_alarm.go`. mirror 전용 cache/log 제거 후 `NewRepository(postgres)`로 축소. `initMemberNewsService` caller는 `bootstrap_llm_scheduler.go` 1곳과 `bootstrap_alarm_llm_helpers_test.go` 3곳 |
| D1 토큰 전제 교정 | `runtime/llm_providers_local.go:68-72`는 cache 없는 metrics recorder. `shared/pkg/config/settings/config_llm_retired_env.go:8-18`은 월 상한·Valkey 월 카운터 퇴역 계약. B04/B13/B26을 현재 계약으로 교정했으며 구 카운터를 되살리지 않음 |
| D1 SQL 회귀 공백 | `repository_test.go` fake는 빈 이름 재구독 시 기존 이름을 보존하지만 `queries/repository_mutation_0033_01.sql`의 COALESCE는 non-null 빈 문자열로 덮어씀. SQL은 유지하고 fake를 교정. `repository_pgx_test.go`는 실제 join 테스트가 아니므로 B20을 `membernews` 내부의 `dbtest.NewPool(t)` 격리 PG 회귀로 추가 |
| D2 producer·backend·mock | `shared/pkg/providers/member_providers.go`, `pkg/service/cache/{member.go,member_cache.go,interface.go}`, `cache/mocks/{client.go,client_domain.go}` 및 대응 테스트. `alarm_service_durability_test.go`의 member mock 연결만 제거하고 durability 회귀 유지 |
| D2 matcher 추가 caller | `matcher` 구현·테스트·benchmark 외에 `providers_alarm_consumers.go`, `services_alarm_stack.go`, `runtime/providers_single_consumer_test.go`, bot command/handler 테스트의 모든 `NewMatcher` 호출 갱신. `matcher_candidate.go:135-137`에서 확인한 퇴역 이름 fallback을 복원하지 않음 |
| D2 YouTube source | `shared/pkg/providers/modules/{youtube_api_stack.go,youtube_stack.go}`, bot `services_alarm_stack.go`, admin `build_runtime_youtube.go`에 기존 member adapter 전달. 통계 cache 인자는 유지. `BuildYouTubeStack`은 checkout 내 caller가 없지만 이번 준비에서 삭제를 추가하지 않으며 기존 계획대로 source 전달을 갱신 |
| D2 대표 이름 | `member/channel_representative.go:7-26`의 최소 ID 규칙을 한 helper로 재사용. 영속 ID fixture와 빈 Name의 기존 title fallback 검증. shared member 내부 caller와 epoch/point-index 회귀도 승격 변경 범위에 포함 |
| K2 실제 검증 경로 | bot/admin/worker의 `BuildInfraModule`뿐 아니라 llm `bootstrap_llm_scheduler.go`의 `ProvideMemberCache`도 현재 writer. initializer 제거 후 네 경로의 epoch/L2 유지 검증. 과거 warmup 도구 경로는 현 checkout에 없음 |
| script·privacy | `bot.sh`의 start/restart/help/status에서 hash/env/flag를 함께 제거. env-loader fixture는 새 invocation으로 실제 literal 검증에 도달. `privacylog/cachekey.go`의 D1 마스킹 및 `cachekey_test.go`의 D1/D2 입력은 보존 |

인접 `shared-go`, `iris-client-go`, `twentyq-bot`에서 hololive import·D2 전용 symbol/key/env/flag의 텍스트 검색은 0건이었습니다. `scripts`·`deploy`의 D1 key/warmup 검색도 0건입니다. 이는 설치된 script·비공개 외부 Go consumer·문자열 조합 호출까지 부재를 증명하지 않습니다. 조사에서는 텍스트 검색과 코드 읽기를 사용했으며, 구현 시 공개 symbol의 타입 참조 분석과 전체 module compile을 생략하지 않습니다.

### 이번에 실제 수행한 확인

모두 kapu의 현재 작업 트리에서 실행했습니다. 기존 동작의 baseline이며 D1/D2 제거 완료나 새 B01~B27 통과 증거는 아닙니다.

| 명령·범위 | 결과 |
|---|---|
| `hostname`, `go version`, `go env GOWORK GOTOOLCHAIN GOOS GOARCH`, `docker version` | 위 환경 확인, exit 0 |
| `go list -mod=readonly`로 membernews/runtime/cache/providers/apiservice/matcher/dbtest 조회 | 13 package 해석, exit 0. compile/test의 대체 아님 |
| 아래 focused baseline 명령 | 12 package 모두 `ok`, exit 0 |
| `go test -mod=readonly -count=1 -v ./internal/workspace -run '^TestRuntimeSplitStandaloneModulesContract$'` | 이름 지정 테스트의 실제 실행·PASS 확인, exit 0. 종전 `go test .`는 테스트 소유 package가 아니므로 V35·README 교정 |
| `bash hololive/hololive-api/scripts/bot.sh help` | 실행 성공 및 현행 구 옵션 노출 확인. start/stop/status·실제 runtime 호출 없음 |
| `bash -n hololive/hololive-api/scripts/bot.sh` | exit 0 |
| `bash hololive/hololive-api/scripts/test-bot-env-loader.sh` | 격리 fixture의 command substitution 거부 PASS, exit 0. 아직 구 옵션을 쓰는 baseline |
| `bash scripts/architecture/check-project-map.sh` | toolchain parity·module inventory·문서 참조 PASS, exit 0 |

```bash
env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  go test -mod=readonly -count=1 \
  ./hololive/hololive-api/internal/planes/llm/internal/service/membernews/... \
  ./hololive/hololive-shared/pkg/service/cache/... \
  ./hololive/hololive-shared/pkg/providers/... \
  ./hololive/hololive-shared/internal/service/youtube/apiservice/... \
  ./hololive/hololive-api/internal/planes/bot/internal/service/matcher/... \
  ./hololive/hololive-shared/pkg/privacylog/...
```

준비 시점 미실행 항목(구현 뒤 결과는 아래 구현 기록): 새 B20 실제 PG join 회귀, 전체 영향 module test/build, runtime bootstrap 전체 suite, NilAway/race·local-ci·Stage 3/prerequisites, architecture 전체 gate, image build, 운영 수용.

## 2026-09-28 구현 기록

### 위치와 상태

- worktree: `~/work/holo-cache-fadeout-20260928/hololive-bot`, branch `refactor/valkey-bigbang-fadeout-20260928`, base `a03eb83f9bdf`. 커밋 전 미커밋 상태이며 Git publication·운영 반영은 하지 않았습니다.
- sibling `shared-go`·`iris-client-go` worktree는 go.mod pin(`v2.8.0`)과 같은 `36d47654f2c0`·`b23933c3179c`로 고정했습니다. 두 저장소 main(v2.9 이후·v3 module)을 쓰면 import graph 산출물이 이번 변경과 무관하게 달라집니다.
- **S(source fadeout): 로컬 검증 완료. R(runtime cutover)·D(key 회수): 미착수.** T37 운영 topology 조사도 미착수입니다.

### 반영 내용

- D1: `repository_cache.go` 삭제, `NewRepository(postgres)`, `WarmupSubscriptionCache` 삭제, `initMemberNewsService` cache 인자와 caller 4곳 정리, 기동 시 `ListSubscribedRooms` 1회 warning-only. fake Exec를 실제 COALESCE 의미로 교정했습니다.
- D2: `cache/member.go`·`member_cache.go`·`member_cache_test.go` 삭제, `DomainCache` embedding·mock field/method·provider 초기화 제거, matcher 동적 hash 경로·`Matcher.cache`·`NewMatcher`/`ProvideMatcher` cache 인자 제거, `matcher_cache_failure_test.go` 삭제. `member.ChannelRepresentatives` 승격, `apiservice.New`와 두 YouTube stack params에 `MemberData` 추가, bot·admin wiring 연결.
- script: `bot.sh`의 hash 대기·상태·`--no-ready-wait`·`CORE_MEMBER_HASH_SOFT_*` 제거. env-loader fixture는 `start`로 literal env 거부에 도달하며, 제거 옵션이 start/restart 모두에서 unknown argument로 거절되는지도 검사합니다.
- 부수 정리: `scripts/deploy/ap-rsync-files.txt`의 삭제 파일 2줄 제거, `artifacts/architecture/go-workspace-import-graph.txt`를 실제 import 변화로 재생성, standalone 계약 명령을 README·V35에서 교정했습니다.

### 새·이관 회귀

| 회귀 | 근거 |
|---|---|
| B20 실제 PG | `membernews/repository_postgres_test.go`: 비-LIVE 알람, 이름 우선순위, members 미존재 channel, DISTINCT, 방 격리, 빈/공백 이름 재구독 덮어쓰기, created_at 순서, 해지. testcontainers PG에서 실행 |
| B06·B05 | matcher provider fixture: 오류 전파 후 재시도 성공(비고정), Hololive 후보 우선, 동명 다른 조직 모호성, alias·partial, allocation budget 유지 |
| B07 | `providers/member_providers_test.go`: 폐기 hash에 임의 값이 있어도 PG 결과만 사용하고 hash를 삭제·변경하지 않음. colon 이름도 기동을 막지 않음(miniredis + testcontainers PG) |
| B08·B09·B13 | `apiservice/service_test.go`: 최소 ID 대표(입력 순서 무관), 동명 다른 조직, colon, nil·빈 channel, 빈 대표 Name의 fallbackTitle, 로드 실패 nonfatal. strict cache mock으로 이름 초기화의 cache 명령 0회 확인 |
| B11·B12 | `test-bot-env-loader.sh` |

B07의 수정 전 실패는 삭제된 초기화가 hash를 DEL/HSET하고 colon field를 거절하던 코드에서 추론한 것이며, 수정 전 코드로 재실행하지 않았습니다.

### 실행한 검증

| 명령 | 결과 |
|---|---|
| 제거 symbol·key·env·flag 검색(`hololive`, `scripts`, `deploy`) | 실행 경로 0. 남은 literal은 `privacylog/cachekey.go`·`cachekey_test.go`와 env-loader의 거절 검사뿐 |
| `go build`·`go vet` 다섯 모듈 | exit 0 |
| 다섯 모듈 전체 `go test -count=1` | youtube-collector `youtubejs` helper 테스트만 새 worktree의 npm 의존성 미설치로 실패. 다른 패키지는 모두 ok. local-ci의 `npm ci` 뒤 해당 테스트는 통과 |
| `bash -n bot.sh`, `test-bot-env-loader.sh`, `bot.sh help` | exit 0, 새 usage 확인 |
| `go test ./internal/workspace -run '^TestRuntimeSplitStandaloneModulesContract$'` | ok |
| `./scripts/ci/local-ci.sh` | **exit 0**: architecture gate, gofmt, go fix, tidy, vet, staticcheck, golangci-lint(0 issues), NilAway, build, PGO, collector gate, AP rsync manifest, PostgreSQL capacity, perf budget, 전체 Go test, race test. integration-tag 테스트는 기본값대로 skip |
| 독립 reviewer 1차 | 정확성 판정 correct. 지적한 계획 기록 불일치와 removed-key fixture 부재를 이 기록과 B07·strict cache 회귀로 반영 |
| 독립 reviewer 2차(최종 diff) | correct, 차단 결함 없음. 14개 package `-race` 통과, import graph 재생성 byte-identical 확인. P3 1건(env-loader의 인자 거절 선행 assertion이 실패할 수 없음)은 stderr의 loader 거절 부재 검사로 고쳤고 fixture·shellcheck 재통과 |

미실행: `scripts/ci/pre-push-gate.sh`(publication 시 필수), `./build-all.sh --build-only --no-bump` image build, `RUN_INTEGRATION_TESTS=true` 통합 테스트, 운영 C0~C6.

### 남은 작업

1. 1차 커밋 `3a28ea2`는 브랜치에 있고 main 통합은 main checkout의 별개 미커밋 작업 때문에 보류 중입니다. 겹치는 파일은 3-way 병합 시험에서 import graph 산출물만 충돌했습니다.
2. 외부 설치 자동화·비공개 Go consumer의 부재는 증명하지 않았습니다. 발견되면 publication을 멈추고 같은 변경 집합에서 처리합니다.
3. 배포는 API·alarm-worker 동시 교체(C0~C5), 폐기 key 회수는 정확한 key 이름으로 별도 승인(C6)이 필요합니다.

## 2026-09-28 2차 축소 기록

같은 worktree·브랜치에서 1차 커밋 위에 이어서 구현했습니다. 사용자 결정: 세션 세대 컬럼, 관리자 방 이름 우선·PG 저장, apiservice 삭제, names/user API 삭제, YouTube producer state store 삭제. next_stream은 리뷰만 했습니다.

### 반영 내용

| 영역 | 변경 | 근거 |
|---|---|---|
| P1 LIVE 구독 누락 | checker가 set 비어 있고 empty marker가 없으면 batch PG 조회로 확정(read-through, set 미warm) | TTL 없는 set의 allkeys-lfu eviction 시 알림이 조용히 빠짐 |
| P1 reset 후 세션 | migration 231 `auth_users.session_generation`, reset이 같은 문장에서 +1, Me·Refresh가 비교, `auth:user_sessions` 삭제 | 인덱스 eviction·폐기 실패 warn·Refresh 경합 |
| P1 방 이름 유실 | migration 232 `alarm_room_display_names`·`alarms.room_name_updated_at`, 관리 목록 PG 전환, 방 index·이름 hash·version key·names/user API 삭제 | 관리자 이름이 Valkey에만 있어 rebuild·재등록 때 사라짐 |
| ACL·설정 | `acl:*` mirror와 `config:update` Pub/Sub 삭제, bot plane은 직렬화된 in-process reload, 실패는 500 `acl_bot_resync_failed`와 재시도 수렴, alarm_advance 단일 HTTP 경로 | mirror reader 0이면서 Valkey 장애가 ACL 변경을 막음, worker 3중 적용 |
| member cache | L2 data 삭제, epoch를 ≠ 변경 신호로 | L2 hit도 snapshot 객체만 반환, Valkey 재시작 뒤 영구 PG 우회 |
| 죽은 코드·캐시 | apiservice·채널 통계 캐시·전용 env 퇴역 가드, producer state store, Holodex 무효 캐시, cache API 축소, dead claim·locker·AlarmDispatchState, `GetChannelStats` | production 호출 0 또는 hit≈0 |
| 문서 | 잠금 fail-closed 정정(감사 N03, 리뷰 R08, 이 계획), settings·QUEUE·DEPLOYMENT·alarm·member-cache runbook 계약 갱신, 폐기 key 회수 표 | 코드와 문서 불일치 |

### next_stream 결론: 삭제(2차 후속)

`alarm:next_stream:*`는 이 저장소 이관 시점(`1da02d2cb`, 2026-03-01)부터 writer가 없습니다. 당시 Rust scraper crate(`keys.rs`, `841526aee`에서 삭제)도 key 상수만 있었습니다. 그래서 `!알람 추가`의 '다음 방송'과 `!알람 목록`의 방송 중 표시는 한 번도 나간 적이 없고 운영에서 이를 알릴 신호도 없습니다. iris-console·chat-bot-go-kakao·twentyq-bot·tools·deploy·iris-bridge·Iris에서 `/next-stream` route와 `NextStream` 소비자를 검색한 결과 0건이었습니다. 이중 경로를 두지 않는 원칙에 따라 reader 전체를 지웠습니다: alarmcache `GetNextStreamInfo`·batch HGETALL, `AlarmStateManager.GetNextStreamInfo`, worker `GET /internal/alarm/next-stream/:id`와 `get_next_stream_info_failed`, client, `domain.NextStreamInfo`, formatter의 다음 방송 view와 전용 상대 시간 문자열 필수 key(`timefmt/relative_*`), 템플릿 미리보기 샘플. migration 233은 알람 추가·목록 표준 본문(전역과 같은 본문의 채널 override)에서 다음 방송 분기를 지우며, 새 본문의 출력은 빈 `NextStream`일 때의 기존 출력과 같습니다. 다른 본문이 `NextStream`을 참조하면 233은 한 transaction 안에서 적용 전체를 거절합니다. 배포 전 `SELECT template_key, channel_id FROM notification_templates WHERE template_key IN ('CMD_ALARM_ADDED','CMD_ALARM_LIST') AND body LIKE '%NextStream%'`로 표준 외 본문이 없는지 확인합니다. 구 API는 `/next-stream` 404를 이미 Debug 로그 후 빈 값으로 처리하므로 전환 창의 혼합 버전도 출력이 같습니다. 필요하면 PG `youtube_live_sessions` 기반으로 별도 재구현합니다.

### 리뷰 후속(VkRuntime·VkDbOps)

- 방 이름 설정 요청의 저장 폭 초과(room_id 100자, 이름 255자)를 PG 오류 500 대신 400으로 거절합니다(worker `invalid_request_body`, 관리자 API `invalid request body`).
- 관리자 방 별칭 이관, 2차 rollback 규칙, 폐기 key 회수 순서·금지 패턴을 위 "복구와 data residue"에 반영했습니다.

### 검증

| 명령·검증 | 결과 |
|---|---|
| 각 구현 unit의 대상 패키지 `go vet`·`go test -race -count=1`(PG는 testcontainers 실제 실행) | 통과. 수정 전 실패 확인: LIVE 복구(0→1 알림), 세션 세대 비교 강제 통과 시 3개 실패, ACL reloadMu 제거 시 3/3 실패, Kakao 이름 정렬을 created_at으로 되돌리면 실패 |
| schema golden 재생성 | `alarm_room_display_names`, `alarms.room_name_updated_at`, `auth_users.session_generation`만 추가 |
| `./scripts/ci/local-ci.sh` | **exit 0**(architecture gate, lint 0 issues, NilAway, build, 전체 test·race). integration-tag 테스트는 기본값대로 skip |
| 독립 리뷰 | 알림·플랫폼·보안 3개. 반영: ACL follower reload 직렬화(P2)와 실패 가시화, 폐기 key 회수 표(P2), Kakao 이름 정렬, LIVE fallback의 SADD 경합, 죽은 admin Valkey 필드·`GetChannelStats`·문서 잔재 |

### 남은 작업·한계

- Iris Console(별도 저장소)의 `POST /api/holo/names/user` 호출 제거와 방 이름 공백=해제 의미 반영. 이전 Hololive는 빈 이름 해제를 400으로 거절하므로 Hololive v7.0.0 배포와 C5 수용이 먼저이고 Console은 그 뒤에 배포합니다. Console 방 이름 변경 동결은 Console 배포까지 유지합니다.
- member epoch ABA: Valkey 재시작 후 재생성된 epoch가 우연히 프로세스의 마지막 값과 같으면 snapshot TTL(5분)까지 이전 snapshot을 쓸 수 있습니다(runbook 기록).
- 단일 채널 구독 조회 경로(`resolveChannelSubscribersFromDB`)는 여전히 set을 warm하므로 같은 경합이 남아 있습니다(범위 밖).
- 배포는 migration 231~233 적용과 API·alarm-worker 동시 교체가 필요하고, 관리자 방 별칭 이관과 폐기 key 회수는 위 절차 순서로 별도 승인이 필요합니다.
