# 알람 워커·API 공유 모듈 리팩토링과 fallback·구형 호환 정리 계획

알람 워커·API·`hololive-shared`의 구조·성능·중복 구현을 분석하고, 다음 네 작업을 하나의 실행 계획으로 묶습니다: 재현한 동작 결함 수정, shared 소유권 재배치, 규칙에 어긋나는 fallback의 단일 경로화, 구형 호환 alias·잔재 정리. 우선 해결할 문제는 전송 배치 대기 중 lease 만료, 달력 조회의 취소 전파와 지연, 공유 멤버 캐시의 종료 누락입니다.

이 문서는 **2026-10-02 분석과 후속 실행 계획**입니다. 같은 날 초안의 주장을 코드와 진단으로 다시 확인했고, fallback·구형 호환 조사를 추가했습니다. 1–3단계는 같은 날 미커밋 worktree에서 구현했으며 결과는 [구현 진행 기록](#구현-진행-기록-2026-10-02)에 있습니다. 운영 적용 결과를 뜻하지 않으며, 문서만으로 runtime·DB·외부 계약 변경 권한을 확대하지 않습니다. 현재 운영 소유권은 [PROJECT_MAP](../current/PROJECT_MAP.md), [SERVICE_OWNERSHIP](../current/SERVICE_OWNERSHIP.md), [CONTRACT_MAP](../current/CONTRACT_MAP.md)을 따릅니다.

## 목표

1. 재현한 동작 결함 3건을 회귀 테스트와 함께 고칩니다.
2. 실행 구현을 소유 서비스로 옮기고, `hololive-shared`에는 실제 다중 소비 계약만 남깁니다.
3. 계약 없는 fallback·default-on-error 경로를 오류가 드러나는 단일 경로로 바꿉니다. 남길 경로는 예외 계약 형식으로 문서화합니다.
4. 구형 이름을 유지하는 alias, production 패키지의 테스트 전용 코드, 삭제된 이름의 재등장 가드를 정리합니다. 저장 데이터 호환 경로는 데이터가 사라졌다는 근거가 있을 때만 제거합니다.

## 적용 규칙과 제약

| 규칙 | 출처 | 이 계획에서의 의미 |
|---|---|---|
| 단일 정상 경로 | [workspace Failure paths](../../../.agents/workflows.md) | retry·fallback·호환·degraded·대체·default-on-error 동작에는 계약과 독립 근거(사고·trace·upstream·재현)가 모두 필요합니다. 예외는 trigger, 시도·시간·신선도·데이터 한도, 종단 동작, telemetry, owner, 검토·제거 조건을 적습니다. production 변경은 `Fallback delta`를 보고합니다. |
| 실패 결과 보존 | 같은 규칙 | invalid·failed·unknown·ambiguous 결과를 기본값이나 빈 성공으로 바꾸지 않습니다. 부수효과 재시도는 durable receipt·idempotency key·ownership token·상태 전이 증거가 있을 때만 허용합니다. |
| 구형 이름 | 전역 작업 규칙 | 계약·owner·제거 조건·승인이 없는 구형 이름 alias·재수출은 유지하지 않습니다. |
| 검사 추가 범위 | [stack AGENTS.md](../../../AGENTS.md) | 삭제된 코드·이름의 재등장 grep guard, checker self-test, ownership·marker 검사, 구조 budget을 추가하지 않습니다. 이 규칙은 과거 DEC·감사의 "영구 계약" 표기보다 우선합니다. |
| 외부 계약 보존 | 현재 계약 문서 | wire JSON, DB 상태 의미, env 이름, metric 이름, HTTP route는 별도 승인 없이 바꾸지 않습니다. |
| 운영 경계 | [hololive AGENTS.md](../../AGENTS.md) | 운영 log·metric·DB 조회는 `hololive-bot-ops`와 `stack-platform-ops`가 소유합니다. 배포·DB 쓰기·Git 게시는 이 계획의 승인 범위가 아닙니다. |

[2026-09-26 stack 감사](../../../docs/agent-workflows/evidence/2026-09-26-stack-audit/)는 Hololive 후보 131건을 분류했고, 그중 97건은 삭제한 뒤 운영 배포까지 마쳤습니다. 이 계획은 그 감사가 다루지 않은 항목만 새로 추가합니다. 조건부로 남긴 퇴역 env 가드와 종단 데이터 경로는 [retired-guards.tsv](../../../docs/agent-workflows/evidence/2026-09-26-stack-audit/retired-guards.tsv)의 조건과 `remove_after`를 그대로 따릅니다.

## 분석 기준과 범위

| 항목 | 기준 |
|---|---|
| Hololive 기준 revision | `5c12d2c503915caf63f21aa86e1aeb346da94e63` (초안 분석과 재검증 모두) |
| shared-go 기준 | workspace의 `1e5c725342e9c3a5c563e2df239720a429e4760a`(`v2.9.1-3`). production build는 `GOWORK=off`로 각 `go.mod`의 `v2.8.0`을 씁니다. 이 문서가 인용한 shared-go 파일은 두 revision 사이에 변경이 없습니다. |
| 실행 환경 | kapu, Go workspace의 Go 1.27.1 |
| 주 대상 | `hololive-alarm-worker`, `hololive-api`, `hololive-shared` |
| 대조 대상 | `shared-go`의 기존 기능, 수집기의 shared 계약·publish 경계. 수집기 내부 alias는 범위 밖 관찰로만 적습니다. |
| 조사 방법 | production Go import 관계와 runtime binary별 `go list -deps` 링크 그래프, 생성자·호출 경로·SQL·테스트 대조, 임시 overlay 진단, 기존 선별 race 테스트, fallback·legacy·alias 키워드 조사와 이전 감사 표 대조 |
| 수행하지 않은 작업 | 제품 코드·설정·migration 수정, 전체 CI·NilAway·이미지 build, 운영 log·metric·DB 조회, 운영 부하 측정, 실제 Iris/Kakao 전송, 배포·Git 게시 |

패키지 import는 실행 호출과 구분합니다. 한 패키지에 여러 책임이 섞이면 사용하지 않는 실행 구현도 소스 의존 그래프에 나타날 수 있습니다. 따라서 다중 import만으로 진정한 공통 기능이라고 판단하지 않으며, 반대로 Go 소비자가 하나라는 이유로 외부 JSON·DB 계약을 제거하지 않습니다. 이번 조사는 모든 함수에 대한 전수 결함 검증이 아닙니다.

근거 수준은 다음과 같이 구분합니다.

- **재현 확인**: 실제 대상 함수를 임시 테스트로 호출해 문제 동작을 확인했습니다.
- **모의 재현**: 실제 실행 함수와 현재 SQL 조건을 모델링한 repository로 조건부 동작을 확인했습니다. 실제 DB 통합 재현과 구분합니다.
- **코드 확인**: 호출·의존성·동기화·SQL 구조를 확인했습니다. 운영 발생 빈도나 성능 저하량은 측정하지 않았습니다.
- **추론**: 코드에서 도출했지만 재현하지 않은 결론입니다.
- **설계 제안**: 현재 계약을 보존하면서 바꿀 위치와 구조를 제안합니다. 새 운영 기준으로 확정한 내용이 아닙니다.

## 1. 우선 수정할 동작

순서는 운영 영향 기준입니다. 초안의 순서(멤버 캐시 → 달력 → lease)를 재검증 결과에 맞춰 바꿨습니다.

### 1.1 전송 순서 대기 중 notification delivery lease 만료

근거 수준은 **모의 재현**입니다. [delivery/dispatcher.go](../../hololive/hololive-shared/pkg/service/delivery/dispatcher.go#L85)는 claim lease를 60초로 두고 배치를 먼저 가져옵니다. [processBatchConcurrent](../../hololive/hololive-shared/pkg/service/delivery/dispatcher.go#L313)는 방별로 묶은 뒤 방 단위로 슬롯을 기다리고, 같은 방의 항목은 순차 전송합니다. 그 사이 뒤쪽 항목의 PENDING lease도 경과합니다.

[request 저장 SQL](../../hololive/hololive-shared/pkg/service/delivery/queries/outbox_save_request.sql)과 [MarkSending SQL](../../hololive/hololive-shared/pkg/service/delivery/queries/outbox_repository_0172_04.sql)은 아직 유효한 lease를 요구합니다. 생성자는 단일 attempt timeout이 lease 안에 들어가는지 검사하지만, 배치 내부의 대기 시간까지 검사하지 않습니다.

실제 `processBatch`를 가상 시간에서 실행한 결과는 다음과 같습니다. lease는 60초입니다.

| 조건 | 전송 | 전송 전 lease 만료 | 경과 |
|---|---|---|---|
| 같은 방 10건, 건당 9초 (초안 진단) | 7 | 3 | 63초 |
| 50개 방에 1건씩, worker 4, 건당 3초 | 50 | 0 | 39초 |
| 같은 조건, 건당 5초 | 48 | 2 | 60초 |
| 같은 조건, 건당 9초 | 28 | 22 | 63초 |

실제 enqueue 생산자([membernews digest](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/scheduler/digest_helper.go), [majorevent 알림](../../hololive/hololive-api/internal/planes/llm/internal/service/majorevent/scheduler/notification_guard.go))는 방마다 1건씩 넣으므로 두 번째 형태가 운영 패턴에 가깝습니다. 아래 행의 값은 운영 profile(batch 50, worker 4)을 적용한 것입니다. 운영 Iris 전송 지연은 측정하지 않았으므로 어느 행이 운영에 해당하는지는 판단하지 않았습니다. 실제 PostgreSQL·Iris 시험이 아니며 영구 유실을 증명한 결과도 아닙니다. 확인한 위험은 불필요한 재claim과 지연입니다.

현재 운영 볼륨에서는 잠재 결함입니다. [2026-09-26 운영 증거](../../../docs/agent-workflows/evidence/2026-09-26-stack-audit/t18-ops-evidence.md)에서 `notification_delivery_outbox`의 SENT 행은 3건이었습니다(SENT 보존 7일). 한 배치가 lease를 넘기려면 위 표처럼 한 번에 수십 건이 쌓여야 합니다. 그래도 고치는 이유는 방 수가 늘면 조용히 지연이 커지고, 수정 범위가 dispatcher 안에 한정되기 때문입니다.

함께 확인한 사실은 다음과 같습니다.

- **코드 확인**: [build_egress.go](../../hololive/hololive-alarm-worker/internal/app/workerapp/build_egress.go#L196)는 profile의 `notification_delivery.executor.attempt_timeout`을 `DispatcherConfig`에 넘기지 않아 코드 기본값 10초가 쓰입니다. 지금은 profile 값(10000ms)과 같아 동작 차이가 없지만, profile을 바꿔도 반영되지 않습니다. alarm-worker에서 이 값을 읽는 곳은 [alarm_dispatch](../../hololive/hololive-alarm-worker/internal/app/workerapp/build_egress.go#L305) 하나뿐입니다.
- **코드 확인**: [applyDefaults](../../hololive/hololive-shared/pkg/service/delivery/dispatcher.go#L170)는 0 이하 값을 기본값으로 바꿉니다. profile 로더가 이미 양수를 검증하므로 이 경로는 두 번째 기본값 출처입니다(3.3절 F10).
- **추론**: [claim SQL](../../hololive/hololive-shared/pkg/service/delivery/queries/outbox_repository_0129_03.sql)은 같은 방의 앞선 항목을 다른 owner가 처리 중인지 보지 않습니다. alarm-worker는 단일 인스턴스이고 한 dispatcher 안에서는 뒤쪽 항목이 앞쪽보다 늦게 만료되므로 순서가 지켜집니다. 배포 중 두 dispatcher가 겹치면 만료된 뒤쪽 항목을 다른 owner가 가져가 순서가 바뀔 수 있습니다. 재현하지 않았습니다.

실행 가능한 슬롯과 방별 순서에 맞춰 claim하도록 바꿉니다. 동일 방의 뒤쪽 항목과 슬롯을 기다리는 방이 실행할 시점에 소유권을 획득하는 설계가 우선 후보입니다. lease 연장을 선택한다면 갱신 실패·소유권 교체·종료 시 처리까지 별도 검증해야 하므로, timeout 숫자만 늘려 해결하지 않습니다. 같은 변경에서 profile의 `attempt_timeout`을 dispatcher에 연결하고, `applyDefaults` 대신 생성자가 잘못된 설정을 거절하게 합니다.

완료 조건은 다음과 같습니다.

- 느린 정상 전송, 동일 방 집중 배치, 50개 방 fan-out에서 실행 전 대기 때문에 claim이 반복 만료되지 않습니다.
- 방별 순서, 전송 전 fencing, 고정 request, 결과 불명 격리가 유지됩니다.
- profile `attempt_timeout` 변경이 dispatcher에 반영됩니다.
- 실제 PostgreSQL lease 조건을 포함한 통합 검증을 추가합니다.

### 1.2 API 달력 조회의 취소 전파와 지연

근거 수준은 **재현 확인**입니다. [calendar_entry_cache.go](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/calendar_entry_cache.go#L72)는 `singleflight.Group.Do`로 동시 요청을 합칩니다. 여기에 두 문제가 있습니다.

1. 선행 호출자의 context가 [공유 조회](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/calendar_entry_cache.go#L86)에 그대로 전달됩니다. 선행 요청이 취소되면 context가 살아 있는 후속 요청도 `context canceled`를 받습니다.
2. 후속 요청은 결과를 기다리는 동안 자신의 `ctx.Done()`을 확인하지 않습니다. 취소돼도 선행 조회가 끝날 때까지 반환하지 않습니다.

같은 경로의 오류는 [내부](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/calendar_entry_cache.go#L88)와 [외부](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/calendar_entry_cache.go#L97)에서 같은 문구로 두 번 감싸져 `find members with celebrations in month: find members with celebrations in month: ...`가 됩니다.

공유 조회는 호출자 취소와 분리하고 제한된 별도 시간 예산을 줍니다. 각 호출자는 `DoChan` 결과와 자신의 취소 중 먼저 오는 쪽을 선택합니다. [알람 구독자 조회](../../hololive/hololive-shared/pkg/service/alarm/targets.go#L269)와 [repository의 조회 예산](../../hololive/hololive-shared/pkg/service/alarm/repository_targets.go#L13)이 같은 패턴입니다. 달력 데이터와 디스크 캐시 정책은 API 내부에 유지합니다.

완료 조건은 다음과 같습니다. 취소된 대기자는 선행 조회를 기다리지 않고 반환합니다. 선행 호출자가 취소돼도 다른 유효한 대기자는 결과를 받습니다. 공유 조회는 유지되고 오류는 한 번만 감쌉니다. 모든 캐시에 같은 취소 정책을 일괄 적용하지 않습니다. 예를 들어 shared의 `AllMembers`는 호출자 취소와 분리된 적재를 기존 테스트에서 명시적으로 요구합니다.

### 1.3 공유 멤버 캐시의 재조회 goroutine 종료 누락

근거 수준은 **재현 확인**이며, 운영 영향은 낮습니다. [cache.go의 memberEpochRuntimeContext](../../hololive/hololive-shared/pkg/service/member/cache.go#L95)는 bootstrap 취소와 분리하려고 `context.WithoutCancel`을 사용합니다. [runEpochReconciliation](../../hololive/hololive-shared/pkg/service/member/cache_epoch.go#L153)은 별도 `runEpochReconcileWorker` goroutine을 시작하지만, 구독이 `valkey.ErrClosing`으로 끝날 때 재조회 작업을 취소하거나 기다리지 않습니다. 임시 테스트에서는 구독 담당 함수가 반환한 뒤 가상 시간 3초 동안 재조회가 3회 발생했습니다.

`ErrClosing`은 client를 닫은 뒤에만 반환되므로, 누수는 종료 과정이나 빌드 실패 정리 이후에만 생깁니다. 그 뒤 프로세스가 끝날 때까지 [기본 15초](../../hololive/hololive-shared/pkg/constants/cache.go#L42)마다 `Current` 실패와 Warn 로그가 남습니다. 초안이 언급한 "같은 프로세스에서 자원을 재구성하는 경로"는 production 코드에서 찾지 못했습니다. 기존 `TestCacheEpoch_ClientClosingStopsWithoutWarn`는 재조회 주기를 1시간으로 두고 구독 함수의 반환만 확인하므로 자식 작업의 종료는 검증하지 않습니다.

**코드 확인**: API 프로세스는 [llm plane](../../hololive/hololive-api/internal/planes/llm/runtime/bootstrap_llm_scheduler.go#L244)과 admin·bot plane([BuildInfraModule](../../hololive/hololive-shared/pkg/providers/modules/infra.go#L91))마다 Valkey client와 멤버 캐시를 따로 만듭니다. 그래서 시작 시 전체 적재, epoch 구독, 재조회 loop가 각각 3번 일어납니다. 결정 기록과의 관계는 10.4절에 정리했습니다. plane별 lifecycle을 유지하는 한 이 결함은 각 plane이 자기 Valkey client를 닫을 때마다 생기므로, 수정은 plane 단위 종료를 기준으로 검증합니다.

`member.Cache`는 다중 서비스가 사용하는 기능이므로 shared에 유지합니다. 대신 runtime 작업용 context·cancel·종료 대기를 명시적으로 소유하게 하고, 구독 종료 시 재조회 작업도 종료하도록 바꿉니다. 정리 순서는 캐시 작업 종료 후 Valkey·DB 해제입니다. bootstrap context 취소로 캐시 실행이 끝나면 안 된다는 기존 요구는 유지합니다.

완료 조건은 client 종료와 명시적 runtime 종료 모두에서 자식 작업까지 끝나고, 종료 뒤 재조회가 발생하지 않으며, bootstrap 취소 분리와 epoch 무효화의 기존 테스트가 유지되는 것입니다.

## 2. 구조와 성능 개선 후보

### 2.1 sourceobservation의 publish와 consume 실행 구현 분리

근거 수준은 **코드 확인**입니다. [repository_roles.go](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_roles.go)는 `PublishRepository`와 `ConsumeRepository`가 같은 `Repository`를 감싸도록 구성합니다. 같은 패키지 안에 publish, claim, finalize, canonical 저장, replay, retention이 함께 있습니다.

수집기는 [NewPublishRepository](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/publisher.go#L32)를 사용합니다. 그러나 해당 패키지에는 API 소유의 reconcile과 [canonical writer](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/community_canonical_writer.go#L17)도 포함됩니다. 역할별 wrapper는 제공 메서드를 좁히지만 패키지 수준 소유권을 분리하지 못합니다. 수집기가 실제 canonical 쓰기를 수행한다거나 binary 크기 증가량을 측정했다는 주장은 아닙니다.

envelope·payload·identity·실패 코드는 이미 [pkg/contracts/sourceobservation](../../hololive/hololive-shared/pkg/contracts/sourceobservation/)에 분리돼 있습니다. 아직 service 패키지에 남은 공유 계약 후보는 [JobContract·JobID](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/job_contract.go)와 [claim·publish·checkpoint 타입](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/types.go)입니다. 이 타입을 계약 패키지로 먼저 옮기고, publish 저장 구현은 수집기로, consume·reconcile·canonical 저장·retention/replay는 API YouTube plane으로 옮깁니다. `poller/runtime/batchrepo`와 `internal/service/youtube/reconcile/*`처럼 함께 연결되는 구현·타입도 추적합니다. 다른 서비스의 `internal` 패키지를 직접 import하는 방식으로 해결하지 않습니다.

이동할 때 collector의 fence 확인·observation·checkpoint·lease complete/defer 원자성과, API의 canonical 저장·notification intent·finalize transaction 경계를 유지합니다.

### 2.2 canonical 필드 변경의 순차 DB 호출

근거 수준은 **코드 확인**입니다. [persistContentFieldUpdates](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/content_persist.go#L125)는 변경 대상마다 `tx.Exec`를 순차 호출합니다. 변경 항목 N개에 대해 동기 DB 호출이 N번 발생합니다.

이미 [dbx.ExecStatements](../../hololive/hololive-shared/pkg/dbx/tx.go#L138)가 같은 transaction 안에서 최대 128개씩 배치 전송하는 기능을 제공하고, 인접한 `live_persist.go`·`content_clock_write.go`·`live_evidence.go`에서도 사용합니다. 기존 batch 기능을 적용하면 새 추상화 없이 호출 대기를 줄일 수 있습니다. SQL statement 수 자체가 한 개로 줄어드는 것은 아닙니다. 입력 순서·실패 시 전체 rollback·오류 위치를 보존하고, 개선량은 query 전송 횟수와 transaction 보유 시간으로 측정합니다.

### 2.3 구독자 조회의 반복 I/O

근거 수준은 **코드 확인**이며 우선순위는 낮습니다. 일반 채널은 [channelAlarmEntriesForItems](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/outbox_grouper_grouping.go#L123)가 이미 `channelID|alarmType` 단위로 중복을 제거하고 캐시 경로로 조회합니다. [ResolveEventSubscribers](../../hololive/hololive-shared/pkg/service/alarm/event_targets.go#L17)가 진행자 조합마다 같은 채널·종류의 구독 목록을 DB에서 반복 조회하는 경우는 `mekparkhost.SupportsSubscriptions`가 참인 UNIT B 채널에만 해당합니다.

UNIT B 배치 안에서 채널·종류별 구독 목록을 한 번 조회한 뒤 제목별 진행자 필터를 적용합니다. 구독 해지 이후 늦은 read-through 결과로 해지된 방을 되살리지 않는 제약과 unknown 진행자 수신 정책은 그대로 둡니다. 완료 조건은 동일 수신 집합과 오류 의미를 유지하면서 DB 호출 수가 줄어드는 것입니다.

### 2.4 알람 변경의 전역 직렬화와 관측 누락

근거 수준은 **코드 확인**입니다. [AddAlarm](../../hololive/hololive-alarm-worker/internal/service/notification/alarmservice/alarm_service_add.go#L17)과 [removeAlarm](../../hololive/hololive-alarm-worker/internal/service/notification/alarmservice/alarm_service_remove.go#L33)은 `cacheMutationMu`를 잡은 채 DB 조회·변경과 캐시 갱신을 수행합니다. 작업 시간 측정용 `startedAt`은 lock 획득 뒤 설정돼 operation 지표에서 lock 대기 시간이 빠집니다. 알람 추가·삭제는 사용자 명령 빈도라 실제 병목 가능성은 낮다고 추론합니다.

먼저 lock 대기와 전체 응답 시간을 측정합니다. 이후 채널 단위 직렬화 등을 검토하되 전체 rebuild와 구독 해지의 순서 보장을 함께 설계합니다. 채널 구독 집합을 여러 방이 공유하므로 단순 방별 lock이나 무조건적인 lock 제거는 충분하지 않습니다.

### 2.5 MetricsRecorder의 DB 상태 변경과 순환 의존성

근거 수준은 **코드 확인**입니다. [recordGroupedSendFailure](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/metrics_recorder_grouped.go#L45)는 기록뿐 아니라 claim 해제를 호출하고, [releaseDeliveryClaims](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/claim_manager_release.go#L166)는 DB의 alarm state claim을 변경합니다. [assembleDispatcher](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/construction.go#L120)는 `ClaimManager → SendEngine → MetricsRecorder → ClaimManager` 관계를 만들고 setter 두 개로 연결을 완성합니다.

claim 해제·완료·실패는 orchestration/transition 계층이 명시적으로 수행하고, recorder는 확정된 결과를 받아 기록하도록 좁힙니다. 상태 전이와 attempt telemetry를 같은 transaction에 기록하는 기존 보장은 유지합니다. worker 내부 책임 정리이며 범용 전송 엔진을 shared에 추가할 근거가 아닙니다.

### 2.6 API 프로세스 내부의 HTTP 호출 경계

근거 수준은 **코드 확인 및 후순위 설계 제안**입니다. [API runtime](../../hololive/hololive-api/internal/app/runtime.go#L63)은 bot/admin/llm을 한 프로세스에 구성하지만, bot plane은 같은 프로세스의 llm plane을 [`LLMSchedulerURL`](../../hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/services_llm_clients.go#L43)로 호출합니다([HTTP client](../../hololive/hololive-api/internal/service/subscriptionclient/client.go#L32)). 직렬화·transport·인증·timeout·종료 관리가 추가되며 실제 병목 여부는 측정하지 않았습니다.

API 내부 application interface를 root에서 주입하고 기존 HTTP 계약은 adapter로 유지하는 방안을 검토합니다. 구현은 소유 plane이 제공하고 조립은 API root에서 담당합니다. 이 interface는 API 내부 계약이므로 `hololive-shared`로 올리지 않습니다. 현재 handler의 입력 검증·인가·오류 해석·취소 예산을 생략하지 않고, 기존 route와 외부 소비자를 제거하지 않습니다. plane별 PostgreSQL pool과 YouTube 전용 pool은 `DEC-20260825-hololive-api-dedicated-plane-pools`가 장애 격리와 독립 drain을 위해 정한 구조이므로 통합하지 않습니다.

### 2.7 runtime 공용 조립 패키지

근거 수준은 **코드 확인**입니다. [pkg/providers](../../hololive/hololive-shared/pkg/providers/infra_providers.go)와 [pkg/providers/modules](../../hololive/hololive-shared/pkg/providers/modules/infra.go)는 세 runtime이 모두 import합니다. 수집기는 [ProvideDatabaseResources](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/infrastructure.go#L37)만 쓰지만 `providers`를 통해 `service/delivery`, holodex provider, scraper, member 구현까지 링크합니다. `modules`는 `service/alarm/checker`도 끌어옵니다. 초안이 지적한 "패키지 혼합"의 가장 큰 사례입니다.

runtime별 조립(`BuildInfraModule`, 멤버 캐시·Iris client 생성)은 각 서비스의 `internal`로 옮기고, shared에는 실제로 여러 runtime이 쓰는 자원 생성 helper만 남깁니다. 설정 loader 이동과 같은 단계에서 진행합니다.

## 3. Fallback 정리

키워드 조사(`fallback` 361줄, `legacy` 64줄)와 계약 문서, 이전 감사 표를 대조해 이전 감사가 다루지 않은 경로만 분류했습니다. 이름에 fallback이 없는 default-on-error 경로는 누락됐을 수 있습니다. 각 변경은 `Fallback delta`(제거·추가·유지한 예외와 근거, 집중 검사)를 보고합니다.

### 3.1 계약 없이 오류를 기본값으로 바꾸는 경로

| ID | 경로 | 현재 동작 | 판단 | 조치 | 선행 근거·결정 |
|---|---|---|---|---|---|
| F1 | [YouTube 단건 포맷](../../hololive/hololive-alarm-worker/internal/service/youtube/outbox/format/formatter.go#L67), [grouped 포맷](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/send_engine_support.go#L85) | 멤버 이름을 Valkey hash `alarm:member_names`에서 읽고, Valkey **오류**이면 `misc/vtuber_fallback` 문구로 바꿔 발송합니다. | [멤버 표시명 예외 계약](../current/contracts/alarm.md)의 trigger는 "이름이 비었거나 행이 없음"뿐이라 Valkey 오류는 계약 밖입니다. 다만 단순히 오류로 바꾸면 Valkey 장애 동안 YouTube 알림 전체가 재시도와 revive로 밀립니다(10.1). | dispatch는 이름을 PostgreSQL 정본에서 읽습니다. DB 오류는 재시도 가능한 포맷 실패로, 이름이 빈 경우만 계약대로 `misc/vtuber_fallback`으로 처리합니다. | 없음(10.1에서 결정) |
| F2 | [grouped 포맷 실패](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/send_engine_support.go#L92) | grouped 메시지를 만들지 못하면 개별 발송으로 내려갑니다. | [Grouped fallback 계약](../current/architecture/youtube-egress-lifecycle-contract-20260831.md)은 전송 실패(known-not-accepted)만 허용하며 포맷 실패 trigger가 없습니다. F1 뒤에 남는 원인은 DB 오류와 실제 데이터에서만 드러나는 템플릿 결함이며, 둘 다 Warn 로그 하나로 가려집니다(10.2). | grouped 포맷 실패를 그룹 전체의 재시도 가능한 포맷 실패(`format_message`)로 처리합니다. 계약이 허용한 전송 실패 fallback(F6)은 유지합니다. | F1 |
| F4 | [membernews prompt](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/summarizer/summarizer_prompt.go#L160), [majorevent prompt](../../hololive/hololive-api/internal/planes/llm/internal/service/majorevent/summarizer/summarizer_prompt_render.go#L81) | prompt JSON 직렬화 실패를 고정 문자열로 대체합니다. | 오류 흡수입니다. | 오류를 반환합니다. | 없음 |

### 3.2 계약은 있으나 예외 형식 필드가 빠진 경로

이 경로들은 동작을 유지하되, 계약 문서에 workspace 예외 형식의 빠진 필드를 채웁니다. 근거가 부족하면 그 항목만 사용자 결정으로 올립니다.

| ID | 경로 | 현재 계약 | 빠진 것 | 조치 |
|---|---|---|---|---|
| F3 | [membernews digest](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/service.go#L169) | 2026-09-26 감사 B5(T09 완료)가 "fallback 단일 소유, ResultType 표기·metric"으로 유지를 정했습니다. 사유 enum과 `hololive_member_news_digest_result_total{result_type,reason}`이 있습니다. | 계약 문서(trigger·한도·종단·owner·검토 조건). `ResultType`은 bot 응답에 쓰이지 않아 사용자에게 fallback 여부가 보이지 않습니다. | 동작은 유지하고 [membernews 계약](../current/contracts/membernews.md)에 예외 계약을 작성합니다(10.3). |
| F5 | [Holodex upcoming → 공식 일정 API](../../hololive/hololive-shared/pkg/service/holodex/provider/service_streams_fallback.go#L22) | [공식 일정 계약](../current/contracts/schedule-hololive-tv-api.md)의 Fallback semantics에 trigger·한도·종단·metric이 있습니다. | owner, 검토 조건 | 문서를 보강합니다. 범용 실행기 `internal/service/fallback`은 호출부가 이 경로 하나뿐이므로 provider 안으로 합치되, metric 이름(`hololive_fallback_primary_total`, `hololive_fallback_execution_total`, `hololive_holodex_official_schedule_fallback_total`)은 유지합니다. |
| F6 | [grouped 전송 fallback](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/send_engine_send.go#L436) | lifecycle 계약의 Grouped fallback 절에 trigger·한도·종단이 있습니다. | telemetry(현재 log만), owner, 검토 조건 | 문서를 보강하고 bounded counter를 추가합니다. |
| F7 | [checker의 Holodex 실패 시 persisted live session 계속](../../hololive/hololive-alarm-worker/internal/service/alarm/checker/checking/youtube_checker_input.go#L42) | 2026-07-07 코드 지도에 설명만 있습니다. | 예외 계약 전체 | trigger(Holodex 오류), 한도(persisted session 범위), 종단(Holodex 오류이면서 session 0건이면 실패), telemetry(`hololive_alarm_youtube_persisted_live_sessions_total`), owner(alarm-worker checker), 검토 조건을 작성합니다. |
| F8 | [도움말](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_help.go#L71)·[달력](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_calendar.go#L103) 이미지 → 텍스트 | [2026-10-01 접기 계획](../current/plans/2026-10-01-function-based-see-more-fold.md)에서 기존 경로로만 언급합니다. 결과 불명이면 텍스트를 보내지 않습니다. | 예외 형식 전체, telemetry(현재 log만) | 계약과 counter를 추가합니다. 달력 `imageRenderer == nil`이면 조용히 텍스트로 가므로, renderer가 설정상 필수인지 확인해 필수라면 기동 실패로 바꿉니다. |
| F9 | [majorevent 링크 검사 HEAD → GET](../../hololive/hololive-api/internal/planes/llm/internal/service/majorevent/scraper/link_checker.go#L303) | 이유 주석만 있습니다. | 예외 형식 전체 | 오류 문자열 일치(`timeout`, `connection reset`, `method not allowed`) 대신 타입·status 기준으로 분류하고, 남길 조건을 문서화합니다. |

이름에 fallback이 없어 키워드 조사에서 빠졌다가 F1 분석 중 확인한 경로도 같은 분류에 넣습니다.

| ID | 경로 | 현재 계약 | 빠진 것 | 조치 |
|---|---|---|---|---|
| F11 | [live catchup 억제 marker](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/outbox_grouper_live_suppression.go#L82) | marker 읽기 오류와 형식 오류를 "억제하지 않음"으로 처리하고 `cache_error`·`invalid_marker` metric을 남깁니다. 결과는 중복될 수 있는 추가 알림입니다. | 계약 문서 전체 | 누락보다 추가 알림을 택한 이유, 한도(억제 창), owner, 검토 조건을 계약으로 적습니다. 근거가 없으면 사용자 결정으로 올립니다. |

### 3.3 중복 기본값 경로

| ID | 경로 | 판단 | 조치 |
|---|---|---|---|
| F10 | [delivery applyDefaults](../../hololive/hololive-shared/pkg/service/delivery/dispatcher.go#L170), [grouper 병렬도](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/outbox_grouper.go#L36), [dispatch 보존 일수](../../hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_maintenance.go#L285) | profile·env 로더가 양수를 이미 검증하는데, 생성자나 runner가 0 이하 값을 같은 기본값으로 다시 바꿉니다. 기본값 출처가 두 곳입니다. | 생성자가 잘못된 값을 거절하고 기본값은 로더 한 곳에만 둡니다. 테스트는 명시 값을 넘깁니다. delivery 부분은 1.1과 같은 변경입니다. |

### 3.4 재작업하지 않는 경로

다음은 계약이나 이전 판정이 있으므로 이 계획에서 다시 다루지 않습니다.

- 알림 멤버 표시명 예외 계약: 제거 조건은 두 지표가 0으로 유지되고 모든 구독 채널이 한국어 표시명을 갖는 것입니다.
- 알람 구독자 DB fallback: [alarm-worker 문서](../current/services/alarm-worker.md)와 metric이 있고, 이전 감사에서 `keep_not_legacy`로 판정했습니다.
- YouTube parser 구조 대체 탐색과 Iris `fallbackBaseURL` 이름: 이전 감사에서 이름만 해당한다고 판정했습니다. Iris base URL은 파일과 URL 중 하나만 쓰는 단일 출처입니다.
- alarm dispatch wakeup: polling이 정상 경로이고 wakeup은 지연 단축입니다([queue 계약](../current/QUEUE_AND_PUBSUB_CONTRACTS.md)).

## 4. 구형 호환 alias·잔재 정리

### 4.1 코드 alias·잔재 (외부 계약 없음)

| ID | 대상 | 근거 | 조치 |
|---|---|---|---|
| L1 | scraping의 parser 재바인딩: [videos.go](../../hololive/hololive-shared/pkg/service/youtube/scraper/scraping/videos.go#L16), [stats_parser.go](../../hololive/hololive-shared/pkg/service/youtube/scraper/scraping/stats_parser.go#L8), [recent_videos_parser.go](../../hololive/hololive-shared/pkg/service/youtube/scraper/scraping/recent_videos_parser.go#L12), [videos_rss.go](../../hololive/hololive-shared/pkg/service/youtube/scraper/scraping/videos_rss.go#L7) | parser 분리 뒤 남은 지역 이름 11개와 상수 1개이며 패키지 안에서 25회 씁니다. 테스트에서 교체하는 seam이 아닙니다. | `parser.X`를 직접 호출하고 지역 이름을 삭제합니다. |
| L2 | [ErrReplyHandoff*](../../hololive/hololive-alarm-worker/internal/egress/iris_sender.go#L20) | `sendoutcome` sentinel의 별칭이며 22회 씁니다. | `sendoutcome`을 직접 씁니다. |
| L3 | [API bot privacylog 상수](../../hololive/hololive-api/internal/planes/bot/internal/privacylog/privacylog.go#L31) | shared privacylog 상수 6개를 재노출합니다. | 상수는 shared를 직접 씁니다. bot 전용 함수(`RoomAttr`·`ChatAttr`의 상관 키 규칙)는 유지하고, 다른 함수의 중복 여부는 구현 때 확인합니다. |
| L4 | [claim.MemoryCache](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/claim/claim_memory.go#L12) | production 호출이 없고 테스트만 씁니다. 주석은 "외부 cache가 없을 때 fallback으로 사용 가능"이라고 설명합니다. | 테스트 파일로 옮기거나 삭제합니다. |
| L5 | [ProcessOnceForTest](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/dispatcher.go#L303) | 호출부는 같은 디렉터리의 테스트뿐입니다. 주석의 "외부 패키지가 의존"과 "부수효과 없음"은 사실과 다릅니다. | `export_test.go`로 격리합니다. |
| L6 | [delivery_format_aliases.go](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/delivery_format_aliases.go) | package 선언만 남아 있습니다. | 삭제합니다. |
| L7 | [AlarmService.Close](../../hololive/hololive-alarm-worker/internal/service/notification/alarmservice/alarm_service_lifecycle.go#L112), [worker bootstrap의 소유권 flag](../../hololive/hololive-alarm-worker/internal/app/workerapp/build_runtime.go#L89) | Close가 아무 작업도 하지 않는데 실패 정리·shutdown 연결이 남아 있습니다. | no-op lifecycle을 정리합니다. 실제 Holodex retry 종료는 별개이며 유지합니다. |
| L8 | `youtubedispatch/message_format_render.go`의 MessageFormatter wrapper(2026-10-02 삭제) | 대부분 shared formatter에 재위임하고 오류를 다시 감쌉니다. | renderer를 worker로 옮길 때(8절 6단계) 제거합니다. |
| L9 | [채널 검색 주석](../../hololive/hololive-shared/pkg/service/holodex/provider/service_channels_search.go#L110) | 삭제된 YouTube scraper fallback을 설명하는 주석만 파일 끝에 남아 있습니다. | 삭제합니다. |
| L10 | `LogAndWrapError` 호출 형태 11곳(holodex provider 2파일, alarmservice 3파일) | helper는 오류가 있으면 항상 non-nil을 반환하므로 뒤의 `return nil, nil`은 도달할 수 없고, `log and wrap error:` 접두어는 정보가 없습니다. | helper 결과를 그대로 반환합니다. |

### 4.2 외부 이름 호환

| ID | 대상 | 근거 | 조치 |
|---|---|---|---|
| L11 | [`hololive_messagestrings_lookup_fallback_total`](../../hololive/hololive-shared/pkg/service/messagestrings/metrics.go#L44) | 코드 대체 문구는 2026-09-26 B6에서 사라졌는데 대시보드 연속성을 이유로 이름만 유지합니다. 계약·owner·제거 조건이 없고, 관리 대상 대시보드·alert·rule test에도 소비자가 없습니다(10.5). | `hololive_messagestrings_lookup_miss_total`(label 동일)로 바꾸고 CHANGELOG에 기록합니다. |

### 4.3 삭제된 코드·이름의 재등장 가드

stack 규칙은 이런 가드를 금지하고 과거의 "영구 계약" 표기보다 우선합니다. runtime의 퇴역 env 거절 가드는 기동 동작이며 제거 조건이 있으므로 이 목록에 넣지 않습니다.

| ID | 대상 | 근거 | 조치 |
|---|---|---|---|
| G1 | [store_ownership_test.go](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store_ownership_test.go) | 삭제된 shared store 디렉터리와 그 import의 재등장을 막는 테스트입니다. 다른 module의 import는 Go `internal` 규칙이 이미 막습니다. | 파일을 삭제합니다. |
| G2 | [readiness_test.go](../../hololive/hololive-alarm-worker/internal/readiness/readiness_test.go#L32) | 퇴역한 `egress_flags` 필드의 재등장을 막는 assertion이며 주석에 "영구 계약(재도입 방지)"으로 적혀 있습니다. | 해당 assertion만 삭제하고 나머지 readiness 검증은 유지합니다. |
| G3 | [ci-notification-egress-gate.sh](../../scripts/architecture/ci-notification-egress-gate.sh#L137) | 퇴역 enablement 이름을 compose에서 grep합니다. runtime 가드가 기동 때 같은 키를 거절합니다. | 이 부분을 삭제합니다. runtime 가드는 조건부 종단으로 유지합니다. |

### 4.4 저장 데이터 호환 경로 (조건부 제거)

이 경로들은 구형 행을 fail-closed로 종단 처리하므로 fallback이 아닙니다. 해당 데이터가 남아 있는 동안은 유지하고, 소유 ops skill로 read-only 조회한 근거가 생기면 제거합니다.

이미 추적 중인 항목은 [retired-guards.tsv](../../../docs/agent-workflows/evidence/2026-09-26-stack-audit/retired-guards.tsv)의 조건을 따릅니다. `config/settings`의 퇴역 env 존재 기준 가드, worker profile drain 필드, [dispatchops의 send_unit_id NULL 조회](../../hololive/hololive-api/internal/planes/admin/internal/service/dispatchops/model.go#L268)(2026-09-28 종단 NULL 420건), [Twitch·Chzzk 전용 envelope 드레인 종단](../../hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_group.go#L240)이 해당하며, 대부분 `remove_after 2026-12-31`입니다. `config_youtube_producer_retired.go` 가드는 추적표에 "2026-10-01 이후 재검토"로 적혀 있으므로 0단계에서 조건을 확인합니다.

이전 감사가 다루지 않은 항목은 다음과 같습니다.

| ID | 경로 | 처리 대상 | 제거 조건 |
|---|---|---|---|
| D1 | [delivery request 준비](../../hololive/hololive-shared/pkg/service/delivery/request.go#L56) | 전송 이력은 있는데 고정 request가 없는 구형 notification delivery 행을 격리합니다. | 활성 상태 행 중 `attempt_count > 0`, request 없음, `known_unsent` 아님이 0건 |
| D2 | [ErrLegacySendRequest](../../hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/send_request.go#L37), [runner 처리](../../hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_runner.go#L284) | 이전 시도가 있는데 send request가 없는 alarm dispatch unit | 같은 형태의 활성 unit 0건 |
| D3 | [ErrLegacyRequestEvidence](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/send_request.go#L20), [revive 분류](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/transition_revive.go#L255) | request 증거가 없는 구형 YouTube delivery | 같은 형태의 활성 delivery 0건 |
| D4 | [OriginLegacyUnknown](../../hololive/hololive-shared/internal/service/youtube/reconcile/live/types.go#L24), [absence 적용](../../hololive/hololive-shared/internal/service/youtube/reconcile/live/apply_absence.go#L76) | origin 기록 이전의 live session | `legacy_unreviewed` 지표 0 유지와 해당 행 0건 |

정확한 조회 SQL은 구현 때 현재 schema로 작성합니다.

### 4.5 범위 밖 관찰

- 수집기의 [collecterr 계약 이름 재노출](../../hololive/hololive-youtube-collector/internal/runtime/collecterr/errors.go#L18)과 [collectorruntime registry type alias](../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/registry.go#L15)는 같은 기준을 적용하면 후보입니다.
- 지역 편의 이름(`send_engine_flow.go`의 logschema 재바인딩, `tracking/observation`의 상수 별칭, `reconcile/live`의 `Status` 별칭, `htmlscraper`의 index 별칭)은 구형 호환이 아닙니다. 단독 작업으로 만들지 않고 해당 파일을 옮길 때 정리합니다.
- repo 수준 script 검사(`test-compose-services.sh`, `repo_compose_*_test.go` 등)가 stack 규칙에 맞는지는 이 계획에서 검토하지 않았습니다.

## 5. shared 유지와 이동 기준

`shared-go`는 스택 여러 프로젝트가 사용할 수 있는 도메인 독립 기능을 담당합니다. `hololive-shared`는 Hololive runtime 간 데이터 계약과 실제 다중 소비 기능을 담당합니다. 실행 순서·전송 정책·canonical 쓰기·runtime 조립은 소유 서비스의 `internal`에 둡니다.

다음 표는 이동 제안입니다. 공개 wire·DB 계약, Go 내부 경로, 실행 구현을 같은 것으로 취급하지 않습니다. 소비자 열은 runtime binary별 `go list -deps` 결과입니다(W: alarm-worker, A: API, C: collector).

| 대상 | 링크 | 권장 위치 또는 조치 | 이유와 선행 작업 |
|---|---|---|---|
| observation/alarm envelope, version, 공통 identity, job/lease 계약 | - | `hololive-shared` 유지 | producer/consumer가 같은 의미를 공유합니다. job/lease 타입은 계약 패키지로 옮깁니다(2.1). |
| YouTube outbox payload 타입 | - | shared 계약으로 모으기 | producer와 renderer의 타입이 분산돼 있습니다. 기존 JSON 필드·저장 데이터 호환성을 먼저 고정합니다. |
| `service/notification/alarmservice`, `internal/service/notification/alarmcache` | W | 워커 `internal`로 이동 | 현재 SERVICE_OWNERSHIP도 이동 대상으로 적고 있습니다. |
| `service/alarm/dedup`, `queue`, `dispatchoutbox` 실행부 | W | 워커로 이동 | envelope·공유 DB 계약은 남깁니다. SERVICE_OWNERSHIP의 facade 잔류 분류를 함께 갱신합니다. |
| `service/alarm`의 HTTP client·DTO·공통 정책 | WA | shared에 유지하되 실행 저장소와 분리 | API와 워커가 사용하는 경계입니다. |
| `service/delivery` | WAC | producer/consumer 계약과 실행부를 부분 분리 | dispatcher·claim·전이 구현은 worker로, API enqueue 구현은 API 소유 후보입니다. C는 `providers` 경유 링크입니다. SERVICE_OWNERSHIP의 "진성 다중 소비자" 분류를 바꾸는 제안입니다. |
| `sourceobservation` consume/reconcile/canonical/retention/replay | AC | API YouTube plane으로 이동 | 연결된 batchrepo·reconcile 타입도 함께 분리합니다. |
| `sourceobservation` publish 실행부 | AC | 수집기로 이동 | publish 계약과 원자성을 먼저 분리·보존합니다. |
| `youtube/outbox/format` 렌더링 실행부 | W | 워커로 이동 | payload 타입 분리가 선행합니다. SERVICE_OWNERSHIP의 "양측 계약면" 분류를 바꾸는 제안입니다. |
| `providers`, `providers/modules` | WAC | runtime별 조립은 각 서비스로 이동 | 2.7. 실제 다중 소비 helper만 남깁니다. |
| `member`, `template`, `messagestrings`, `kakaoroom`, `xspaces`의 공통 계약·저장 기능 | WA, `member`는 WAC | 필요한 공통 부분 유지 | 기능별 실행 정책까지 shared로 확대하지 않습니다. |
| `config/settings/alarmworker`, `apiplane`, `collector` loader와 role별 조립 | 각 1개 | 각 서비스로 단계적 이동 | 공통 설정 타입·파싱을 먼저 분리합니다. 환경변수·기본값·검증·worker profile 계약은 유지합니다. |
| nil 검사·HTTP body 상한 등 범용 기능 | - | 기존 `shared-go` 직접 사용 | 6절. |
| 달력·방송 카드·API plane 간 application interface | - | API 내부 유지 | 현재 다른 runtime의 공통 기능이 아닙니다. |

[SERVICE_OWNERSHIP의 Shared Package Retention](../current/SERVICE_OWNERSHIP.md#shared-package-retention)은 `service/youtube/outbox/{store,format,deliverysql,dispatchstate}`를 잔류 대상으로 적지만, `store`와 `dispatchstate`는 이미 shared에 없습니다. 첫 이동 단계에서 이 분류를 실제 소비자에 맞게 갱신합니다.

YouTube payload는 [producer의 domain embedding](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/content_persist.go#L89), [poller helper의 payload](../../hololive/hololive-shared/pkg/service/youtube/poller/runtime/helpers.go#L69), [renderer의 VideoPayload와 CommunityPayload](../../hololive/hololive-alarm-worker/internal/service/youtube/outbox/format/formatter.go#L206)에 나뉘어 있습니다. 단일 wire 타입과 명시적 변환을 두면 domain 필드 추가가 알림 JSON에 자동 전파되는 결합을 줄일 수 있습니다. 기존 필드를 임의로 줄이거나 저장된 payload를 다시 렌더링·재발급하는 작업은 포함하지 않습니다.

## 6. 공유 구현 재사용

아래 shared-go 기능은 production pin인 `v2.8.0`에도 있으므로 버전 상향 없이 재사용할 수 있습니다.

| 현재 구현 | 기존 재사용 대상 | 변경 방향과 보존 조건 |
|---|---|---|
| [달력 finder nil 검사](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/calendar_entry_cache.go#L225), [deliverysql.IsNilDB](../../hololive/hololive-shared/pkg/service/youtube/outbox/deliverysql/pgx.go#L25) | [shared-go reflectutil.IsNil](../../../shared-go/pkg/reflectutil/nil.go#L5) | typed nil 판정을 유지하고 기존 함수를 직접 사용합니다. 새 facade는 만들지 않습니다. |
| [썸네일 body 읽기](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/broadcast_thumbnail_downloader.go#L187), [달력 사진 body 읽기](../../hololive/hololive-api/internal/planes/bot/internal/render/calendar_photos.go#L244) | [shared-go httputil.ReadAllLimited](../../../shared-go/pkg/httputil/body.go#L20) | 바이트 상한 읽기를 재사용합니다. 빈 이미지 허용 여부·MIME·호스트·크기 정책·close 소유권은 각 호출부가 유지합니다. |
| [content 필드 변경](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/content_persist.go#L125) | [dbx.ExecStatements](../../hololive/hololive-shared/pkg/dbx/tx.go#L138) | 기존 transaction batch 기능을 사용합니다. |
| [dbx SQL helpers](../../hololive/hololive-shared/pkg/dbx/sqlhelpers.go), [deliverysql SQL helpers](../../hololive/hololive-shared/pkg/service/youtube/outbox/deliverysql/pgx.go) | 하나의 DB 실행 구현 | Exec/Select/Get·transaction 처리를 정리하되 도메인 row scan은 repository에 남깁니다. |

두 SQL placeholder 변환기는 SQL 문법을 해석하지 않고 모든 `?`를 치환합니다. 문자열 리터럴·JSONB 연산자도 바뀐다는 제한이 기존 주석에 있습니다. 현재 사용 SQL이 이 제한을 위반한다고 확인한 것은 아닙니다. 변경하는 SQL부터 `$n`과 배열 인자를 사용하며 새 자체 SQL parser를 만들지 않습니다.

이름이 비슷해도 다음 구현은 즉시 치환하지 않습니다.

- [pgxutil.Rollback](../../hololive/hololive-shared/pkg/pgxutil/rollback.go)은 취소 분리·5초 timeout을 적용하고 이미 닫힌 transaction 오류를 그대로 반환합니다. [pgxdb.RollbackDeferred](../../../shared-go/pkg/db/pgxdb/tx.go)는 호출자 context를 쓰고 `ErrTxClosed`를 무시하며 오류를 named return에 합칩니다. 계약을 맞춘 뒤 공통화 여부를 판단합니다.
- [sqlassets.MustReader](../../hololive/hololive-shared/pkg/sqlassets/sqlassets.go)는 SQL 조각 연결에 필요한 끝 공백을 유지합니다. [sqlutil.MustQuery](../../../shared-go/pkg/sqlutil/sql.go)는 trim하므로 단순 교체하면 토큰이 붙을 수 있습니다.
- 최초 구현이라는 이유로 기존 상태 머신·도메인 identity·idempotency 규칙을 라이브러리로 교체하지 않습니다.

## 7. 컨벤션 정리

작성 주체는 코드만으로 판정할 수 없습니다. 여기서 정리 대상은 근거 없이 늘어난 추상화, 사라진 기능을 위한 잔여 구조, 실제 동작과 다른 설명입니다. 구체 항목은 4.1절에 모았습니다.

- [pgxutil](../../hololive/hololive-shared/pkg/pgxutil/rollback.go)과 [sqlassets](../../hololive/hololive-shared/pkg/sqlassets/sqlassets.go)는 public contract·side effect 주석이 영어와 한국어로 섞여 있습니다. 변경하는 범위에서 실제 부수효과·제약·이유를 한국어로 정리합니다.
- 오류 wrapping은 실패 action과 context를 추가할 때만 유지합니다. 같은 문구를 반복하는 wrapper(1.2절의 달력 조회, L10)는 제거하되 원인 오류와 `errors.Is/As` 판정은 보존합니다.
- 로그 마스킹은 repository의 기존 방식을 따릅니다. 이번 조사로 모든 로그 경로의 안전성을 검증했다고 주장하지 않습니다.

claim token, frozen request, 결과 불명 격리, 방별 순서, 상태 전이 회귀 테스트는 실제 계약을 지키는 장치입니다. 파일 수나 추상화 개수만으로 불필요하다고 분류하지 않습니다. `alarm_dispatch`, `youtube_delivery`, `notification_delivery`의 상태 의미가 다르므로 세 엔진을 하나의 범용 framework로 합치는 제안은 하지 않습니다.

## 8. 실행 순서와 의존성

동작 수정, fallback 정리, 코드 이동을 분리하면 회귀 원인을 좁힐 수 있습니다. 후속 구현 시 현재 코드를 다시 확인하고 이미 해결된 항목은 제외합니다. 아래 완료 판단은 계획이며 아직 수행한 항목이 아닙니다.

| 단계 | 작업 | 실제 의존성 | 완료 판단 |
|---|---|---|---|
| 0 | 근거 수집과 결정 | 없음. 운영 조회는 소유 ops skill로 read-only 수행 | F3·F7·F8·F11의 30일 log·metric 수(예외 계약의 검토 조건 근거), D1–D4 행 수, 추적 중인 퇴역 가드 조건의 충족 여부를 기록합니다. 10.4의 결정을 받습니다. |
| 1 | 동작 결함 수정: 1.1(attempt_timeout 연결과 F10 delivery 부분 포함), 1.2, 1.3 | 재현 진단을 회귀 테스트로 전환 | 1절의 완료 조건 |
| 2 | 즉시 가능한 정리: F4, F10 나머지, L1–L7, L9–L11, G1–G3 | 없음. 1단계와 병행할 수 있으나 같은 파일은 순차로 진행 | 빌드·테스트 통과. F4·F10·L11 외에는 동작 변화가 없습니다. |
| 3 | fallback 단일 경로화와 계약 보강: F1 → F2, 계약 작성 F3·F5–F9·F11 | F2는 F1 뒤. 계약 보강의 검토 조건은 0단계 근거를 씁니다. | 계약 문서와 코드가 일치하고, 오류가 드러나는 경로를 테스트합니다. |
| 4 | 기존 공통 함수 재사용, recorder 부수효과 분리 | 기존 계약·오류 의미 확인 | 동일 입력·실패 조건에서 같은 결과와 상태 전이 |
| 5 | payload와 observation job/lease 공유 계약 분리, `providers` 조립 분리 준비 | 현재 producer/consumer와 저장 데이터 형태 확인 | 직렬화·검증 결과와 기존 payload 호환성 보존 |
| 6 | 워커 전용 shared 실행 구현 이동(L8과 지역 편의 이름 정리 포함) | 5단계 | API가 worker 실행 구현을 의존하지 않고 runtime 이름·설정·DB 의미 유지 |
| 7 | sourceobservation publish/consume와 연결 구현 분리 | 5단계 | publish와 canonical/intent/finalize 원자성, 권한·소유권 경계 유지 |
| 8 | SQL batch·구독 조회 최적화, 필요 시 API 내부 호출 전환 | 이동 후 소유자와 기준 측정 확정 | PG·Valkey 호출 수, pool 대기, transaction 보유 시간, 처리 지연을 전후 비교 |
| 9 | 조건부 제거: D1–D4, 추적 중인 퇴역 가드·종단 경로 | 0단계 근거와 각 조건 충족 | 조건 충족 근거와 삭제를 같은 변경에 기록 |

role별 loader 이동은 공통 설정 타입의 소비 관계를 먼저 분리한 뒤 진행합니다. 단순 파일 이동을 위해 서비스 간 `internal` import나 새 compatibility alias를 만들지 않습니다.

## 9. 검증

- 변경한 서비스와 shared의 실제 소비자를 대상으로 targeted 테스트를 먼저 실행합니다. concurrency 변경에는 race, transaction·lease·receipt 변경에는 격리 PostgreSQL 통합 시험, payload·렌더 변경에는 기존 저장 payload와 최종 전송 본문 검증이 필요합니다. canonical·outbox atomicity와 실패 후 결과 불명 상태는 별도 회귀 사례로 유지합니다.
- alias·잔재 제거는 workspace 빌드와 함께 각 module을 `GOWORK=off`로 빌드해 production pin 기준으로도 확인합니다. 삭제한 이름의 재등장을 막는 검사는 추가하지 않습니다.
- fallback 변경은 실패가 오류로 드러나는 경로를 테스트하고, 변경마다 `Fallback delta`와 집중 검사 결과를 보고합니다.
- stack 검사는 해당 surface를 바꿀 때 실행합니다: retry·retention·replay를 건드리는 1.1·D1–D4는 `bash tools/checks/check-stack-retry-contract.sh`, reissue 경로를 건드리면 `check-stack-reissue-contract`, worker profile·schema·fixture를 바꾸면 `check-stack-worker-contract`.
- 저장소 검증은 `./build-all.sh --build-only --no-bump`와 영향받은 module 테스트입니다. Stage 3·prerequisite·적용 대상 NilAway/race는 계속 차단 조건이며, 게시는 `scripts/ci/pre-push-gate.sh`를 따릅니다. 검증 소유권은 [README](../../README.md)를 따릅니다.
- 계약 위치·소유권·route·queue·fallback 계약이 바뀌는 단계에서는 SERVICE_OWNERSHIP, `contracts/alarm.md`, `contracts/membernews.md`, 공식 일정 계약, lifecycle 계약을 같은 변경에서 갱신합니다.
- 새 구조 budget, checker self-test, 삭제한 이름의 재등장 grep guard, 별도 plan gate는 추가하지 않습니다.

## 10. 결정 분석

2026-10-02에 결정 항목을 코드, 결정 기록, [2026-09-26 운영 증거](../../../docs/agent-workflows/evidence/2026-09-26-stack-audit/t18-ops-evidence.md), 로컬 `observability-stack` 저장소로 다시 분석했습니다. 운영 시스템에는 새로 접속하지 않았습니다. F1·F2·F3·L11은 근거로 정했고, 사용자 결정이 남은 항목은 10.4 하나입니다.

| 항목 | 결정 | 이전 판본과 달라진 점 |
|---|---|---|
| F1 | dispatch가 멤버 이름을 PostgreSQL 정본에서 읽습니다. | 권장안 "오류를 포맷 실패로"를 철회했습니다. |
| F2 | grouped 포맷 실패를 그룹 전체의 재시도 가능한 실패로 처리합니다. | 결정을 확정했습니다. |
| F3 | 동작을 유지하고 예외 계약을 문서화합니다. | 권장안 "majorevent처럼 퇴역"을 철회했습니다. 이미 감사 B5가 유지로 정한 경로입니다. |
| L11 | `hololive_messagestrings_lookup_miss_total`로 바꿉니다. | 소비자가 없어 별도 관측 승인이 필요하지 않습니다. |
| 10.4 | 사용자 결정 필요. 권장은 현재 plane별 구조를 결정 기록으로 승인하는 것입니다. | 결정 기록과 코드의 불일치를 새로 확인했습니다. |

### 10.1 F1: YouTube 알림의 멤버 이름 조회 오류

| 사실 | 근거 |
|---|---|
| 이름의 원천은 Valkey hash `alarm:member_names` 하나입니다. 필드가 없으면 빈 문자열이고, 연결 오류만 오류가 됩니다. | [GetMemberName](../../hololive/hololive-alarm-worker/internal/service/youtube/outbox/format/formatter.go#L223), [HGet](../../hololive/hololive-shared/pkg/service/cache/service_hash.go#L44) |
| 포맷 실패는 재시도 가능한 실패입니다. youtube_delivery는 60초 간격으로 3회 재시도하고, 그 뒤 revive가 5분마다 1시간 신선도 기간 안에서 다시 시도합니다. | [format 실패 전이](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/send_engine_send.go#L259), worker profile |
| 같은 dispatch는 Valkey 오류 때 구독자 조회를 DB로 넘기고(`requireCacheSuccess=false`), 억제 marker 읽기 오류도 발송 쪽으로 처리합니다(F11). Valkey 장애 중에도 발송을 계속하도록 설계돼 있습니다. | [구독자 조회](../../hololive/hololive-shared/pkg/service/alarm/targets.go#L62) |
| 같은 hash를 읽는 live checker는 Valkey 오류 때 그 주기 전체를 실패로 처리합니다. | [LoadMemberNamesByChannel](../../hololive/hololive-alarm-worker/internal/service/alarm/checker/checking/common.go#L138) |
| PostgreSQL에는 예외 계약 순서(members → `alarms.member_name`)를 구현한 조회가 있고, dispatch의 claim·전이는 이미 PostgreSQL에 의존합니다. | [Repository.GetMemberName](../../hololive/hololive-shared/pkg/service/alarm/repository.go#L186) |
| Valkey 축소 계획은 이 hash를 "이름 우선순위·구독 fallback·batch 의미를 검증하기 전" 유지(K4)로 남겼습니다. | [Valkey 축소 계획](../current/plans/2026-09-28-valkey-dependency-reduction.md) |

| 안 | Valkey 장애 때 결과 | 규칙 적합성 |
|---|---|---|
| (a) 오류를 포맷 실패로 처리 | 이름 하나 때문에 YouTube 알림 전체가 최대 1시간 지연되고, 넘으면 발송되지 않습니다. 구독자 DB 조회로 확보한 가용성을 이름 조회가 깨뜨립니다. | 충족 |
| (b) 대체 문구 유지, 계약 trigger 확장 | 일반 문구로 발송합니다. 사용자에게 틀린 이름이 보입니다. | 계약과 독립 근거가 필요합니다. |
| (c) PostgreSQL 정본에서 이름 조회 | 정확한 이름으로 발송합니다. DB 오류는 재시도 가능한 실패이며, DB가 멈추면 dispatch 전이도 멈추므로 새 가용성 의존이 생기지 않습니다. | 충족(단일 경로) |

결정은 (c)입니다. 남는 차이는 예외 계약의 3단계(alarm cache 기록 때 호출자 값)가 dispatch에서 쓰이지 않는 것입니다. members와 `alarms.member_name`이 모두 빈 채널에만 해당하며, 2026-09-26 기준 알림 채널 18개는 모두 members에 등록돼 있었습니다. 구현은 dispatch 주기마다 대상 채널의 이름을 한 번에 읽는 조회로 합니다. `GetAllMemberNames`는 cache warm용 gauge를 갱신하므로 그대로 재사용하지 않습니다. live checker도 같은 원천으로 맞추면 hash 소비자가 줄어드므로, 그때 K4의 유지 조건을 다시 검토합니다. 이 후속은 이 계획의 범위 밖입니다.

### 10.2 F2: grouped 포맷 실패의 개별 발송

- grouped 템플릿 key는 baseline seed에 있습니다(`OUTBOX_VIDEO_GROUP`의 seed를 확인했습니다).
- 관리자 템플릿 저장(`AdminService.Save`)은 샘플 데이터 검증을 거칩니다.
- F1 이후 grouped 포맷 실패의 원인은 DB 오류(재시도가 맞음)이거나 실제 데이터에서만 드러나는 템플릿 결함(드러나야 함)입니다. 개별 발송 전환은 둘 다 Warn 로그 하나로 가리고, 묶음 메시지 대신 여러 메시지를 보냅니다.

결정은 grouped 포맷 실패를 그룹 전체의 재시도 가능한 포맷 실패(`format_message`)로 처리하는 것입니다. 대가로, 실제 데이터에서만 깨지는 템플릿이 있으면 수정 전까지 해당 그룹을 발송하지 못합니다. 이 상태는 lifecycle 실패 사유로 드러나며, 템플릿을 고치면 revive 기간 안에서 다시 발송됩니다.

### 10.3 F3: membernews 결정적 digest

- 2026-09-26 감사 B5가 이 경로를 검토해 "fallback 단일 소유, ResultType 표기·metric 유지"로 정했고 T09에서 구현했습니다. 이전 판본의 "감사 목록에 없었다"는 서술은 틀렸습니다. majorevent의 비슷한 경로를 퇴역시킨 같은 날의 결정과 방향이 다르지만, 두 결정 모두 사용자가 확정했습니다.
- fallback digest는 출처 검증과 prompt guard를 통과한 후보 최대 5건을 날짜·분류·제목·원문 링크로 나열하므로 내용이 틀리지 않습니다.
- 사유 구분은 정확합니다. `llm_disabled`는 LLM client 미설정이고, provider 장애는 `summarizer_error`입니다.
- 남은 공백은 계약 문서가 없다는 점과, `ResultType`이 bot 응답에 쓰이지 않아 사용자가 fallback 여부를 모른다는 점입니다.

결정은 동작을 유지하고 [membernews 계약](../current/contracts/membernews.md)에 예외 계약을 쓰는 것입니다. 내용은 다음과 같습니다.

- trigger: `llm_disabled`, `summarizer_error`, `validation_empty`, `empty_result`
- 한도: 검증된 후보 최대 5건, 외부 호출·재시도 없음, 호출자 취소 시 만들지 않음
- 종단: 후보가 없으면 fallback이 아니라 빈 digest
- telemetry: `hololive_member_news_digest_result_total{result_type,reason}`과 Warn 로그
- owner: hololive-api llm plane의 membernews
- 검토 조건: 0단계에서 확인한 `summarizer_error` 비율

사용자 응답에 fallback 여부를 표시하는 것은 bot 문구 변경이므로 이 결정에 포함하지 않습니다.

### 10.4 API 프로세스의 Valkey client·멤버 캐시 3벌 (사용자 결정 필요)

| 기록·현황 | 내용 |
|---|---|
| `DEC-20260626-hololive-api-three-runtime-consolidation` (accepted/implemented) | PostgreSQL pool과 Valkey client는 process당 한 번만 만들고 shared Valkey client는 1개입니다. 메모리 예산은 공통 infra와 domain cache를 중복 생성하지 않는다는 전제입니다. |
| `DEC-20260825-hololive-api-dedicated-plane-pools` (accepted/verified) | PostgreSQL pool만 plane별로 바꿨습니다(장애 격리와 독립 drain). "다른 consolidation 계약은 변경하지 않는다"고 명시했습니다. |
| 현재 코드 | Valkey client 3개(bot·admin·llm), PostgreSQL pool 4개(YouTube 포함), 멤버 캐시 3개입니다. Valkey client를 plane별로 둔다는 결정 기록은 찾지 못했습니다. |

코드는 `DEC-20260626`의 Valkey client 조항과 어긋나고, 멤버 캐시 중복은 어떤 결정도 다루지 않습니다. 비용은 작습니다: 시작 적재 3회, 구독 연결 3개, 15초마다 Valkey GET 3회입니다. 멤버 캐시는 plane의 PostgreSQL pool로 적재하므로, 하나로 합치면 한 plane의 pool에 다른 plane이 기대거나 root 소유 pool이 하나 더 필요합니다.

- **(a) 권장: 현재 구조를 결정 기록으로 승인합니다.** `DEC-20260825`처럼 Valkey client와 멤버 캐시도 plane lifecycle에 묶는다고 기록하고, 코드는 1.3의 plane 단위 종료 수정만 합니다. 이미 accepted된 plane별 pool 결정과 lifecycle 모델이 같고, 합쳐서 얻는 자원 이득이 작으며, 7단계 조립 분리에서 root 소유 자원을 새로 만들지 않아도 됩니다.
- **(b) `DEC-20260626`대로 되돌립니다.** API root가 Valkey client 하나를 소유해 plane에 주입하고 모든 plane drain 뒤 닫습니다. 멤버 캐시는 plane별로 두거나 root 소유 pool을 새로 둡니다.

어느 쪽이든 결정 기록이나 코드 중 하나는 바꿔야 합니다.

### 10.5 L11: message_strings metric 이름

- 이름의 "fallback"은 2026-08 호출자 대체 문구가 있던 시절의 이름입니다. `DEC-20260926-hololive-message-strings-startup-validation`(감사 B6)이 대체 문구를 지웠고, 지금은 "조회했지만 값이 없음"을 셉니다.
- 대시보드와 alert는 `observability-stack`이 관리하며 대시보드는 `grafana/build-dashboards.py`로 생성합니다. 2026-10-02 `40e2303` 기준으로 이 metric을 쓰는 대시보드·alert·rule test는 없습니다. 저장소 안의 언급은 코드, CHANGELOG, 2026-08 조사 문서뿐입니다.
- 이름을 바꾸면 Prometheus의 기존 시계열은 끊깁니다. 필수 key는 기동 때 검증하므로 값은 0에 가까워야 합니다.

결정은 `hololive_messagestrings_lookup_miss_total`(label 동일)로 바꾸고 CHANGELOG에 기록하는 것입니다. 관리 대상 소비자가 없으므로 일반 코드 변경과 같은 범위로 처리합니다. Grafana UI에서 수동으로 만든 대시보드가 있는지는 확인하지 않았습니다.

### 승인 경계

0단계의 운영 조회는 소유 ops skill로 read-only 수행합니다. DB 쓰기, 배포, Git 게시는 각각 별도 승인 대상입니다.

## 구현 진행 기록 (2026-10-02)

작업 위치는 worktree `.worktrees/hololive-alarm-api-refactor-20261002`(branch `refactor/alarm-api-shared-20261002`)입니다. 처음에는 `5c12d2c50`에서 시작했고, 같은 날의 [코드 감사 후속 수정](2026-10-02-code-audit-fixes.md)(`fix/code-audit-20261002`, `32e3d2ae8`) 위로 다시 쌓았습니다. 로컬 커밋만 했고 게시·배포는 하지 않았습니다.

### 완료한 항목

| 단계 | 항목 |
|---|---|
| 1 | 세 결함은 감사 수정의 구현을 기준으로 남겼습니다(아래 "감사 수정과의 통합"). 1.1은 실행 슬롯이 빈 방의 첫 항목만 claim하고, `attempt_timeout`을 profile에서 받습니다. 이 branch는 dispatcher가 0 이하 설정을 거절하는 F10 delivery 부분을 더했습니다. 1.2는 공유 조회를 호출자 취소와 분리하고 봇 명령 예산(10초)으로 제한합니다. 1.3은 멤버 캐시의 `Close`가 epoch 작업을 멈추고 기다리며, bot·admin infra와 llm runtime이 Valkey·DB를 닫기 전에 호출합니다. 이는 10.4 (a)의 plane 단위 소유와 같습니다. |
| 2 | F4, F10 나머지, L1–L7, L9–L11, G1–G3. L3는 범위를 조사한 뒤 보류하지 않고 수행했으며, bot privacylog에는 `RoomAttr`·`ChatAttr`·`correlationSource`만 남겼습니다. |
| 2(추가) | 같은 기준의 재등장 가드를 더 지웠습니다: `check-youtube-egress-lifecycle-ownership.sh`의 legacy store·retired writer 검사, `test-three-runtime-topology.sh`의 퇴역 서비스·컨테이너·alias·이미지 검사, `test-compose-services.sh`의 퇴역 이름 거절 반복문 3개·admin 검사·정적 profile 검색, `build-youtube-collector-go_test.sh`의 `build-bin` 검사. `ap-rsync-files.txt`는 삭제·추가한 파일에 맞췄습니다. |
| 3 | F1, F2와 예외 계약 F3·F5–F9·F11. 10.4 결정은 [hololive-api 서비스 문서](../current/services/hololive-api.md)의 "Plane별 공유 자원"에 기록했습니다. |

### 계획과 달라진 점

- **F1 조회 형태**: 10.1은 dispatch 주기마다 한 번에 읽는 조회였으나, 구현은 메시지마다 기존 `alarm.Repository.GetMemberName`을 호출합니다. 조회는 claim된 행이 있을 때만 일어나고, 한 주기의 호출 수는 batch 안의 outbox 수와 group 수의 합 이하입니다. 각 조회는 `idx_members_channel_id`와 `idx_alarms_channel_member_latest`를 쓰며 새 SQL이 없습니다. PostgreSQL 호출 수가 문제가 되면 batch 조회로 바꿉니다.
- **F8 필수 의존성**: 계획은 renderer가 필수이면 기동 실패로 바꾸는 것이었으나, 명령 생성 경로가 오류를 돌려주지 않아 명령 실행 때 의존성 오류(`ensureDeps`)로 드러내게 했습니다. 운영 조립은 이 의존성을 항상 연결하므로 운영 동작은 같습니다.
- **telemetry 추가**: 예외 형식의 telemetry를 채우려고 `hololive_youtube_outbox_grouped_send_fallback_total{result}`(F6), `hololive_alarm_youtube_persisted_live_sessions_total`의 `result="holodex_error_continued"`(F7), `hololive_bot_image_text_fallback_total{command,reason}`(F8), `hololive_majorevent_link_get_fallback_total{result}`(F9)를 추가했습니다.
- **F9 분류**: `method not allowed` 오류 문자열 분기는 net/http client가 이를 오류가 아니라 405 상태로 돌려주므로 지웠습니다. 상태 분기는 그대로입니다.
- **F10 범위**: YouTube dispatch의 `DefaultConfig`·`NormalizeDispatcherConfig`와 grouper·send engine의 병렬도 기본값, dispatch maintenance의 `effective*` 함수까지 지웠습니다. dispatch 보존 로더의 `min(limit, 10000)` 상한과 저장소 계층의 `clampAlarmDispatchRetentionLimit`은 상한 계약이라 남겼으며 후속 검토 대상입니다.
- **검토 조건의 근거**: 예외 계약의 검토 조건은 현재 값 대신 metric 기준 조건(예: 90일 동안 0이면 제거)으로 적었습니다. 0단계 운영 근거는 아래 표에 따로 기록했습니다.
- **F1에 따른 정리**: shared YouTube formatter는 Valkey 조회 실패 경고에만 쓰던 logger가 필요 없어져 `NewMessageFormatter`의 logger 인자를 지웠습니다.

### 감사 수정과의 통합

두 작업이 같은 결함과 같은 파일을 고쳤으므로, 감사 수정을 먼저 반영하고 이 branch를 그 위에 다시 적용했습니다. 겹친 영역은 다음처럼 처리했습니다.

| 영역 | 남긴 쪽 | 내용 |
|---|---|---|
| 범용 delivery claim | 감사 | 슬롯 기반 claim(`processReady`, `outbox_claim_ready.sql`)을 남기고, 이 branch의 실행 직전 재claim(`RenewClaim`, `outbox_renew_claim.sql`)과 그 테스트를 지웠습니다. dispatcher 설정 검사(`validate`)는 이 branch 것입니다. |
| 달력 공유 조회, 멤버 캐시 종료 | 감사 | 이 branch의 구현과 테스트를 버렸습니다. 달력 finder의 nil 판정에만 `reflectutil.IsNil`을 다시 적용했습니다. |
| YouTube 표시명(F1) | 이 branch | 감사는 Valkey 원천을 유지하면서 조회 오류를 포맷 오류로 반환했습니다. 이 branch는 10.1 (c)대로 PostgreSQL 정본을 읽고, 조회 오류를 포맷 오류로 처리하는 점은 감사와 같습니다. |
| grouped 포맷 실패와 claim 해제 | 합침 | 감사의 배치 단위 claim 해제(모든 소비자가 확정 실패한 claim만 배치 끝에 해제)와 이 branch의 4단계 기록 wrapper를 합쳤습니다. wrapper가 배치 resolver에 해제를 요청하므로 해제 시점은 감사 구현과 같습니다. |
| 도움말·달력 이미지(F8) | 합침 | 감사의 결과 불명 처리(텍스트를 보내지 않고 오류로 올림)에 이 branch의 필수 의존성 검사와 `outcome_unknown` counter를 더했습니다. |
| YouTube 발송 timeout | 합침 | 감사의 `delivery_send_timeout_ms`와 executor attempt timeout 일치 검사를 이 branch의 `youtubeDispatchConfig`로 옮겼습니다. |
| `deliverysql` helper | 이 branch | 감사가 남긴 얇은 wrapper(`IsNilDB`, `PostgresPlaceholders`, `AppendDelivery{Int64,String}Args`)도 지우고, 호출부가 `reflectutil`과 `dbx`를 직접 씁니다. |
| `claim` 패키지 | 합침 | 감사는 decision cache를 `youtubedispatch`로 옮기고 나머지를 테스트 전용 파일로 바꿨고, 이 branch는 미사용 구현으로 보고 지웠습니다. 남은 테스트 전용 파일에는 테스트가 없어 패키지 전체를 지웠습니다. |
| `test-compose-services.sh` | 합침 | 감사가 지운 문자열 검사와 이 branch가 지운 퇴역 이름 거절 반복문을 모두 지웠습니다. |

### 실행한 검증 (감사 수정 위로 재구성한 뒤)

- 다섯 module(`hololive-shared`, `hololive-api`, `hololive-alarm-worker`, `hololive-youtube-collector`, `hololive-dbtest`)과 root module의 workspace build·vet가 통과했습니다. 네 업무 module의 `GOWORK=off` build·vet(`-mod=readonly`)와 다섯 module의 `GOWORK=off go mod tidy -diff`도 통과했습니다.
- 전체 테스트: 테스트가 있는 패키지 기준으로 shared 82개, alarm-worker 23개, api 64개, collector 13개와 dbtest, root module(`internal/workspace` 제외)이 통과했습니다. collector의 `youtubejs`는 worktree에 `node_modules`가 없으므로, 같은 lockfile을 쓰는 메인 checkout의 `node_modules`를 임시로 연결해 실행하고 연결을 지웠습니다.
- race: 감사 tip 대비 바뀐 42개 패키지(shared 12, api 14, worker 16)가 통과했습니다. api `privacylog`는 병렬 race 부하에서 내부 `go list`가 30초 제한을 넘었고, 단독 재실행에서 통과했습니다.
- 바뀐 42개 패키지의 golangci-lint(v2.13.2)와 NilAway(고정 모델 바이너리, `go vet -vettool`)가 0건입니다.
- `check-ap-rsync-manifest.sh`, `check-youtube-egress-lifecycle-ownership.sh`, `ci-notification-egress-gate.sh`, `test-compose-services.sh`, `test-three-runtime-topology.sh`, `build-youtube-collector-go_test.sh`, `ci-boundary-gate.sh`, `check-pgo-default.sh`, `check-production-go-workspace.sh`, `check-youtube-plane-budget.sh`가 통과했습니다.
- `./build-all.sh --build-only --no-bump --skip-local-ci`가 통과했습니다.

### 실행하지 못한 검증

- `./build-all.sh`의 local CI gate와 `grep-sensitive-logs.sh`는 worktree에서 실행할 수 없습니다. `go.work`의 `../iris-client-go`·`../shared-go`가 `.worktrees/` 밖으로 풀려 workspace 경계 검사가 거절합니다. local CI의 다른 단계는 위 개별 실행으로 대신했고, 민감 로그는 같은 세 패턴을 Hololive 네 module에 직접 적용해 0건이었습니다.
- 모노레포 전체 suite를 다시 도는 root `internal/workspace` 테스트는 module별 전체 테스트와 겹치므로 실행하지 않았습니다.
- `check-stack-retry-contract`와 `check-stack-worker-contract`는 실행하지 않았습니다. 앞의 검사가 보는 Hololive 파일(`reply_outbox_repository.go`, migrations)은 바뀌지 않았고 뒤의 검사는 Hololive를 보지 않으며, 둘 다 worktree가 아니라 메인 checkout을 읽습니다.
- 운영 조회, 실제 Iris/Kakao 전송, 배포 검증은 하지 않았습니다.

### 0단계 운영 근거 (2026-10-02, read-only)

`hololive-bot-ops`와 `stack-platform-ops`로 조회했습니다. Prometheus는 180일 보존이라 30일 집계를 썼고, Loki의 hololive-osaka 로그는 2026-09-18부터만 있어 로그 수치는 약 14일 범위입니다. DB 조회는 `holo-postgres`에서 `transaction_read_only=on`을 확인한 뒤 집계만 했습니다.

| 항목 | 근거 | 판단 |
|---|---|---|
| F3 membernews digest | `hololive_member_news_digest_result_total` 시계열이 30일 동안 없습니다. 이 metric은 첫 기록 때 생성되므로 digest 생성이 0회였습니다. fallback 로그도 0건입니다. | 예외 계약 유지. 검토 조건의 비율은 사용량이 생긴 뒤에 셉니다. |
| F5 공식 일정 fallback | primary는 30일 동안 `live_streams` 88회·`upcoming_streams` 61회 모두 `success`였습니다. 실행 metric과 fallback 로그는 0건입니다. metric의 `service` label은 scrape target label에 덮여 원래 값이 `exported_service=holodex`로 남습니다. | 계약 유지. 대시보드·쿼리는 `exported_service`를 써야 합니다. |
| F6 grouped 전송 fallback | 관련 로그 3종 모두 0건입니다. | 계약 유지. |
| F7 Holodex 실패 시 저장 세션 | 2026-09-30 장애(Holodex 요청 timeout 55회, DNS 조회 timeout 53회) 동안 경로가 108회 쓰였고 101회가 주기 실패였습니다. 그중 54회는 저장 세션 조회의 `context deadline exceeded`였습니다. 30일 metric은 `load_error` 233회, `empty` 9,215회입니다. | **결함**: Holodex 재시도(시도당 20초, 최대 4회)가 주기 예산(45초)을 모두 써서 fallback이 동작하지 못했습니다. Holodex 조회에 25초 한도를 두어 고쳤고, 한도 없이는 같은 오류로 실패하는 회귀 테스트를 추가했습니다. |
| F8 도움말·달력 이미지 | 관련 로그 4종 모두 0건입니다. | 계약 유지. |
| F11 live catchup 억제 marker | `hololive_youtube_outbox_live_catchup_suppression_total` 시계열이 30일 동안 없고(억제·오류 모두 0회) 로그도 0건입니다. | 계약 유지. |
| F1·F2(변경 전 경로) | 변경 전 로그 문구 3종 모두 0건입니다. | 변경으로 사라진 동작이 실제로 쓰이지 않았습니다. |
| 멤버 표시명 예외 | `hololive_alarm_member_name_fallback_channels` 0, `hololive_alarm_member_name_caller_fallback_total` 30일 0입니다. | 3.4절 제거 조건 중 지표 조건은 충족했습니다. 구독 채널 전부의 한국어 표시명 여부는 확인하지 않았습니다. |
| L11 | 이전 metric 이름의 30일 증가량이 모든 label에서 0입니다. | 이름 변경의 영향이 없습니다. |
| D1 notification delivery | `notification_delivery_outbox`가 비어 있어 조건 행 0건입니다. | 데이터 조건은 충족했지만 **남깁니다**. 오래된 행을 구분하는 표시가 없어, 이 검사가 "발송을 시도했는데 고정 요청이 없으면 재발송하지 않는다"는 불변식도 지킵니다. 지우면 결과 불명 행을 현재 설정으로 재발송할 수 있습니다. |
| D2 alarm dispatch | request 없이 발송이 시작된 unit은 `cancelled` 28, `quarantined` 1, `sent` 535건이고 활성 unit은 0건입니다. | D1과 같은 이유로 **남깁니다**. |
| D3 YouTube delivery | migration 249 이전 표시(`request_snapshot_allowed=false`) 67행이 `SENT` 62, `FAILED` 5(2026-05-17, revive 창 밖)이고 활성 행은 0건입니다. 이 사유로 실패한 행도 0건입니다. | 새 행은 표시가 `TRUE`라 검사에 걸리지 않습니다. **검사를 지웠습니다**. 열 삭제는 별도 migration과 승인이 필요합니다. |
| D4 live session origin | `legacy_unknown` 중 비종단 행이 `LIVE` 9(마지막 관측 2026-06), `UPCOMING` 59(마지막 관측 2026-08)건이고, `legacy_unreviewed` 지표는 Prometheus에 없습니다. | 조건 미충족입니다. 오래된 비종단 행을 정리하려면 DB 쓰기 승인이 필요합니다. |
| YouTube producer 퇴역 가드 | 중앙 env 파일 5개, AP a·b·d env, stack-secrets master 사본 전부 `YOUTUBE_PRODUCER_*` 키 0건(키 이름만 셈). 가드가 든 v7.1.1 API·worker가 2026-10-01부터 정상 기동 중이고 프로세스 env도 0건입니다. 남은 복구점은 `v7.0.1-pre-20260929T024252Z` 하나로, 키 정리(09-28) 이후 상태입니다. | 제거 조건 충족. **가드를 지웠습니다**. |

### 4단계 (2026-10-02)

- `deliverysql.IsNilDB`와 달력 finder의 nil 판정을 shared-go `reflectutil.IsNil`로 바꾸고 두 중복 구현을 지웠습니다.
- 썸네일과 달력 사진 body 읽기를 shared-go `httputil.ReadAllLimited`로 바꿨습니다. 썸네일의 빈 body 거절은 호출부에 남겼습니다.
- `persistContentFieldUpdates`의 행별 `tx.Exec`를 `dbx.ExecStatements` 묶음 전송으로 바꾸고, 여러 행을 한 transaction에서 반영하는 PostgreSQL 테스트를 추가했습니다.
- `deliverysql`의 `ExecDeliverySQL`·`SelectDeliverySQL`·`GetDeliverySQL`(미사용)·`PostgresPlaceholders`·`AppendDelivery{Int64,String}Args`가 `dbx`와 같은 구현이라 지우고 호출부를 `dbx`로 옮겼습니다. 빈 목록을 `FALSE`로 만드는 `DeliveryInClause`와 domain 타입 변환 helper는 남겼습니다.
- MetricsRecorder의 실패 기록 7곳이 하던 claim 해제(DB 상태 변경)를 SendEngine 기록 wrapper로 옮겼습니다. recorder는 로그·audit·결과 집계만 합니다. 쓰이지 않던 `ClaimManager.metrics` 필드와 `setMetricsRecorder`를 지웠고, recorder를 ClaimManager보다 먼저 만듭니다. `ClaimManager`와 `SendEngine` 사이의 `setExecutor` 순환은 남았습니다.

### 6단계 일부 (2026-10-02)

runtime binary별 `go list -deps`로 worker만 링크하는 shared 패키지 7개를 찾았습니다. 그중 4개를 `hololive-alarm-worker/internal`로 옮겼습니다.

| 패키지 | 결과 |
|---|---|
| `service/notification/alarmservice`, `internal/service/notification/alarmcache`, `service/alarm/dedup`, `service/alarm/queue` | `internal/service/{notification,alarm}/`로 옮겼습니다. 다른 shared 패키지가 import하지 않고, 이들은 공개 `pkg` 패키지만 씁니다. API 테스트 1곳은 같은 파일의 fake로 바꿨습니다. |
| `config/settings/alarmworker` | settings 하위 `internal/load`를 쓰므로 공통 설정 타입·파싱을 분리한 뒤 옮깁니다. |
| `service/youtube/outbox/format` | `internal/service/youtube/outbox/format`으로 옮겼습니다. 막던 것은 `sampledata`를 쓰는 golden 테스트뿐이었고, 이 테스트는 template 본문 복사본을 렌더링하고 있었습니다. 복사본은 실제 seed와 8건 모두 달랐습니다(묶음 형식·markdown 링크 등). 실제 seed 본문을 렌더링하는 golden으로 바꿔 template 패키지로 옮기고, 복사본 기반 테스트는 지웠습니다. 같은 변경에서 L8(worker의 `MessageFormatter` wrapper)을 지우고 SendEngine과 `dispatchrun`이 `format`을 직접 씁니다. payload 타입 분리는 이동의 선행 조건이 아니었으므로 5단계에 남깁니다. |
| `service/alarm/dispatchoutbox` | upcoming 후보 저장소가 API canonical writer와 같은 `alarm_upcoming_candidates` 행을 다루며, `sourceobservation` 테스트가 두 쪽을 함께 검증합니다. 저장소 계약을 분리한 뒤 옮깁니다. |

`SERVICE_OWNERSHIP.md`의 shared 잔류 목록과 현재 문서의 경로를 함께 고쳤습니다.

### collector 작업 인계 (2026-10-02)

메인 checkout에 커밋되지 않고 남아 있던 [YouTube 컬렉터 리팩토링](2026-10-02-youtube-collector-shared-refactoring.md)의 구현을 인계받았습니다. 감사 수정과 같은 내용인 변경 144개를 뺀 collector 고유 변경 65개를 이 branch에 적용하고, Go build·vet, collector 전체 테스트, shared 관측 패키지 테스트, YouTube.js 358개 테스트와 타입 검사, AP 매니페스트·경계 gate를 다시 통과시켰습니다. 메인 checkout의 미커밋 상태는 적용 전에 `backup/main-worktree-20261002`로 보존했습니다.

### 5–8단계와 4.5절 (2026-10-02)

| 항목 | 결과 |
|---|---|
| 5단계 payload 계약 | `pkg/contracts/youtubeoutbox`의 `Video`·`Short`·`Community`로 payload를 정의했습니다. producer는 domain 값을 필드마다 옮기고, renderer·구독 대상·finalize·live 억제·저장 직전 검증이 같은 타입으로 읽습니다. 바꾸기 전 producer 출력 7종과 바이트가 같음을 확인했고, JSON 형태를 계약 테스트로 고정했습니다. 같은 변경에서 묶음 renderer가 읽지 못한 항목을 빈 줄로 보내던 경로와 알 수 없는 kind를 영상 template으로 렌더링하던 경로를 포맷 실패로 바꿨습니다. |
| 5단계 job/lease 계약 | job 계약은 publish 저장소와 collector만 씁니다. 7단계에서 publish 패키지가 이를 소유하므로 별도 계약 패키지로 옮기지 않았습니다. |
| 6단계 `dispatchoutbox` | 옮기지 않았습니다. API와 collector production은 이 패키지를 링크하지 않아 이동으로 바뀌는 링크가 없습니다. 또 upcoming 후보 저장소의 `Stage`가 dispatch ledger 식별자(`buildLedgerRows`)를 쓰고, consume 테스트가 같은 후보 행을 API canonical 저장과 함께 검증합니다. 후보 계약만 떼어 내도 식별자 핵심부가 shared에 남습니다. |
| 6단계 설정 loader | 옮기지 않았습니다. `alarmworker`·`collector`·`apiplane` loader는 `settings/internal/load`의 함수 30여 개를 쓰므로, 옮기려면 사실상 `internal/load` 전체를 공개해야 합니다. collector 설계 문서가 이를 금지했고, 각 loader는 이미 자기 runtime만 링크합니다. |
| 7단계 publish/consume | `sourceobservation`(collector 발행)과 `sourceobservation/consume`(API 소비)으로 나눴습니다. 두 패키지는 서로 import하지 않습니다. 계획은 publish를 collector `internal`로, consume을 API `internal`로 옮기는 것이었지만, 소비 테스트 대부분이 실제 발행 경로로 데이터를 넣고 collector 통합 테스트가 실제 소비를 실행하므로 shared 안의 패키지 분리로 바꿨습니다. 테스트 함수 213개는 분리 전후 같고, 공통 DB fixture는 `observationtest`와 `testqueries` 자산으로 모았습니다. 역할 wrapper(`PublishRepository`, `ConsumeRepository`)와 읽지 않던 SQL 5개를 지웠습니다. |
| 2.7절 `providers` | DB 자원 생성을 `providers/dbresource`로 분리했습니다. collector production이 링크하는 shared 패키지는 51개(7단계 전)에서 36개로 줄었습니다. `providers`·`providers/modules`의 나머지는 API와 worker가 함께 써서 남겼습니다. |
| 8단계 | UNIT B 구독 조회를 (채널, 알림 종류)당 한 번으로 묶었고, 조회 횟수 테스트와 기존 실제 DB 수신 집합 테스트로 확인했습니다. 알람 변경 lock 대기를 `hololive_alarm_service_mutation_lock_wait_seconds`로 기록합니다. SQL batch(2.2)는 4단계에서 했습니다. API 내부 HTTP 호출 전환(2.6)은 병목 측정 근거가 없어 하지 않았습니다. |
| 4.5절 | collector `collecterr` 이름은 오류 분류 어휘로 900여 곳에서 쓰여 유지했고, 쓰이지 않던 registry alias를 지웠습니다. repo 수준 검사는 compose 렌더·보안 설정 검사를 남기고, 문자열만 확인하던 검사(collector 6개, batch 저장소 소유권 grep, QUIC UDP buffer 스크립트 문자열)를 지웠습니다. |
| 멤버 표시명 예외 | 운영 DB 읽기 전용 조회로 구독 채널 21개 모두 한국어 표시명을 가짐을 확인해 제거 조건을 충족했습니다. 중간 단계 두 개와 두 지표를 지우고, 남은 종단 문구에 `hololive_youtube_outbox_member_name_missing_total`을 붙였습니다. |

### 보류 항목 처리 (2026-10-02, 7.2.2)

- `request_snapshot_allowed`: migration 256이 열을 지웠습니다. 적용 전 운영 조회에서 `false` 행은 `SENT` 54, `FAILED` 5였고 끝나지 않은 행은 0건이었습니다.
- D4: 계약(`2026-09-30-live-reconciliation-lifecycle.md`)상 정보가 부족한 행을 상태 추정으로 끝내지 않고, 운영자 결정은 `record_youtube_live_review`의 `closed_unresolved` receipt로만 남깁니다. 비종단 `legacy_unknown` 68행 중 검토 가능한 `UPCOMING` 53행에 receipt를 시도해 27행을 닫았습니다(`operator_id=kapu`).
  - 26행은 원본 snapshot이 receipt 상한 256 KiB를 넘어 거절됐습니다. 원인은 아래 head 배열 증가입니다.
  - `UPCOMING` 6행은 가용성 기록이 없고, `LIVE` 9행(마지막 관측 2026-05-24~06-03)은 head·가용성이 없어 검토 함수 대상이 아닙니다. 둘 다 활성 수집 대상 밖이라 영상별 확인도 받지 않습니다.
  - 종단 `legacy_unknown`도 5,536행이 있어 `OriginLegacyUnknown` 처리 제거 조건은 여전히 충족되지 않습니다.
- 새로 확인한 결함: `youtube_live_reconciliation_heads.ignored_absence_scheduled_for`에 상한이 없습니다. LIVE 시작을 관측하지 못한 head(`last_live_positive_at` 없음)에 목록 부재 slot마다 시각이 하나씩 쌓이고 종료 뒤에도 남아 최대 17,006개, 1,000개 초과 head 1,661개, `ENDED` head에만 59 MB가 쌓였습니다. head를 갱신할 때마다 큰 배열을 다시 쓰고, 위 26행의 검토도 막습니다. reducer의 재생 방지 의미를 유지하면서 배열을 줄이는 설계가 필요합니다.

### 남은 작업

- `ignored_absence_scheduled_for` 상한 설계와 기존 배열 정리(운영 DB 쓰기 포함)
- 위 처리 뒤 남은 D4 41행: 26행은 배열 정리 뒤 검토를 다시 시도하고, 15행은 활성 수집 대상 밖 영상의 확인 경로나 검토 계약을 정해야 합니다.
- 위 표에서 근거를 들어 하지 않은 항목은 근거가 바뀌면 다시 검토합니다.

## 수행한 검증과 한계

### 초안의 임시 진단

진단은 checkout의 제품 파일을 변경하지 않는 Go overlay로 수행했습니다. 아래 FAIL은 기대 동작과 실제 동작의 차이를 드러낸 진단 assertion이며 기존 suite 전체 실패를 뜻하지 않습니다. 임시 파일은 저장소 회귀 테스트로 추가하지 않았으므로 1단계에서 정식 테스트로 옮겨야 합니다.

| 진단 테스트 | 재현 조건 | 관찰 결과 |
|---|---|---|
| `TestReviewEpochReconcilerStopsWithSubscription` | epoch authority의 Subscribe가 `valkey.ErrClosing` 반환, 재조회 주기 1초, `testing/synctest` | 구독 함수 반환 이후 가상 3초 동안 Current 호출 3회 증가 |
| `TestReviewCalendarFollowerCancellation` | 동일 월·연도, 선행 repository 조회를 channel로 대기, 후속 context 취소 | 후속 호출은 선행 조회를 풀어줄 때까지 미반환 |
| `TestReviewBatchWaitFitsClaimLease` | 실제 processBatch, 동일 방 10건, sender 9초, 현재 SQL의 PENDING lease 조건을 모의 repository에 적용 | claimed=10, sent=7, expired-before-send=3, elapsed=63초, lease=60초 |

첫 두 진단은 해당 member·handlers 패키지에 `-run '^TestReview' -count=1 -timeout=60s`, 세 번째 진단은 delivery 패키지에 `-run '^TestReviewBatchWaitFitsClaimLease$' -count=1 -timeout=30s`를 사용했습니다.

### 2026-10-02 재검증

초안과 별개로 작성한 overlay 진단 4개로 주장을 다시 확인했습니다. 제품 파일은 바꾸지 않았고, 실행 뒤 작업 트리에는 문서 변경만 남았습니다.

| 진단 테스트 | 조건 | 관찰 결과 |
|---|---|---|
| `TestAuditEpochWorkerOutlivesSubscription` | Subscribe가 즉시 `ErrClosing` 반환, 주기 1초, `synctest` | 구독 담당 함수 반환 뒤 가상 3초 동안 읽기 3회 |
| `TestAuditCalendarLeaderCancelFailsFollower` | 선행 호출자 취소, 후속 호출자 context는 유지 | 후속 호출이 `context canceled` 오류를 받음 |
| `TestAuditCalendarFollowerCancelWaitsForLeader` | 후속 호출자만 취소 | 선행 조회가 끝날 때까지 미반환 |
| `TestAuditCrossRoomSlotWaitExpiresLease` | 실제 processBatch, 50개 방에 1건씩, worker 4, 건당 3·5·9초 | 1.1절 표의 결과 |

함께 수행한 확인은 다음과 같습니다.

- 아래 기존 선별 race 테스트를 저장소 root에서 다시 실행했고 대상 6개 패키지가 모두 통과했습니다. 패키지 전체 테스트가 아니라 정규식으로 선택한 사례이며, `-race`가 모든 실행 경로의 경합 부재를 증명하지는 않습니다.
- runtime binary 3개의 `go list -deps` 링크 그래프로 5절의 소비자 열을 확인했습니다.
- 문서의 모든 상대 링크와 줄 번호를 대조해 어긋난 2개(`formatter.go`, `publisher.go`)를 고쳤습니다.
- fallback·legacy·alias 키워드 조사 결과를 [2026-09-26 감사 표](../../../docs/agent-workflows/evidence/2026-09-26-stack-audit/row-results.tsv)와 현재 계약 문서에 대조했습니다.
- 10절의 결정 분석은 코드 경로 추적, 결정 기록(`DEC-20260626`, `DEC-20260825`, `DEC-20260926-hololive-message-strings-startup-validation`), 감사 B5·B6 기록, 2026-09-26 운영 증거, 로컬 `observability-stack`(`40e2303`)의 대시보드 생성기·alert·rule test 검색으로 수행했습니다. 운영 시스템에는 접속하지 않았습니다.

```bash
go test -race \
  ./hololive/hololive-shared/pkg/service/member \
  ./hololive/hololive-shared/pkg/service/delivery \
  ./hololive/hololive-api/internal/planes/bot/internal/command/handlers \
  ./hololive/hololive-alarm-worker/internal/egress/youtubedispatch \
  ./hololive/hololive-shared/pkg/dbx \
  ./hololive/hololive-shared/pkg/service/youtube/outbox/deliverysql \
  -run '^(TestCacheEpoch_|TestCachedCelebrationCalendarFinder_|TestProcessItem_MarkSendingFenceSkipsSend$|TestProcessBatchPreservesOrderWithinRoom$|TestProcessOnce_RespectsMaxConcurrent$|TestCollectRoomsByChannel_PerformsTypedLookupsConcurrently$|Test.*PgxTx|Test.*DeliveryTx|Test.*Placeholders)' \
  -count=1 -timeout=90s
```

`-overlay`로 임시 진단 파일을 추가했으므로 진단 이름만 복사해 기존 checkout에서 실행하면 같은 검증이 되지 않습니다. 가상 시간 관찰값은 wall-clock 성능 측정값이 아닙니다.

### 한계

- 운영 log·metric·DB를 조회하지 않았으므로 fallback 발생 빈도, D1–D4 잔존 행 수, 운영 Iris 전송 지연은 모릅니다. 0단계가 이를 채웁니다.
- SQL batch·구독 조회·mutex 분할·API 내부 호출 전환의 처리량, p95, CPU, 메모리 개선량은 측정하지 않았습니다. 실제 Iris/Kakao를 통한 검증도 수행하지 않았습니다.
- fallback 조사는 키워드 기반이므로 이름에 fallback이 없는 default-on-error 경로는 누락됐을 수 있습니다. F11은 그렇게 빠졌다가 F1 분석 중 찾았습니다. 수집기 내부와 repo 수준 script는 일부만 확인했습니다.
- 2026-09-26 운영 증거는 6일 전 값입니다. F1 (c)의 조회 형태와 비용은 시제품으로 확인하지 않았고, Grafana UI에서 수동으로 만든 대시보드는 확인하지 않았습니다.
- 이 문서의 우선순위는 코드상 영향과 재현 결과에 기반하며 운영 빈도에 기반한 순위가 아닙니다.

이전 [2026-09-30 워커·수집기 리뷰](../history/architecture/2026-09-30-alarm-worker-collector-reliability-review.md)는 당시 snapshot입니다. 그 문서의 미수정 상태를 이번 기준 revision으로 그대로 가져오지 않으며, 이번 lease 대기 문제도 과거의 단일 attempt deadline 부재와 구분합니다.
