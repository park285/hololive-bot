# Hololive Bot 확장 모듈화 - 상세 TODO (Phase 7~10)

> 생성: 2026-03-03
> 기반 문서: `docs/modularization/PLAN_EXTENDED.md`
> 규칙: `TODO.md`와 동일 (`[x]` 완료, `[ ]` 미착수, `[~]` 진행중, Phase/Step별 독립 PR)

---

## Phase 7: Go 서비스 내부 God Object 분할

> Risk: LOW | 의존: 없음 (즉시 병렬 가능) | 대상: admin, kakao-bot

### P7-1. admin `api.go` 분할 (1,750 LOC -> 10개 파일)

- [x] `internal/server/api.go` 현재 구조 파악 (핸들러 struct/method 목록 확인)
- [x] `api_auth.go` 추출: AuthHandler (Register/Login/Refresh/Reset) ~260 LOC
- [x] `api_member.go` 추출: MemberAPIHandler ~200 LOC
- [x] `api_profile.go` 추출: ProfileAPIHandler ~150 LOC
- [x] `api_stream.go` 추출: StreamAPIHandler ~200 LOC
- [x] `api_room.go` 추출: RoomAPIHandler ~150 LOC
- [x] `api_alarm.go` 추출: AlarmAPIHandler ~180 LOC
- [x] `api_settings.go` 추출: SettingsAPIHandler ~150 LOC
- [x] `api_template.go` 추출: TemplateAPIHandler ~150 LOC
- [x] `api_milestone_majorevent.go` 추출: MilestoneAPIHandler + MajorEventAPIHandler + OAuthAPIHandler ~310 LOC
- [x] 잔여 `api.go`: 핸들러 struct 정의 + 공통 미들웨어만 (~100 LOC)
- [x] `go build ./... && go vet ./... && golangci-lint run ./... && go test ./...` (hololive-admin)

### P7-2. bot `api_bot.go` 분할 (1,754 LOC -> 동일 패턴)

- [x] `internal/server/api_bot.go` 현재 구조 파악
- [x] admin P7-1과 동일 패턴으로 도메인별 파일 분할
- [x] `go build ./... && go vet ./... && golangci-lint run ./... && go test ./...` (hololive-kakao-bot-go)

### P7-3. admin `bootstrap_admin.go` 분할 (780 LOC -> 3개 파일)

- [x] `bootstrap_admin.go` 내 함수 책임 분류 (infra / service / routes)
- [x] `bootstrap_services.go` 추출: 서비스 계층 DI (alarm, youtube, holodex 등) ~300 LOC
- [x] `bootstrap_routes.go` 추출: 라우터 등록 + 미들웨어 설정 ~230 LOC
- [x] 잔여 `bootstrap_admin.go`: 핵심 진입점 + config 로드 (~250 LOC)
- [x] `go build ./... && go vet ./... && go test ./...` (hololive-admin)

### P7-4. bot bootstrap 통합 정리 (726 LOC + 5개 보조 파일)

- [x] 기존 `bootstrap*.go` 6개 파일 책임 매핑
- [x] `bootstrap_core.go` 통합: infra (DB, cache, config) 초기화 ~280 LOC
- [x] `bootstrap_services.go` 통합: 서비스 계층 DI ~300 LOC
- [x] `bootstrap_bot.go` 통합: Bot 생성 + command router 바인딩 ~200 LOC
- [x] 기존 임시 분할 파일 제거 (bootstrap_alarm.go, bootstrap_youtube.go, bootstrap_tools.go 등)
- [x] `go build ./... && go vet ./... && go test ./...` (hololive-kakao-bot-go)

### P7-5. bot `command/` 구조화 (28개 파일)

- [x] 현재 파일 목록 + 각 파일의 역할 분류
- [x] 파일 접두사 컨벤션 vs 서브디렉토리 도입 결정
  - 서브디렉토리: import path 변경 발생 (영향 범위 대)
  - 접두사 컨벤션: `handler_*.go`, `info_*.go`, `news_*.go` (import path 유지)
- [x] 확정된 방식으로 파일 재배치/rename
- [x] `go build ./... && go vet ./... && go test ./...` (hololive-kakao-bot-go)

---

## Phase 8: Go 서비스 간 계약 인터페이스화

> Risk: MED | 의존: Phase 2/3 완료 권장 | 대상: hololive-shared providers + 4개 서비스 소비자

### P8-1. 인프라 인터페이스 추출

#### P8-1a. cache.Client 인터페이스
- [x] `hololive-shared/pkg/service/cache/interface.go` 신규 작성
  - `Client` 인터페이스 정의 (Get/Set/Del/Exists + 기존 public 메서드)
- [x] `*cache.Service`가 `cache.Client` 만족하는지 컴파일 검증
- [x] `providers/infra_providers.go`: `ProvideCacheService` 반환 타입 -> `cache.Client`
- [x] 소비자 4개 모듈에서 `*cache.Service` -> `cache.Client` 타입 변경
- [x] 전체 Go 모듈 build + test

#### P8-1b. database.Client 인터페이스
- [x] `hololive-shared/pkg/service/database/interface.go` 신규 작성
- [x] `*database.PostgresService`가 인터페이스 만족 확인
- [x] providers 반환 타입 변경
- [x] 소비자 4개 모듈 타입 변경
- [x] 전체 Go 모듈 build + test

#### P8-1c. member.DataProvider 인터페이스
- [x] `hololive-shared/pkg/service/member/interface.go` 신규 작성
  - `DataProvider` (조회), `CacheProvider` (캐시 워밍) 분리
- [x] providers 반환 타입 변경
- [x] 소비자 3개 모듈 타입 변경
- [x] 전체 Go 모듈 build + test

#### P8-1d. settings.Reader / settings.Writer 인터페이스
- [x] `hololive-shared/pkg/service/settings/interface.go` 신규 작성
- [x] providers 반환 타입 변경 (Reader/Writer 조합인 `ReadWriter`로 우선 적용)
- [x] 소비자 2개 모듈 타입 변경
- [x] 전체 Go 모듈 build + test

### P8-2. providers 반환 타입 전수 점검

- [x] `grep -n 'func Provide' hololive-shared/pkg/providers/` 전체 목록 추출
- [x] 각 함수의 반환 타입이 인터페이스인지 구체 타입인지 분류
- [x] P8-1에서 미처리된 구체 타입 반환 함수 식별
- [x] 추가 인터페이스 추출 필요 여부 판단

### P8-3. contracts 패키지 확장

- [x] `hololive-shared/pkg/contracts/delivery/contracts.go` 신규 작성 (Outbox 메시지 포맷)
- [x] `hololive-shared/pkg/contracts/delivery/contracts_test.go` 호환성 테스트
- [x] `hololive-shared/pkg/contracts/settings/contracts.go` 신규 작성 (Settings Pub/Sub 포맷)
- [x] `hololive-shared/pkg/contracts/settings/contracts_test.go` 호환성 테스트

### P8-4. Mock 기반 구축 (선택)

- [x] mock 생성 방식 결정: 수동 mock (함수 필드 기반, `*/mocks` 패키지)
- [x] P8-1에서 추출한 인터페이스 대상 mock 생성 (`cache.Client`, `database.Client`, `settings.ReadWriter`, `domain.MemberDataProvider`)
- [x] 기존 테스트 중 구체 타입 직접 생성 -> mock 주입 전환 (샘플: `AcquireIngestionLease` 테스트)

### P8-5. 전체 검증

- [x] 전체 Go 모듈 `go build + go vet + golangci-lint + go test`
- [x] `go mod tidy` 전체 모듈

---

## Phase 9: Go 대형 패키지 이동 / 모듈 재배치

> Risk: MED-HIGH | 의존: Phase 2/3 완료 필수, Phase 8 권장

### P9-1. adapter 패키지 -> kakao-bot 이동 (3,884 LOC)

- [x] adapter 소비자 최종 재확인 (admin/llm-sched/stream-ingester 포함)
- [x] hololive-shared에서 adapter 기반 DI 제거 (ProvideMessageStack/MessageStack 등 제거)
- [x] youtube/scheduler.go: adapter 의존 → `youtube.MilestoneMessageFormatter` 인터페이스 주입으로 전환
- [x] llm-sched/stream-ingester: adapter 의존 제거(템플릿 기반 formatter 대체 + nil 허용)
- [x] `hololive-shared/pkg/adapter/` 전체 → `hololive-kakao-bot-go/internal/adapter/` 이동
- [x] kakao-bot 내부 import path 갱신
- [x] hololive-shared에서 adapter 디렉토리 삭제
- [x] `go mod tidy` 전체 모듈
- [x] 전체 Go 모듈 build + test (go test ./...)

### P9-2. majorevent 패키지 -> llm-sched 이동 (5,458 LOC)

- [x] majorevent 소비자 최종 재확인
  - admin: trigger proxy (contracts/trigger 경유)
  - llm-sched: scheduler 직접 사용
  - kakao-bot: command에서 참조 여부 재확인
- [x] admin trigger proxy가 contracts/trigger 인터페이스만 의존하는지 확인
- [x] kakao-bot 참조 시 인터페이스 추출 필요
- [x] P9-5 서브패키지 구조화 선행 (summarizer/, scheduler/ 분리)
- [x] `hololive-shared/pkg/service/majorevent/` -> `hololive-llm-sched/internal/service/majorevent/` 이동
- [x] import path 갱신
- [x] hololive-shared에서 majorevent 디렉토리 삭제
- [x] `go mod tidy` 전체 모듈
- [x] 전체 Go 모듈 build + vet + lint + test

### P9-3. membernews 패키지 -> llm-sched 이동 (5,048 LOC)

- [x] membernews 소비자 최종 재확인
  - llm-sched: scheduler 직접 사용
  - kakao-bot: command에서 참조 여부 재확인
- [x] kakao-bot 참조 시 인터페이스 추출 필요
- [x] P9-5 서브패키지 구조화 선행 (summarizer/, scheduler/, filter/ 분리)
- [x] `hololive-shared/pkg/service/membernews/` -> `hololive-llm-sched/internal/service/membernews/` 이동
- [x] import path 갱신
- [x] hololive-shared에서 membernews 디렉토리 삭제
- [x] `go mod tidy` 전체 모듈
- [x] 전체 Go 모듈 build + vet + lint + test

### P9-4. youtube 인터페이스 경계 강화 (이동 보류, 인터페이스 우선)

- [x] `youtube.Scheduler` public API 인터페이스 정의
- [x] `youtube.Service` 인터페이스 정의
- [x] stats_repository_*.go 8개 파일 → `youtube/stats/` 서브패키지 승격 (+ 호환 alias 레이어)
- [x] 모든 소비자 모듈에서 `*youtube.Service/*youtube.Scheduler` → 인터페이스 타입으로 갱신
- [x] 전체 Go 모듈 build + test (go test ./...)

### P9-5. shared 내부 대형 패키지 서브패키지 구조화 (P9-2/P9-3 선행)

#### majorevent 서브패키지화
- [x] `summarizer/` 서브패키지: summarizer.go, prompt.go, consensus.go + tests
- [x] `scheduler/` 서브패키지: scheduler.go, monthly_scheduler.go + tests
- [x] 잔여 root: errors.go, repository.go

#### membernews 서브패키지화
- [x] `summarizer/` 서브패키지: summarizer.go, consensus.go + tests
- [x] `scheduler/` 서브패키지: scheduler.go, monthly_scheduler.go + tests
- [x] `filter/` 서브패키지: filter.go + tests
- [x] 잔여 root: facade + repository.go + service.go + source_validator.go + internal/model

### P9-6. 전체 검증

- [x] 전체 Go 모듈 `go build + go vet + golangci-lint + go test`
- [x] `go mod tidy` 전체 모듈
- [x] hololive-shared LOC 측정 (현재: 53,499 lines; `find hololive/hololive-shared -name '*.go' | xargs wc -l`)

---

## Phase 10: Rust crate 내부 세분화

> Risk: LOW | 의존: Phase 0/1 완료 권장, 그 외 독립

### P10-1. alarm config 서브모듈화 (675 LOC -> 4개 파일) [우선]

- [x] `crates/alarm/infra/src/config.rs` 내 struct 목록 확인 (21개)
- [x] `crates/alarm/infra/src/config/mod.rs` 생성: pub use + validation (~50 LOC)
- [x] `config/app.rs` 추출: AlarmAppConfig (~100 LOC)
- [x] `config/external.rs` 추출: HolodexConfig, ChzzkConfig, TwitchConfig (~180 LOC)
- [x] `config/internal.rs` 추출: ValkeyConfig, DatabaseConfig, IrisConfig (~150 LOC)
- [x] `config/observability.rs` 추출: HealthConfig, LoggingConfig (~120 LOC)
- [x] 기존 `config.rs` 삭제
- [x] `cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings`

### P10-2. scraper date_extractor 분할 (428 LOC -> 4개 파일) [우선]

- [x] `date_extractor/mod.rs` 내 구조 확인 (Regex, AhoCorasick, 파싱 함수)
- [x] `date_extractor/patterns.rs` 추출: static Regex/LazyLock (~120 LOC)
- [x] `date_extractor/keywords.rs` 추출: AhoCorasick matchers + constants (~80 LOC)
- [x] `date_extractor/scoring.rs` 추출: context scoring + tie-breaker (~78 LOC)
- [x] `mod.rs` 축소: public API + main parse (~150 LOC)
- [x] `cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings`

### P10-3. alarm tier 서브모듈화 (564 LOC -> 3개 파일)

- [x] `crates/alarm/service/src/tier.rs` 내 구조 확인
- [x] `tier/mod.rs` 생성: TieredScheduler + select_due_channels() (~250 LOC)
- [x] `tier/state.rs` 추출: ChannelScheduleState + accessors (~150 LOC)
- [x] `tier/policies.rs` 추출: tier 선택 로직 + interval 계산 (~164 LOC)
- [x] 기존 `tier.rs` 삭제
- [x] `cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings`

### P10-4. alarm scheduler 통합 (499 LOC + 기존 서브모듈)

- [x] 현재 `scheduler.rs` (독립)와 `scheduler/` (서브모듈) 이중 구조 파악
- [x] `scheduler/mod.rs`로 AlarmScheduler struct + public API 통합 (~250 LOC)
- [x] `scheduler/timing.rs` 추출: 시간 유틸 (~80 LOC)
- [x] `scheduler/loops.rs`, `scheduler/health.rs` 기존 유지
- [x] 기존 독립 `scheduler.rs` 삭제
- [x] `cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings`

### P10-5. shared keys 서브모듈화 (498 LOC -> 4개 파일)

- [x] `crates/shared/core/src/keys.rs` 내 함수 목록 확인 (17개)
- [x] `keys/mod.rs` 생성: trait + re-exports (~50 LOC)
- [x] `keys/alarm.rs` 추출: alarm_key(), room-level keys (~120 LOC)
- [x] `keys/channel.rs` 추출: channel_subscribers_key(), type variants (~100 LOC)
- [x] `keys/dedup.rs` 추출: notified_*, claim_*, event_* keys (~150 LOC)
- [x] `keys/helpers.rs` 추출: build_title_fingerprint(), utils (~78 LOC)
- [x] 기존 `keys.rs` 삭제
- [x] `cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings`

### P10-6. alarm template 서브모듈화 (454 LOC -> 3개 파일) [선택]

- [x] `template/mod.rs`: AlarmTemplateRenderer + render_message() (~180 LOC)
- [x] `template/resolve.rs`: resolve_channel_name(), resolve_org_prefix() (~100 LOC)
- [x] `template/format.rs`: truncate_title(), stream_title(), build_url_line() (~174 LOC)
- [x] 기존 `template.rs` 삭제
- [x] `cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings`

### P10-7. alarm dedup fallback 분리 (364 LOC -> 2개 파일) [선택]

- [x] `dedup/mod.rs` 축소: DedupService + primary Valkey claim (~180 LOC)
- [x] `dedup/fallback.rs` 추출: local in-memory dedup (~184 LOC)
- [x] `dedup/tests.rs` 기존 유지 (467 LOC)
- [x] `cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings`

### P10-8. shared observability 서브모듈화 (422 LOC -> 3개 파일) [선택]

- [x] `observability/mod.rs` 생성: re-exports (~50 LOC)
- [x] `observability/logging.rs` 추출: LoggingConfig + log layer setup (~220 LOC)
- [x] `observability/layers.rs` 추출: structured_layer(), json_layer() builders (~152 LOC)
- [x] 기존 `observability.rs` 삭제
- [x] `cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings`

---

## 병렬 실행 가이드 (전체 Phase 0~10 통합)

### 담당자 배분

```
담당자 A (Rust shared):   Phase 0 ──→ Phase 1
담당자 B (Go shared):     Phase 2 step 1-2 ──→ step 3-5 ──→ Phase 3 ──→ Phase 4
담당자 C (CI):            Phase 6 (독립)
담당자 D (Go shared-go):  Phase 5 (독립)
담당자 E (Go 내부):       Phase 7 (독립, 즉시 시작)
담당자 F (Rust 내부):     Phase 10 (독립, Phase 0/1 후 권장)
합류:                     Phase 8 (Phase 2/3 완료 후) ──→ Phase 9 (Phase 8 후)
```

### 의존성 그래프 (전체)

```
[Rust shared 정리]
P0-1 → P0-2 → P0-3 → P0-4
                ↓
P1-1 → P1-2 → P1-3 → P1-4 → P1-5 → P1-6

[Go shared 분리]
P2-1 → P2-2 → P2-3 → P2-4 → P2-5 → P2-6 → P2-7
                                               ↓
                                        P3-1 → ... → P3-6
                                                        ↓
                                                 P4-1 → ... → P4-6

[독립 트랙]
P5-1 ~ P5-5 (독립, 즉시 가능)
P6-1 ~ P6-4 (독립, 즉시 가능)

[Go 내부 분할 — 독립 트랙]
P7-1 → P7-2 (admin/bot api 분할, 병렬 가능)
P7-3 → P7-4 (admin/bot bootstrap 분할, 병렬 가능)
P7-5 (command 구조화, P7-4 후 권장)

[Go 인터페이스화 — Phase 2/3 의존]
P8-1a → P8-1b → P8-1c → P8-1d (순차, 각 인터페이스 추출)
P8-2 (P8-1 완료 후)
P8-3 (독립)
P8-4 (P8-1 완료 후)
P8-5 (최종 검증)

[Go 대형 패키지 이동 — Phase 8 의존]
P9-5 (서브패키지 구조화, P9-2/P9-3 선행)
P9-1 (adapter 이동, Phase 8 후)
P9-2 (majorevent 이동, Phase 8 + P9-5 후)
P9-3 (membernews 이동, Phase 8 + P9-5 후)
P9-4 (youtube 인터페이스, Phase 8 후, P9-1~3과 병렬)
P9-6 (최종 검증)

[Rust 내부 세분화 — 독립 트랙]
P10-1, P10-2 (우선, 병렬 가능)
P10-3, P10-4, P10-5 (P10-1/2 후, 병렬 가능)
P10-6, P10-7, P10-8 (선택, 병렬 가능)
```

### 최대 병렬도 (6 트랙 동시)

```
Track 1 (Rust shared):  P0 → P1
Track 2 (Go shared):    P2 → P3 → P4
Track 3 (shared-go):    P5
Track 4 (CI):           P6
Track 5 (Go 내부):      P7-1 + P7-2 → P7-3 + P7-4 → P7-5
Track 6 (Rust 내부):    P10-1 + P10-2 → P10-3 + P10-4 + P10-5 → P10-6 + P10-7 + P10-8

[합류점]
Track 2 완료 → Track 7 (인터페이스): P8
Track 7 완료 → Track 8 (이동):      P9
```

### 충돌 회피 규칙 (레이스 컨디션 방지)

| 규칙 | 설명 |
|------|------|
| **R1: 동일 파일 편집 금지** | 서로 다른 Track이 같은 파일을 동시 수정하지 않음 |
| **R2: Phase 7 ↔ Phase 2 분리** | P7은 `internal/server/`, `internal/app/` 파일만 수정. P2는 `hololive-shared/pkg/` 파일만 수정. 겹침 없음 |
| **R3: Phase 10 ↔ Phase 0/1 분리** | P10은 crate `src/` 내부 파일만 분할. P0/1은 `Cargo.toml` + 디렉토리 이동. 겹침 없음 |
| **R4: Phase 8은 P2/3 완료 대기** | providers 파일을 P3에서 정리한 후 P8에서 인터페이스화. 동시 수정 방지 |
| **R5: Phase 9는 P8 완료 대기** | 인터페이스 추출 없이 대형 패키지를 이동하면 소비자 코드에서 구체 타입 의존 잔존 |
| **R6: PR 머지 순서** | 같은 모듈 내 복수 PR 시 번호순 머지. 충돌 시 후순위 PR이 rebase |

### Track별 수정 파일 영역 (배타적 소유권)

| Track | 소유 영역 | 절대 건드리지 않는 영역 |
|-------|----------|---------------------|
| Track 1 (P0/P1) | `hololive-rs/Cargo.toml`, `crates/shared/services/` | `crates/*/src/` 내부 |
| Track 2 (P2/P3/P4) | `hololive-shared/pkg/service/{errors,twitch,chzzk,matcher,notification}`, `providers/` | `internal/server/`, `internal/app/` 구조 변경 |
| Track 3 (P5) | `shared-go/pkg/` | hololive-shared, hololive-* internal |
| Track 4 (P6) | `.golangci.yml`, `build-all.sh`, `scripts/` | 소스 코드 |
| Track 5 (P7) | `hololive-admin/internal/server/`, `hololive-admin/internal/app/`, `hololive-kakao-bot-go/internal/server/`, `hololive-kakao-bot-go/internal/app/`, `hololive-kakao-bot-go/internal/command/` | `hololive-shared/`, providers |
| Track 6 (P10) | `hololive-rs/crates/*/src/` 내부 파일 분할 | `Cargo.toml` workspace 구조 |
| Track 7 (P8) | `hololive-shared/pkg/*/interface.go` (신규), `providers/*.go` 반환 타입 | 패키지 이동/삭제 |
| Track 8 (P9) | `hololive-shared/pkg/adapter/`, `hololive-shared/pkg/service/{majorevent,membernews}`, `hololive-kakao-bot-go/internal/adapter/`, `hololive-llm-sched/internal/service/` | 다른 Track 소유 영역 |

---

## 분석 결과 기반 결정 사항

| 항목 | 분석 결과 | 결정 |
|------|----------|------|
| admin/bot api.go 중복 | 동일 구조 (1,750/1,754 LOC) | 각각 분할 후 공통 패턴은 Phase 8에서 인터페이스로 처리 |
| command/ 디렉토리 도입 | Go 패키지=디렉토리로 import path 변경 대량 발생 | 파일 접두사 컨벤션 우선 검토, 서브디렉토리는 효과 확인 후 |
| adapter 이동 | bot 전용이나 admin/youtube에서 소량 참조 | 인터페이스 추출(P8) 선행 후 이동(P9) |
| majorevent/membernews 이동 | llm-sched 주 소비자, kakao-bot도 command에서 참조 | 소비자 재확인 필수, 인터페이스 추출 선행 |
| youtube 모듈 분리 | 3개 서비스 소비, 복잡도 높음 | 즉시 분리 보류, 인터페이스 경계만 강화 |
| Rust config.rs 675 LOC | 21개 struct 혼재 | 서브모듈 분할 (Track 6, 즉시 가능) |
| Rust 대형 테스트 (700+ LOC) | checker/tests 756, link_checker/tests 777, dispatch/tests 702 | 현재 유지, 향후 integration suite 분리 고려 |
