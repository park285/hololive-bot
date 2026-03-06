# 코드베이스 리팩토링 TODO

> 작성일: 2026-03-06  
> 참조: `docs/20260306/CODEBASE_REFACTOR_AUDIT_20260306.md`

---

## 상태 표기

- [ ] 미착수
- [~] 진행중
- [x] 완료

---

## P0 — 즉시 착수 권장

### 1. 알람 구독 경로의 전체 플랫폼 재동기화 제거
- [x] `SyncPlatformMappings()`를 요청 경로에서 제거
- [x] add/remove/clear 시 증분 업데이트 API 설계
- [x] 전체 rebuild는 reconcile/background job으로 분리
- 대상 파일
  - `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go`
  - `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_platform_mapping.go`
- 완료 기준
  - 구독 1건 변경 시 전체 해시 재작성 없음
  - add/remove/clear 경로의 Valkey round-trip 감소

### 2. 알람 목록 batch read API 도입
- [x] `ListRoomAlarmsView(ctx, roomID)` 또는 동등 API 추가
- [x] member name / next stream 정보를 batch read로 묶기
- [x] `GetNextStreamInfo()` 원격 I/O 구간 mutex 제거
- 대상 파일
  - `hololive/hololive-kakao-bot-go/internal/command/handler_alarm.go`
  - `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_cache.go`
  - `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go`
- 완료 기준
  - 알람 목록 조회 시 per-alarm remote call 제거

### 3. YouTube scheduler latest stats / milestone 배치화
- [x] scheduler 계약에 batch latest-stats 조회 추가
- [x] milestone achieved 여부도 batch prefetch 경로 설계
- [x] `fetchRecentVideosRotation()` bounded parallelism 적용
- 대상 파일
  - `hololive/hololive-shared/pkg/service/youtube/scheduler.go`
  - `hololive/hololive-shared/pkg/service/youtube/stats/stats_repository_interfaces.go`
  - `hololive/hololive-shared/pkg/service/youtube/stats/*`
- 완료 기준
  - `GetLatestStats()` per-channel 루프 제거
  - 순차 recent-videos fetch 제거

### 4. Outbox 상태 업데이트와 전송 경로 배치화
- [x] `MarkFailedBatch` 활용 또는 동등 batch failure update 도입
- [x] outbox aggregate update를 batch 또는 dedup + bulk 경로로 전환
- [x] room send 경로에 bounded parallelism 적용
- 대상 파일
  - `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go`
  - `hololive/hololive-shared/pkg/service/delivery/outbox_repository.go`
  - `hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`
- 완료 기준
  - delivery별 개별 실패 update 최소화
  - 순차 send 병목 완화

### 5. Auth 세션 발급 원자화 + 중복 서비스 정리 시작
- [x] `Exists + Set`를 `SetNX` 기반 발급으로 변경
- [x] shared/bot auth service 공통 core 추출 범위 정의
- 대상 파일
  - `hololive/hololive-kakao-bot-go/internal/service/auth/service.go`
  - `hololive/hololive-shared/pkg/service/auth/service.go`
- 완료 기준
  - 세션 발급 경로 원자화
  - auth 중복 제거 설계 문서 초안 준비 (`docs/20260306/AUTH_CORE_UNIFICATION_DRAFT_20260306.md`)

---

## P1 — 구조 개선

### 6. MemberMatcher snapshot index화
- [x] exact/alias/token index 설계
- [x] `GetAllMembers()` 전체 읽기 의존 제거
- [x] `singleflight` 기반 인덱스 refresh 추가
- 대상 파일
  - `hololive/hololive-kakao-bot-go/internal/service/matcher/matcher.go`
  - `hololive/hololive-shared/pkg/service/cache/member_cache.go`

### 7. `!live` 조회를 CHZZK batch API 기반으로 전환
- [x] `GetLives()` / `GetChannels()` 사용 경로 설계
- [x] 기존 per-member `GetLiveStatus()` 루프 제거
- 대상 파일
  - `hololive/hololive-kakao-bot-go/internal/command/handler_live.go`
  - `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go`

### 8. Outbox dispatcher 역할 분리
- [x] claim/load
- [x] format
- [x] send
- [x] retry/aggregate
- 대상 파일
  - `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go`

### 9. YouTube/Holodex fallback executor 공통화
- [x] `FallbackPolicy` / `FetchPlan` 초안 작성
- [x] `GetUpcomingStreams`, `GetChannelStatistics`, Holodex stream fallback 통합
- 대상 파일
  - `hololive/hololive-shared/pkg/service/youtube/service.go`
  - `hololive/hololive-shared/pkg/service/holodex/service.go`
  - `hololive/hololive-shared/pkg/service/holodex/scraper.go`

---

## P2 — 품질 / 운영 최적화

### 10. LLM summarizer budget mode
- [x] cache hit 시 search skip 정책 정의
- [x] reviewer/adjudicator/final review 조건부 실행
- [x] fallback 결과 타입 명시 (`primary/fallback/empty`)
- 대상 파일
  - `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/*.go`
  - `hololive/hololive-llm-sched/internal/llm/openai_client.go`

### 11. YouTube scraper parser 분리
- [x] extractor / parser / scorer 계층 분리
- [x] bounded scan 또는 tokenizer 접근 검토
- 대상 파일
  - `hololive/hololive-shared/pkg/service/youtube/scraper/parser.go`
  - `hololive/hololive-shared/pkg/service/youtube/scraper/client.go`
  - `hololive/hololive-shared/pkg/service/youtube/scraper/videos.go`

### 12. 관측성 보강
- [x] 알람 쓰기/읽기 latency metric
- [x] queue drain size / send throughput metric
- [x] fallback hit ratio metric
- 대상 파일
  - 알람/YouTube/LLM 관련 서비스 전반

---

## 권장 실행 순서

1. P0-1 ~ P0-4
2. P0-5 + P1-6
3. P1-7 ~ P1-9
4. P2-10 ~ P2-12

---

## 메모

- 신규 라이브러리 도입보다 **기존 프리미티브 재사용**이 우선
  - `singleflight`
  - `errgroup.SetLimit()`
  - `pgx.Batch` / `CopyFrom`
  - `SetNX`
- 새 대형 캐시 라이브러리는 snapshot index 적용 후 재평가
