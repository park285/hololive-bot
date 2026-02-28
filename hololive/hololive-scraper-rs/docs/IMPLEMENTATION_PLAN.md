# hololive-scraper-rs 구현 계획서 (Phase 0~2)

> `hololive-kakao-bot-go` 모놀리스에서 MajorEvent Scraper + LinkChecker를 분리하여
> Rust 마이크로서비스로 재구현하는 Phase 0~2 구현 계획서.

---

## 목차

### 개요 및 공통
1. [프로젝트 개요](#1-프로젝트-개요)
2. [테스트 전략](#2-테스트-전략)
3. [리스크 및 대응](#3-리스크-및-대응)
4. [의존성 버전 매트릭스](#4-의존성-버전-매트릭스)

### Phase별 구현 계획
- [Phase 0: 인프라 준비](PHASE_0_INFRASTRUCTURE.md)
- [Phase 1: MajorEvent Scraper](PHASE_1_MAJOREVENT_SCRAPER.md)
- [Phase 2: LinkChecker](PHASE_2_LINKCHECKER.md)

### 작업 추적
- [종합 TODO 리스트](TODO.md) -- 76개 항목, 의존관계 그래프 포함

---

## 1. 프로젝트 개요

### 1.1 목적

`hololive-kakao-bot-go`의 `internal/service/majorevent/` 패키지 내 스크래핑 컴포넌트(Scraper, RSSParser, DateExtractor, LinkChecker, ScraperScheduler)를 독립 Rust 마이크로서비스로 분리한다.
Go 봇은 알림/포매팅/API/명령어 처리에만 집중하고, 스크래핑 로직은 Rust 서비스가 동일 DB를 직접 읽고 쓰는 구조로 전환한다.

### 1.2 범위

| Phase | 내용 | 산출물 |
|-------|------|--------|
| Phase 0 | 인프라 (DB user, Cargo workspace, Docker, CI) | 빌드 가능한 skeleton + health endpoint |
| Phase 1 | MajorEvent Scraper (RSS parse, date extract, upsert) | Go 동치 scrape + store 파이프라인 |
| Phase 2 | LinkChecker (HEAD/GET probe, private IP blocking) | stale link 재검증 파이프라인 |

### 1.3 기술 스택 결정사항

| 항목 | 결정 |
|------|------|
| Rust Edition | 2024 |
| Toolchain | nightly (latest) |
| Feature flag | 없음 -- 완전 cutover 후 Go scraper 코드 deprecate |
| Go 봇 변경 | Phase 0~2 범위에서 없음 |
| DB 접근 | Rust 서비스가 `major_events` 테이블 직접 R/W (동일 `holo-postgres`) |
| SOCKS5 Proxy | VPN scraper proxy (`vpn-scraper-proxy:1080`) 경유 |
| 스케줄링 | Rust 내부 tokio 기반 scheduler (cron 사용 안 함) |

---

## 2. 테스트 전략

### 2.1 Go testdata 공유 방법

Go `hololive-kakao-bot-go/internal/service/majorevent/testdata/` 디렉토리의 파일을 Rust `crates/scraper/service/testdata/`에 복사한다.

| 파일 | 용도 |
|------|------|
| `events_feed.xml` | RSS parser 테스트 (3개 RSS 아이템) |
| `supernova_reboot_real.html` | DateExtractor 실제 HTML 테스트 (76줄) |

복사 후 동일 파일을 유지하되, 변경 시 양쪽을 동기화한다. CI에서 파일 checksum을 비교하여 drift를 감지할 수 있다.

### 2.2 단위 테스트 목록 (파일별)

| 파일 | 테스트 수 | 포팅 원본 |
|------|----------|----------|
| `rss_parser.rs` | 6 | `rss_parser_test.go` |
| `date_extractor.rs` | 18 | `date_extractor_test.go` |
| `scraper.rs` | 8+ | `scraper_test.go` + `scraper_partial_feed_test.go` |
| `link_checker.rs` | 9 | `link_checker_test.go` |
| `scheduler.rs` | 5+ | `scraper_scheduler_test.go` + `scraper_scheduler_retry_window_test.go` |
| `repository.rs` | 4+ | `repository_test.go` |
| `model.rs` | 3+ | `major_event_test.go` |

**총계**: 약 53개 이상

### 2.3 통합 테스트 (실제 DB)

`cargo test --features integration` 또는 `INTEGRATION_TEST=true cargo test`:

1. **Repository CRUD**: 실제 PostgreSQL 대상 upsert/query 검증
2. **Full pipeline**: RSS fetch (mock) -> parse -> date extract -> upsert -> query 검증
3. **LinkChecker E2E**: 실제 URL 대상 HEAD/GET (네트워크 의존 -- CI에서 optional)

통합 테스트 DB 환경:
```
SCRAPER__DATABASE__HOST=localhost
SCRAPER__DATABASE__PORT=5432
SCRAPER__DATABASE__NAME=hololive_test
SCRAPER__DATABASE__USER=hololive_scraper
SCRAPER__DATABASE__PASSWORD=test
```

### 2.4 Go vs Rust 결과 비교 자동화

```bash
#!/bin/bash
# scripts/cross_validate.sh

# 1. Go scraper dry-run (DB에 저장 없이 파싱 결과만 JSON 출력)
go test -run TestCrossValidation -v -json \
    ./internal/service/majorevent/ > /tmp/go_results.json

# 2. Rust scraper dry-run
cargo test -p scraper-service --test cross_validation -- --nocapture \
    > /tmp/rust_results.json

# 3. diff
diff <(jq -S . /tmp/go_results.json) <(jq -S . /tmp/rust_results.json)
```

DateExtractor 전용 cross-validation 픽스처 형식:

```json
[
  {
    "name": "supernova_reboot",
    "input_file": "testdata/supernova_reboot_real.html",
    "expected_dates": ["2026-03-08", "2026-03-09"]
  }
]
```

---

## 3. 리스크 및 대응

### 3.1 DateExtractor 파싱 불일치

| 리스크 | 확률 | 영향 | 대응 |
|--------|------|------|------|
| Regex 엔진 차이 (Go RE2 vs Rust) | 중 | 높 | 동일 테스트 케이스 + cross-validation |
| NFKC 정규화 차이 | 낮 | 높 | Go `golang.org/x/text/unicode/norm` vs Rust `unicode-normalization` -- 둘 다 Unicode 표준 구현이므로 차이 없어야 함. Edge case 확인 필요. |
| UTF-8 바이트 오프셋 vs 문자 오프셋 | 높 | 높 | Go `strings.Index`는 byte 오프셋, Rust `str::find`도 byte 오프셋. 일치하지만 일본어 문자열에서 바이트 위치가 클러스터링에 영향을 줄 수 있음. `clusterGap=150`은 byte 단위인지 확인 필요. |
| 멀티바이트 키워드 거리 계산 | 중 | 중 | Go/Rust 모두 byte offset 기반이므로 동치. 단, `strings.ToLower`와 `str::to_lowercase`의 일본어 처리 차이 확인. |

**완화 전략**:
- Phase 1 완료 후 실제 RSS feed 100건에 대해 Go/Rust 결과 전수 비교
- 불일치 건은 `cross_validation_diff.json`에 기록하여 수동 검토

### 3.2 reqwest SOCKS5 호환성

| 리스크 | 확률 | 영향 | 대응 |
|--------|------|------|------|
| SOCKS5 프록시 인증/프로토콜 불일치 | 낮 | 높 | reqwest의 `socks` 기능은 tokio-socks 기반. `vpn-scraper-proxy:1080`은 인증 없는 SOCKS5이므로 호환 문제 없을 것. Docker 네트워크 내에서 연결 테스트 필수. |
| 프록시 경유 DNS 해석 | 중 | 중 | SOCKS5는 기본적으로 remote DNS. reqwest의 `Proxy::all`은 이를 지원. LinkChecker의 `validate_host`에서 DNS 해석은 프록시 경유가 아닌 직접 해석이므로, 프록시 경유 요청과 별도로 동작한다. |

### 3.3 sqlx 타입 매핑

| PostgreSQL 타입 | Rust 타입 | 주의 |
|----------------|-----------|------|
| `TEXT[]` | `Vec<String>` | sqlx는 `Vec<String>` <-> `text[]` 매핑을 기본 지원. `COALESCE(members, '{}')` 처리 필요. |
| `TIMESTAMPTZ` | `DateTime<Utc>` | sqlx `chrono` feature로 직접 매핑. |
| `DATE` | `NaiveDate` | sqlx `chrono` feature로 직접 매핑. |
| `SERIAL` (id) | `i32` | 기본 매핑. |
| `VARCHAR` (status enums) | `String` | enum을 String으로 바인딩 후 변환. sqlx `Type` derive로 직접 매핑 시도 가능하나, 기존 DB의 varchar 값과 호환성을 위해 String 우선 사용. |

### 3.4 기타 리스크

| 리스크 | 대응 |
|--------|------|
| `content:encoded` XML 네임스페이스 파싱 실패 | quick-xml가 네임스페이스를 올바르게 처리하는지 검증. 실패 시 raw XML 파싱으로 fallback. |
| Nightly Rust 컴파일러 breaking change | `rust-toolchain.toml`에 특정 nightly 날짜 고정 가능 (예: `nightly-2026-02-20`). CI에서 latest nightly 빌드도 병렬 실행. |
| Memory 128MB 제한 내 동작 | RSS feed + HTML 파싱은 메모리 집약적이지 않으나, 큰 feed (20 pages x 10 items x HTML) 시 peak 확인 필요. Rust의 zero-copy 파싱으로 Go 대비 낮을 것으로 예상. |

---

## 4. 의존성 버전 매트릭스

| Crate | Version | 용도 | 사용 crate |
|-------|---------|------|-----------|
| `tokio` | 1.44 | Async runtime | all |
| `tokio-util` | 0.7 | CancellationToken | scraper-service, scraper-app |
| `reqwest` | 0.12 | HTTP client (SOCKS5, rustls) | scraper-service, scraper-app |
| `axum` | 0.8 | Health endpoint | scraper-app |
| `tower-http` | 0.6 | HTTP middleware (trace) | scraper-app |
| `sqlx` | 0.8 | PostgreSQL (async, chrono, macros) | scraper-core, scraper-infra |
| `serde` | 1.0 | Serialization | all |
| `serde_json` | 1.0 | JSON | scraper-core |
| `quick-xml` | 0.37 | RSS XML deserialization | scraper-service |
| `regex` | 1.11 | Date pattern matching | scraper-service |
| `unicode-normalization` | 0.1 | NFKC normalization | scraper-service |
| `chrono` | 0.4 | Date/time (serde feature) | all |
| `chrono-tz` | 0.10 | Timezone (KST) | scraper-service |
| `config` | 0.15 | TOML + env config | scraper-infra |
| `clap` | 4.5 | CLI args | scraper-app |
| `tracing` | 0.1 | Structured logging | all |
| `tracing-subscriber` | 0.3 | Log subscriber (env-filter, json) | scraper-app |
| `opentelemetry` | 0.28 | OTEL traces (Phase 0에서는 stub) | scraper-app |
| `thiserror` | 2.0 | Error types | scraper-core, scraper-infra |
| `url` | 2.5 | URL parsing (canonical key) | scraper-service |
| ~~`once_cell`~~ | - | **불필요** -- nightly `std::sync::LazyLock` 사용 | - |
| `anyhow` | 1.0 | Application error handling | scraper-app |
| `rand` | 0.9 | Retry jitter | scraper-service |

> `scraper` crate (HTML, 0.22)는 Phase 0~2에서 사용하지 않는다. DateExtractor는 regex 기반 HTML 파싱을 사용한다.
> 향후 description에서 구조화된 데이터 추출이 필요할 경우 추가한다.
