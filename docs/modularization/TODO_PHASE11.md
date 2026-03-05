# Phase 11: 코드베이스 품질 강화 - 상세 TODO

> 생성: 2026-03-05
> 기반 문서: `docs/modularization/PLAN_PHASE11.md`
> 규칙: 각 task는 독립 PR 단위. `[x]` 완료, `[ ]` 미착수, `[~]` 진행중
> 스냅샷 기준: 2026-03-05 코드/파일 구조 점검 결과 반영 (라운드4 검증 스냅샷 포함, CI 재실행 체크박스는 별도 추적)


---

## P11-A: 보안 강화

> Risk: MED | 소요: 3-5일 | 의존: 없음

### P11-A1. PostgreSQL DSN 비밀번호 마스킹 `[x완료]`

- [x] `hololive-shared/internal/dbx/config.go` DSN()/SafeDSN() 경로 확인
- [x] pgx 파싱 단계 마스킹 DSN 사용 + `ConnConfig.Password` 재주입
  - `pgxpool.ParseConfig()` 대신 `pgxpool.NewWithConfig()` 사용
  - 또는 DSN 반환 시 비밀번호를 `***` 로 마스킹하는 `SafeDSN()` 추가
- [x] 에러 래핑/로깅 경로 DSN 노출 grep 확인
  - `grep -rn 'DSN\|dsn\|password=' hololive-shared/internal/dbx/`
- [ ] `go vet + golangci-lint + go test` (hololive-shared)

### P11-A2. WebSocket Origin 검증 강화 `[x완료]`

- [x] `hololive-shared/pkg/server/websocket.go` 확인
- [x] `WEBSOCKET_ALLOWED_ORIGINS`(콤마 구분) 로드로 전환
- [x] Host 헤더 fallback 검증 제거
- [x] 미허용 origin 403 반환 테스트 추가 (`TestWSUpgrader_DisallowedOriginReturns403`)
- [ ] `go vet + golangci-lint + go test` (hololive-shared)

### P11-A3. 에러 메시지 클라이언트 노출 방지 `[x완료]`

- [x] `kakao-bot/internal/server/api_room.go` 오류 응답 경로 확인
- [x] `err.Error()` 직접 반환 제거 (고정 메시지 + 서버 로그 기록)
  - 에러 코드 맵: `map[error]string` 또는 switch 기반
  - 상세 에러는 `logger.Error("...", slog.Any("error", err))` 로만 기록
- [x] 전체 API 핸들러 `err.Error()` 직접 반환 패턴 grep 확인
  - `grep -rn 'gin.H.*error.*err.Error()' kakao-bot/internal/server/`
- [x] 패턴 위반 시 실패하는 검증 테스트 추가 (`api_error_sanitization_test.go`)
- [ ] `go vet + golangci-lint + go test` (kakao-bot)

---

## P11-B: 중복 코드 제거

> Risk: LOW-MED | 소요: 5-7일 | 의존: 없음

### P11-B1. Subscription DTO 통합 `[x완료]`

- [x] `hololive/hololive-shared/pkg/contracts/subscription/` 생성
- [x] `types.go` 작성:
  ```go
  type SubscribeRequest struct {
      RoomID   string `json:"room_id"`
      RoomName string `json:"room_name"`
  }
  type SubscriptionStatusResponse struct {
      Subscribed bool `json:"subscribed"`
  }
  ```
- [x] `llm-sched/internal/app/api_internal_majorevent.go` — 로컬 DTO 제거, import 교체
- [x] `llm-sched/internal/app/api_internal_membernews.go` — 로컬 DTO 제거, import 교체
- [x] `kakao-bot/internal/service/majoreventclient/client.go` — 로컬 DTO 제거, import 교체
- [x] `kakao-bot/internal/service/membernewsclient/client.go` — 로컬 DTO 제거, import 교체
- [ ] `go vet + golangci-lint + go test` (shared, llm-sched, kakao-bot)

### P11-B2. Auth Error 타입 통합 `[x완료]`

- [x] `kakao-bot/internal/service/auth/errors.go` 확인 — shared auth 타입 alias/re-export 적용
- [x] `hololive-shared/pkg/service/auth` import로 통합
- [x] kakao-bot auth 패키지 ErrorCode/Error/newError 참조 경로 갱신
- [ ] `go vet + golangci-lint + go test` (kakao-bot)

### P11-B3. Consensus Review 타입 통합 `[x완료]`

- [x] `hololive/hololive-llm-sched/internal/service/consensus/` 생성
- [x] `types.go` 작성: ReviewIssue, ReviewVerdict, ConsensusConfig (exported, 통합)
- [x] `majorevent/summarizer/summarizer_consensus.go` — 공통 타입 사용
- [x] `membernews/summarizer/summarizer_consensus.go` — 공통 타입 alias 적용
- [x] `go vet + golangci-lint + go test` (llm-sched)

### P11-B4. SearchResult DTO 통합 `[x완료]`

- [x] `mkdir -p hololive/hololive-llm-sched/internal/model/`
- [x] `search.go` 작성: SearchResult struct
- [x] `majorevent/summarizer/searcher.go` — 로컬 DTO 제거, import 교체
- [x] `membernews/summarizer/summarizer.go` — 로컬 DTO 제거, import 교체
- [x] `go vet + golangci-lint + go test` (llm-sched)

### P11-B5. HTTP Client 생성 유틸 추출 `[x완료]`

- [x] `mkdir -p shared-go/pkg/httputil/`
- [x] `client.go` 작성:
  ```go
  func NewClient(timeout time.Duration) *http.Client
  func DefaultClient() *http.Client  // 30s timeout
  ```
- [x] 주요 사용처 교체 (우선 6곳):
  - `kakao-bot/internal/service/majoreventclient/client.go`
  - `kakao-bot/internal/service/membernewsclient/client.go`
  - `hololive-shared/pkg/service/alarm/client.go`
  - `hololive-shared/pkg/iris/h2c_client.go`
  - `llm-sched/internal/service/majorevent/summarizer/exa_client.go`
  - `hololive-shared/pkg/service/holodex/api_client.go`
- [~] 잔여 `&http.Client{` 직접 생성 패턴 grep 및 순차 교체 (핵심 경로 우선 반영 완료)
- [ ] `go vet + golangci-lint + go test` (전체 모듈)

### P11-B6. HTTP 에러 응답 처리 추출 `[x완료]`

- [x] `shared-go/pkg/httputil/response.go` 작성:
  ```go
  func CheckStatus(resp *http.Response) error           // status < 200 || >= 300 → error
  func DecodeJSON(resp *http.Response, v any) error      // body close + json decode
  ```
- [x] 주요 사용처 교체 (6곳):
  - majoreventclient, membernewsclient, trigger/client
  - system/stats, exa_client, alarm/client
- [ ] `go vet + golangci-lint + go test` (전체 모듈)

### P11-B7. 테스트 로거 유틸 통합 `[x완료]`

- [x] 로컬 `testLogger()` / `newTestLogger()` 정의 전수 grep
  - `grep -rn 'func testLogger\|func newTestLogger' --include='*_test.go'`
- [x] 각 로컬 정의 → `logging.NewTestLogger()` 계열 import 교체 완료
  - `rg -n "func (testLogger|newTestLogger)" -g '*_test.go' hololive shared-go` 결과 0건 (2026-03-05)
- [ ] `go vet + golangci-lint + go test` (전체 모듈)

### P11-B8. miniredis 테스트 헬퍼 추출 `[x완료]`

- [x] `hololive/hololive-shared/internal/testutil/` 생성
- [x] `cache.go` 작성: `NewTestCacheService` / `NewTestCacheServiceWithMini`
- [~] 4곳 중복 boilerplate → 공통 헬퍼 호출로 교체:
  - [x] `kakao-bot/internal/service/notification/alarm_test_helpers_test.go`
  - [x] `hololive-shared/pkg/service/cache/service_test.go`
  - [x] `hololive-shared/pkg/providers/ingestion_lock_test.go`
  - [x] `hololive-shared/pkg/server/middleware/ratelimit_middleware_test.go`
- [ ] `go vet + golangci-lint + go test` (shared, kakao-bot)

### P11-B9. Subscription Repository 인터페이스 통합 `[x완료]`

- [x] `llm-sched/internal/service/majorevent/repository.go` 와 `membernews/repository.go` 비교
- [x] 공통 `SubscriptionRepository` 인터페이스 추출 (Subscribe, IsSubscribed, ListSubscribedRooms)
- [x] 양쪽 repository가 공통 인터페이스 구현하도록 조정
- [x] `go vet + golangci-lint --new + go test` (llm-sched)

---

## P11-C: I/O 성능 개선

> Risk: LOW-MED | 소요: 3-5일 | 의존: 없음

### P11-C1. YouTube Stats 배치 INSERT `[x완료]`

- [x] `hololive-shared/pkg/service/youtube/stats/stats_repository_write.go` 확인
- [x] `SaveStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error` 추가
  - multi-value INSERT: `VALUES ($1,$2,...), ($3,$4,...), ...`
  - ON CONFLICT 동일 처리
- [x] 호출부(`youtube/scheduler.go`) 단건/배치 경로 정리
- [x] 배치 테스트 추가 (`stats_repository_batch_test.go`)
- [ ] `go vet + golangci-lint + go test` (hololive-shared)

### P11-C2. Outbox 배치 메서드 추가 `[x완료]`

- [x] `hololive-shared/pkg/service/delivery/outbox_repository.go` 확인
- [x] `EnqueueBatch(ctx, items []OutboxItem) error` 추가
- [x] `MarkSentBatch(ctx, ids []int64) error` 추가
- [x] `MarkFailedBatch(ctx, ids []int64, reason string) error` 추가
- [x] 배치 테스트 추가 (`outbox_repository_test.go`)
- [ ] `go vet + golangci-lint + go test` (hololive-shared)

### P11-C3. 슬라이스 프리할당 `[x완료]`

- [x] `hololive-shared/pkg/service/member/repository.go` GetAllChannelIDs 프리할당 적용
  - `var channelIDs []string` → `channelIDs := make([]string, 0, 256)` (또는 COUNT 쿼리)
- [x] `hololive-shared/pkg/service/alarm/repository.go` GetAllChannelIDs 프리할당 적용
  - 동일 패턴 수정
- [x] `kakao-bot/internal/service/alarm/checker/youtube_checker.go` notifications 프리할당 적용
  - `make([]*domain.AlarmNotification, 0)` → `make(..., 0, len(dueChannels)*5)`
- [ ] `go vet + golangci-lint + go test` (shared, kakao-bot)

### P11-C4. GORM fetch-then-update 제거 `[x완료]`

- [x] `hololive-shared/pkg/service/member/repository.go` 4건 확인:
  - `AddAlias` (line 429-471)
  - `RemoveAlias` (line 474-517)
  - `SetGraduation` (line 521-531)
  - `UpdateChannelID` (line 535-545)
- [x] 각 메서드 `db.Model(&Model{}).Where(...).Update(...)` 직접 갱신으로 전환
- [x] 기존 repository 테스트 파일 기준 회귀 경로 확인
- [ ] `go vet + golangci-lint + go test` (hololive-shared)

---

## P11-D: 테스트 커버리지 확대

> Risk: LOW | 소요: 7-10일 | 의존: 없음

### P11-D1. 알람 체커 테스트 (CRITICAL) `[x완료]`

- [x] `kakao-bot/internal/service/alarm/checker/youtube_checker_test.go` 신규
  - [x] 생성자/보조 유틸/구독자 로딩 회귀 테스트 보강 (`checker_additional_test.go` 포함)
  - [x] table-driven: stream found (정상 알림)
  - [x] table-driven: stream not found (알림 없음)
  - [x] table-driven: API timeout (graceful 실패)
  - [x] table-driven: 5xx 응답 (에러 핸들링)
  - [x] table-driven: dedup 중복 알림 방지
- [x] `kakao-bot/internal/service/alarm/checker/chzzk_checker_test.go` 신규 (table-driven 반영)
  - [x] table-driven: 라이브 상태 변경 감지
  - [x] table-driven: API 에러 시나리오
- [x] `kakao-bot/internal/service/alarm/checker/twitch_checker_test.go` 신규
  - [x] 라이브 상태 변경 + dedup 회귀 시나리오
  - [x] API 에러 시나리오
- [x] `kakao-bot/internal/service/alarm/checker/notifier_test.go` 신규
  - [x] 시나리오: dedup skip / queue publish 성공 경로
- [x] `go test -cover ./internal/service/alarm/checker/...` 50%+ 확인 (89.9%, 2026-03-05)

### P11-D2. Notification 서비스 테스트 `[x완료]`

- [x] `kakao-bot/internal/service/notification/alarm_service_test.go` 신규
  - [x] table-driven: 알람 추가/제거/조회 회귀 보강 (`TestAlarmService_AddRemoveCacheScenarios_TableDriven`, `TestGetRoomAlarms_*`)
  - [x] 캐시 히트/미스 회귀 보강 (`TestGetMemberNameWithFallback_CacheHit`, `TestGetMemberNameWithFallback_NoCache_ReturnsChannelID`)
  - [x] 영속화 성공/실패 회귀 보강 (`TestAlarmPersistence_RoundTripScenarios_TableDriven`, `TestAlarmPersistence_MarkAsNotifiedTimeout`)
- [x] `kakao-bot/internal/service/notification/alarm_persistence_test.go` 신규
  - [x] Valkey 영속화 write/read 라운드트립
  - [x] 타임아웃 시나리오
- [x] `go test -cover ./internal/service/notification/` 60%+ 확인 (76.6%, 2026-03-05)

### P11-D3. Auth 서비스 테스트 `[x완료]`

- [x] `kakao-bot/internal/service/auth/service_test.go` 신규/보강
  - [x] table-driven/시나리오 기반: 유효 토큰 검증
  - [x] 만료/무효 토큰 거부 시나리오 검증
  - [x] 잘못된 형식/세션 토큰 거부 시나리오 검증
- [x] `kakao-bot/internal/service/auth/validation_test.go` 신규
  - [x] table-driven: 입력 검증 (빈 값, 초과 길이, 특수문자)
- [x] `kakao-bot/internal/service/auth/tokens_test.go` 신규
  - [x] 토큰 생성/파싱 라운드트립
  - [x] 리프레시 토큰/세션 재발급 로직
- [x] `go test -cover ./internal/service/auth/...` 70%+ 확인 (74.5%, 2026-03-05)

### P11-D4. Stream Ingester 런타임 테스트 `[x완료]`

- [x] `stream-ingester/internal/app/stream_ingester_runtime_builder_test.go` 신규
  - [x] 정상 빌드 (모든 의존성 제공)
  - [x] 의존성 누락/초기화 실패 시 에러 반환 (`TestBuildStreamIngesterRuntime_Preconditions`, `TestBuildStreamIngesterRuntime_ReturnsErrorOnInfraInitFailure`)
- [x] `stream-ingester/internal/app/stream_ingester_poller_registrations_test.go` 신규
  - [x] 폴러 등록 성공 경로 검증
- [x] `go test -cover ./internal/app/` 40%+ 확인 (62.0%, 2026-03-05)

### P11-D5. YouTube Scraper playlists.go 테스트 `[x완료]`

- [x] `hololive-shared/pkg/service/youtube/scraper/playlists_test.go` 신규
  - [x] table-driven: 정상 JSON 파싱
  - [x] 빈/플레이리스트 없음 응답
  - [x] table-driven: 잘못된 JSON 형식
  - [x] table-driven: 페이지네이션 edge case
- [x] 회귀 안정화: malformed JSON 혼합 케이스 방어 로직 반영 + 회귀 테스트 통과
- [x] `go test -cover ./pkg/service/youtube/scraper/` playlists 부분 60%+ 확인 (61.7%, 2026-03-05)

---

## P11-E: 모듈 구조 보강

> Risk: MED | 소요: 5-7일 | 의존: 없음

### P11-E1. `pkg/config/config.go` 도메인별 분할 `[x완료]`

- [x] `hololive-shared/pkg/config/config.go` 현재 struct 목록 확인
- [x] 파일 분할:
  - [x] `config_db.go` — PostgresConfig, 관련 validation
  - [x] `config_cache.go` — ValkeyConfig, 관련 validation
  - [x] `config_iris.go` — IrisConfig, IrisBotConfig
  - [x] `config_notification.go` — NotificationConfig
  - [x] `config_telemetry.go` — TelemetryConfig, LoggingConfig
  - [x] `config_kakao.go` — BotConfig, CORSConfig, KakaoConfig
  - [x] `config_llm.go` — LLMConfig, ExaConfig
- [x] 1차 보조 분리: `config_env_loaders.go`, `config_parsers.go` 추출
- [x] 도메인별 struct 분할 완료 (`config.go`는 조합/로드 중심으로 축소)
- [x] 잔여 `config.go`: 최상위 Config struct + Load() 함수 중심 유지
- [ ] `go build + go vet + golangci-lint + go test` (전체 모듈)

### P11-E2. `pkg/server/` 서브패키지 분리 `[x완료]`

- [x] server 패키지 파일 역할 분류/재배치
- [x] `hololive/hololive-shared/pkg/server/middleware/` 분리
  - [x] `auth.go`, `auth_test.go` 이동
  - [x] `security.go`, `security_test.go` 이동
  - [x] `ratelimit_middleware.go`, `ratelimit_middleware_test.go` 이동
  - [x] `logger.go`, `logger_test.go` 이동
  - [x] `client_hints.go`, `client_hints_test.go` 이동
- [x] `hololive/hololive-shared/pkg/server/settings/` 분리
  - [x] `settings_handler.go` 이동
  - [x] `settings_result.go`, `settings_result_test.go` 이동
  - [x] `settings_applier_local.go` 이동
- [ ] 잔여 `server/`: h2c.go, response.go, websocket.go, trigger.go, channel_ids.go
- [x] 전체 import path 갱신 (4개 앱 모듈 기준)
- [ ] `go build + go vet + golangci-lint + go test` (전체 모듈)

### P11-E3. K8s Secret 서비스별 분리 `[x완료]`

- [x] 기존 단일 secret-app 경로 제거/대체 확인
- [x] 서비스별 Secret 분할:
  - [x] `secret-common.yaml` — POSTGRES_USER, POSTGRES_PASSWORD, HOLODEX_API_KEY
  - [x] `secret-bot.yaml` — IRIS_WEBHOOK_TOKEN, IRIS_BOT_TOKEN, API_SECRET_KEY
  - [x] `secret-dispatcher.yaml` — IRIS_*, ALARM_DISPATCH_*
  - [x] `secret-llm.yaml` — OPENAI_API_KEY, EXA_API_KEY
- [x] 각 Deployment envFrom 참조 갱신
- [ ] `kubectl apply --dry-run=client -f k8s/base/` 검증

### P11-E4. K8s ConfigMap 서비스별 분리 `[x완료]`

- [x] 기존 ConfigMap 구성 확인 및 분리 기준 정리
- [x] 서비스별 분리 적용 (common/bot/dispatcher/llm)
- [x] 분리 실행 및 Deployment envFrom 참조 갱신
- [ ] `kubectl apply --dry-run=client -f k8s/base/` 검증

---

## 병렬 실행 가이드

```
Track A (보안):          P11-A1 → P11-A2 → P11-A3
Track B (중복 제거):     P11-B1 → P11-B2 → P11-B3 → P11-B4 → P11-B5 → P11-B6 → P11-B7 → P11-B8 → P11-B9
Track C (성능):          P11-C1 → P11-C2 → P11-C3 → P11-C4
Track D (테스트):        P11-D1 → P11-D2 → P11-D3 → P11-D4 → P11-D5
Track E (구조):          P11-E1 → P11-E2 → P11-E3 → P11-E4
```

- Track A~E 전부 독립 (병렬 실행 가능)
- Track 내부는 순차 (파일 충돌 회피)
- 최대 병렬도: 5 트랙 동시

### Track별 수정 파일 영역 (배타적 소유권)

| Track | 소유 영역 | 충돌 회피 |
|-------|----------|----------|
| A | `dbx/config.go`, `server/websocket.go`, `kakao-bot/server/api_room.go` | 보안 관련 파일만 |
| B | `contracts/subscription/`, `auth/errors.go`, `consensus/`, `httputil/` | DTO/유틸 추출 |
| C | `stats/stats_repository_write.go`, `outbox_repository.go`, `member/repository.go` | 저장소 성능 |
| D | `checker/*_test.go`, `notification/*_test.go`, `auth/*_test.go` | 테스트 파일만 (신규) |
| E | `config/config*.go`, `server/middleware/`, `k8s/base/` | 구조 분할 |

### 우선순위 권장 순서

1. **P11-A** (보안) — 즉시 (DSN 노출은 잠재적 위험)
2. **P11-D1** (알람 체커 테스트) — 즉시 (핵심 파이프라인 0% 커버리지)
3. **P11-B1** (Subscription DTO) — 1주 내 (4곳 계약 위반 위험)
4. **P11-C1** (Stats 배치) — 2주 내 (성능 개선)
5. **P11-E** (구조) — 3주 내 (유지보수성)

---

## 검증 체크리스트 (Phase 11 완료 기준)

### 라운드4 검증 스냅샷 (2026-03-05)

- 타입체크 등가(`go test ./... -run TestDoesNotExist`)
  - PASS: `hololive-llm-sched`, `hololive-dispatcher-go`, `shared-go`
  - FAIL: `hololive-shared`, `hololive-kakao-bot-go`, `hololive-stream-ingester`
- lint
  - PASS: `hololive-kakao-bot-go`의 `majoreventclient/membernewsclient/trigger` 대상 lint (`0 issues`)
  - FAIL: 전체 lint 기준 `alarm/checker`, `youtube/stats`, `server/websocket` 관련 typecheck 에러 잔존
- coverage
  - `hololive-kakao-bot-go/internal/app`: **53.8%**
  - `majoreventclient`: **88.9%**, `membernewsclient`: **86.8%**, `trigger`: **82.9%**
- 현재 핵심 blocker
  - `alarm/checker` 중복 테스트명(`TestLoadSubscriberRoomsByChannel`)으로 빌드 실패
  - `stream-ingester/internal/app` 중복 테스트명 2건으로 빌드 실패
  - `hololive-shared`의 `contracts/common` import/typecheck 및 `stats_repository_batch_test` 불일치

- [x] DSN 문자열에 평문 비밀번호 포함 경로 0건 (dbx 경로 grep 기준)
- [x] `err.Error()` 클라이언트 직접 반환 0건 (server 핸들러 grep 기준)
- [x] WebSocket origin 하드코딩 0건 (`WEBSOCKET_ALLOWED_ORIGINS` 사용)
- [x] Subscription DTO 로컬 중복 정의 0건 (4개 경로 공통 contracts 사용)
- [x] Auth Error 로컬 중복 정의 0건 (shared alias/re-export)
- [ ] `&http.Client{` 직접 생성 0건 (httputil 경유만)
- [x] `func testLogger()` 로컬 정의 0건 (`rg -n "func (testLogger|newTestLogger)" -g '*_test.go' hololive shared-go`)
- [x] 알람 체커 테스트 커버리지 50%+ (`go test -cover ./internal/service/alarm/checker/...` = 89.9%, 2026-03-05)
- [x] Auth 서비스 테스트 커버리지 70%+ (`go test -cover ./internal/service/auth/...` = 74.5%, 2026-03-05)
- [ ] `go vet + golangci-lint + go test` 전체 모듈 통과

### phase11-7 병렬 실행 검증 업데이트 (2026-03-05)

- 타입체크 등가:
  - PASS: `hololive-kakao-bot-go` (`go test ./... -run TestDoesNotExist`)
  - PASS: `hololive-stream-ingester` (`go test ./... -run TestDoesNotExist`)
- lint:
  - PASS: `hololive-kakao-bot-go/internal/service/alarm/checker`, `.../notification` (`0 issues`)
  - PASS: `hololive-stream-ingester/internal/app` (`0 issues`)
- coverage:
  - `kakao-bot/internal/service/alarm/checker`: **82.5%**
  - `kakao-bot/internal/service/notification`: **76.6%**
  - `stream-ingester/internal/app`: **60.8%**

### phase11-8 D5/E1 검증 업데이트 (2026-03-05)

- 타입체크 등가:
  - FAIL: `cd hololive/hololive-shared && go test ./... -run TestDoesNotExist`
    - `pkg/contracts/common` import 누락, `pkg/server/websocket_test.go` 심볼 불일치, `youtube/stats` 배치 테스트 빌드 실패
  - PASS: `cd hololive/hololive-shared && go test ./pkg/config ./pkg/service/youtube/scraper -run TestDoesNotExist`
- 테스트(변경 영역):
  - PASS: `cd hololive/hololive-shared && go test -count=1 -cover ./pkg/service/youtube/scraper/...` (**61.7%**)
  - PASS: `cd hololive/hololive-shared && go test -cover ./pkg/config` (**87.5%**)
- lint:
  - PASS: `cd hololive/hololive-shared && golangci-lint run ./pkg/config` (`0 issues`)
  - PASS: `cd hololive/hololive-shared && golangci-lint run --new ./pkg/service/youtube/scraper/...` (`0 issues`)
- End-to-End 성격 시나리오(패키지 레벨):
  - PASS: `cd hololive/hololive-shared && go test ./pkg/service/youtube/scraper -run 'Test(GetPlaylists_(GridRenderer|ShelfRenderer|NoPlaylistsTab|ChannelNotFound)|ParseGridPlaylistRenderer_VideoCountFormats)$'`
- 관련 회귀:
  - PASS: `cd hololive/hololive-shared && go test -run TestGetPlaylists_MalformedPlaylistJSON_TableDriven -v ./pkg/service/youtube/scraper`

### phase11-11 B7/D1 문서 동기화 검증 업데이트 (2026-03-05)

- 타입체크 등가:
  - PASS: `cd hololive/hololive-kakao-bot-go && go test ./... -run TestDoesNotExist`
  - PASS: `cd hololive/hololive-stream-ingester && go test ./... -run TestDoesNotExist`
  - PASS: `cd hololive/hololive-llm-sched && go test ./... -run TestDoesNotExist`
- lint:
  - PASS: `cd hololive/hololive-kakao-bot-go && golangci-lint run ./internal/service/alarm/checker/... ./internal/service/notification/...`
  - PASS: `cd hololive/hololive-stream-ingester && golangci-lint run ./internal/app/...`
  - FAIL: `cd hololive/hololive-llm-sched && golangci-lint run ./internal/app/...`
    - `internal/app/bootstrap_alarm.go:110:33` staticcheck(S1016) (`membernews.SearchResult` 변환 권장)
- coverage:
  - PASS: `cd hololive/hololive-kakao-bot-go && go test -count=1 -cover ./internal/service/alarm/checker/...` (**82.5%**)
  - PASS: `cd hololive/hololive-kakao-bot-go && go test -count=1 -cover ./internal/service/notification/...` (**76.6%**)
  - PASS: `cd hololive/hololive-stream-ingester && go test -count=1 -cover ./internal/app/...` (**62.0%**)
- B7 증빙:
  - PASS: `rg -n "func (testLogger|newTestLogger)" -g '*_test.go' hololive shared-go` → 결과 0건
- 잔여 항목(명시):
  - B8: `hololive-shared/pkg/service/cache/service_test.go` miniredis 헬퍼 치환
  - D1: youtube checker table-driven 5개(정상/미발견/timeout/5xx/dedup) 시나리오
  - D2: alarm_service/alarm_persistence table-driven 시나리오
  - D4: runtime_builder 정상/의존성 누락 시나리오

### phase11-12 B8/D2/D4 문서 검증 업데이트 (2026-03-05)

- 타입체크 등가:
  - FAIL: `cd hololive/hololive-shared && go test ./... -run TestDoesNotExist`
    - `pkg/contracts/common` import 해석 실패, `pkg/server/websocket_test.go` 심볼 불일치, `youtube/stats` 배치 테스트 빌드 오류
  - PASS: `cd hololive/hololive-kakao-bot-go && go test ./... -run TestDoesNotExist`
  - PASS: `cd hololive/hololive-stream-ingester && go test ./... -run TestDoesNotExist`
- lint(변경 영역):
  - PASS: `cd hololive/hololive-shared && golangci-lint run ./pkg/service/cache/...`
  - PASS: `cd hololive/hololive-kakao-bot-go && golangci-lint run ./internal/service/notification/...`
  - PASS: `cd hololive/hololive-stream-ingester && golangci-lint run ./internal/app/...`
- coverage(변경 영역):
  - PASS: `cd hololive/hololive-shared && go test -count=1 -cover ./pkg/service/cache/...` (**42.0%**)
  - PASS: `cd hololive/hololive-kakao-bot-go && go test -count=1 -cover ./internal/service/notification/...` (**76.6%**)
  - PASS: `cd hololive/hololive-stream-ingester && go test -count=1 -cover ./internal/app/...` (**62.0%**)
- End-to-End/회귀 시나리오(변경 영역):
  - PASS: `cd hololive/hololive-kakao-bot-go && go test -count=1 ./internal/service/notification -run 'TestAlarmService_AddRemoveCacheScenarios_TableDriven|TestAlarmPersistence_RoundTripScenarios_TableDriven'`
  - PASS: `cd hololive/hololive-stream-ingester && go test -count=1 ./internal/app -run 'TestBuildStreamIngesterRuntime_Preconditions|TestBuildStreamIngesterRuntime_ReturnsErrorOnInfraInitFailure'`
  - PASS: `cd hololive/hololive-shared && go test -count=1 ./pkg/service/cache -run 'TestCacheServiceSetGetAndExists|TestService_Builder'`
- B8 잔여 확인:
  - `service_test.go`는 여전히 로컬 `newTestCacheService`를 사용 (`hololive/hololive-shared/pkg/service/cache/service_test.go`)
  - 4개 대상 중 3개는 공통 헬퍼 치환 반영(`notification`, `ingestion_lock`, `ratelimit`)

### phase11-13 D1/D4/B8 문서 동기화 검증 업데이트 (2026-03-05)

- 타입체크 등가:
  - PASS: `cd hololive/hololive-kakao-bot-go && go test ./... -run TestDoesNotExist`
  - PASS: `cd hololive/hololive-stream-ingester && go test ./... -run TestDoesNotExist`
  - FAIL: `cd hololive/hololive-shared && go test ./... -run TestDoesNotExist`
    - `pkg/contracts/common` import 해석 실패, `pkg/server/websocket_test.go` 심볼 불일치, `youtube/stats` 배치 테스트 빌드 오류
- lint(변경 영역):
  - PASS: `cd hololive/hololive-kakao-bot-go && golangci-lint run ./internal/service/alarm/checker/...` (`0 issues`)
  - PASS: `cd hololive/hololive-stream-ingester && golangci-lint run ./internal/app/...` (`0 issues`)
  - PASS: `cd hololive/hololive-shared && golangci-lint run ./pkg/service/cache/...` (`0 issues`)
- coverage(변경 영역):
  - PASS: `cd hololive/hololive-kakao-bot-go && go test -count=1 -cover ./internal/service/alarm/checker/...` (**82.7%**)
  - PASS: `cd hololive/hololive-stream-ingester && go test -count=1 -cover ./internal/app/...` (**62.0%**)
  - PASS: `cd hololive/hololive-shared && go test -count=1 -cover ./pkg/service/cache/...` (**42.0%**)
- End-to-End/회귀 시나리오(변경 영역):
  - PASS: `cd hololive/hololive-kakao-bot-go && go test -count=1 ./internal/service/alarm/checker -run 'Test(YouTubeCheckerCheck_EmptyChannelRegistry|YouTubeNotificationBuilders|LoadSubscriberRoomsByChannel_Table)$'`
  - PASS: `cd hololive/hololive-stream-ingester && go test -count=1 ./internal/app -run 'Test(BuildStreamIngesterRuntime_Preconditions|BuildStreamIngesterRuntime_ReturnsErrorOnInfraInitFailure|BuildStreamIngesterHTTPServer_Success)$'`
  - PASS: `go test ./hololive/hololive-kakao-bot-go/internal/service/alarm/checker/... ./hololive/hololive-stream-ingester/internal/app/... ./hololive/hololive-shared/pkg/service/cache/...`
- 잔여 항목(명시):
  - B8: `hololive-shared/pkg/service/cache/service_test.go` miniredis 공통 헬퍼 치환
  - D1: youtube checker table-driven 5개(정상/미발견/timeout/5xx/dedup) 시나리오
  - D4: `stream_ingester_runtime_builder_test.go` 정상 빌드(모든 의존성 제공) 시나리오

### phase11-14 ultrawork 병렬 검증 업데이트 (2026-03-05)

- 변경 사항:
  - B8 잔여 완료: `hololive-shared/pkg/service/cache/service_test.go` 로컬 miniredis 보일러플레이트를 `internal/testutil/cacheclient.NewValkeyClientWithMini` 공통 헬퍼로 치환
  - D1 완료: `kakao-bot/internal/service/alarm/checker/youtube_checker_test.go` table-driven 5개 시나리오 반영 상태 확인
  - D4 완료: `stream-ingester/internal/app/stream_ingester_runtime_builder_test.go` 정상 빌드 시나리오 확장(Proxy on/off 전환, 설정 반영, cleanup 검증, 설정 저장 실패 가드)
  - 커버리지 확장: `kakao-bot/internal/app/bootstrap_guard_additional_test.go`, `runtime_wrappers_additional_test.go` 추가
- 타입체크 등가(최신):
  - PASS: `cd hololive/hololive-shared && go test ./... -run TestDoesNotExist`
  - PASS: `cd hololive/hololive-kakao-bot-go && go test ./... -run TestDoesNotExist`
  - PASS: `cd hololive/hololive-stream-ingester && go test ./... -run TestDoesNotExist`
  - PASS: `cd hololive/hololive-llm-sched && go test ./... -run TestDoesNotExist`
- lint(변경 영역):
  - PASS: `cd hololive/hololive-kakao-bot-go && golangci-lint run ./internal/app/... ./internal/service/notification/...`
  - PASS: `cd hololive/hololive-stream-ingester && golangci-lint run ./internal/app/...`
  - PASS: `cd hololive/hololive-shared && golangci-lint run ./pkg/service/cache/...`
- coverage(변경 영역):
  - PASS: `cd hololive/hololive-kakao-bot-go && go test -count=1 -cover ./internal/app/...` (**65.7%**, 이전 53.8%)
  - PASS: `cd hololive/hololive-kakao-bot-go && go test -count=1 -cover ./internal/service/alarm/checker/...` (**89.9%**)
  - PASS: `cd hololive/hololive-stream-ingester && go test -count=1 -cover ./internal/app/...` (**63.9%**, 이전 62.0%)
  - PASS: `cd hololive/hololive-shared && go test -count=1 -cover ./pkg/service/cache/...` (**42.0%**)

### phase11-15 llm-sched+문서(TODO/NEXT_TODO) 최종 동기화 (2026-03-05)

- llm-sched 코드 정리:
  - `internal/app/bootstrap_alarm.go` memberNewsSearcherAdapter 변환 루프 단순화(`append(..., results...)`)
  - `internal/service/membernews/source_validator.go` allowlist 파일 로딩 gosec 예외 사유 명시(`#nosec G304`)
- 타입체크 등가:
  - PASS: `cd hololive/hololive-llm-sched && go test ./... -run TestDoesNotExist`
- 정적 검증:
  - PASS: `cd hololive/hololive-llm-sched && go vet ./...`
  - PASS: `cd hololive/hololive-llm-sched && golangci-lint run ./internal/app/... ./internal/service/majorevent/... ./internal/service/membernews/... ./internal/model/... ./internal/service/subscription/...` (`0 issues`)
- 테스트(변경 영역):
  - PASS: `cd hololive/hololive-llm-sched && go test -count=1 ./internal/app ./internal/service/majorevent/... ./internal/service/membernews/... ./internal/service/subscription/...`
- End-to-End/회귀 시나리오(변경 영역):
  - PASS: `cd hololive/hololive-llm-sched && go test -count=1 ./internal/app -run 'Test(MemberNewsSearcherAdapter|ResolveMemberNewsXAllowlistPath|ProvideDeliveryAndTriggerProviders)'`
  - PASS: `cd hololive/hololive-llm-sched && go test -count=1 ./internal/service/membernews -run 'TestSourceValidator_XAllowlistAndDomainValidation'`
  - PASS: `cd hololive/hololive-llm-sched && go test -count=1 ./internal/service/subscription -run 'TestSubscriptionRepositoryContract'`

### phase11-15 llm-sched 전체 재검증 (2026-03-05, round5)

- 문서 수치/상태 최종 동기화:
  - `TODO_PHASE11`/`NEXT_TODO`의 llm-sched 상태를 "전체 vet/lint/test green 유지" 기준으로 재확인
- 타입체크 등가:
  - PASS: `cd hololive/hololive-llm-sched && go test ./... -run TestDoesNotExist`
- 정적 검증(전체):
  - PASS: `cd hololive/hololive-llm-sched && go vet ./...`
  - PASS: `cd hololive/hololive-llm-sched && golangci-lint run ./...` (`0 issues`)
- 테스트(전체):
  - PASS: `cd hololive/hololive-llm-sched && go test -count=1 ./...`
- End-to-End/회귀 시나리오(핵심):
  - PASS: `cd hololive/hololive-llm-sched && go test -count=1 ./internal/app -run 'Test(MemberNewsSearcherAdapter|ResolveMemberNewsXAllowlistPath|ProvideDeliveryAndTriggerProviders)'`
  - PASS: `cd hololive/hololive-llm-sched && go test -count=1 ./internal/service/membernews -run 'TestSourceValidator_XAllowlistAndDomainValidation'`
  - PASS: `cd hololive/hololive-llm-sched && go test -count=1 ./internal/service/subscription -run 'TestSubscriptionRepositoryContract'`
