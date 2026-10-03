# Hololive API 레거시 계층과 성능 부채 심층 분석

2026-10-02의 로컬 체크아웃을 대상으로 [최초 API 조사](2026-10-02-hololive-api-refactoring.md#최초-api-조사-근거)를 확장했습니다. 기존 미커밋 변경을 포함한 현재 소스를 읽었으며, 이 조사에서는 제품 코드를 변경하지 않았습니다. 구조 이관을 구현하거나 운영에 적용한 결과가 아닙니다.

추가 조사에서는 설정 응답 유실·0 입력, 실제 자원 Close 순서·종료 시한, 병렬 성능의 선택률 민감도, 필터 CPU/할당, 관리 집계 SQL, observation의 파일·SQL·테스트 이동 위치까지 확인했습니다. [통합 실행 순서](2026-10-02-hololive-api-refactoring.md#실행-순서와-완료-기준)는 이 문서와 [저장소 경로 분석](2026-10-02-repository-layout-refactoring.md)의 근거를 합친 유일한 실행계획입니다. 아래 목표 package·포트·prototype은 설계 근거이며 실제 이관 완료가 아닙니다.

**내부 호환 계층을 남기지 않는 일괄 변경은 권고할 근거가 있습니다.** 특히 parser 함수 alias 제거, API 구성 책임 회수, worker 전용 구현 이관은 기존 소비자를 같은 체크아웃에서 함께 바꿀 수 있습니다. observation 이관도 한 소스 변경으로 끝내는 편이 타당하지만, SQL 최적화·저장 형식 변경·운영 데이터 이행까지 묶을 근거는 없습니다.

성능에서는 member-news 기간 사전 필터가 합성 실험에서 효과를 보였습니다. 활성 후보 10,000건 중 기간 내 후보 100건을 20회 조회·필터링할 때, 실행 시간 중앙값은 **581.2ms → 103.8ms**, 누적 Go 할당은 **496.5MB → 18.5MB**였습니다. 이는 DB 조회와 후보 필터 구간만의 로컬 측정입니다. 전체 digest, 실제 방 20개의 scheduler 실행, 운영 지연이나 처리량 개선율은 아닙니다.

## 조사 방법과 근거의 범위

- kapu의 `go1.27.1 linux/amd64`와 현재 `go.work`에서 네 업무 모듈의 `go list -e -json`을 다시 실행했습니다. package 자체의 Error는 0개였습니다. 전체 컴파일·lint 통과를 뜻하지 않습니다.
- API/shared의 비테스트 `.go` 파일을 Go AST로 파싱하여 type alias, selector 값을 재노출하는 선언, 단일 `return other.Call(...)` 형태를 조사했습니다. 이후 실제 참조를 읽어 판정했습니다.
- PostgreSQL은 `hololive-dbtest.NewBlankPool`이 만드는 격리 테스트 DB만 사용했습니다. `TEST_DATABASE_URL`, `TEST_DATABASE_OWNER_TOKEN`, `ALLOW_EXTERNAL_TEST_DB`를 제거하고 실행했습니다. 운영 DB·운영 설정·비밀 값은 읽지 않았습니다.
- SQL 실험은 실제 `Repository.ListActiveMajorEvents`와 `filter.FilterCandidates`를 호출했습니다. 비교 SQL은 테스트 querier에서 기간 조건을 붙였습니다. 기존 제품 파일을 바꾸지 않는 Go overlay로 실행했습니다.
- Holodex는 실제 provider와 직렬화 캐시 경로를 사용하고 원천 응답만 로컬 mock으로 대체했습니다. 실제 Holodex나 YouTube를 호출하지 않았습니다.
- 언어 검토에는 `modern-go-guidelines:use-modern-go`를 적용했습니다. alias, 구성 경계, 성능 해석, payload GC의 검토를 병렬로 보강했습니다.

파일 개수와 import 수는 구조를 설명하는 근거입니다. 이를 품질 점수나 새 gate로 사용하지 않습니다. API 72개 package/348개 GoFiles, shared 98/528이라는 기존 수치는 재확인됐습니다.

## 레거시 alias의 실제 규모와 제거 범위

### Go 타입 alias는 두 개입니다

API/shared 비테스트 소스 전체에서 AST의 `TypeSpec.Assign`이 존재하는 선언은 **2개**였습니다. 따라서 현재 부채를 “구형 타입 alias가 대량으로 쌓인 구조”라고 설명하면 부정확합니다.

| 선언 | 현재 역할 | 판단 |
|---|---|---|
| `officialScheduleIdentityIndex = officialidentity.Index` | htmlscraper의 필드와 테스트에서 과거 로컬 이름을 사용 | 같은 실제 타입을 직접 사용하고 alias 삭제 가능. 동작·직렬화 변경 불필요 |
| `live.Status = domain.LiveStatus` | live reducer가 domain 상태와 같은 타입을 사용. 상태 상수 3개도 재노출 | 구현 이관과 함께 원 타입 직접 사용 가능. `UPCOMING/LIVE/ENDED` 저장 값은 유지. 별개의 새 상태 타입을 만들 이유는 없음 |

근거: [identity alias](../../hololive/hololive-shared/internal/service/holodex/provider/htmlscraper/member_matcher.go#L10), [live 상태 alias](../../hololive/hololive-shared/internal/service/youtube/reconcile/live/types.go#L10). 타입 alias 삭제 자체의 실행 성능 개선을 측정하거나 주장하지 않습니다.

### 실제로 정리할 대상은 함수 alias와 전달 계층입니다

AST상 selector를 값으로 보관한 선언은 48개, 단일 호출을 반환하는 함수는 175개였습니다. 여기에 `time.Minute`, 색상 팔레트, SDK 계약값, 정상적인 adapter도 포함됩니다. 이 수치를 제거 대상 수로 쓰지 않습니다.

확정적인 정리 후보는 다음과 같습니다.

| 대상 | 실제 소비와 정책 | 권고 |
|---|---|---|
| scraping parser 함수 변수 11개 | 초기 선언 외 재대입·주소 취득·함숫값 전달 없음. 4개는 제품 호출, 7개는 테스트에서만 호출 | 제품과 테스트를 기존 `parser.X` 직접 호출로 한 번에 변경하고 변수 삭제 |
| bot `buildBotDependencyModules` | 제품 caller 없음. 테스트 2곳이 실제 builder로 그대로 전달하는 wrapper를 호출 | wrapper 삭제. 테스트는 실제 builder나 결과 동작을 검증 |
| bot privacylog의 단순 재노출 | 상수 6개, `RoomIDAttr`, `ChatIDAttr`, `IsCanonicalRoomID`, `Pseudonym`은 shared에 그대로 전달 | 필요 caller를 shared 구현으로 전환. 아래의 bot 고유 정책은 유지 |
| LLM standalone `Run` | production Fx 경로가 사용하지 않으며 lifecycle 테스트에서 호출 | 별도 signal/lifecycle 경로 삭제. 실제 Start/Shutdown/Close 검증은 유지 |

parser alias의 정확한 목록:

- 제품 호출이 있는 것: `parseShortNumber`, `parseViewCount`, `parseVideoCount`, `parseVideosFromRSSFeed`.
- 테스트에서만 호출하는 것: `findVideosTabContent`, `collectVideoRenderers`, `parseLockupVideoViewModel`, `pickLockupMetadataTexts`, `collectLockupTexts`, `pickViewCountAndPublished`, `fallbackPickMetadata`.

선언은 [stats_parser.go](../../hololive/hololive-shared/pkg/service/youtube/scraper/scraping/stats_parser.go), [videos_rss.go](../../hololive/hololive-shared/pkg/service/youtube/scraper/scraping/videos_rss.go), [recent_videos_parser.go](../../hololive/hololive-shared/pkg/service/youtube/scraper/scraping/recent_videos_parser.go), [videos.go](../../hololive/hololive-shared/pkg/service/youtube/scraper/scraping/videos.go#L15)에 있습니다. parser 알고리즘을 검증하는 테스트는 보존하거나 구현 소유 package로 옮깁니다. 함수 재노출 자체를 유지하기 위한 테스트는 필요하지 않습니다.

이 정리를 기능 삭제로 확대하면 안 됩니다. `parseVideosFromInitialData`는 `checkAlerts`와 오류 문맥을 추가하는 adapter이며, RSS 경로도 `GetRecentVideoPublishedTimes`의 제품 호출이 남아 있습니다. bot의 [RoomAttr/ChatAttr](../../hololive/hololive-api/internal/planes/bot/internal/privacylog/privacylog.go#L55)는 비어 있는 chat ID를 room name으로 보완하는 상관 키 정책을 가집니다. 이 정책을 일반 shared logger로 옮기는 것은 별도 책임 변경입니다.

### 유지 중인 호환 계약과 제품 별칭

| 항목 | 확인된 의미 | 제거에 필요한 근거 |
|---|---|---|
| Holodex `org=indie` | 이전 공개 query 값이며 `independents`로 해석하는 실제 호환 alias | API 소비자 전환 확인과 공개 입력 계약 이행. 내부 Go alias와 구분 |
| `HOLODEX_API_KEY_1` 등 퇴역 env 검사 | 구 키를 읽는 fallback이 아니라 존재 자체를 거절하는 이행 guard | 해당 키가 배포 원천과 실행 환경에서 없어졌다는 근거. 이번 조사에서는 확인하지 않음 |
| Stream의 Twitch/Chzzk 필드 | HTTP 응답과 보관 payload의 필드. worker는 구 제공자 payload를 잘못 발송하지 않도록 검사 | 미종결 delivery·보관 payload 드레인 및 외부 Stream API 소비자 확인 |
| 명령어·방송 종류·멤버 이름 alias | 현재 사용자 입력을 해석하는 제품 기능 | 명령/검색 계약 변경이 없는 한 유지 |
| `trimLegacyLeading` | 공백과 zero-width 문자를 제거하는 현재 입력 정규화 | 이름에 Legacy가 있다는 이유로 삭제하지 않음 |

근거: [공개 org alias](../../hololive/hololive-shared/pkg/service/holodex/provider/service_streams.go#L204), [퇴역 env 거절과 제거 조건](../../hololive/hololive-shared/pkg/config/settings/internal/load/retired_env_aliases.go#L8), [Stream 필드](../../hololive/hololive-shared/pkg/domain/stream.go#L71), [worker의 드레인 종단](../../hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_group.go#L224), [입력 정규화](../../hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/message.go#L67).

퇴역 guard의 `remove_after=2026-12-31`은 재검토 기한입니다. 날짜 도달이나 로컬 검색 결과만으로 운영 드레인이 완료됐다고 판단할 수 없습니다. 반대로 이런 guard를 영구 일반화 계층으로 확대할 이유도 없습니다.

## shared 책임 역전의 정확한 범위

### 직접 소비자보다 전이 의존성을 기준으로 봐야 합니다

세 runtime 모듈의 모든 일반 package에서 도달하는 shared package를 집계했습니다. command package를 포함하며 테스트 import는 제외합니다.

| 도달 가능한 runtime | shared package | GoFiles |
|---|---:|---:|
| API만 | 9 | 15 |
| worker만 | 7 | 59 |
| collector만 | 1 | 5 |
| API와 worker | 22 | 81 |
| API와 collector | 11 | 100 |
| worker와 collector | 1 | 1 |
| 세 runtime 모두 | 38 | 255 |
| 이 기준으로 도달하지 않음 | 9 | 12 |

마지막 행에는 테스트 helper, shared 자체 command 등이 있습니다. 미사용 제품 코드 9개라는 뜻이 아닙니다. 세 runtime 모두에 도달하는 package도 실제 공유 사업 책임인지, 넓은 package 때문에 딸려 들어오는 것인지 구분해야 합니다.

worker 전용 7개는 `internal/service/notification/alarmcache`, `pkg/config/settings/alarmworker`, `pkg/service/alarm/dedup`, `pkg/service/alarm/dispatchoutbox`, `pkg/service/alarm/queue`, `pkg/service/notification/alarmservice`, `pkg/service/youtube/outbox/format`입니다. **이 59개 GoFiles는 제품의 소비 관계가 단순한 소유권 회수 후보**입니다. 추가 조사에서는 테스트 58개 파일, SQL 32개와 API/dbtest의 테스트 의존성까지 확인했습니다. 아래 테스트 이관 설계와 함께 처리해야 하며 59개 파일 복사만으로 끝나지 않습니다.

API 전용 9개 중 `contracts/irisrooms`, `contracts/majorevent`, `contracts/membernews`, `contracts/settings`, `contracts/subscription`은 wire 계약입니다. 직접 Go 소비자가 한 모듈이라는 이유로 외부 HTTP 계약까지 API 구현 내부에 숨기지는 않습니다. 나머지 `apiplane`, `apperrors`, `net/imagehost`, `sqlsplit`은 실제 용도에 따라 이관을 판단할 수 있습니다.

### 구성 정책이 공통 구현을 오염시킵니다

| 현재 경로 | 책임 문제 | 이관 시 주의할 점 |
|---|---|---|
| `settings/apiplane` | API 포트·loopback·plane pool·YouTube runtime 설정을 shared가 소유 | 모든 소비자는 API. 그러나 `settings/internal/load` import 때문에 디렉터리만 API로 이동할 수 없음 |
| `providers` | DB factory를 쓰는 collector까지 Iris delivery·Holodex 조립에 의존 | infra 생성 primitive와 runtime별 조립을 분리. 전체 package를 API에 옮기면 worker/collector가 깨짐 |
| `providers/modules` | 전역 `settings.Config`로 DB/cache/member/settings를 함께 생성 | 필요한 options를 소유 composition에서 해석. 기존 strict validation과 오류 보존 |
| `internalhttp` | transport 생성자가 env를 직접 다시 읽음 | TLS/H3 옵션을 composition에서 한 번 해석하여 주입. 기동 시 오류와 client close 소유권 유지 |
| `service/settings → alarm/checker` | 파일 저장 adapter가 알림 검사 package의 정규화 규칙에 의존 | target-minute 규칙을 순수 정책으로 분리. 저장 adapter와 worker 모두 같은 규칙 사용 |
| `domain → util → valkey-go` | 시간 연산 때문에 domain의 package graph에 인프라가 들어옴 | 순수 시간 연산과 Valkey helper의 package 경계 분리 |

근거: [API 설정의 internal import](../../hololive/hololive-shared/pkg/config/settings/apiplane/runtime.go#L17), [복합 providers](../../hololive/hololive-shared/pkg/providers/infra_providers.go#L23), [internalhttp 환경 해석](../../hololive/hololive-shared/pkg/service/internalhttp/json_client.go#L50), [settings.Update](../../hololive/hololive-shared/pkg/service/settings/service.go#L164), [시간 규칙](../../hololive/hololive-shared/pkg/domain/stream.go#L122).

collector는 [infrastructure.go](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/infrastructure.go#L37)에서 `providers.ProvideDatabaseResources`만 호출합니다. metadata graph에서 이 edge를 `service/database`로 대체하는 정적 모의 계산을 하면 collector가 도달하는 shared package는 **51 → 48**입니다. 빠지는 것은 providers, delivery, sendoutcome 세 개입니다. 이는 추출 위치를 정한 구현안이 아닌 그래프 가설입니다. 통합 계획의 실제 DB 구성 목적지는 입력·cleanup 계약을 보존하는 `pkg/providers/database`입니다. 소스 패치를 빌드한 결과가 아니며, 51개가 모두 불필요하다거나 collector binary가 같은 비율로 작아진다는 주장은 아닙니다.

또한 providers는 `hololive-shared/internal/.../htmlscraper`를 사용합니다. API composition으로 옮길 때는 필요한 constructor의 경계도 함께 정리해야 합니다. private 구현을 호출하기 위해 새로운 광범위한 shared façade를 만드는 방식은 책임 역전을 다시 남깁니다.

### Bot에는 생성 계층뿐 아니라 종료 책임도 중복됩니다

현재 실제 경로는 `BotRuntime.Shutdown → Bot.Shutdown → BotLifecycle.Shutdown`입니다. 내부 [BotLifecycle](../../hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/lifecycle/bot_lifecycle.go#L111)이 Holodex를 중지하고 cache와 PostgreSQL을 닫습니다. 이후 상위 [plane cleanup](../../hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/services_cleanup.go#L27)이 내부 client→Iris→infra 순서로 닫고, [infra cleanup](../../hololive/hololive-shared/pkg/providers/modules/infra.go#L118)은 member cache를 중지한 뒤 DB와 cache를 다시 닫습니다.

**동일 자원의 Close 책임이 두 층에 있다는 점은 확인됐습니다.** 모든 반복 Close가 실제 오류를 내거나 운영에서 종료 경합이 발생했다는 뜻은 아닙니다. [member cache는 상위 context 취소와 분리](../../hololive/hololive-shared/pkg/service/member/cache.go#L97)되어 있습니다. 하위 cache.Close로 구독이 종료될 수 있으나, member-cache 작업을 먼저 명시적으로 종료·대기하는 infra cleanup 순서와는 다릅니다. 상위 resource owner의 순서만 수정하면 하위 Shutdown의 조기 Close가 남습니다. bot 내부는 자신의 작업을 drain하고, DB/cache와 그 위의 member-cache 수명은 plane owner 한 곳에서 관리하도록 같은 패치에서 바꿔야 합니다.

의존성 전달에서도 삭제할 실제 슬롯이 확인됐습니다. `Dependencies.MemberCache`, `Activity`, `Settings`는 private view로 복사되지만 orchestration의 실제 사용으로 이어지지 않습니다. `BotInfrastructure.AlarmCRUD/HolodexService`도 제품에서는 저장만 합니다. `InitAlarmYouTubeStack`의 Iris·formatter 인자는 이미 `_`로 무시됩니다. 근거: [Deps와 view](../../hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/deps.go#L52), [NewBot의 실제 소비](../../hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/bot.go#L94), [무시하는 인자](../../hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/services_alarm_stack.go#L24).

이는 `BotDependencyModules → Dependencies → 여섯 view`를 하나의 실제 입력 조립으로 줄일 구체적인 근거입니다. 반면 `commandInitView`는 repository·renderer·command adapter를 생성하므로 단순 필드 복사와 다릅니다. 또한 settings service를 읽지 않는 슬롯을 없앤다고 bot의 settings 파일 기동 검증까지 삭제하면 기존 실패 계약이 바뀝니다. 검증 자체는 명시적으로 유지해야 합니다.

최소 구성 패치는 기존 shared provider의 반환값을 사용하면서 API의 중복 foundation만 합칠 수도 있습니다. providers 전체 해체와 API 내부 중복 제거를 반드시 동시에 해야 하는 것은 아닙니다. 이후 provider 경계를 분리할 때 실제 worker/collector 소비자까지 완결된 변경으로 옮깁니다.

## member-news 성능 비용과 실제 실험

### 반복되는 작업은 SQL만이 아닙니다

[GenerateRoomDigest](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/service.go#L94)의 경로는 다음과 같습니다.

```text
매 방마다
  구독 멤버 조회
  전체 active major_events 조회와 pgx scan
  기간 후보 slice 생성
  방 멤버 profile 생성
  후보마다 제목·본문 정규화와 멤버 token map 생성
  방 멤버 매칭과 URL 검증
  stable sort
  prompt guard → 요약/기존 fallback → formatter → outbox enqueue
```

N개 방, 전체 활성 후보 M개, 기간 내 후보 K개라면 현재 공통 SQL은 N회, scan은 N×M행입니다. 본문 정규화·후보 멤버 토큰 생성은 대략 N×K회 반복됩니다. 멤버 매칭에는 방의 profile/token 수와 본문 길이도 영향을 줍니다. 단순 O(N×M) 한 식으로 전체 CPU 비용을 설명하지 않습니다.

[applyPeriodFilter](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/filter/filter_period.go#L30)는 실제 남는 후보 수가 작아도 원래 입력 길이 M만큼 capacity를 가진 결과 slice를 만듭니다. 이는 SQL scan 뒤에도 각 방에서 M에 비례한 추가 할당이 생기는 구체적인 이유입니다. [matchMembers](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/filter/filter_source_member.go#L118)는 같은 후보 본문과 멤버 token을 방마다 다시 정규화합니다.

현재 [만료 SQL](../../hololive/hololive-api/internal/planes/llm/internal/service/majorevent/queries/repository_events_0244_05.sql)은 event 종료/시작 날짜를 기준으로 상태를 바꿉니다. `pub_date`만 있고 두 event 날짜가 NULL인 과거 news는 그 조건에 해당하지 않습니다. 따라서 오래된 active news가 조회에 남을 수 있는 경로는 있습니다. 실제 운영에 몇 건이 남아 있는지는 측정하지 않았습니다.

### 합성 실험 조건

실제 테이블의 해당 열 타입과 기존 두 날짜 index를 따르는 최소 테스트 테이블을 만들었습니다. `pub_date`는 `timestamptz`, `event_start_date`는 `date`입니다. 실제 스키마 전체를 replay한 테스트나 운영 데이터 복제는 아닙니다.

- 활성 news 10,000건. 100건의 `pub_date`는 2026-10-02, 나머지는 2020-01-01. event 시작 날짜는 NULL.
- 한 후보의 설명은 `미코 공식 기사 `를 64회 반복했습니다. 모든 후보는 같은 멤버에 매칭되며, 기간 내 100건은 매 방에 남습니다.
- KST 2026-10-02 정오를 기준으로 weekly를 적용했습니다.
- 방 20개에 해당하는 조회·필터 작업을 **직렬**로 반복했습니다. 실제 scheduler의 최대 동시성 5를 재현하지 않았습니다.
- 각 대안을 5회 실행하고 실행별 GC 이후 `runtime.MemStats.TotalAlloc/Mallocs` 차이를 측정했습니다. 순서는 full → period → snapshot → index였으며 무작위 교차 실행은 아닙니다.
- `membersData`와 source validator는 nil입니다. 실제 멤버 조회, URL 검증, prompt guard, LLM, 검색, formatter, enqueue는 제외했습니다. 특히 후보가 있는 전체 `GenerateRoomDigest`를 nil prompt guard로 실행했다는 뜻이 아닙니다.

### 측정 결과

| 조회 대안 | 공통 SQL 수 | DB에서 Go로 반환한 행 | 문자열 필드 합계 | 중앙값과 범위 | 누적 할당 중앙값 |
|---|---:|---:|---:|---:|---:|
| 현재 전체 조회를 방마다 반복 | 20 | 200,000 | 279.76MB | 581.2ms, 563.5–696.2 | 496.5MB |
| 기간 SQL을 방마다 실행 | 20 | 2,000 | 2.80MB | 103.8ms, 100.0–107.6 | 18.5MB |
| 같은 기간 SQL에 임시 expression index 적용 | 20 | 2,000 | 동일 조회 열과 행 | 52.2ms, 51.6–52.4 | 18.5MB |
| 기간 후보를 run에서 한 번 읽는 실험 대안 | 1 | 100 | 0.140MB | 39.5ms, 38.0–40.5 | 14.4MB |

문자열 합계는 title·description·URL의 Go 문자열 길이입니다. PostgreSQL wire byte, RSS나 peak heap이 아닙니다. 할당량은 해당 프로세스 구간의 누적 할당이며 DB 서버 메모리를 포함하지 않습니다. 모든 대안의 최종 필터 결과 합계는 2,000건이었습니다. snapshot 대안은 index 추가 전에 측정했습니다.

`EXPLAIN (ANALYZE, BUFFERS)`에서는 기존 index만 있을 때 full과 period 모두 sequential scan이고 shared hit는 2,000이었습니다. period는 전송·scan 행을 줄였지만 읽은 DB page 수를 줄이지 않았습니다. 임시 expression index를 추가하면 100행 index scan, shared hit 102, 실행 시간 0.107ms가 관찰됐습니다. index 결과는 한 EXPLAIN 실행이며, 전송·Go 필터 비용까지 포함한 52.2ms와 혼동하지 않습니다.

**판단:** SQL prefilter는 높은 탈락률에서 명확한 개선 근거가 있습니다. 다만 1% 선택률, 긴 설명, 같은 멤버라는 합성 조건이므로 실제 M/K, 설명 길이, 방 구독 중복률에 따라 효과가 달라집니다. 후보 대부분이 기간 안이면 효과가 작고, 새 index는 쓰기·저장 비용을 추가합니다. 운영 index 도입을 이 결과만으로 확정하지 않습니다.

### 날짜 선택 규칙을 SQL에 정확하게 보존해야 합니다

현재 규칙은 `news`면 PubDate 우선, `event`면 EventStartDate 우선입니다. 선호 날짜가 기간 밖일 때 다른 날짜를 대신 선택하는 규칙이 아닙니다. `pub_date가 기간 안 OR event_start_date가 기간 안`만으로 기존 필터를 대체할 수 없습니다.

weekly는 KST **7일 전의 시작부터 21일 후의 끝까지**, 즉 날짜 29개를 포함합니다. monthly는 현재 KST 달입니다. SQL에서는 동일 구간을 `[start, endExclusive)`로 표현할 수 있습니다. weekly의 endExclusive는 오늘 0시에서 **22일 후**입니다.

실험에서 기존 pgx scan과 Go 필터를 보존한 effective date 표현은 다음과 같습니다.

```sql
CASE WHEN type = 'news'
     THEN COALESCE(pub_date,
                   event_start_date::timestamp AT TIME ZONE 'UTC')
     ELSE COALESCE(event_start_date::timestamp AT TIME ZONE 'UTC',
                   pub_date)
END
```

이 값에 `>= start AND < endExclusive`를 붙이고 기존 status/type/link_status 조건을 유지했습니다. `UTC`는 제품의 날짜 의미를 새로 정한 것이 아니라, 이 실험에서 관찰한 현재 `date` scan 결과의 시각을 보존하기 위한 것입니다.

두 period의 경계 직전·정각·직후, 날짜 NULL 조합, news/event 우선순위를 조합해 140행을 만들었습니다. DB session timezone을 UTC, Asia/Seoul, America/Los_Angeles, Pacific/Kiritimati로 바꿔 비교했습니다.

| 조건 | 기존 최종 후보 | SQL 사전 필터 후 기존 Go 필터 | 판정 |
|---|---:|---:|---|
| UTC 명시 변환, 4개 시간대·2개 period | 각각 68 | 각각 68 | ID 집합 일치 |
| date 암묵 변환, Pacific/Kiritimati | 각각 68 | 각각 60 | 정상 후보 8개 누락 |

이는 **최적화안의 위험을 재현한 것**입니다. 현재 제품 SQL은 기간 조건이 없으므로 이 누락 버그가 현재 운영에 있다는 뜻이 아닙니다. DB session timezone이 바뀌어도 결과가 같도록 타입과 변환을 명시해야 합니다.

유효하지 않은 날짜의 오류 의미도 따로 확인했습니다. 현재 스키마에 finite 제약이 없는 `pub_date='infinity'` 활성 news를 격리 DB에 넣자, 전체조회는 `cannot scan Infinity into *time.Time` 오류였고 기간 사전 필터는 그 행을 제외해 0건 성공했습니다. **정상 후보 집합 보존과 실패 의미 보존은 별개의 조건**입니다. 운영에 이 값이 있다는 주장은 아닙니다. 변경 시 non-finite 행을 명시적 오류로 계속 드러내거나, 검증된 데이터·쓰기 계약으로 finite 전제를 보장해야 합니다. 최적화가 잘못된 저장 값을 조용히 숨기는 효과를 허용했다고 가정하지 않습니다.

### 후보 순서와 snapshot 의미는 아직 별도 검증이 필요합니다

실험의 경계값 검증은 ID를 정렬한 뒤 비교했습니다. 전체 `FilteredCandidate` 값과 순서의 동등성 검증은 아닙니다. 현재 SQL에는 ORDER BY가 없고 [정렬 비교기](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/filter/filter_ranking.go#L35)는 날짜→source tier→category→title까지만 비교합니다. ID·URL은 tie breaker가 아닙니다. stable sort이므로 동률 후보는 조회 순서에 영향을 받습니다.

index나 snapshot은 원래의 비결정적 조회 순서를 바꿀 수 있습니다. 추가 로컬 probe에서는 date/tier/category/title이 같고 URL만 다른 두 후보의 입력 순서를 뒤집었습니다. ID 집합은 같지만 필터 결과가 `[1,2] → [2,1]`이 되고 fallback의 첫 출처 URL도 바뀌었습니다. 실제 운영 index가 순서를 바꿨다는 재현은 아닙니다. 최종 후보 순서, prompt 입력, 기존 fallback의 선택 결과까지 비교해야 합니다. 결정적인 ID/URL 순서를 새로 도입한다면 그것도 명시적인 결과 정렬 규칙 변경으로 다룹니다.

run snapshot을 적용한다면 다음 경계가 필요합니다.

1. run 시작의 `now`, period, 공통 후보 version/시점을 고정합니다. 현재는 방마다 조회 시점이 다르고 `s.now()`도 필터와 요약 입력에서 별도로 호출됩니다.
2. 현재 뉴스 구독 방 목록은 run 시작에 수집하지만 alarm 구독 멤버는 방마다 읽습니다. 실행 중 뉴스 구독 해지를 매 방에서 다시 확인하는 보장은 현재도 없습니다. 후보 snapshot과 함께 alarm 멤버·alias profile까지 고정하면 추가 시점 변경이므로 별도로 결정합니다.
3. run 수명 안에서만 immutable 후보를 공유합니다. `Candidate.Members`, 날짜 pointer 등도 공유 후 변이하지 않습니다. 전역 TTL cache를 새로 만들 필요는 없습니다.
4. 공통 DB 실패는 run 실패로 표현합니다. 이전 snapshot이나 빈 성공으로 대체하지 않습니다. 방별 guard·LLM·render·enqueue 실패는 기존 정책을 유지합니다.
5. 수동 단일 방 경로와 scheduler는 같은 후보 준비 함수를 사용합니다. 서로 다른 filtering 구현 두 개를 만들지 않습니다.
6. 기존 lock, room 처리 동시성 5, periodKey, outbox identity와 중복 방지 경계를 보존합니다.

월 경계도 추가 재현했습니다. 9월 30일 23:59:59에 만든 기간 snapshot을 10월 1일 00:00:01의 필터에 넣으면 0건이고, 전체 후보를 같은 새 시각에 필터하면 10월 후보 1건이 남았습니다. 이는 제안하는 snapshot의 clock을 일관되게 전달해야 한다는 증거이며, 현재 미구현 snapshot의 위험입니다.

그 다음 최적화 후보는 normalized body, candidate member token set, 날짜·category 등 **방과 무관한 메타데이터의 run 내 사전 계산**입니다. matching용 `NormalizeKey`는 공백을 제거하지만 category 판정은 원문을 소문자로 바꿔 `solo live` 같은 문자열을 찾습니다. 두 표현을 하나로 합치면 category가 달라질 수 있으므로 각각 보존합니다. 추가 조사에서 이 대안과 방별 lazy 정규화를 prototype으로 비교했습니다. 결과와 적용 전제는 아래 성능 확장 절에 있습니다.

### 출력 다섯 건이 입력 다섯 건을 뜻하지 않습니다

[요약 schema](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/summarizer/summarizer_prompt.go#L75)의 `top_items.maxItems=5`는 출력 제한입니다. 실제 prompt는 [buildMemberNewsUserPrompt](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/summarizer/summarizer_prompt.go#L137)를 통해 후보들을 전달합니다. 기간·방 필터 후 후보가 많으면 입력 작성·guard·모델 비용이 커질 수 있습니다.

기간 prefilter는 기존 최종 후보를 보존하므로 이 입력 수를 줄이는 최적화가 아닙니다. 상위 K개 truncation, 방 간 LLM 응답 재사용, 본문 요약·축약은 결과 의미를 바꾸는 별도 제안입니다. 외부 모델을 호출해 token 비용을 측정하지 않았습니다.

## 다른 성능 후보의 우선순위

### Holodex 실패 직렬화는 실제로 존재하며 현재 계약이기도 합니다

동일 key 동시 caller 6개, 원천 100ms 지연을 실제 provider 경로에서 재측정했습니다.

| 원천 결과 | 원천 호출 수 | caller 오류 수 | 모든 caller 완료 |
|---|---:|---:|---:|
| 성공 | 1 | 0 | 105ms |
| 실패 | 6 | 6 | 607ms |

gate는 완료 신호만 공유합니다. 실패가 캐시에 기록되지 않으므로 다음 caller가 새 owner가 됩니다. [기존 테스트](../../hololive/hololive-shared/pkg/service/holodex/provider/service_streams_cache_fill_test.go#L405)는 실패 caller 각각의 원천 호출을 명시적으로 기대합니다. 따라서 현재 동작을 단순 동시성 버그로 판정하지 않습니다.

in-flight 오류까지 공유하면 실패 폭증 시 원천 부하와 꼬리 대기를 줄일 가능성이 있지만, 첫 caller의 실패·취소를 다른 caller에게 전파하는 의미가 달라집니다. 독립된 유한 fill 예산, waiter별 취소, 마지막 waiter 종료, 기존 retry scheduler와의 관계를 먼저 정해야 합니다. negative cache·오래된 성공 결과·추가 retry를 동시에 넣지 않습니다.

### 내부 H3는 아직 우선 성능 패치로 확정하지 않습니다

bot/admin→LLM은 같은 프로세스 loopback 호출이지만 JSON·routing·transport 경계를 통과합니다. 그러나 이번에 H3/local 비교를 측정하지 않았습니다. bot major-event client의 30초와 member-news client의 60초 timeout 등도 호출 계약입니다. HTTP를 없애는 순간 다른 plane pool/동시성/admission/error 경계를 우회하지 않도록 application port가 먼저 필요합니다.

구성 정리 패치에서는 명시적 options와 client 수명 소유권을 정리할 수 있습니다. in-process 전환, 외부 route 제거, 인증·timeout 변경까지 같은 패치로 묶을 증거는 없습니다.

### 관리 집계는 인덱스와 갱신 빈도를 함께 봐야 합니다

[dispatch summary SQL](../../hololive/hololive-api/internal/planes/admin/internal/service/dispatchops/queries/summary.sql)은 보존 중인 전체 delivery의 상태별 `count(id), min(created_at)`을 읽습니다. 추가 실험은 baseline의 해당 테이블 정의를 격리 DB에 만들고 PK 및 기존 `(status, created_at DESC)` index를 적용했습니다. 전체 운영 스키마·인덱스·부하를 복제한 것은 아닙니다. 합성 행은 sent 98%, pending 1%, DLQ 1%이며 설명 길이도 고정했습니다.

`id NOT NULL`이므로 `count(*)`는 같은 count/min/status 결과를 반환합니다. 변경 SQL은 이 함수 하나만 치환했으며 모든 비교에서 전체 결과값을 확인했습니다. 새 index·cache·시간 범위를 추가하지 않았습니다.

| 보존 행 수 | 현재 count(id) 중앙값 | count(*) 중앙값 | 조건 |
|---|---:|---:|---|
| 10,000 | 2.268ms | 0.859ms | VACUUM ANALYZE 후, 7회 |
| 100,000 | 22.618ms | 8.629ms | 동일 |
| 500,000 | 61.942ms | 40.543ms | 동일 |
| 500,000 중 5% UPDATE 후 | 73.227ms | 119.808ms | 재VACUUM 없이 7회 |

VACUUM 직후 `count(*)`는 기존 index의 index-only scan과 Heap Fetches 0을 사용했습니다. 갱신 후에는 Heap Fetches가 500,068로 늘었습니다. 이 조건에서는 index-only 계획도 heap 확인 비용을 크게 지불했습니다. 따라서 SQL 한 단어 변경을 모든 운영 상태의 속도 향상으로 일반화하지 않습니다.

500,000행, 미리 연결 4개를 준비한 pool, 동시 요청 12개인 별도 batch에서는 caller latency 중앙값이 327.1→136.8ms, 최대값이 564.9→257.8ms였습니다. 누적 pool wait는 2,056→879ms이고 양쪽 모두 대기 acquire는 8건이었습니다. 이는 한 batch의 관찰값이며 p95/p99나 운영 동시성을 추정하지 않습니다. 네트워크·DB·개발 호스트 부하까지 포함한 결과이고, 총 wait는 여러 caller의 합이어서 wall time보다 클 수 있습니다.

**권고:** schema의 NOT NULL 의미를 보존하는 `count(*)`는 작은 후보지만, 정적 보관 상태와 갱신 후 상태를 모두 검증해야 합니다. 지속 갱신 환경의 통계·visibility·실행 계획을 보기 전에는 확정 성능 패치로 묶지 않습니다. 정확한 count를 유지한 채 O(N)을 O(1)로 바꾼 결과도 아닙니다. 최근 24시간 필터, stale summary cache, 쓰기 경로에 새 counter를 넣는 변경은 별개입니다.

요청마다 embed SQL을 읽고 문자열을 치환하는 `querySQL("failures")`는 별도 microbenchmark에서 1,741ns/op, 3,624B/op, 5 allocs/op였습니다. 고정 SQL을 미리 준비할 수 있지만 이 표본에서는 DB 집계보다 우선할 비용이 아닙니다. sourceobservation의 기존 batch·receipt insert처럼 이미 줄인 왕복을 중복 최적화하지 않습니다.

표시 오류코드를 다시 query로 넣는 실험에서는 `SEND_FAILED`, `provider.timeout`이 400 대상이 됐습니다. 그러나 [현재 runbook](../current/runbooks/admin-dispatch-operations.md#http-계약)은 대문자·구두점·빈 코드는 분포에 포함하되 선택 불가능하다고 명시합니다. 이는 새로 찾은 미문서화 버그가 아닙니다. 지원 범위 확대를 원하면 공개 입력 계약 변경으로 별도 다룹니다.

## observation 이관을 하나의 소스 변경으로 묶을 이유

이미 [contracts/sourceobservation](../../hololive/hololive-shared/pkg/contracts/sourceobservation/envelope.go)에 envelope·identity·payload 계약이 있습니다. 새 계약 package를 또 만들기보다, 현재 implementation package에 남은 계약과 실제 runtime 구현을 분리하는 작업입니다.

[Repository](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository.go#L28)는 pool 외에도 supported contracts, job contracts, publish fence verifier, publish fault hook을 함께 보유합니다. consume facade도 같은 constructor와 validate 경로를 사용합니다. façade 두 개가 capability를 좁혀 보여주는 효과는 있지만 compile-time 책임 분리는 아닙니다.

| 현재 구현 | 목표 소유권 | 함께 보존할 경계 |
|---|---|---|
| envelope·kind/schema·hash·clock | 기존 shared 계약 | 저장 identity와 canonical bytes |
| `job_contract*.go`, publish 입력·checkpoint 값 | 직접 제품 caller가 collector뿐이므로 collector 내부 | emissions·membership·projection 의미와 slice 변이 방지 |
| `repository_publish*.go`, publish terminal/checkpoint SQL | collector 내부 | lease→projection→target→contract 검증, publish/checkpoint/defer 동일 tx |
| claim/failure/finalize, channel/content/live/schedule/viewer consumer와 persist | API YouTube 내부 | claim token, canonical/intent/application receipt/complete 동일 tx |
| replay epoch·replay·retention·live finalizer | API YouTube 및 관리 command 경계 | 지원 generation·replay count·retention·payload GC |
| payload 참조와 GC 경합 protocol | 양 runtime이 이해하는 좁은 저장 계약 | GC와 publish의 잠금·참조 재검사·원자성 |

collector의 `JobContract`, `JobID`, `CheckpointEntry` 사용만으로도 같은 implementation package를 import합니다. 그래서 Repository façade만 옮겨서는 의존성을 끊을 수 없습니다. [collectutil.RunInput](../../hololive/hololive-youtube-collector/internal/runtime/collectutil/runner.go#L85), [joblease](../../hololive/hololive-youtube-collector/internal/runtime/joblease/repository_candidates.go#L54)까지 같이 이전해야 합니다.

API에는 runtime 외에 [source-observation-replay-epoch command](../../hololive/hololive-api/cmd/source-observation-replay-epoch/main.go#L74)가 실제 소비자로 남습니다. 구현을 `planes/youtube/internal`에 넣으면 상위 cmd는 Go internal 규칙상 직접 import할 수 없습니다. plane의 좁은 maintenance 진입점을 노출하거나 API의 공통 internal 경로에 이 관리 구현을 두는 선택이 필요합니다. CLI를 다시 shared 전체 Repository로 연결하는 임시 alias는 목표를 되돌립니다.

[Finalize](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_finalize.go#L58)는 한 transaction에서 claim lock→canonical callback→application receipts→queue 완료와 offset 저장을 수행합니다. 이 callback을 transaction 밖의 새 service 호출로 바꾸면 파일 이관이 아니라 원자성 변경입니다. publish도 검증 SQL을 `pgx.Batch`로 묶되 검증 순서와 rollback을 유지합니다.

payload GC의 안전성은 같은 Go Repository 객체가 아니라 DB protocol이 보장합니다.

- publisher끼리는 `(kind, schema_version, digest)` 순서의 transaction advisory lock을 공유합니다. 그 뒤 payload ID를 구하고 observation/queue/checkpoint를 같은 transaction에 기록합니다.
- publisher의 dictionary lookup은 `FOR KEY SHARE`, GC는 `FOR UPDATE SKIP LOCKED`를 사용합니다. GC가 publisher의 advisory lock까지 공유하는 구조는 아닙니다.
- GC는 행 잠금과 참조 재검사 DELETE를 별도 문장으로 실행합니다. `VOLATILE` DB 함수와 READ COMMITTED의 새 snapshot을 사용하여 잠금 대기 전후의 commit을 관측하도록 설계돼 있습니다.
- `payload_id NOT NULL`과 `ON DELETE RESTRICT` FK, dictionary UNIQUE 및 payload resolution 검사도 보존합니다. GC cursor는 DB singleton 행으로 직렬화됩니다.

근거: [publish의 잠금과 resolution](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/queries/repository_publish_set_0032_32.sql#L98), [lookup과 GC DB 함수](../../hololive/hololive-api/scripts/migrations/241_source_observation_payload_cutover.sql#L55), [최종 최소 권한](../../hololive/hololive-api/scripts/migrations/255_payload_dictionary_least_privilege.sql#L10). `repository_payload.go`는 GC 구현이 아니라 finalize/replay의 payload 검증 helper라는 점도 구분해야 합니다.

이 protocol을 유지하면 publish 구현과 GC 구현을 서로 다른 Go 모듈로 이관할 수 있습니다. collector에 dictionary DELETE나 GC state 쓰기 권한을 새로 줄 필요가 없습니다. 다만 [transaction helper](../../hololive/hololive-shared/pkg/dbx/tx.go#L57)는 isolation을 명시하지 않으므로 운영 기본 isolation까지 이번에 확인한 것은 아닙니다.

보존할 회귀에는 `TestPayloadGCConcurrentPublishNeverLosesEvidence`, `TestPayloadConcurrentPublishSeesCommittedDictionaryAfterWaiting`, `TestPayloadNullOrMissingReferenceIsRejected`, `TestPayloadDictionaryKeepsIndependentObservationSlotsAndReplay`, `TestPayloadDigestCollisionAndCorruptionFailClosed`가 있습니다. [GC 경쟁 테스트](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_payload_gc_test.go#L16)는 동시 실행을 확인하지만 모든 잠금 순서를 강제하지는 않습니다. 이관 후에는 같은 Repository 하나를 만드는 테스트 대신 분리된 publish/consume 구현을 함께 사용하는 격리 DB 테스트로 보존합니다. 이번 조사에서는 이 회귀들을 실행하지 않았습니다.

따라서 **API·collector caller, 계약값, 구현·SQL·테스트를 한 번에 이관하고 구 package façade/alias를 제거하는 소스 패치**를 권고합니다. 저장 schema와 wire 계약을 바꾸지 않는다면 소스 일괄 변경이 운영 fleet의 동시 배포를 요구하는 것은 아닙니다. 실제 버전 혼용 가능성은 계약 회귀로 확인하며, DB epoch 활성화나 replay는 이관 검증으로 실행하지 않습니다.

## 빅뱅 변경의 권고 단위

여기서 빅뱅은 **한 책임 경계의 모든 내부 caller를 한 번에 바꾸고 구 구현을 삭제하는 것**입니다. 큰 파일 수 자체가 목적이 아닙니다. 변경하지 않은 기능까지 전면 재작성하거나 서로 독립적인 정책을 한 배포에 묶지 않습니다.

| 묶음 | 일괄 변경 판단 | 구체적 효과와 완료 조건 |
|---|---|---|
| 설정 저장·통계 취소·부분 bootstrap cleanup | 가장 먼저 독립 수정 | 기존 재현 결함 해결. 실패 후 snapshot/worker 적용, 취소 waiter, 획득 단계별 exactly-once cleanup |
| parser 함수 alias·불필요한 타입 alias·테스트 전용 wrapper | 즉시 가능한 내부 정리 후보 | 기존 구현 직접 사용. 실제 parser/명령/lifecycle 검증 보존. alias를 다시 만들지 않음 |
| API composition과 apiplane 회수 | 한 경계 안에서 일괄 이행 권고 | bot/admin 공통 builder, 명시적 options, 의존성 전달·자원 Close 중복 제거. internal/load 제약 해결; 기존 providers 재사용은 가능 |
| worker 전용 shared 7개 package 회수 | 별도의 일괄 이관 후보 | worker 소유 구현·SQL·테스트를 이동. 공용 contracts와 alarm client는 유지 |
| observation 구현과 caller 이관 | 범위가 크더라도 하나의 완결된 소스 변경 권고 | publish/consume 구현 분리, 구 Repository façade 제거. 실제 DB tx/role/fencing/replay/GC 회귀 |
| member-news SQL prefilter | 좁은 최적화 패치 권고 | 높은 탈락률의 scan/할당 감소 입증. NULL/기간/시간대/후보 순서와 fallback 검증 |
| member-news run snapshot | 시점 의미를 정한 뒤 별도 변경 | N회 공통 조회를 1회로. 기존 동시성·방별 처리·outbox 멱등 보존 |
| H3 제거·Holodex 실패 공유·outbox 통합 | 위 구조 변경과 동시 적용 근거 없음 | 각각 비용/실패 의미/저장 계약을 별도로 판단 |

구성 경계의 목표는 다음과 같습니다.

```text
API composition ── 명시적 options와 resource owner ── bot/admin/llm/youtube
                         │
                         └─ 필요한 공통 infra primitive

collector 내부 publish ── 공유 observation 계약 ── API 내부 consume
       │                                                │
       └─ collection/checkpoint transaction                └─ canonical/intent transaction

worker 내부 dispatch와 alarm 구현 ── 공유 값/HTTP 계약
```

plane별 DB pool, 서로 다른 세 outbox, formatter 실패 정책, weekly/monthly 기간 정책은 유지합니다. module 수 축소나 Fx provider 전면 재작성은 이 조사에서 효과를 입증한 변경이 아닙니다.

부분 bootstrap cleanup 중 admin의 최종 cleanup 누락은 추가 실행으로 확인했습니다. [admin](../../hololive/hololive-api/internal/planes/admin/app/build_runtime.go#L95)은 alarm 생성 뒤 ACL/auth/settings/bot-room/X store 실패에서 infra만 닫습니다. 최종 cleanup 조립 뒤 router 실패 probe에서도 공급한 runtime cleanup은 0회, infra cleanup만 1회였습니다. [bot의 두 LLM client 생성](../../hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/services_llm_clients.go#L38)은 두 번째 실패에서 첫 client를 닫지 않고, alarm client 생성 뒤 settings 실패에서도 같은 소유권 문제가 있습니다. 이 bot 경로는 정적 확인입니다.

실제 socket/goroutine 누수 발생량은 재현하지 않았습니다. H3 constructor는 transport를 구성하고 요청 때 dial하며, 두 LLM client 생성 사이에 요청도 없습니다. 따라서 현재 근거는 실패 시 transport 소유권 누락입니다. 획득 즉시 rollback 소유권을 등록하고 성공 시 runtime으로 넘기되, 하위 BotLifecycle의 자원 Close도 같이 정리하는 방식이 적절합니다.

## 설정 적용의 결과불명과 입력 계약을 먼저 복원해야 합니다

### 적용한 뒤 응답을 잃으면 현재 모델은 사실과 다른 이유를 표시합니다

실제 `alarm.Client`와 `NewLocalSettingsApplier`에 로컬 httptest worker를 연결했습니다. 첫 PUT은 성공 응답하여 client의 마지막 확인값을 `[10,5,1]`로 만들었습니다. 두 번째 PUT은 합성 worker 상태를 15로 바꾼 뒤 응답 연결을 종료했습니다.

```text
server_applied=15
admin_applied=false
reason="alarm worker did not apply alarm advance minutes"
confirmed_cache=[10 5 1]
requests=2
```

적용을 확인하지 못한 `false` 자체보다 **worker가 적용하지 않았다고 단정하는 이유와 상태 해석**이 문제입니다. `Client.UpdateAlarmAdvanceMinutes`는 모든 오류를 빈 slice로 바꾸고, [applier](../../hololive/hololive-api/internal/server/settings/settings_applier_local.go#L52)는 빈 slice로부터 미적용을 추론합니다. `GetTargetMinutes`도 HTTP GET이 아니라 이 client가 마지막으로 받은 성공값의 복사입니다. 이 재현은 실제 worker·운영 설정을 바꾸지 않았으며 전체 admin 파일 저장 경로를 호출한 시험도 아닙니다.

권고하는 내부 결과 계약은 다음과 같습니다. 이름은 제안이며 public JSON field를 추가하는 안은 아닙니다.

```go
type ApplyOutcome string

const (
    ApplyConfirmed ApplyOutcome = "applied"
    ApplyRejected  ApplyOutcome = "not_applied"
    ApplyUnknown   ApplyOutcome = "outcome_unknown"
)

type AdvanceMinutesResult struct {
    RequestedMinutes int
    Outcome          ApplyOutcome
    TargetMinutes    []int // 이번 적용 응답으로 확인한 값만 포함합니다.
}

type AlarmAdvanceUpdater interface {
    UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) (AdvanceMinutesResult, error)
}
```

| 관측한 경계 | 내부 판정 | 보존할 정보 |
|---|---|---|
| encode/URL 검사/전송 전 명시적 취소 | not_applied | 실제 전송하지 않았다는 증거와 error |
| worker의 인증·입력 거부가 확인된 응답 | not_applied | 확정된 거부의 status/code. 임의 proxy 응답까지 동일하게 가정하지 않음 |
| 전송 이후 EOF/reset/deadline | outcome_unknown | 적용됐을 가능성, 요청값, 원래 오류 |
| 500 또는 비정상 성공 envelope/JSON/target 부재 | outcome_unknown | 응답 실패와 적용 여부를 분리 |
| 유효한 성공 응답과 target 값 | applied | clone한 이번 응답값 |

전송 후 `ctx.Err()`가 있다는 이유로 미전송으로 분류하지 않습니다. 최소 구현에서는 `Do` 이후의 전송 오류를 모두 unknown으로 분류할 수 있습니다. error를 감추는 기존 무오류 method와 wrapper를 남기지 않고 local worker 구현·remote client·HTTP adapter·API caller·mock을 함께 변경합니다.

파일 저장 성공 후의 admin 200 응답과 기존 `alarm_requested_advance_minutes`, `alarm_applied`, `alarm_reason`, `alarm_target_minutes` 키는 유지할 수 있습니다. `alarm_applied`는 확인 여부로 해석하고 unknown 이유를 명확히 표현합니다. 이전 성공값은 현재 worker 상태로 표시하지 않습니다. 추가 공개 outcome key나 기존 optional field의 생략 의미를 바꾸는 작업은 별도 응답 계약으로 정해야 합니다. 자동 재적용, polling, 저장 rollback은 추가하지 않습니다.

### 0 입력은 공개 계약과 실제 저장서비스가 충돌합니다

[공개 settings 계약](../current/contracts/settings.md#request)과 handler는 `0..1440`을 허용하지만, `settings.Service.Update`는 `<=0`을 거절합니다. 실제 저장서비스를 handler에 넣은 probe 결과는 **입력 0 → HTTP 500, worker 호출 0회, 기존 snapshot 5 유지**였습니다. 디스크 실패가 아닌 입력 거부가 저장 실패로 표시되는 경로입니다.

구성 이관에서 0을 1로 보정하거나 문서 하한만 바꾸면 안 됩니다. 0의 제품 의미를 정한 뒤, 허용한다면 저장·정규화·worker까지 일치시키고 거절한다면 공개 입력 계약과 400 검증을 함께 이행해야 합니다. 이 결정은 다른 내부 이관을 막는 선행 조건은 아닙니다. 기존 메모리 선반영 수정은 0 결정과 독립적으로 진행할 수 있습니다.

## API 구성과 포트의 구체적인 목표

### 설정을 한 번 파싱하는 것과 역할별 정책을 합치는 것은 다릅니다

현재 `LoadRuntime`은 bot broad Config→admin broad Config→LLM config를 읽고, plane port/pool/URL override→YouTube→worker profile→최종 validation을 적용합니다. 성공 경로의 정적 호출 수는 DotEnv 3회, token/CORS 읽기 3회, PostgreSQL/Valkey/logging/Cliproxy/LLM/Exa 읽기 각각 3회입니다. 기동 시간 측정 결과가 아닙니다.

`StrictEnv`는 unset/blank에는 기본값을 쓰지만 잘못된 nonblank 값은 오류로 모으며, 값 원문은 오류에 노출하지 않습니다. required-positive loader는 blank도 거부합니다. duration overflow도 검사합니다. 또한 loader의 앞부분은 fail-fast, base section은 `errors.Join`입니다. 단일 parser로 합친다는 이유로 이를 모두 같은 fallback이나 첫 오류 반환으로 바꾸지 않습니다.

권고 위치와 최소 책임은 다음과 같습니다.

| 위치 제안 | 책임 | 넣지 않을 것 |
|---|---|---|
| API `internal/config` | apiplane 구현, LLM/YouTube config, API 역할 validation·override | worker/collector composition |
| shared `pkg/config/envload` | 필요한 strict parsing, duration 검사, dotenv, list parsing의 실제 구현 | settings.Config, transport, provider, 구 함수 alias |
| shared `pkg/config/runtimepolicy` | 명시적 environment/role/TLS/API-key 값의 순수 검증 | 내부 `os.Getenv` 재조회 |
| API `internal/apifoundation` | bot/admin 공통 생성 순서와 자원 bundle | root app, Fx, plane import |

`internal/app` 자체에 공통 builder를 넣고 bot bootstrap이 import하면 `app → bot/runtime → bot/bootstrap → app` cycle입니다. 별도 leaf package인 `apifoundation`을 사용합니다. `internal/app/foundation`이라는 별도 package도 기술적으로 가능하지만, 위 경로로 책임을 명확하게 두는 안을 권고합니다.

`settings.Config`를 한 번에 전부 교체할 필요는 없습니다. 다음 순서가 작습니다.

1. strict parser/policy의 실제 구현을 leaf로 이동하고 API·shared·worker·collector의 해당 import를 동시에 바꿉니다. `internal/load`에 forwarding alias를 남기지 않습니다.
2. apiplane와 그 테스트를 API로 이동합니다. bot/admin의 기존 `*settings.Config`는 우선 유지합니다.
3. `BuildInfraModule`은 실제 읽는 Valkey/Postgres만 `InfraOptions`로 받게 합니다. 실제 제품 caller는 API bot/admin과 worker 세 곳이며 collector에 이 builder 사용을 강요하지 않습니다.
4. 공통 foundation은 YouTube/Holodex/OfficialSchedule 옵션과 plane별 infra를 명시적으로 받습니다. 기존 `OfficialScheduleRuntime()`의 값을 그대로 사용합니다.
5. 중복 section 파싱을 회수합니다. bot/admin의 CORS default와 bot/admin·LLM의 version default 차이는 역할 조립에서 보존합니다. slice/map이 있는 Config를 얕게 복사하여 plane끼리 변경을 공유하지 않습니다.

LLM bootstrap의 `MEMBER_NEWS_X_ALLOWLIST_PATH` 직접 읽기도 LLM config로 옮겨야 config 회수가 끝납니다. Iris base URL 파일의 동적 재읽기는 별도 계약이므로 startup-only 값으로 바꾸지 않습니다.

### 내부 HTTP client는 옵션과 수명을 명시적으로 받습니다

`internalhttp` 내부 env 읽기를 없애려면 bot/admin alarm client만 바꿔서는 부족합니다.

| caller | 현재 timeout | options 이행 후 유지할 것 |
|---|---:|---|
| bot/admin alarm | 각 10초 | strict origin, dedicated H3, 각 plane Close |
| bot major-event | 30초 | subscription adapter와 error 매핑 |
| bot member-news | 60초 | digest/구독 context와 transport 소유권 |
| admin trigger | 30초 | conflict 매핑과 server 실행의 별도 수명 |
| admin bot rooms | 30초 | 허용 origin/path 검사와 room DTO |
| admin system health | 2초 | 생성 오류를 availability에 반영하는 기존 정책 |

같은 옵션을 전달하는 것과 transport 인스턴스를 공유하는 것은 다릅니다. pool·timeout·cleanup 격리는 유지합니다. concrete options/자원 bundle에 구현이 하나뿐이면 factory interface를 새로 만들 필요가 없습니다.

### AlarmCRUD의 소비자별 최소 포트

현재 12개 메서드의 `AlarmCRUD`를 각 소비자가 모두 요구할 이유는 없습니다.

| 소비자 | 필요한 메서드 |
|---|---|
| bot alarm command | AddAlarm, RemoveAlarm, RemoveHostAlarm, ListRoomAlarmsView, ClearRoomAlarms |
| admin 알림 관리/통계 | GetAllAlarmKeys, RemoveAlarm |
| admin 방 이름 설정 | SetRoomName |
| admin 설정 적용 | 결과와 error를 반환하는 UpdateAlarmAdvanceMinutes, 마지막 적용 관측 결과 |
| worker scheduler | GetTargetMinutes, WarmCacheFromDB |
| worker HTTP adapter | 실제 공개 route의 9개 작업 |

도메인 request/value DTO는 재사용하고 포트는 소비자가 정의합니다. worker composition은 scheduler와 settings HTTP에 같은 concrete AlarmService를 전달해 상태를 일치시킵니다. remote client의 no-op `WarmCacheFromDB`를 계약 충족용으로 남기지 않습니다. 기존 error 없는 업데이트 method·큰 interface·mock의 전달 계층까지 같은 변경에서 없앱니다.

### 인프로세스 호출은 아직 별도 조건부 변경입니다

H3/local의 실제 latency·alloc 비교는 아직 없습니다. local adapter를 도입하더라도 아래 차이를 보존해야 합니다.

- trigger caller는 30초지만 서버 실행은 `context.WithoutCancel` 뒤 5분 제한입니다. 요청 취소를 곧바로 작업 취소로 바꾸거나 실행을 무제한으로 만들지 않습니다.
- major-event lock 충돌은 409이고 member-news lock 미획득은 현재 skip 후 200입니다. 공통 scheduler 인터페이스로 만들며 둘을 하나의 busy 정책으로 바꾸지 않습니다.
- LLM의 pool과 room concurrency 5를 유지합니다. bot pool에서 LLM repository를 다시 만들지 않습니다.
- room trim/empty 검증, period 정규화, 미구독 오류의 계약 sentinel, digest의 복사·빈 slice·현재 노출 필드를 유지합니다.
- 외부 route의 auth/body limit/error/status는 유지합니다. plane의 private repository를 다른 plane에 노출하지 않고 LLM 소유 application 진입점에서 같은 동작을 호출합니다.
- HTTP middleware가 제공하던 tracing·request context·panic 경계를 application에서 어떻게 유지할지 확인해야 합니다. `local 실패 → HTTP 재시도`는 만들지 않습니다.

근거: [trigger 실행 수명](../../hololive/hololive-shared/pkg/server/httpserver/trigger.go#L37), [member-news lock skip](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/scheduler/digest_helper.go#L204), [HTTP 값 변환](../../hololive/hololive-api/internal/planes/llm/runtime/api_internal_membernews.go#L45).

## 종료 경로의 실제 관찰과 최종 자원 소유권

### Start 전부터 살아 있는 작업이 있습니다

member-cache의 epoch subscription/reconcile은 Build 중 시작하고, durable worker는 Start에서 시작하며 둘 다 상위 context 취소만으로 종료되지 않습니다. 따라서 `Close`는 Start 전 build rollback도 처리해야 합니다. `apiPlanes.shutdown`은 이름과 달리 build 실패에서 Close만 호출하므로 이 조건이 특히 중요합니다.

현재 순서는 Start에서 YouTube→LLM→admin→bot, Shutdown/Close에서 역순입니다. Fx의 drain context **10초는 전체 plane이 공유**하며 plane마다 새로 받는 10초가 아닙니다. 바깥 process stop 상한은 30초입니다.

| 실제 경로를 사용한 probe | 관찰 | 한계 |
|---|---|---|
| BotLifecycle + infra + outer cleanup | PG/cache Close 각각 2회. 첫 Close 때 member-cache alive, infra cleanup 뒤 종료 | stub 자원의 Close 오류는 주입하지 않음 |
| durable Start/Stop + 실제 queue sampler | Stop=nil인데 sampler callback이 아직 실행 중 | callback의 취소 완료를 의도적으로 보류. 실제 DB 지연 재현은 아님 |
| 실제 Fx와 BotRuntime, 종료를 보류한 tracked worker | drain 시한 뒤 exit 1, worker alive 상태에서 resource/telemetry cleanup 각 1회 | 작은 drain 예산과 보류 작업 사용 |
| 실제 member-cache/infra/Fx | 25ms hard deadline에서 Close 진행 중, DB·telemetry cleanup 미도달. release 후 같은 cleanup 완료 | 운영 30초 길이를 기다린 시험은 아님 |
| admin 실제 HTTP builder의 router 실패 | infra cleanup 1회, 공급한 runtime cleanup 0회 | DB·외부 요청 없이 생성 실패 경로 확인 |
| 실제 loopback H3 + BotRuntime Shutdown | 정상 client와 명시 Close client는 nil, socket만 사라진 client는 80ms 시한 오류 | 요청 0개인 합성 연결, H3/local 비용 비교 아님 |

두 sampler는 [plain goroutine](../../hololive/hololive-api/internal/planes/bot/runtime/durable_lifecycle.go#L36)이며 worker/maintenance와 달리 WaitGroup에 들어가지 않습니다. Stop의 nil 반환을 “모든 DB 사용자 종료”로 사용하려면 이 task도 join 대상에 넣어야 합니다.

정상 HTTP/3 client는 GOAWAY를 처리하므로 client가 살아 있다는 것만으로 drain이 지연되지는 않았습니다. 이 가설은 반증됐습니다. socket만 사라진 경우 bot의 raw H3 shutdown은 idle-only 상태도 deadline 오류로 반환했습니다. admin/LLM이 쓰는 [공통 H3 drain](../../hololive/hololive-shared/pkg/server/httpserver/h3_drain.go#L56)과 경로가 다릅니다. 공통 동작으로 정리하려면 idle-only와 실제 in-flight 요청을 구분하는 기존 검증까지 함께 가져와야 합니다. timeout 오류를 일괄 무시하는 수정은 아닙니다.

### 새 owner와 Stop의 의미

| owner | 소유할 책임 |
|---|---|
| Fx | process context, supervisor, hard deadline, telemetry |
| aggregate | plane 생성 rollback, 의존 순서, 종료 결과의 취합 |
| 각 plane | listeners, 모든 background 작업의 cancel/join, 내부 client/Iris, member-cache, DB/cache |
| bot orchestration | command/readiness 동작과 자신의 작업. DB/cache/Holodex의 수명은 소유하지 않음 |

bot lifecycle의 cache 입력은 readiness만 남기고 Close capability를 제거할 수 있습니다. lifecycle constructor의 PG/Holodex 인자도 제거합니다. 하지만 command repository·room catalog가 실제 사용하는 Postgres dependency까지 지우는 것은 아닙니다.

정상 Stop 성공은 sampler 등을 포함해 해당 자원을 사용하는 작업의 join 완료를 뜻하게 해야 합니다. 종료 오류와 미종료 상태는 구분합니다. 예를 들어 listener 종료 오류가 있어도 모든 작업을 join한 경우와, drain timeout 때문에 작업이 살아 있는 경우에 같은 무조건 Close를 적용하지 않습니다. 미종료라면 남은 process 예산으로 정해진 종료를 수행하고, 완료하지 못하면 불완전 종료와 exit 1을 보존합니다. 병렬로 두 번째 cleanup을 시작하거나 hard deadline을 없애지 않습니다.

30초 hard deadline은 무조건 깨끗한 종료의 보장이 아닙니다. 현재 main의 `os.Exit` 뒤 goroutine이 프로세스 밖에서 살아남는다는 뜻도 아닙니다. bounded Close와 마지막 종료 실패의 보고를 설계할 때 이 한계를 숨기지 않습니다.

구체적인 성공 순서는 ingress 차단→durable·sampler·profile checker·cert reload 등의 종료 확인→Holodex retry 및 member-cache 종료 확인→내부 client/Iris→DB/cache 해제입니다. dependency와 실제 goroutine 접근을 기준으로 정하고, 같은 resource owner를 build rollback과 정상 Close 양쪽에서 사용합니다. 범용 DI/cleanup framework를 새로 만들 필요는 없습니다.

기존 `TestLifecycleAppliesBoundedPlaneDrainAndStillCloses`는 모든 drain 오류 후 Close를 기대합니다. 이를 단순 삭제하지 않고 “오류지만 quiesced”와 “join 미완료”로 나누어 새 보장을 검증해야 합니다. 또한 shutdown headroom은 상수만 검사하지 말고 실제 worker profile의 `SettlementTimeoutMS`가 constructor에 전달된 값을 기준으로 검증해야 합니다.

## 성능 우선순위를 선택률과 프로파일로 보강합니다

### 기간 사전 필터의 이득은 선택률에 달려 있습니다

추가 비교는 후보 1,000/10,000건, 기간 선택률 1/10/100%, 방-equivalent 20개, 최대 병렬 5입니다. 기존 index만 사용하고 SQL별 준비 실행 후 3회 순서를 교차했습니다. 최종 표는 다른 조사 DB 측정이 끝난 뒤 실행한 표본입니다. 동일 개발 호스트의 다른 부하까지 통제한 실험은 아닙니다.

| 전체 후보 | 기간 선택률 | 전체→기간 SQL 중앙값 | 누적 Go 할당 |
|---:|---:|---:|---:|
| 1,000 | 1% | 29.656→3.937ms | 48.538→1.916MB |
| 1,000 | 10% | 49.904→22.075ms | 61.024→18.514MB |
| 1,000 | 100% | 157.522→159.208ms | 185.504→185.519MB |
| 10,000 | 1% | 290.558→42.691ms | 496.495→18.515MB |
| 10,000 | 10% | 421.202→202.922ms | 620.978→185.535MB |
| 10,000 | 100% | 1,480.736→1,542.803ms | 1,865.121→1,865.138MB |

실제 병렬 peak는 모두 5, pool wait count는 0이었습니다(pool max 12). 기간 선택률 100%에서는 할당 이득이 없고 시간 범위도 겹쳤습니다. 예를 들어 10,000건 조건은 전체 1,201.7–1,532.8ms, 기간 1,175.1–1,706.8ms입니다. 중앙값만 보고 보편적인 퇴보라고 판정하지도 않습니다.

기존 직렬 실험처럼 단일 멤버, 한국어 설명 반복, nil member provider/source validator, 실제 Repository+Filter만 사용했습니다. DB·URL 검증·LLM·운영 scheduler 전체의 비용 분해가 아닙니다. 조건별 EXPLAIN을 새로 수집하지 않았으므로 앞의 한 plan을 모든 선택률에 일반화하지 않습니다.

### 본문 정규화와 category 판정이 큰 비용이었습니다

기간 내 후보 1,000개를 실제 `FilterCandidates`로 2초간 104회 처리한 CPU profile에서 `matchMembers` 누적 비중은 43.38%, `classifyCategory`는 42.92%였습니다. `NormalizeKey` 40.64%는 matchMembers 내부 비용이므로 두 비율을 더하지 않습니다.

할당 표본에서는 matchMembers 누적 69.56%, classifyCategory 자체 24.61%, 후보 token set 누적 3.77%였습니다. token set도 matchMembers 내부입니다. 이 fixture에서 “map이 가장 큰 비용”이라는 설명은 지지되지 않았습니다. 한국어 본문·category 키워드 미일치·정렬된 제목·모든 후보가 기간 안이라는 조건이므로 긴 다국어 본문이나 다양한 정렬 분포 전체를 대표하지 않습니다.

### snapshot보다 먼저 적용할 수 있는 좁은 대안

현재는 정확한 멤버 token으로 매칭할 수 있어도 본문을 먼저 정규화합니다. **먼저 candidate member token과 profile token을 비교하고, 해결되지 않은 profile이 있을 때만 본문을 한 번 정규화**하는 prototype을 비교했습니다. 기존 조건인 `정확 token 일치 OR 정규화 본문 contains`와 profile 순서를 유지합니다.

기간 내 후보 1,000개, 긴 설명, 방 20개·병렬 5, 3회 중앙값입니다.

| 정확 token으로 매칭되는 후보 비율 | 현재→lazy 시간 | 누적 할당 |
|---:|---:|---:|
| 0% | 118.436→117.498ms | 141.138→141.138MB |
| 50% | 135.146→95.189ms | 141.139→93.858MB |
| 100% | 112.916→46.900ms | 141.138→46.578MB |

0%에서는 이득이 없습니다. 실제 여러 멤버를 구독한 방은 추가 profile을 배제하기 위해 본문을 읽어야 할 수 있습니다. 따라서 후보 `members`가 비어 있지 않은 비율을 본문 생략률로 쓰지 않습니다.

이 변경은 방별 DB 조회·clock·member profile·URL 검증·category·정렬·guard·LLM·outbox를 그대로 둘 수 있습니다. 새 cache·retry·runtime dependency가 필요 없습니다. 순수한 CPU 변경으로 먼저 구현할 근거가 있으며, SQL prefilter의 날짜/오류/순서 문제와 별도 검증할 수 있습니다.

### 공통 metadata 준비는 더 큰 이득과 시점 전제를 가집니다

같은 후보 snapshot을 이미 공유한다는 조건에서 날짜·matching용 본문·멤버 token·category를 한 번 준비하는 prototype도 비교했습니다. URL 검증과 방별 profile·matched member·stable sort는 기존 함수로 수행했습니다. 준비 비용을 포함해 짧은 설명은 23.806→4.932ms·27.858→6.239MB, 긴 설명은 121.396→21.499ms·141.139→11.903MB였습니다.

두 prototype 모두 **152개 후보 × 60개 조합**에서 전체 `FilteredCandidate` 값과 순서를 `reflect.DeepEqual`로 비교해 통과했습니다. 날짜 우선순위·NULL·기간·별명·중복·빈 멤버·nil/mock validator·동률 URL을 포함합니다. 동적으로 바뀌는 DB/member provider와 실제 source validator 설정, LLM prompt guard까지 동일하다는 증명은 아닙니다.

따라서 최종 성능 순서는 lazy 정규화와 기간 SQL 의미 보존→필요 시 run snapshot+prepared metadata→실제 데이터 분포에 따른 index→별도 입력 예산입니다. 한 합성 개선율을 이 단계들의 합산 효과로 계산하지 않습니다.

## observation 이관의 파일과 테스트 배치를 확정합니다

### 목표 경로와 실제 이동량

API 구현은 `hololive-api/internal/youtube/sourceobservation`, collector 구현은 `hololive-youtube-collector/internal/runtime/sourceobservation`을 권고합니다. API cmd와 YouTube plane 모두 API internal 경로에 접근할 수 있습니다. 중첩된 `planes/youtube/internal`에 넣고 cmd용 Repository wrapper를 추가할 필요가 없습니다.

| 대상 | 분류 |
|---|---|
| implementation Go 46개 | collector 8개, API 34개, 선언별 분해 4개 |
| SQL 85개 | collector 13개, API 72개 |
| 직접 caller Go 파일 50개 | API 제품 5·테스트 3, collector 제품 23·테스트 19 |
| 기존 implementation 테스트 파일 | 46개, 현재 무태그 |
| 함께 옮길 shared private 구현 | community와 content/live/photo/profile/schedule/viewer reducer, 7 packages |
| 위 private 구현의 파일 | 제품 23개·테스트 21개 |

shared private 구현을 API로 함께 옮기지 않으면 observation의 23개 파일이 Go internal 규칙에 걸립니다. 이 7개 package의 현재 외부 caller는 sourceobservation뿐입니다. shared의 공개 poller/runtime helper 등은 이번 이관의 compiler blocker가 아니므로 그대로 사용할 수 있습니다. 이 패치 하나로 shared를 domain/contracts만 남기는 상태까지 달성했다고 주장하지 않습니다.

collector의 8개 파일은 `job_contract.go`, `job_contract_initial.go`, `repository_publish.go`, `repository_publish_batch.go`, `repository_publish_prepare.go`, `repository_publish_terminal.go`, `repository_publish_validate.go`, `retry_schedule.go`입니다. claim/finalize/canonical 및 replay/retention 구현 34개는 API로 갑니다. `repository.go`, `repository_roles.go`, `types.go`, `sql.go`는 분해합니다.

```text
collector Repository
  pool, jobContracts, fenceVerifier, publishFault, rewritePublishResult

API Repository
  pool, supported
```

`supported`는 finalize/replay가 사용하고 publish는 DB의 current-contract SQL로 검증합니다. `jobContracts`는 publisher verifier의 구성에 필요합니다. 각 constructor와 validate가 자기 필드만 요구하도록 바꿉니다. `JobContract`, `CheckpointEntry`, `PublishBatchInput/Result`는 collector 내부로 옮기고, `Claim/Observation/Reconcile/Replay`와 supported set은 API 내부로 옮깁니다. shared에는 이미 있는 envelope/lease/schema/hash/clock 계약을 유지합니다.

양쪽의 작은 private text/hex 검증까지 하나의 shared Repository 유틸 package로 재결합하지 않습니다. 현재 byte-length·whitespace·64 lowercase hex 규칙을 각각 보존하면 됩니다. 오류를 `errors.Is`로 분류하는 caller는 원 타입과 함께 import를 바꿉니다.

SQL 파일은 내용을 바꾸지 않고 owner별 embed로 이동합니다. collector 13개 중 5개는 현재 파일명 literal 참조를 찾지 못했고 API 72개 중 1개는 test-only입니다. 이번 이관에서는 일단 보존하며, literal 검색만으로 dead SQL이라고 삭제하지 않습니다. `testqueries`와 production query는 구분합니다.

정확한 검토 목록은 [Go 파일 이동표](evidence/2026-10-02-hololive-api-expansion/observation_file_move_map.tsv), [SQL 이동표](evidence/2026-10-02-hololive-api-expansion/observation_sql_move_map.tsv), [호출자 이동표](evidence/2026-10-02-hololive-api-expansion/observation_caller_move_map.tsv), [기존 Test/Benchmark 목록](evidence/2026-10-02-hololive-api-expansion/observation_existing_tests.tsv)에 있습니다. target은 제안이며 source는 저장소 루트 상대경로입니다. 개수는 이번 검토의 범위이며 새 구조 gate가 아닙니다.

### cross-module 회귀는 실제 양 구현을 계속 호출해야 합니다

서로 다른 module의 internal 구현을 제3 module의 테스트가 직접 import할 수는 없습니다. 기존 회귀 중 publish와 consume을 함께 호출하는 시험은 다음의 좁은 testkit으로 연결하는 안을 권고합니다.

- collector module root `testkit/sourceobservation`: 실제 publisher를 호출하되 입력은 기존 계약 envelope·lease와 fixture 값, 출력은 observation ID/outcome으로 제한합니다.
- API module root `testkit/sourceobservation`: 실제 Repository/Consumer/CanonicalWriter를 구성하여 consume합니다. API의 private ReconcileWrite나 community.Batch를 외부 계약으로 노출하지 않습니다.
- 격리 pool·migration과 DB seed는 기존 dbtest를 사용합니다. private fault/rewrite hook은 owner의 same-package 테스트에 남깁니다.

testkit은 peer 테스트의 진입점이고 production runtime import에는 연결하지 않습니다. 새 원격 dependency나 공개 HTTP route가 필요 없습니다. 기존 46개 무태그 테스트를 `integration` 태그 뒤로 옮겨 compile 문제를 피하는 것은 검증 범위를 줄이므로 허용하지 않습니다. testkit 자체도 이 경우 무태그 package여야 합니다. 각 module의 test binary를 별도 프로세스로 실행하는 대안은 가능하지만 별도 기동·barrier·수명 protocol 비용이 커집니다.

| 기존 회귀 | 배치 |
|---|---|
| PayloadGCConcurrentPublishNeverLosesEvidence | API private GC + collector 실제 publisher testkit |
| PayloadDictionaryKeepsIndependentObservationSlotsAndReplay | API finalize/replay + collector publish |
| PayloadDigestCollisionAndCorruptionFailClosed | publisher mismatch 거부와 API callback 이전 corruption 거부를 모두 유지 |
| ChannelLiveCheckSlotAdvancesWhileSnapshotRetries | collector private executor 테스트 유지 + API consume testkit |
| PayloadConcurrentPublishSeesCommittedDictionaryAfterWaiting | 두 publisher끼리이므로 collector same-package |
| PayloadNullOrMissingReferenceIsRejected | publisher와 DB FK 검사이므로 API consume 불필요 |

파일 단위 통째 이동만 하면 같은 파일에 들어 있는 publisher-only와 GC/consume 테스트가 잘못 배치됩니다. `repository_test.go`도 publisher duplicate/fence/checkpoint와 consumer claim/expiry/finalize/replay를 함수 단위로 나눕니다. 기존 `TestPUB001`~`TestPUB014`, failure precedence, stale holder, terminal rollback, supported old generation, replay epoch, payload/receipt 보존 회귀를 유지합니다.

[경로 분석의 수동 대조](2026-10-02-repository-layout-refactoring.md#observation-파일과-혼합-테스트)는 기존46개 파일을 교차32·collector7·API5·helper 미확정2로 분류했습니다. 이전 TSV의 `dependency_hint`는 최종 owner가 아닙니다. 특히 `claim_backlog_plan_test.go`와 `shorts_claim_plan_test.go`는 publisher seed 이후 API claim SQL을 검사하므로 API/교차 시험으로 이행합니다. 교차32파일과 이전 양 메서드 도달124함수는 다른 단위이며 합산하지 않습니다.

### worker 회수도 테스트까지 추적해야 완결됩니다

제품 기준 worker 전용 7개 package는 integration-tag까지 포함해 테스트 파일 58개, dispatch SQL 27개와 과거 migration fixture 5개를 가집니다. [조사 목록](evidence/2026-10-02-hololive-api-expansion/worker-ownership.json)에 파일과 import 소비자를 남겼습니다.

| 현재 위치 | 목표 worker 위치 제안 | 추가 의존 처리 |
|---|---|---|
| shared settings/alarmworker | `internal/config` | shared strict parser/policy 추출과 함께 이동 |
| alarm/dedup | `internal/service/alarm/dedup` | checker/notifier/scheduler import 동시 변경 |
| alarm/dispatchoutbox | `internal/service/alarm/dispatchoutbox` | SQL·send unit·claim/settle 회귀 함께 이동 |
| alarm/queue | `internal/service/alarm/queue` | dispatchoutbox와 같은 패치 |
| notification/alarmservice, private alarmcache | worker 구독 서비스 및 그 private cache | 둘을 함께 이동해 internal 접근 해결 |
| youtube/outbox/format | worker YouTube egress 내부 format | 실제 renderer/payload 회귀 유지 |

추가로 다음 네 곳을 처리해야 합니다.

1. API `handler_alarm_test.go`의 InvalidAction 시험은 실제 `AlarmService{}`를 사용하지만 해당 메서드를 호출하지 않습니다. 이미 있는 `alarmListViewerStub`으로 바꾸면 구현 import가 필요 없습니다.
2. dbtest의 send-unit 실제 Repository 시험 4개는 worker 소유 테스트로 옮기고 `dbtest.NewPool`을 씁니다. DB CHECK만 확인하는 시험은 dbtest에 남깁니다.
3. observation의 `canonical_fact_clock_test.go`는 worker의 UpcomingCandidates를 사용합니다. 두 clock 시험은 worker에 두고 API/collector testkit으로 실제 생산·소비를 연결합니다.
4. admin dispatchops 통합 시험은 과거 shared fixture 경로를 직접 읽습니다. setup만 `dbtest.NewPool`로 바꾼 overlay에서 **현재 전체 migration으로 기존 8개 시험이 모두 통과**했습니다. 새 fixture helper/복제 없이 과거 경로 loader를 제거하는 안을 권고합니다.

따라서 worker 회수와 observation 이관의 순서를 강제로 묶을 필요는 없습니다. worker를 먼저 옮기면 아직 shared인 observation 구현으로 clock 시험을 유지한 뒤 observation 패치에서 testkit으로 전환합니다. observation을 먼저 옮기면 아직 shared인 dispatchoutbox를 쓰다가 worker 패치에서 시험을 옮깁니다. 어느 순서든 중간 source commit은 빌드·행동 시험을 통과해야 하며 구 runtime 호환 alias는 필요 없습니다.

## 실제 검증과 재실행 자료

이번에 실행한 검증:

- Go AST 조사와 네 업무 모듈 `go list` metadata 재집계.
- 격리 PostgreSQL의 날짜 경계·NULL·시간대 비교, 기간 prefilter/임시 index/run snapshot 비용 실험.
- 별도 의미 검증 3개: 동률 후보 순서와 fallback 출처, 월 경계 snapshot 누락, infinity 날짜의 scan 오류 의미 차이. 모두 재현 테스트 통과.
- Holodex 실제 provider/로컬 캐시 경로의 동시 성공·실패 실험.
- member-news filter, admin system, shared settings, Holodex provider에서 아래 정규식에 맞는 기존 테스트 통과. 패키지 전체 테스트나 전체 서비스 회귀를 실행한 것은 아닙니다.

```sh
env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  go test ./hololive/hololive-api/internal/planes/llm/internal/service/membernews/filter \
  ./hololive/hololive-api/internal/planes/admin/internal/service/system \
  ./hololive/hololive-shared/pkg/service/settings \
  ./hololive/hololive-shared/pkg/service/holodex/provider \
  -run 'Test(Filter|StreamCacheFill|Collector|GetCurrent|Settings|Service|Update)' \
  -count=1 -timeout=90s
```

실험 소스는 자동 테스트/gate에 추가하지 않고 문서 증거로 보관했습니다.

- [PostgreSQL과 후보 필터 probe](evidence/2026-10-02-hololive-api-deep-review/news_probe_test.go.txt)
- [Holodex 동시 호출 probe](evidence/2026-10-02-hololive-api-deep-review/holodex_probe_test.go.txt)
- [후보 순서와 월 경계 및 scan 오류 probe](evidence/2026-10-02-hololive-api-deep-review/performance_review_semantics_test.go.txt)
- [각 반복의 결과와 EXPLAIN](evidence/2026-10-02-hololive-api-deep-review/results.txt)
- [추가 의미 검증 결과](evidence/2026-10-02-hololive-api-deep-review/semantics-results.txt)

kapu의 저장소 루트에서 기존 Go 소스에 파일을 추가하지 않고 재실행할 수 있습니다.

```sh
python3 - <<'PY'
import json, os, pathlib, subprocess, tempfile

root = pathlib.Path.cwd()
evidence = root / 'docs/design/evidence/2026-10-02-hololive-api-deep-review'
packages = [
    'hololive/hololive-api/internal/planes/llm/internal/service/membernews',
    'hololive/hololive-shared/pkg/service/holodex/provider',
]
sources = ['news_probe_test.go.txt', 'holodex_probe_test.go.txt']
replacements = {
    str(root / package / 'zz_deep_review_test.go'): str(evidence / source)
    for package, source in zip(packages, sources)
}
replacements[str(root / packages[0] / 'zz_performance_review_test.go')] = str(
    evidence / 'performance_review_semantics_test.go.txt'
)
env = os.environ.copy()
for key in ['TEST_DATABASE_URL', 'TEST_DATABASE_OWNER_TOKEN', 'ALLOW_EXTERNAL_TEST_DB']:
    env.pop(key, None)
with tempfile.TemporaryDirectory(prefix='holo-api-analysis-') as directory:
    overlay = pathlib.Path(directory) / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': replacements}))
    subprocess.run([
        'go', 'test', '-overlay=' + str(overlay),
        *['./' + package for package in packages],
        '-run', '^(TestDeepReview|TestPerformanceReview)',
        '-count=1', '-v', '-timeout=120s',
    ], env=env, check=True)
PY
```

원래 두 결함의 재현 기록은 기존 분석 문서에 있습니다. 이번에는 해당 구현이 여전히 같은 순서로 동작함을 소스로 재확인했고 새 저장 실패/취소 probe는 실행하지 않았습니다. 추가 조사에서 lifecycle의 대상 race는 실행했지만 전체 race/NilAway/lint/build는 실행하지 않았습니다. 운영 성능, H3/local 비용 비교, observation 대이관의 DB 회귀도 미실행입니다. 제품 변경이 없으므로 fallback 추가·변경도 없습니다.

### 확장 조사에서 추가로 실행한 검증

| 표면 | 실제 실행 | 보관 자료 |
|---|---|---|
| 설정 | 실제 client/applier 응답 유실, 실제 저장서비스를 넣은 handler 0 입력, 선택한 config/HTTP/DTO 기존 시험 | [결과](evidence/2026-10-02-hololive-api-expansion/config-results.txt) |
| lifecycle | Close 순서·sampler·Fx deadline·admin partial failure·loopback H3 probe | [결과](evidence/2026-10-02-hololive-api-expansion/lifecycle-results.txt) |
| lifecycle race | 기존 35개와 probe 5개, 추가 H3 probe에 race detector 적용 | [race](evidence/2026-10-02-hololive-api-expansion/lifecycle-race-results.txt), [H3](evidence/2026-10-02-hololive-api-expansion/lifecycle-h3-results.txt) |
| member-news SQL | 6조건×두 조회 방식×3회, 실제 병렬5 | [최종 matrix](evidence/2026-10-02-hololive-api-expansion/performance-matrix-results.txt) |
| member-news CPU | metadata/lazy의 값·순서 동등성 및 비용, 2초 CPU/할당 profile | [동등성](evidence/2026-10-02-hololive-api-expansion/performance-equality-results.txt), [비용](evidence/2026-10-02-hololive-api-expansion/performance-metadata-results.txt), [CPU](evidence/2026-10-02-hololive-api-expansion/performance_cpu_cumulative.txt), [할당](evidence/2026-10-02-hololive-api-expansion/performance_alloc_cumulative.txt) |
| dispatchops | 기존 integration 시험, summary 비용/plan/pool wait, 현재 schema로 기존8개 이식 | [비용](evidence/2026-10-02-hololive-api-expansion/dispatch-results.txt), [현재 schema](evidence/2026-10-02-hololive-api-expansion/dispatch-current-schema-results.txt) |

마지막 fixture 대안은 `setupOpsIntegration`만 `dbtest.NewPool`로 바꾼 overlay입니다. 기존 시험이나 SQL을 완화하지 않고 통과했으며 제품 테스트 파일은 수정하지 않았습니다. lifecycle의 고의 보류 작업은 각 probe에서 release하고 종료를 기다렸습니다. 모든 최종 probe가 통과했고 대상 race에서 race 보고가 없었습니다. 최초 H3 가설의 예상 실패는 정상 client가 지연 원인이 아니라는 반증으로 해석했으며 최종 probe는 live/closed/vanished 상태를 구분합니다.

추가 자료는 [expansion evidence](evidence/2026-10-02-hololive-api-expansion/)에 있습니다. 새 Go 소스는 `.go.txt`로 보관하여 일반 빌드·테스트 대상에 넣지 않았습니다. 바이너리, pprof 원본, 컨테이너 로그·연결정보는 설계 산출물에 포함하지 않았습니다. 측정 결과는 설계의 근거이지 새 성능 gate가 아닙니다.

다음 명령은 저장소 루트에서 `profile`만 골라 해당 소스를 overlay로 연결합니다. 기존 파일을 덮어쓰지 않습니다. `lifecycle`은 대상 probe를 race로 재실행하고, `dispatch-current`는 기존 8개 시험을 현재 schema fixture로 실행합니다. `performance`는 DB 민감도와 CPU prototype 비용을 포함해 최대 수십 초 걸릴 수 있습니다.

```sh
python3 - <<'PY'
import json, os, pathlib, subprocess, tempfile

profile = 'config'  # config, lifecycle, performance, dispatch, dispatch-current
root = pathlib.Path.cwd()
evidence = root / 'docs/design/evidence/2026-10-02-hololive-api-expansion'
cases = {
    'config': ([], '^TestConfigExpansion', [
        'hololive/hololive-api/internal/server/settings',
        'hololive/hololive-api/internal/planes/admin/internal/server/api']),
    'lifecycle': (['-race'], '^TestExpansion', [
        'hololive/hololive-api/internal/planes/bot/internal/app/bootstrap',
        'hololive/hololive-api/internal/planes/bot/runtime',
        'hololive/hololive-api/internal/fxapp',
        'hololive/hololive-api/internal/planes/admin/app']),
    'performance': ([], '^TestPerformance(SensitivityMatrix|PreparedMetadataEqual|PreparedMetadataCost|LazyMetadataCost)$', [
        'hololive/hololive-api/internal/planes/llm/internal/service/membernews',
        'hololive/hololive-api/internal/planes/llm/internal/service/membernews/filter']),
    'dispatch': (['-tags=integration'], '^TestExpansion', [
        'hololive/hololive-api/internal/planes/admin/internal/service/dispatchops']),
    'dispatch-current': (['-tags=integration'], '^TestOpsIntegration', [
        'hololive/hololive-api/internal/planes/admin/internal/service/dispatchops']),
}
flags, pattern, packages = cases[profile]
mapping = json.loads((evidence / (profile + '-overlay-files.json')).read_text())
env = os.environ.copy()
for key in ['TEST_DATABASE_URL', 'TEST_DATABASE_OWNER_TOKEN', 'ALLOW_EXTERNAL_TEST_DB']:
    env.pop(key, None)
with tempfile.TemporaryDirectory(prefix='holo-api-expansion-') as directory:
    overlay = pathlib.Path(directory) / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(root / target): str(evidence / source)
        for target, source in mapping.items()
    }}))
    subprocess.run([
        'go', 'test', *flags, '-overlay=' + str(overlay),
        *['./' + package for package in packages],
        '-run', pattern, '-count=1', '-v', '-timeout=180s',
    ], env=env, check=True)
PY
```

이관 구현 후에는 위 조사 probe만으로 완료를 판정하지 않습니다. 실제 이행한 owner package와 peer 회귀, 기존 boundary 검사, 해당 NilAway/race, 필요한 module build를 실행해야 합니다. dispatchops의 현재 integration 시험은 일반 `go test`에 포함되지 않고 local-ci의 명시적 integration package 목록에도 없으므로, 이 표면 변경 시 `-tags=integration` 명령을 검증에 명시해야 합니다.
