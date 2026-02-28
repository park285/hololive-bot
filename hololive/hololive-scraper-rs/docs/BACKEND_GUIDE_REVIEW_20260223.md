# Backend Guide 준수 점검 보고서 (2026-02-23)

## 1) 점검 범위

- 대상 프로젝트: `hololive-scraper-rs`
- 기준 커밋 맥락: `a78c9a855` 이후 현재 `main`
- 점검 기준: `backend-guide` skill 규칙
  - 중점 규칙: `S3`, `S5`, `R4`, `R3`, `A4`, `Q2`, `A1`, `A2`, `D3`

## 2) 실행 검증 결과

아래 명령은 2026-02-23 기준 로컬에서 재실행해 통과를 확인했다.

```bash
cd /home/kapu/gemini/llm/hololive-scraper-rs
cargo test --all
cargo clippy --all-targets --all-features -- -D warnings
cargo test -p scraper-service --test link_checker_integration -- --ignored
```

- `cargo test --all`: PASS
- `cargo clippy --all-targets --all-features -- -D warnings`: PASS
- `link_checker_integration --ignored`: PASS (실 URL 2개 테스트)

## 3) 점검 결론

- 결론: `REQUEST CHANGES`
- 사유: 핵심 구조/테스트는 양호하나, 보안/신뢰성에서 운영 차단급 이슈 2건 존재

## 4) Must-fix (차단 이슈)

### 4.1 [HIGH][S3,S5] Link check SSRF 우회 가능성

- 현상
  - `validate_host()`로 선검증 후 실제 HTTP 요청 시 redirect 및 재해석 경로를 추가 제어하지 않는다.
  - DNS rebinding/redirect 체인으로 내부망 접근 가능성이 남는다.
- 근거 위치
  - `crates/scraper/service/src/link_checker.rs:254`
  - `crates/scraper/service/src/link_checker.rs:302`
  - `crates/scraper/service/src/link_checker.rs:96`
  - `crates/scraper/service/src/link_checker.rs:367`
- 권장 조치
  - redirect policy를 명시적으로 제한하거나 금지한다.
  - redirect hop마다 host/IP 재검증을 적용한다.
  - 필요 시 DNS pinning 전략 또는 별도 outbound allowlist를 적용한다.

### 4.2 [HIGH][R4,R3] Scraper 본 요청 타임아웃 부재

- 현상
  - link checker는 timeout(기본 8s)을 두지만, scraper RSS fetch 경로는 timeout이 설정되지 않는다.
  - 외부 I/O hang 시 `run_cycle()` 지연 및 shutdown 지연 위험이 있다.
- 근거 위치
  - `crates/scraper/app/src/main.rs:148`
  - `crates/scraper/service/src/scraper.rs:376`
  - `crates/scraper/service/src/scheduler.rs:89`
- 권장 조치
  - scraper용 request timeout/connect timeout/read timeout을 설정한다.
  - scheduler 관점에서 cycle 최대 실행시간 가드(취소/격리)를 둔다.

## 5) Should-fix (개선 권장)

### 5.1 [MEDIUM][A4] Health/readiness 신호 불충분

- 현상
  - `/health`는 DB 연결 여부만 반영하며 scheduler disable 상태를 readiness에 반영하지 않는다.
- 근거 위치
  - `crates/scraper/app/src/main.rs:118`
  - `crates/scraper/app/src/main.rs:184`
- 권장 조치
  - liveness/readiness 분리 및 scheduler 상태 신호 추가

### 5.2 [MEDIUM][Q2,S4] 디버그 로그 민감정보 노출 가능

- 현상
  - 실패 로그에 `link` 원문 및 상세 에러를 그대로 기록한다.
  - query token 포함 URL일 경우 민감값 노출 가능성이 있다.
- 근거 위치
  - `crates/scraper/service/src/link_checker.rs:215`
  - `crates/scraper/service/src/link_checker.rs:216`
- 권장 조치
  - URL redaction(쿼리 제거, 민감 key 마스킹) 후 기록

### 5.3 [MEDIUM][A1,A2] Service 계층의 concrete infra 의존

- 현상
  - service가 `Repository` concrete 타입에 직접 결합되어 계층 경계가 약하다.
- 근거 위치
  - `crates/scraper/service/src/scraper.rs:7`
  - `crates/scraper/service/src/scheduler.rs:4`
- 권장 조치
  - consumer 경계 인터페이스(trait) 주입으로 결합도 완화

### 5.4 [LOW][D3] CI 보안 스캔 부재 + workflow path 오타

- 현상
  - workflow에 dependency vulnerability scan 단계가 없다.
  - trigger path가 `scraper-rs.yml` 파일명과 불일치(`scraper-rs-ci.yml`)한다.
- 근거 위치
  - `.github/workflows/scraper-rs.yml:7`
  - `.github/workflows/scraper-rs.yml:11`
- 권장 조치
  - cargo-audit(or 등가 도구) 단계 추가
  - trigger path 오타 수정

## 6) 후속 작업 제안 (우선순위)

1. SSRF 차단 강화 (redirect/재해석 경로 포함)
2. scraper HTTP timeout 및 cycle execution budget 도입
3. 로그 redaction 적용
4. health/readiness 분리
5. CI 보안 스캔 + workflow path 정정

