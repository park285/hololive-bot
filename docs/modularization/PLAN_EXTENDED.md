# Hololive Bot 확장 모듈화 계획 (Phase 7~10)

> 작성일: 2026-03-03
> 범위: 서비스 내부 구조 분할, 서비스 간 계약 인터페이스화, Go 모듈 신설, Rust crate 내부 세분화
> 전제: `PLAN.md` Phase 0~6 (shared 분리)과 독립 실행 가능하나, Phase 2/3 완료 후 효과 극대화
> 전략: Phase 0~6과 동일 (Incremental Strangler, Phase별 독립 PR, 기능/구조 혼합 금지)

---

## 분석 근거 요약

| 영역 | 핵심 발견 | 출처 |
|------|----------|------|
| Go 서비스 내부 | admin `api.go` 1,750 LOC, bot `api_bot.go` 1,754 LOC (god object) | 구조 분석 |
| Go 서비스 간 | 교차 import 0건 (양호), 구체 타입 결합 과다, interface 25+개 존재하나 providers 반환 타입이 구체 | 의존성 분석 |
| Go 모듈 분리 | adapter(3.9K, bot 전용), majorevent(5.5K), membernews(5K) 이동 후보 | 모듈 분석 |
| Rust 내부 | alarm config.rs 675 LOC, tier.rs 564 LOC, keys.rs 521 LOC 등 9개 파일 분할 후보 | crate 분석 |

---

## Phase 7: Go 서비스 내부 God Object 분할

- **Risk:** LOW (파일 분할, API 변경 없음)
- **의존:** 없음 (Phase 0~6과 완전 병렬)

### P7-1. admin `api.go` 분할 (1,750 LOC -> 9개 파일)

현재 단일 파일에 AuthHandler + 9개 도메인 핸들러가 혼재.

| 분할 파일 | 대상 핸들러 | 예상 LOC |
|-----------|-----------|---------|
| `api_auth.go` | AuthHandler (Register/Login/Refresh/Reset) | ~260 |
| `api_member.go` | MemberAPIHandler | ~200 |
| `api_profile.go` | ProfileAPIHandler | ~150 |
| `api_stream.go` | StreamAPIHandler | ~200 |
| `api_room.go` | RoomAPIHandler | ~150 |
| `api_alarm.go` | AlarmAPIHandler | ~180 |
| `api_settings.go` | SettingsAPIHandler | ~150 |
| `api_template.go` | TemplateAPIHandler | ~150 |
| `api_milestone_majorevent.go` | MilestoneAPIHandler, MajorEventAPIHandler, OAuthAPIHandler | ~310 |

잔여 `api.go`: 핸들러 구조체 정의 + 공통 미들웨어만 잔존 (~100 LOC)

### P7-2. bot `api_bot.go` 분할 (1,754 LOC -> 동일 패턴)

admin과 동일 구조이므로 동일 분할 패턴 적용.

### P7-3. admin `bootstrap_admin.go` 분할 (780 LOC -> 3개 파일)

| 분할 파일 | 책임 | 예상 LOC |
|-----------|------|---------|
| `bootstrap_admin.go` | 핵심 진입점 + config 로드 | ~250 |
| `bootstrap_services.go` | 서비스 계층 DI (alarm, youtube, holodex 등) | ~300 |
| `bootstrap_routes.go` | 라우터 등록 + 미들웨어 설정 | ~230 |

### P7-4. bot `bootstrap.go` 통합 정리 (726 LOC + 5개 보조 파일)

현재 `bootstrap*.go` 6개 파일이 기능별이 아닌 임시 분할 상태.

| 통합 후 파일 | 책임 | 예상 LOC |
|-------------|------|---------|
| `bootstrap_core.go` | infra (DB, cache, config) 초기화 | ~280 |
| `bootstrap_services.go` | 서비스 계층 DI | ~300 |
| `bootstrap_bot.go` | Bot 생성 + command router 바인딩 | ~200 |

### P7-5. bot `command/` 패키지 디렉토리 구조화 (28개 파일)

현재 flat 구조 -> 역할별 서브디렉토리:

```
command/
  handlers/     alarm.go, live.go, upcoming.go, schedule.go, subscriber.go, stats.go, major_event.go
  info/         member_info.go, member_info_directory.go, member_info_resolver.go, member_info_request.go
  news/         member_news.go, member_news_subscription.go
```

**주의**: Go 패키지 = 디렉토리이므로 서브디렉토리 도입 시 import path 변경 발생. 대안으로 파일 접두사 컨벤션(`handler_*.go`, `info_*.go`) 유지도 검토.

### 검증

```bash
cd hololive
for mod in hololive-admin hololive-kakao-bot-go; do
  (cd "$mod" && go build ./... && go vet ./... && golangci-lint run ./... && go test ./...)
done
```

---

## Phase 8: Go 서비스 간 계약 인터페이스화

- **Risk:** MED (providers 반환 타입 변경, 소비자 코드 수정)
- **의존:** Phase 2/3 완료 권장 (providers 정리 후 진행이 효율적)

### 현황

- 교차 서비스 import: 0건 (아키텍처 위반 없음, 우수)
- `contracts/` 패키지: 3개 계약 존재 (alarm, iris, trigger)
- **문제**: providers 반환 타입이 구체 struct (`*cache.Service`, `*database.PostgresService` 등)

### P8-1. 인프라 인터페이스 추출 (High Priority)

| 대상 | 현재 반환 타입 | 추출 인터페이스 | 소비자 수 |
|------|--------------|---------------|----------|
| Cache | `*cache.Service` | `cache.Client` | 4 모듈 |
| Database | `*database.PostgresService` | `database.Client` | 4 모듈 |
| Member | `*member.ServiceAdapter` | `member.DataProvider` | 3 모듈 |
| Settings | `*settings.Service` | `settings.Reader` / `settings.Writer` | 2 모듈 |

각 인터페이스는 해당 패키지 내 `interface.go`에 정의.

### P8-2. providers 반환 타입 인터페이스화

```go
// Before
func ProvideCacheService(resources *bootstrap.CacheResources) *cache.Service

// After
func ProvideCacheService(resources *bootstrap.CacheResources) cache.Client
```

모든 소비자가 인터페이스 타입으로 수신하도록 변경.

### P8-3. contracts 패키지 확장

| 신규 계약 | 목적 |
|----------|------|
| `contracts/delivery/` | Outbox 메시지 포맷 (Go ↔ Rust 미래 확장용) |
| `contracts/settings/` | Settings Pub/Sub 메시지 포맷 |

### P8-4. Mock 생성 기반 구축

- `go generate` + `mockgen` 또는 수동 mock으로 인터페이스 기반 테스트 전환
- 기존 테스트에서 구체 타입 직접 생성 -> interface mock 주입 패턴으로 전환

### 검증

```bash
cd hololive
for mod in hololive-admin hololive-kakao-bot-go hololive-llm-sched hololive-stream-ingester hololive-shared; do
  (cd "$mod" && go build ./... && go vet ./... && golangci-lint run ./... && go test ./...)
done
```

---

## Phase 9: Go 대형 패키지 이동 / 모듈 재배치

- **Risk:** MED-HIGH (대형 패키지 이동, import path 대량 변경)
- **의존:** Phase 2/3 완료 필수 (providers 정리 후), Phase 8 권장

### P9-1. adapter 패키지 -> kakao-bot 이동 (3,884 LOC)

**근거**: KakaoTalk 플랫폼에 완전히 종속. bot이 유일한 실질 소비자.

| 현재 | 이동 후 |
|------|---------|
| `hololive-shared/pkg/adapter/` | `hololive-kakao-bot-go/internal/adapter/` |

**부수 작업**:
- admin에서 adapter 참조 시 -> shared에 얇은 인터페이스(`MessageFormatter`) 잔존, bot에서 구현
- youtube/scheduler.go에서 adapter 사용 부분 -> 인터페이스 주입으로 전환

### P9-2. majorevent 패키지 -> llm-sched 이동 검토 (5,458 LOC)

**근거**: LLM 의존성 강함, 주 소비자가 llm-sched.

| 현재 | 이동 후 |
|------|---------|
| `hololive-shared/pkg/service/majorevent/` | `hololive-llm-sched/internal/service/majorevent/` |

**주의**: admin에서 trigger proxy로 사용 중 -> `contracts/trigger/` 경유 인터페이스로 분리 필수.

### P9-3. membernews 패키지 -> llm-sched 이동 검토 (5,048 LOC)

**근거**: majorevent과 유사 패턴 (LLM 요약, monthly digest), 주 소비자 llm-sched.

| 현재 | 이동 후 |
|------|---------|
| `hololive-shared/pkg/service/membernews/` | `hololive-llm-sched/internal/service/membernews/` |

**주의**: kakao-bot command에서도 참조 -> 인터페이스 추출 필수.

### P9-4. youtube 패키지 인터페이스 경계 강화 (독립 모듈은 보류)

**근거**: 3개 서비스에서 사용, 복잡도 높음. 즉시 모듈 분리보다 인터페이스 정의 우선.

작업:
1. `youtube.Scheduler` public API 인터페이스 정의
2. `youtube.Service` 인터페이스 정의
3. `stats_repository_*.go` 8개 파일 -> `youtube/stats/` 서브패키지로 승격

### P9-5. shared 내부 대형 패키지 서브패키지 구조화

| 패키지 | 현재 | 구조화 후 |
|--------|------|----------|
| `majorevent/` (이동 전 정리) | 19개 flat 파일 | `summarizer/`, `scheduler/` 서브패키지 |
| `membernews/` (이동 전 정리) | 18개 flat 파일 | `summarizer/`, `scheduler/`, `filter/` 서브패키지 |

### 검증

```bash
cd hololive
for mod in hololive-kakao-bot-go hololive-shared hololive-admin hololive-llm-sched hololive-stream-ingester; do
  (cd "$mod" && go build ./... && go vet ./... && golangci-lint run ./... && go test ./...)
done
```

---

## Phase 10: Rust crate 내부 세분화

- **Risk:** LOW (모듈 내부 분할, public API 변경 없음)
- **의존:** Phase 0/1 완료 권장, 그 외 독립

### P10-1. alarm config 서브모듈화 (675 LOC -> 4개 파일) [P1 우선]

`crates/alarm/infra/src/config.rs`에 21개 config struct 혼재.

```
config/
  mod.rs          (~50 LOC) pub use + validation
  app.rs          (~100 LOC) AlarmAppConfig
  external.rs     (~180 LOC) HolodexConfig, ChzzkConfig, TwitchConfig
  internal.rs     (~150 LOC) ValkeyConfig, DatabaseConfig, IrisConfig
  observability.rs (~120 LOC) HealthConfig, LoggingConfig
```

### P10-2. scraper date_extractor 분할 (428 LOC -> 4개 파일) [P1 우선]

`crates/scraper/service/src/date_extractor/mod.rs`에 Regex + 파싱 + 키워드매칭 혼재.

```
date_extractor/
  mod.rs          (~150 LOC) public API + main parse
  patterns.rs     (~120 LOC) static Regex/LazyLock
  keywords.rs     (~80 LOC) AhoCorasick matchers
  scoring.rs      (~78 LOC) context scoring + tie-breaker
```

### P10-3. alarm tier 서브모듈화 (564 LOC -> 3개 파일) [P2]

```
tier/
  mod.rs          (~250 LOC) TieredScheduler + select_due_channels()
  state.rs        (~150 LOC) ChannelScheduleState + accessors
  policies.rs     (~164 LOC) tier 선택 로직 + interval 계산
```

### P10-4. alarm scheduler 통합 (499 LOC + 기존 서브모듈) [P2]

현재 `scheduler.rs`(독립)와 `scheduler/`(서브모듈) 이중 구조 -> 통합:

```
scheduler/
  mod.rs          (~250 LOC) AlarmScheduler struct + public API
  loops.rs        (201 LOC) 기존 유지
  timing.rs       (~80 LOC) 시간 유틸 추출
  health.rs       (79 LOC) 기존 유지
```

### P10-5. shared keys 서브모듈화 (498 LOC -> 4개 파일) [P2]

`crates/shared/core/src/keys.rs`에 17개 key builder 함수 혼재.

```
keys/
  mod.rs          (~50 LOC) trait + re-exports
  alarm.rs        (~120 LOC) alarm_key(), room-level keys
  channel.rs      (~100 LOC) channel_subscribers_key(), type variants
  dedup.rs        (~150 LOC) notified_*, claim_*, event_* keys
  helpers.rs      (~78 LOC) build_title_fingerprint(), utils
```

### P10-6. alarm template 서브모듈화 (454 LOC -> 3개 파일) [P3]

```
template/
  mod.rs          (~180 LOC) AlarmTemplateRenderer + render_message()
  resolve.rs      (~100 LOC) resolve_channel_name(), resolve_org_prefix()
  format.rs       (~174 LOC) truncate_title(), stream_title(), build_url_line()
```

### P10-7. alarm dedup fallback 분리 (364 LOC -> 2개 파일) [P3]

```
dedup/
  mod.rs          (~180 LOC) DedupService + primary Valkey claim
  fallback.rs     (~184 LOC) local in-memory dedup (outage handling)
  tests.rs        (467 LOC) 기존 유지
```

### P10-8. observability 서브모듈화 (422 LOC -> 3개 파일) [P3]

```
observability/
  mod.rs          (~50 LOC) re-exports
  logging.rs      (~220 LOC) LoggingConfig + log layer setup
  layers.rs       (~152 LOC) structured_layer(), json_layer() builders
```

### 검증

```bash
cd hololive/hololive-rs
cargo build --workspace && cargo test --workspace && cargo clippy --workspace -- -D warnings
```

---

## 일정 요약 (Phase 7~10)

```
Week 5:  Phase 7 P7-1~P7-2 (god object 분할) + Phase 10 P10-1~P10-2 (Rust P1)
Week 6:  Phase 7 P7-3~P7-5 (bootstrap/command 정리) + Phase 10 P10-3~P10-5 (Rust P2)
Week 7:  Phase 8 (인터페이스 추출 + providers 전환)
Week 8:  Phase 9 P9-1 (adapter 이동) + Phase 10 P10-6~P10-8 (Rust P3)
Week 9:  Phase 9 P9-2~P9-3 (majorevent/membernews 이동)
Week 10: Phase 9 P9-4~P9-5 (youtube 인터페이스 + 서브패키지 구조화)
```

### 병렬 실행 가능 조합

```
Phase 7 (독립, 즉시 가능)
Phase 10 (독립, Phase 0/1 후 권장)
Phase 8 (Phase 2/3 완료 후)
Phase 9 (Phase 2/3/8 완료 후)
```

### Phase 0~6과의 통합 의존 그래프

```
[기존]                          [확장]
Phase 0 ──→ Phase 1             Phase 7  (독립)
Phase 2 ──→ Phase 3 ──→ Phase 4 Phase 10 (독립)
Phase 5 (독립)                       ↓
Phase 6 (독립)              Phase 8 (Phase 2/3 후)
                                     ↓
                            Phase 9 (Phase 8 후)
```

---

## 확장 성공 지표 (Definition of Done)

| # | 지표 | 측정 |
|---|------|------|
| 7 | Go god object 0개 (500+ LOC 단일 핸들러 파일 없음) | `wc -l` |
| 8 | providers 반환 타입 100% 인터페이스화 | grep `func Provide` 반환타입 확인 |
| 9 | adapter 패키지 kakao-bot internal 이동 완료 | import path 확인 |
| 10 | Rust 500+ LOC 비테스트 파일 0개 (config.rs 등 분할 완료) | `wc -l` |
| 11 | 전체 테스트 회귀 0건 | CI green |

---

## 리스크 및 대응

| 리스크 | Phase | 대응 |
|--------|-------|------|
| P7 파일 분할 시 unexported 함수 접근 깨짐 | 7 | 같은 패키지 내 분할이므로 접근성 유지 (Go 패키지 = 디렉토리) |
| P8 인터페이스 추출 시 기존 테스트 깨짐 | 8 | 구체 타입 -> 인터페이스 단계적 전환, mock 병행 도입 |
| P9 대형 패키지 이동 시 circular import | 9 | 인터페이스 계약(Phase 8) 선행 필수, trigger 경유 패턴 유지 |
| P9 majorevent/membernews 이동 후 llm-sched 비대화 | 9 | 서브패키지 구조화(P9-5) 선행, `internal/service/` 하위 격리 |
| P10 Rust 모듈 분할 시 pub(crate) 접근성 변경 | 10 | mod.rs에서 pub use re-export, crate 내부 API 유지 |
| command/ 서브디렉토리 도입 시 import path 대량 변경 | 7 | 파일 접두사 컨벤션 대안 검토 (서브패키지 없이 논리적 그룹화) |
