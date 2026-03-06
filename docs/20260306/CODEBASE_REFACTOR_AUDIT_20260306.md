# 코드베이스 리팩토링 감사 보고서

> 작성일: 2026-03-06  
> 범위: `hololive-bot` 전체 Go 코드베이스  
> 방식: 정적 분석 기반 점검 (실시간 프로파일링/부하테스트 미포함)

---

## 1. 요약

현재 코드베이스의 핵심 문제는 **조건문이 많다**는 표면 현상보다, 아래 4가지가 겹쳐진 구조입니다.

1. **fallback 정책이 여러 서비스에 인라인으로 분산**
2. **작은 요청이 전체 상태 재동기화로 번지는 쓰기 경로**
3. **배치 API가 있는데도 남아있는 N+1 / 순차 I/O**
4. **god-object 성격의 대형 파일과 중복 서비스**

가장 먼저 손봐야 할 축은 다음입니다.

- 알람 구독/조회 경로
- YouTube scheduler / outbox 경로
- 멤버 매칭 경로
- LLM 요약 fallback 경로

---

## 2. 정적 기준 주요 핫스팟

### 2-1. 대형 파일

| 파일 | LOC | 비고 |
|---|---:|---|
| `hololive/hololive-shared/pkg/service/youtube/scraper/client.go` | 1052 | transport/state/retry/parser 혼재 |
| `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go` | 1048 | claim/load/format/send/retry/aggregate 혼재 |
| `hololive/hololive-shared/pkg/service/holodex/service.go` | 960 | fallback/orchestration 과집중 |
| `hololive/hololive-shared/pkg/service/member/repository.go` | 790 | 수동 row scan 반복 |
| `hololive/hololive-shared/pkg/service/youtube/scheduler.go` | 788 | batch 저장과 per-item 후처리 혼재 |

### 2-2. 중복 서비스

| 파일 | LOC | 비고 |
|---|---:|---|
| `hololive/hololive-kakao-bot-go/internal/service/auth/service.go` | 572 | |
| `hololive/hololive-shared/pkg/service/auth/service.go` | 573 | |

- 두 파일의 텍스트 유사도는 약 **0.9929**.
- 보안/세션 로직이 사실상 이중 유지보수 상태입니다.

---

## 3. 확인된 구조 문제

## 3-1. 알람 쓰기 경로가 전체 재동기화로 번짐

### 근거
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go:187-271`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go:275-357`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_platform_mapping.go:14-94`

### 문제
- `AddAlarm` / `RemoveAlarm` / `ClearRoomAlarms` 이후 매번 `SyncPlatformMappings()` 호출
- `SyncPlatformMappings()`는 전체 `AlarmChannelRegistryKey`를 다시 읽음
- `replaceHashMappings()`는 `Del` 후 `HSet` 반복으로 전체 해시 재작성

### 영향
- 구독 1건 변경이 전체 구독 채널 수에 비례하는 I/O로 커짐
- `Del` 이후 재작성 사이의 짧은 공백 상태가 생길 수 있음

### 권장 방향
- 요청 경로에서 전체 sync 제거
- 증분 upsert/delete + 주기적 reconcile job으로 분리
- 전체 교체가 필요하면 temp key + rename 방식 사용

## 3-2. 알람 목록 조회가 N+1 + 원격 I/O 중 락 보유

### 근거
- `hololive/hololive-kakao-bot-go/internal/command/handler_alarm.go:181-199`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_cache.go:240-298`

### 문제
- `handleList()`가 알람별로 `GetMemberNameWithFallback()` + `GetNextStreamInfo()` 호출
- `GetNextStreamInfo()`가 `RLock`을 잡은 상태로 `HGetAll` 수행

### 영향
- 방 알람 수에 비례한 cache/DB 왕복
- 느린 Valkey 상황에서 reader 경합 악화

### 권장 방향
- `ListRoomAlarmsView(ctx, roomID)` 형태의 조합 조회 API 도입
- `HMGET`/pipeline 기반 batch read 추가
- 원격 I/O 구간에서는 mutex를 잡지 않도록 수정

## 3-3. 멤버 매칭이 매 요청마다 전체 스캔

### 근거
- `hololive/hololive-kakao-bot-go/internal/service/matcher/matcher.go:51-83`
- `hololive/hololive-kakao-bot-go/internal/service/matcher/matcher.go:126-163`
- `hololive/hololive-kakao-bot-go/internal/service/matcher/matcher.go:368-450`
- `hololive/hololive-shared/pkg/service/cache/member_cache.go:77-89`

### 문제
- exact/partial/alias 전략이 전부 선형 탐색
- 후보 해소 경로에서 `HGETALL` 전체 읽기 사용
- `context.Background()` fallback도 섞여 있음

### 영향
- 멤버 수 증가 시 CPU/I/O 동시 악화
- 취소 전파가 흐려짐

### 권장 방향
- immutable snapshot index 도입
  - exact name map
  - exact alias map
  - normalized token index
- `singleflight`로 인덱스 rebuild 중복 방지

## 3-4. fallback 정책이 여러 서비스에 중복 구현

### 근거
- `hololive/hololive-shared/pkg/service/youtube/service.go:219-291`
- `hololive/hololive-shared/pkg/service/youtube/service.go:523-668`
- `hololive/hololive-shared/pkg/service/holodex/scraper.go:82-131`
- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer.go:90-160`
- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_consensus.go:16-164`
- `hololive/hololive-llm-sched/internal/llm/openai_client.go:89-140`

### 문제
- scraper→API, Responses→Chat, reviewer→adjudicator 등 fallback 정책이 공통 계층 없이 각 서비스 내부에 박혀 있음

### 영향
- 정책 일관성 저하
- 테스트 포인트 증가
- 장애 시 동작 예측이 어려움

### 권장 방향
- `FallbackPolicy`, `FetchPlan`, `DigestPipeline` 형태의 공통 orchestration 계층 도입

---

## 4. 확인된 성능 / I/O 병목

## 4-1. YouTube scheduler의 확정 N+1

### 근거
- `hololive/hololive-shared/pkg/service/youtube/scheduler.go:166-249`
- `hololive/hololive-shared/pkg/service/youtube/scheduler.go:291-325`
- `hololive/hololive-shared/pkg/service/youtube/stats/stats_repository_interfaces.go:60-74`

### 문제
- `trackAllSubscribers()`가 채널별 `GetLatestStats()`
- milestone별 `HasAchievedMilestone()` 반복

### 권장 방향
- scheduler 계약에 batch read API 추가
- latest stats / achieved milestones 모두 batch prefetch로 전환

## 4-2. recent videos 수집이 순차 호출

### 근거
- `hololive/hololive-shared/pkg/service/youtube/scheduler.go:378-412`

### 문제
- `fetchRecentVideosRotation()`이 채널별 `GetRecentVideos()`를 순차 실행

### 권장 방향
- `errgroup.SetLimit()` 기반 bounded parallelism 적용
- 결과 cache write도 batch 또는 비동기 분리 검토

## 4-3. Queue drain과 outbox dispatch가 순차 I/O에 묶임

### 근거
- `hololive/hololive-shared/pkg/service/alarm/queue/consumer.go:63-99`
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go:245-329`
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go:400-451`
- `hololive/hololive-shared/pkg/service/delivery/outbox_repository.go:155-172`

### 문제
- queue consumer가 배치 확보를 위해 `BRPOP`를 반복 호출
- outbox dispatch가 room별 send를 순차 처리
- 실패 상태도 개별 업데이트 중심

### 권장 방향
- batched pop/Lua 검토
- `MarkFailedBatch` 활용
- room send를 제한 병렬화

## 4-4. Live 조회가 batch API를 활용하지 않음

### 근거
- `hololive/hololive-kakao-bot-go/internal/command/handler_live.go:123-143`
- `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go:71-107`
- `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go:247-359`

### 문제
- `!live` 경로가 멤버 수만큼 `GetLiveStatus()` 개별 호출
- 같은 클라이언트에 `GetLives()` / `GetChannels()` batch API가 이미 존재

### 권장 방향
- open API batch endpoint 기반으로 조회 경로 교체

## 4-5. HTML 파서 CPU 부담

### 근거
- `hololive/hololive-shared/pkg/service/youtube/scraper/parser.go:46-200`

### 문제
- anchor 탐색 + regex 다중 스캔 + JSON validity / scoring

### 권장 방향
- extractor / parser / scorer 분리
- bounded scan 또는 tokenizer 기반 접근 검토

---

## 5. 라이브러리 / 프리미티브 도입 판단

## 5-1. 우선 재사용할 것

- `errgroup.SetLimit()` / `x/sync/semaphore`
  - outbox send, YouTube/Holodex fallback fan-out
- `singleflight`
  - matcher index rebuild, next-stream cache miss
- `pgx.Batch` / `pgx.CopyFrom`
  - stats/milestone/delivery bulk write
- `SetNX`
  - auth session 발급 원자화

## 5-2. 선택적 도입

- `ttlcache/v3` 또는 `ristretto`
  - matcher hot cache에서만 선택적으로 검토
  - 현재는 새 의존성보다 snapshot index가 우선

---

## 6. 리팩토링 우선순위

### Phase 1 — 즉시 효과
1. 알람 구독 경로의 전체 재동기화 제거
2. 알람 목록 batch read API 도입
3. scheduler latest-stats / milestone batch화
4. outbox 상태 업데이트 batch화
5. auth session `Exists + Set` → `SetNX`

### Phase 2 — 구조 개선
6. matcher snapshot index화
7. outbox dispatcher 역할 분리
8. YouTube/Holodex fallback executor 공통화

### Phase 3 — 품질/운영 최적화
9. LLM summarizer budget mode
10. YouTube scraper parser 분리
11. auth core 단일화

---

## 7. 결론

현재 코드베이스는 기능 자체보다 **운영 비용과 변경 비용이 누적되는 구조적 부채**가 더 큽니다.

가장 먼저 손대야 할 3개는 아래입니다.

1. 알람 구독 경로 전체 sync 제거
2. YouTube scheduler / outbox batch화
3. matcher 인덱스화

상세 실행 체크리스트는 `docs/20260306/CODEBASE_REFACTOR_TODO_20260306.md`에 정리합니다.
