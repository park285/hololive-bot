# Phase 11: 코드베이스 품질 강화 계획

> 작성일: 2026-03-05
> 범위: 보안, 중복 코드 제거, I/O 성능, 테스트 커버리지, 라이브러리 검증
> 전제: Phase 0~10 완료 상태. Phase 11은 독립 실행 가능
> 전략: 기존과 동일 (Phase별 독립 PR, 기능/구조 혼합 금지)

---

## 분석 근거 요약

| 영역 | 핵심 발견 | 분석 방법 |
|------|----------|----------|
| 보안 | DSN 평문 노출, WebSocket origin 검증 미흡, 에러 메시지 클라이언트 노출 | 7개 병렬 에이전트 전수 조사 |
| 중복 코드 | Subscription DTO 4곳, Auth Error 2곳, Consensus 타입 2곳, HTTP 클라이언트 27+곳 | Grep 기반 패턴 매칭 |
| I/O 성능 | YouTube Stats 개별 INSERT (20/min), Outbox 배치 부재, 슬라이스 미프리할당 | 코드 흐름 추적 분석 |
| 테스트 | 알람 체커 0% 커버리지, Auth 서비스 6/7 미테스트, Notification 핵심 미테스트 | test 파일 매핑 |
| 라이브러리 | 교체 필요 없음 확인 (Gin/pgx/valkey-go/sonic/slog 전부 최적) | 의존성 트리 분석 |

---

## P11-A: 보안 강화

> Risk: MED | 소요: 3-5일 | 의존: 없음

### P11-A1. PostgreSQL DSN 비밀번호 마스킹

- **파일**: `hololive-shared/internal/dbx/config.go:37-50`
- **문제**: `fmt.Sprintf("password=%s", c.Password)` — 에러/로그 시 평문 노출 가능
- **조치**: pgx `ConnConfig` 직접 구성 또는 DSN 반환 시 비밀번호 마스킹 래퍼 추가
- **검증**: DSN 문자열이 로그/에러에 절대 포함되지 않는지 grep 확인

### P11-A2. WebSocket Origin 검증 강화

- **파일**: `hololive-shared/pkg/server/websocket.go:18-36`
- **문제**: `allowedOrigins` 하드코딩 + Host 헤더 기반 fallback (스푸핑 취약)
- **조치**:
  - 환경변수 `WEBSOCKET_ALLOWED_ORIGINS` 기반 origin 로드
  - Host 헤더 기반 검증 제거
- **검증**: 미허용 origin 요청 시 403 반환 테스트

### P11-A3. 에러 메시지 클라이언트 노출 방지

- **파일**: `kakao-bot/internal/server/api_room.go:36,73,109`
- **문제**: `c.JSON(400, gin.H{"error": err.Error()})` — 내부 에러 상세 노출
- **조치**: 정의된 에러 코드 맵 반환, 상세는 서버 로그만
- **검증**: 모든 API 핸들러에서 `err.Error()` 직접 반환 패턴 0건 확인

---

## P11-B: 중복 코드 제거

> Risk: LOW-MED | 소요: 5-7일 | 의존: 없음

### P11-B1. Subscription DTO 통합 (CRITICAL — 4곳 중복)

- **중복 위치**:
  - `llm-sched/internal/app/api_internal_majorevent.go:15-22`
  - `llm-sched/internal/app/api_internal_membernews.go` (동일 구조)
  - `kakao-bot/internal/service/majoreventclient/client.go:34-41`
  - `kakao-bot/internal/service/membernewsclient/client.go:35-42`
- **DTO**: `SubscribeRequest{RoomID, RoomName}`, `SubscriptionStatusResponse{Subscribed}`
- **조치**: `hololive-shared/pkg/contracts/subscription/types.go` 추출, 4곳에서 import
- **검증**: 4개 파일에서 로컬 DTO 정의 0건

### P11-B2. Auth Error 타입 통합 (HIGH — 2곳 완전 중복)

- **중복 위치**:
  - `hololive-shared/pkg/service/auth/errors.go:5-45`
  - `kakao-bot/internal/service/auth/errors.go:5-45`
- **내용**: ErrorCode, Error struct, Error(), Unwrap(), newError() 100% 동일
- **조치**: kakao-bot에서 hololive-shared의 타입 import
- **검증**: kakao-bot에 로컬 auth error 타입 정의 0건

### P11-B3. Consensus Review 타입 통합 (HIGH — 2곳 불일치)

- **중복 위치**:
  - `llm-sched/internal/service/majorevent/summarizer/summarizer_consensus.go:14-29`
  - `llm-sched/internal/service/membernews/summarizer/summarizer_consensus.go:16-28`
- **문제**: 동일 로직 + 다른 네이밍 (lowercase vs exported)
- **조치**: `llm-sched/internal/service/consensus/types.go` 추출
- **검증**: 양쪽 summarizer에서 로컬 타입 정의 0건

### P11-B4. SearchResult DTO 통합 (MEDIUM — 2곳 중복)

- **중복 위치**:
  - `llm-sched/internal/service/majorevent/summarizer/searcher.go:16-22`
  - `llm-sched/internal/service/membernews/summarizer/summarizer.go:26-31`
- **조치**: `llm-sched/internal/model/search.go` 추출
- **검증**: 양쪽 summarizer에서 로컬 SearchResult 정의 0건

### P11-B5. HTTP Client 생성 유틸 추출 (MEDIUM — 27+ 파일 분산)

- **문제**: 각 서비스가 `&http.Client{Timeout: X}` 독립 생성, 타임아웃 불일치 (10s~60s)
- **조치**: `shared-go/pkg/httputil/client.go` — `NewClient(timeout)`, `DefaultClient()`
- **검증**: `&http.Client{` 패턴 직접 사용 0건 (httputil 경유만)

### P11-B6. HTTP 에러 응답 처리 추출 (MEDIUM — 6+ 파일 동일 패턴)

- **패턴**: status check → LimitReader → fmt.Errorf → json.Decode (12줄 반복)
- **조치**: `shared-go/pkg/httputil/response.go` — `CheckStatus()`, `DecodeJSON()`
- **검증**: 동일 패턴 인라인 구현 0건

### P11-B7. 테스트 로거 유틸 통합 (LOW — 40+ 파일 재구현)

- **문제**: `testLogger()`, `newTestLogger()` 로컬 구현 반복
- **기존 유틸**: `hololive-shared/internal/logging/logging.go:63-69` — `NewTestLogger()`
- **조치**: 로컬 구현 → 기존 유틸 import 교체
- **검증**: `func testLogger()` / `func newTestLogger()` 로컬 정의 0건

### P11-B8. miniredis 테스트 헬퍼 추출 (LOW — 4곳 중복)

- **중복 위치**:
  - `kakao-bot/internal/service/notification/alarm_test_helpers_test.go:49-77`
  - `hololive-shared/pkg/service/cache/service_test.go:22-30`
  - `hololive-shared/pkg/providers/ingestion_lock_test.go:20-36`
  - `hololive-shared/pkg/server/ratelimit_middleware_test.go:21`
- **조치**: `hololive-shared/internal/testutil/cache.go` — `NewTestCacheService()`
- **검증**: miniredis boilerplate 인라인 구현 0건

### P11-B9. Subscription Repository 인터페이스 통합 (LOW)

- **중복 위치**:
  - `llm-sched/internal/service/majorevent/repository.go:31-69`
  - `llm-sched/internal/service/membernews/repository.go:60-111`
- **문제**: Subscribe/IsSubscribed/GetSubscribedRooms 동일 시그니처
- **조치**: 공통 인터페이스 추출 또는 generic repository
- **검증**: 양쪽 repository에서 동일 메서드 시그니처 0건

---

## P11-C: I/O 성능 개선

> Risk: LOW-MED | 소요: 3-5일 | 의존: 없음

### P11-C1. YouTube Stats 배치 INSERT

- **파일**: `hololive-shared/pkg/service/youtube/stats/stats_repository_write.go:12-52`
- **문제**: SaveStats 단건 INSERT, 100+ 채널 × 5분 간격 = 분당 20 개별 쿼리
- **조치**: `SaveStatsBatch(ctx, stats []*TimestampedStats)` multi-value INSERT
- **효과**: 라운드트립 95% 감소 (20회 → 1-2회/분)
- **검증**: 기존 단건 SaveStats 호출부를 배치 호출로 전환, 기존 테스트 + 배치 테스트

### P11-C2. Outbox 배치 메서드 추가

- **파일**: `hololive-shared/pkg/service/delivery/outbox_repository.go:32-104`
- **문제**: Enqueue/MarkSent/MarkFailed 단건 연산
- **조치**: `EnqueueBatch()`, `MarkSentBatch()`, `MarkFailedBatch()` 추가
- **효과**: 대량 알림 발송 시 5x 처리량 향상
- **검증**: 배치 메서드 table-driven 테스트

### P11-C3. 슬라이스 프리할당

- **파일**:
  - `hololive-shared/pkg/service/member/repository.go:189-211` (GetAllChannelIDs)
  - `hololive-shared/pkg/service/alarm/repository.go:183-195` (GetAllChannelIDs)
  - `kakao-bot/internal/service/alarm/checker/youtube_checker.go:95` (notifications)
- **조치**: `make([]T, 0, estimatedSize)` 프리할당
- **효과**: 스타트업 5-10% 시간 단축, 알람 체크 주기당 메모리 할당 감소
- **검증**: 기존 테스트 통과 + 벤치마크 비교 (선택)

### P11-C4. GORM fetch-then-update 제거

- **파일**: `hololive-shared/pkg/service/member/repository.go:429-471,474-559`
- **문제**: 전체 행 조회 후 단일 컬럼 업데이트 (AddAlias, RemoveAlias, SetGraduation, UpdateChannelID)
- **조치**: `db.Model(&Model{}).Where("id=?", id).Update("col", val)` 직접 사용
- **검증**: 기존 테스트 통과

---

## P11-D: 테스트 커버리지 확대

> Risk: LOW | 소요: 7-10일 | 의존: 없음

### P11-D1. 알람 체커 테스트 (CRITICAL — 0% 커버리지)

- **대상 파일**:
  - `kakao-bot/internal/service/alarm/checker/youtube_checker.go`
  - `kakao-bot/internal/service/alarm/checker/chzzk_checker.go`
  - `kakao-bot/internal/service/alarm/checker/twitch_checker.go`
  - `kakao-bot/internal/service/alarm/checker/notifier.go`
  - `kakao-bot/internal/service/alarm/checker/common.go`
- **요구사항**: table-driven 테스트, 케이스 포함:
  - 스트림 발견/미발견
  - API 에러 (타임아웃, 5xx)
  - Dedup 로직 (중복 알림 방지)
  - 멀티 플랫폼 통합
- **검증**: `go test -cover` 50%+ 달성

### P11-D2. Notification 서비스 테스트 (HIGH)

- **대상 파일**:
  - `kakao-bot/internal/service/notification/alarm_service.go`
  - `kakao-bot/internal/service/notification/alarm_persistence.go`
  - `kakao-bot/internal/service/notification/alarm_cache.go`
- **요구사항**: mock cache 기반 add/remove/clear 연산 테스트
- **검증**: `go test -cover` 60%+ 달성

### P11-D3. Auth 서비스 테스트 (HIGH)

- **대상 파일**:
  - `kakao-bot/internal/service/auth/service.go`
  - `kakao-bot/internal/service/auth/validation.go`
  - `kakao-bot/internal/service/auth/tokens.go`
  - `kakao-bot/internal/service/auth/db.go`
- **요구사항**: table-driven token validation + 만료 edge case + 리프레시 로직
- **검증**: `go test -cover` 70%+ 달성

### P11-D4. Stream Ingester 런타임 테스트 (MEDIUM)

- **대상 파일**:
  - `stream-ingester/internal/app/stream_ingester_runtime_builder.go`
  - `stream-ingester/internal/app/stream_ingester_poller_registrations.go`
- **요구사항**: 전제조건 실패 + 등록 타이밍 테스트
- **검증**: `go test -cover` 40%+ 달성

### P11-D5. YouTube Scraper playlists.go 테스트 (MEDIUM)

- **대상 파일**: `hololive-shared/pkg/service/youtube/scraper/playlists.go`
- **요구사항**: 정상/비정상 JSON, 빈 응답, 페이지네이션 edge case (5-10 케이스)
- **검증**: `go test -cover` 60%+ 달성

---

## P11-E: 모듈 구조 보강 (Phase 0~10 후속)

> Risk: MED | 소요: 5-7일 | 의존: 없음

### P11-E1. `pkg/config/config.go` 도메인별 분할

- **파일**: `hololive-shared/pkg/config/config.go` (800+ LOC, 30+ struct)
- **조치**: `config_db.go`, `config_cache.go`, `config_iris.go`, `config_notification.go` 등 분할
- **규칙**: struct 정의 + validation 함수를 같은 파일에 배치
- **검증**: `go build + go test` 통과, 외부 import 경로 변경 없음

### P11-E2. `pkg/server/` 서브패키지 분리

- **파일**: `hololive-shared/pkg/server/` (28파일 혼합)
- **조치**:
  - `server/middleware/` → auth.go, security.go, ratelimit_middleware.go, logger.go
  - `server/settings/` → settings_handler.go, settings_result.go, settings_applier_local.go
  - `server/` root → h2c.go, response.go, websocket.go (인프라 코어)
- **검증**: import path 갱신 후 전체 모듈 build + test

### P11-E3. K8s Secret 서비스별 분리

- **파일**: `k8s/base/secret-app.yaml` (단일 Secret, 모든 서비스가 모든 시크릿 접근)
- **조치**:
  - `k8s/base/secret-bot.yaml` (IRIS_*, API_SECRET_KEY)
  - `k8s/base/secret-dispatcher.yaml` (IRIS_*, ALARM_*)
  - `k8s/base/secret-llm.yaml` (OPENAI_*, EXA_*)
  - `k8s/base/secret-common.yaml` (POSTGRES_*, HOLODEX_*, YOUTUBE_*)
- **검증**: `kubectl apply --dry-run=client -f k8s/base/` 통과

### P11-E4. K8s ConfigMap 서비스별 분리

- **파일**: `k8s/base/configmap-app.yaml` (3개 ConfigMap, 일부 공유)
- **조치**: 서비스별 ConfigMap 세분화 (공유 DB/Cache 설정은 common으로 유지)
- **검증**: `kubectl apply --dry-run=client -f k8s/base/` 통과

---

## 라이브러리 검증 결과 (교체 불필요 확인)

| 카테고리 | 현재 | 버전 | 판정 | 근거 |
|----------|------|------|------|------|
| HTTP Framework | Gin | v1.12.0 | **유지** | 미들웨어 생태계 완성, OTel 통합 |
| DB Driver | pgx v5 | v5.8.0 | **유지** | Postgres 최적, lib/pq 대비 10-15% 빠름 |
| Valkey Client | valkey-go | v1.0.72 | **유지** | RESP3 네이티브, go-redis 대비 5-10% 빠름 |
| JSON | sonic + goccy | v1.15/v0.10.5 | **유지** | SIMD 5-10x 성능, graceful fallback |
| Logging | slog + tint | stdlib/v1.1.3 | **유지** | Go 1.26 stdlib, Fluent Bit SSOT |
| Config | envconfig | v1.4.0 | **유지** | 경량, ENV 전용 |
| Testing | testify | v1.11.1 | **유지** | 1120+ 사용 |
| Observability | OTel + Prometheus | v1.41/v1.23.2 | **유지** | 업계 표준 |
| Resilience | gobreaker + ants | v2.4/v2.11.5 | **유지** | 안정적 |

---

## 긍정적 발견 (변경 불필요)

- 모듈 간 순환 의존성 0건
- 4개 앱 모듈 간 HTTP contract 경계 완벽 유지
- 의존성 버전 충돌 0건 (Gin, pgx, Valkey 전 모듈 동일 버전)
- SQL 전량 매개변수화 (SQL injection 없음)
- API 인증 — `subtle.ConstantTimeCompare` 사용
- CORS — 프로덕션 와일드카드 필터링
- 보안 헤더 — nosniff, DENY, HSTS, CSP 완비
- Valkey — SCAN 사용 (KEYS 미사용)
- errgroup + SetLimit 패턴으로 goroutine 제어
- context.Background() — main/signal handler에서만 사용, 핸들러 내 사용 없음
