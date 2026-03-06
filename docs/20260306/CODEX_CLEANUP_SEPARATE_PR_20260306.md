# 별도 PR 리팩토링 항목

> 작성일: 2026-03-06
> 참조: `docs/20260306/CODEX_CLEANUP_REFACTOR_20260306.md`
> 사유: 변경 범위가 넓거나 외부 의존 확인이 필요하여 본 PR과 분리

---

## PR-A: YouTube Scraper client.go 분리 (1,051줄 -> 3-4개 파일)
202603067
### 현재 상태
- `shared/pkg/service/youtube/scraper/client.go` -- 1,051줄, 55개 메서드
- 역할 5개 혼재: HTTP 전송, 레이트 리미팅, 백오프 관리, 상태 저장, 프록시 관리

### 분리 안

| 새 파일 | 추출 대상 | 추정 줄 수 |
|---------|----------|----------|
| `scraper/state_manager.go` | cacheState (2 인스턴스) + channel별 상태 (community missing, video RSS backoff) | ~200 |
| `scraper/proxy_manager.go` | createHTTPClient, dialSOCKS5WithContextFallback, SetProxyEnabled, currentHTTPClient | ~200 |
| `scraper/client.go` (축소) | fetchPage, fetchPageOnce, applyScraperHeaders + 조합 | ~350 |

### 전제 조건
- RateLimiter, BackoffState는 이미 독립 struct (변경 불필요)
- parser.go (292줄) 분리는 이 PR에 포함 가능 (extractor + numberparser)

### 위험
- proxy 관련 로직이 Client 내부 상태(proxyClient, directClient)와 밀접
- 분리 시 Client struct 필드 접근 패턴 변경 필요
- 기존 테스트가 Client 전체를 대상으로 하므로 테스트 분할 필요

---

## PR-B: matcher context.Background() 제거 + context 전파

### 현재 상태
- `kakao-bot-go/internal/service/matcher/matcher.go:82,677,685`
- handler 체인에서 호출되는 함수에서 `if ctx == nil { ctx = context.Background() }` 패턴
- CLAUDE.md 규칙 위배: `MUST NOT use context.Background() in handlers`

### 변경 범위
- matcher.go 3곳: nil ctx fallback 제거
- 호출자 체인 전체 확인 필요:
  - `command/handler_alarm.go` -> matcher
  - `command/handler_live.go` -> matcher
  - `command/handler_*.go` -> matcher (전체 command handler 점검)
- 모든 호출자가 request context를 전달하는지 검증 필요

### 위험
- matcher는 background refresh (singleflight)도 수행 -- 이 경로는 Background() 사용이 정당
- handler 호출 경로 vs. background refresh 경로를 분리해야 함
- 잘못 제거하면 context cancel이 background refresh까지 전파

### 제안
- handler 경로: request context 전파 (필수)
- background refresh 경로: 별도 context.WithTimeout(context.Background(), ...) 유지 (정당)
- 두 경로를 명확히 분리하는 메서드 시그니처 변경 필요

---

## PR-C: Outbox Dispatcher 전체 분해 (P1-8 잔여)

### 현재 상태
- `shared/pkg/service/youtube/outbox/dispatcher.go` -- 1,167줄, 57개 메서드
- 본 PR의 Phase 3에서 MessageFormatter만 우선 추출
- 나머지 분리(RoomGrouper, DeliveryDispatcher, StatusManager)는 별도 PR

### 분리 안

| 새 파일 | 메서드 수 | 줄 수 | 의존성 |
|---------|---------|------|--------|
| `outbox/formatter.go` | 11 | ~320 | template.Renderer, cache |
| `outbox/grouper.go` | 5 | ~180 | cache (SMembers) |
| `outbox/delivery_dispatcher.go` | 3 | ~60 | iris.Client, MessageFormatter |
| `outbox/status.go` | 5 | ~100 | db |
| `outbox/dispatcher.go` (축소) | ~10 | ~250 | 폴링/조정만 |

### 전제 조건
- MessageFormatter 추출이 본 PR에서 완료된 후 진행
- 각 분리 단위마다 독립 테스트 작성

### 위험
- Dispatcher 내부 상태(cfg, mu, logger)를 분리된 struct 간 공유하는 방식 설계 필요
- 기존 테스트가 Dispatcher 전체 메서드를 호출하므로 테스트 재구성 필요

---

## PR-D: envconfig vs envutil 통일 (S3, CODEX_CLEANUP_TODO 잔여)

### 현재 상태
- `shared/pkg/config/admin_api.go`, `llm_scheduler.go`: envconfig.Process() 사용
- `shared/pkg/config/config.go`: envutil 기반

### 변경 범위
- envconfig struct 제거
- builder 함수 재작성 (envutil 개별 호출)
- 테스트 전면 수정

### 위험
- Config 구조체 전면 리팩토링
- 3개 바이너리(bot, llm-sched, stream-ingester)의 config 로딩 경로 모두 영향

---

## PR-E: Holodex Service 분해 (966줄, 21메서드)

### 현재 상태
- `shared/pkg/service/holodex/service.go` -- 966줄
- API 요청 + 캐싱 + 폴백(스크래핑) + 필터링 + 재시도 혼재

### 분리 안
- HolodexCacheManager: 캐싱 전담
- HolodexScraperFallback: 폴백 스크래핑 전담
- service.go (축소): API 조합/조정만

### 전제 조건
- fallback executor (fallback.Policy + FetchPlan) 표준화 완료 후 진행 권장

---

## 실행 우선순위

```
PR-A (Scraper 분리)       -- 독립적, 언제든 진행 가능
PR-B (matcher context)    -- 독립적, handler 체인 점검 필요
PR-C (Dispatcher 전체)    -- Phase 3 MessageFormatter 추출 후
PR-D (envconfig 통일)     -- 독립적, 영향 범위 넓음
PR-E (Holodex 분해)       -- Phase 3 fallback 표준화 후
```
