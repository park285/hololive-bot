# Hololive 2026-04-14 잔여 이슈 및 후속 과제

작성 기준:
- `hololive_20260414_static_review_and_refactor_plan.md`
- `hololive_20260414_p0_alarm_fix.patch`
- `hololive_20260414_issue_register_from_canonical_sources.md`
- `.worktrees/close-canonical-issues-20260414` 에서 완료한 실제 수정/검증 결과

## 1. 먼저 결론

현재 기준으로 **운영을 막는 즉시성 버그는 남아 있지 않습니다.**

이미 닫힌 항목:
- 5분 전 알람 누락 버그
- persistence saturation 시 request blocking / durable gap의 즉시성 문제
- channel subscriber lookup 의 과도한 `SMembers` fan-out
- cache warm 의 per-record write amplification
- dispatcher 의 immediate requeue hot loop
- member adapter 의 cancellation 전파 상실
- stale alarm runtime 문서

남은 것은 대부분 **구조 보강** 또는 **장기 운영 리스크 완화** 항목입니다.
즉, 지금 문서는 “아직 고장난 기능 목록”이 아니라, “이번 클로저 이후에도 남는 후속 아키텍처 과제”를 정리한 문서입니다.

## 2. 이번 클로저의 검증 기준

이번 수정 묶음은 아래 검증을 통과했습니다.

- `go test -count=1 ./hololive/hololive-shared/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-dispatcher-go/...`
- `go build ./hololive/hololive-shared/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-dispatcher-go/...`
- `go vet ./hololive/hololive-shared/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-dispatcher-go/...`
- `cd hololive/hololive-kakao-bot-go && make lint`
- `git diff --check`

따라서 아래 잔여 항목은 **현재 검증을 깨뜨리는 회귀**가 아니라, 추후 설계 수준에서 더 개선할 수 있는 부분들입니다.

## 3. 잔여 이슈

## 3.1 P1 — dispatcher retry가 아직 durable DLQ 모델은 아님

### 현재 상태
이번 수정으로 `hololive-dispatcher-go/internal/dispatch/dispatcher.go` 는 send 실패 시 즉시 `Requeue()` 하지 않고,
**bounded in-memory parking retry** 로 hot loop 를 끊습니다.

즉, 아래는 해결됐습니다.
- dequeue → send fail → immediate requeue → next loop 재실패 무한 반복
- 하위 전송 계층 장애 시 queue churn 증폭

### 아직 남은 점
하지만 현재 retry state 는 **프로세스 메모리 내부 상태**입니다.
따라서 아래는 아직 남습니다.

- 프로세스 restart 시 parked envelope 유실 가능
- durable DLQ 없음
- retry metadata 가 queue envelope 자체의 영속 필드가 아님
- backoff 가 fixed 값이며 attempt 증가에 따른 ramp/jitter 없음

### Owning seam
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher_test.go`

### 후속 권장
장기적으로는 아래 중 하나로 가는 것이 맞습니다.

- envelope 자체에 `attempt`, `retry_after`, `next_visible_at` 를 영속화
- delayed retry queue 또는 ZSET 도입
- max attempt 초과 시 DLQ / parking queue 로 이동
- retry budget 과 운영 메트릭을 명시적으로 추가

### 지금 당장 안 해도 되는 이유
운영상 가장 위험한 hot loop 는 이미 닫혔고, 현재는 fail-fast + bounded retry 로 tail risk 가 크게 줄었습니다.

## 3.2 P1 — alarm persistence 는 durable outbox 구조까지는 가지 않음

### 현재 상태
이번 수정으로 `stripedExecutor` 는 saturated 시 즉시 실패하고,
`alarm_persistence` 는 saturation 을 만나면 **inline fallback** 으로 저장을 수행합니다.

즉, 아래는 해결됐습니다.
- queue full 시 request thread blocking
- saturation 때문에 비동기 submit 만 실패하고 DB write 가 그냥 사라지는 즉시성 gap

### 아직 남은 점
하지만 현재 구조는 여전히 아래 한계를 가집니다.

- write path 가 durable outbox 기반은 아님
- process crash 중간 지점에서 cache 와 DB 의 완전한 재구성 모델이 없음
- saturation 시 inline fallback 이므로 latency spike 가능
- async persistence 의 observability / retry queue / dead-letter 흐름은 없음

### Owning seam
- `hololive/hololive-kakao-bot-go/internal/service/notification/striped_executor.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persist_ordering_test.go`

### 후속 권장
장기적으로는 아래 둘 중 하나가 필요합니다.

- DB write 를 authoritative path 로 두고 cache 는 파생 상태로 취급
- durable command/outbox 를 두고 cache rebuild 도 그 durable record 기준으로 수행

### 지금 당장 안 해도 되는 이유
이번 단계의 목표였던 “사용자 요청이 막히거나, saturation 시 조용히 유실되는 문제”는 닫혔습니다.

## 3.3 P2 — member adapter 의 오류 모델은 아직 과도기 상태

### 현재 상태
이번 수정으로 `member/adapter.go` 는 다음을 보장합니다.

- `context.WithoutCancel(...)` 제거
- `nil context` 는 `context.Background()` 로 fallback
- `WithContext(...)` 는 caller cancellation 을 그대로 보존
- `GetAllMembers()` 는 load 실패 시 빈 슬라이스 대신 `nil` 반환

즉, 아래는 해결됐습니다.
- cancellation 전파 상실
- 장애가 “정상 empty” 처럼 보이는 가장 나쁜 형태의 은폐

### 아직 남은 점
다만 인터페이스 자체는 여전히 legacy shape 입니다.

- `GetAllMembers()` 가 `([]*domain.Member, error)` 가 아님
- 호출자가 `nil` 반환만 보고 실패 원인을 직접 알 수는 없음
- `FindMembersByName/Alias` 도 explicit error-return 모델이 아님

### Owning seam
- `hololive/hololive-shared/pkg/service/member/adapter.go`
- 관련 caller 전반

### 후속 권장
장기적으로는 아래 방향이 좋습니다.

- `MemberDataProvider` 계열 인터페이스를 error-return 중심으로 재정의
- 검색/조회 경로에서 “empty result” 와 “lookup failure” 를 구분
- caller/UI 층에서 degraded mode 를 명시적으로 처리

### 지금 당장 안 해도 되는 이유
기존 public surface 를 크게 깨지 않으면서도, 가장 위험한 cancellation/error 은폐는 이미 줄였습니다.

## 3.4 P2 — alarm 문서는 현재 런타임 경로에 맞췄지만, 운영 카탈로그까지는 완성되지 않음

### 현재 상태
`hololive-kakao-bot-go/docs/local/alarm-notification.md` 는 현재 구조 기준으로 다시 썼습니다.

이제 문서는 최소한 아래 흐름을 맞게 설명합니다.
- `RuntimeScheduler`
- platform checkers
- `Notifier`
- shared `dedup`
- alarm queue publish
- `dispatcher-go` consume / render / send / retry

### 아직 남은 점
하지만 이번 문서는 **runtime path 정합성 복구**가 목적이었고,
세부 운영 카탈로그까지 전부 복원한 문서는 아닙니다.

예를 들어 아직 후속 정리가 가능한 부분:
- key/TTL 상세 표
- claim key / logical event / schedule transition key 카탈로그
- retry/parking 운영 절차
- cache warm / rebuild 운영 runbook

### Owning seam
- `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`
- 필요 시 runbook / ops docs 추가

## 3.5 P2 — target minute helper wrapper 들은 아직 compatibility surface 로 남아 있음

### 현재 상태
이번 수정으로 `TargetMinutePolicy` 가 추가되어,
checker/settings/dedup/alarm service 쪽 target-minute 의미론은 한 객체 중심으로 정리됐습니다.

### 아직 남은 점
다만 기존 함수 surface 는 compatibility 때문에 그대로 남아 있습니다.

예:
- `NormalizeTargetMinutes`
- `BuildRuntimeTargetMinutes`
- `ResolveConfiguredTargetMinutes`
- `ResolvePersistedTargetMinutes`

현재는 이 함수들이 내부적으로 policy object 를 사용하므로 split-brain 위험은 크게 줄었지만,
장기적으로는 아래가 가능합니다.

- 새 코드가 wrapper 함수 대신 policy object 를 직접 사용하도록 수렴
- helper 중 중복 의미가 사라진 것들을 정리
- policy object 기반 테스트를 더 늘리고 wrapper 테스트는 축소

### 지금 당장 안 해도 되는 이유
현재는 behavior 단일화가 이미 달성됐고, wrapper 는 compatibility adapter 역할만 합니다.

## 3.6 P2 — cache warm 최적화는 안전한 집계 단계까지만 수행됨

### 현재 상태
이번 수정으로 아래는 이미 좋아졌습니다.

- `compactUniqueStrings()` 의 O(n²) 제거
- key별 집계 후 `SAdd`
- 이름 캐시는 `HMSet` 우선, strict mock 경계에서는 `HSet` fallback
- checker 쪽 subscriber-room lookup 은 pipelined `SMEMBERS`

### 아직 남은 점
다만 cache warm 쪽은 **full cross-key pipeline** 까지 밀어붙이지는 않았습니다.
이유는 현재 cache client / strict mock / test harness 경계에서,
안전하게 round-trip 을 줄이는 선이 이번 수정의 최적점이었기 때문입니다.

따라서 향후 더 최적화하려면 아래가 필요합니다.
- cache abstraction 이 cross-key batched write 를 더 자연스럽게 허용하도록 정리
- strict mock 도 `B()/DoMulti()` 와 `HMSet` fallback 양쪽을 더 잘 모델링
- batch size / chunking / error-path metrics 를 운영 기준으로 튜닝

### Owning seam
- `hololive/hololive-shared/pkg/service/alarm/cache_warm.go`
- `hololive/hololive-shared/pkg/service/cache/*`
- 관련 strict mock/test helper

## 4. 권장 후속 순서

### 1단계 — 운영 안정성 추가 보강
- dispatcher durable retry / DLQ 설계
- persistence durable outbox 또는 authoritative DB write 결정

### 2단계 — 인터페이스 정리
- member adapter 계열 explicit error-return 모델 전환
- target minute wrapper surface 축소 여부 검토

### 3단계 — 문서/운영성 강화
- alarm key / TTL / retry runbook 문서화
- cache warm / rebuild 운영 절차 문서화

### 4단계 — 성능 심화 최적화
- cache warm full batching / chunk tuning
- retry backoff policy jitter / ramp / metrics 개선

## 5. 이번 시점의 최종 판단

현재 시점에서 남은 잔여 이슈는 **즉시 장애를 일으키는 미해결 버그가 아니라**,
아래 두 축으로 나뉩니다.

- **durability/operability 를 더 강하게 만들 구조 보강 과제**
- **compatibility surface 를 더 정리할 장기 리팩토링 과제**

즉, 지금은 “급한 불은 껐다”가 맞고,
이 문서는 그 다음에 어디를 손봐야 하는지의 우선순위를 남기기 위한 기록입니다.
