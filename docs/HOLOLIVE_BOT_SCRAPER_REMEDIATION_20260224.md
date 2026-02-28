# Hololive Bot + Scraper RS Remediation (2026-02-24)

## 목표
- 대상: `hololive-kakao-bot-go`, `hololive-scraper-rs`
- 제외: EXA API key 전달 방식(MCP 사용) 이슈
- 해결 범위:
  - SSRF/redirect 검증 강화
  - 성능 병목 완화
  - 구형/중복 라이브러리 정리
  - 보안 스캔 경고 해소(가능 범위)

---

## 팀 구성(실행 단위)

### 1) Security/Network Track
- 담당: Link checker SSRF/redirect hardening
- 소유 파일:
  - `hololive-kakao-bot-go/internal/service/majorevent/link_checker.go`
  - `hololive-kakao-bot-go/internal/service/majorevent/link_checker_test.go`
  - `hololive-scraper-rs/crates/scraper/service/src/link_checker/mod.rs`

### 2) Performance/Data Track
- 담당: DB/Cache 병목 완화
- 소유 파일:
  - `hololive-kakao-bot-go/internal/service/notification/alarm_check.go`
  - `hololive-kakao-bot-go/internal/service/majorevent/scraper.go`
  - `hololive-scraper-rs/crates/scraper/app/src/bootstrap.rs`

### 3) Dependency/Supply-chain Track
- 담당: 취약·구형 의존성 정리, audit 기준 정비
- 소유 파일:
  - `hololive-kakao-bot-go/go.mod`, `go.sum`
  - `hololive-scraper-rs/Cargo.toml`, `Cargo.lock`, `.cargo/audit.toml`
  - `hololive-scraper-rs/crates/alarm/infra/src/circuit_breaker.rs`

---

## 적용 결과

## A. hololive-kakao-bot-go

### A-1) SSRF redirect 우회 차단
- 변경:
  - redirect hop 마다 `CheckRedirect`에서 host/IP 재검증 수행
  - redirect 중 차단 오류를 `Blocked` 상태로 정확 분류
- 효과:
  - 초기 URL 검증만 통과하고 redirect로 내부망 진입하는 우회 차단

### A-2) Alarm Redis RTT 병목 완화
- 변경:
  - 채널별 `SMEMBERS` 반복 호출 → `DoMulti` 파이프라인 배치 조회
  - 구독자 없는 채널의 registry 정리도 배치 `SRem` 적용
- 효과:
  - 대규모 채널 환경에서 Redis 왕복 수 감소

### A-3) MajorEvent 저장 병렬화
- 변경:
  - 이벤트 upsert를 `errgroup` limit 기반 병렬 처리
  - 설정값 추가: `constants.MajorEventConfig.ScrapeUpsertConcurrency`
- 효과:
  - 이벤트 수 증가 시 저장 단계 처리량 개선

### A-4) Go direct dependency patch update
- `github.com/openai/openai-go/v3`: `v3.22.0 -> v3.23.0`
- `google.golang.org/api`: `v0.267.0 -> v0.268.0`

---

## B. hololive-scraper-rs

### B-1) recloser 경로 제거(unsound 경고 해소)
- 변경:
  - `alarm-infra`에서 `failsafe/recloser` 의존 경로 제거
  - 자체 circuit breaker 상태머신(Closed/Open/HalfOpen)으로 대체
- 효과:
  - `crossbeam-utils/memoffset` unsound 경고 유입 경로 제거

### B-2) DNS 라이브러리 현대화
- 변경:
  - `trust-dns-resolver` → `hickory-resolver 0.25.2`
  - 새 API(`TokioResolver` builder)로 코드 반영
- 효과:
  - 구형 DNS 스택 교체

### B-3) reqwest 중복 스택 제거
- 변경:
  - `opentelemetry-otlp`에 `default-features = false` 적용
- 효과:
  - `reqwest 0.12 + 0.13` 중복 해소 (`reqwest 0.13.2` 단일화)

### B-4) Link checker concurrency 정합
- 변경:
  - link checker concurrency를 DB `max_connections` 기반으로 설정
- 효과:
  - 기본값 불일치(16 vs 5)로 인한 DB 압박 완화

### B-5) audit 기준/잠금 정리
- 변경:
  - yanked 경고 대상 업데이트:
    - `js-sys 0.3.88 -> 0.3.89`
    - `wasm-bindgen 0.2.111 -> 0.2.112`
    - 관련 wasm 계열 동반 업데이트
  - `.cargo/audit.toml` ignore 목록 축소
    - `RUSTSEC-2024-0421` 제거(마이그레이션 완료)
    - `RUSTSEC-2023-0071`만 임시 유지(고정 릴리스 부재, lockfile optional 경로 감지)

---

## 검증 결과

### Go
- `make lint` ✅
- `go test ./...` ✅
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` ✅ (No vulnerabilities found)

### Rust
- `cargo fmt --all -- --check` ✅
- `cargo clippy --all-targets --all-features -- -D warnings` ✅
- `cargo test --workspace` ✅
- `cargo audit -D warnings` ✅

---

## 남은 리스크 / 운영 메모
- `RUSTSEC-2023-0071`은 upstream에서 fixed stable release가 없어 완전 제거 불가.
- 현재는 lockfile optional graph에서 감지되는 케이스로 운영 경로(PostgreSQL only)와 분리되어 있으나,
  upstream fixed release 공개 시 즉시 ignore 제거 + 재검증 필요.
