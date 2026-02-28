# LIB-01 SSRF/DNS 검증 표준화 계획 및 적용 결과 (2026-02-23)

## 목표

- `scraper/service/link_checker`의 host/IP 검증 규칙을 CIDR 기반으로 일원화합니다.
- redirect/fallback 경로에서 재검증 누락을 줄여 SSRF 우회 가능성을 낮춥니다.
- 변경사항을 단위 테스트로 고정합니다.

## 실행 계획

1. **IP 정책 표준화**
   - `ipnet` 기반 deny CIDR 집합을 정의합니다.
   - IPv4/IPv6 private, loopback, link-local, documentation/reserved 대역을 동일 규칙으로 차단합니다.
2. **Redirect/Fallback 재검증 강화**
   - redirect 최종 URL의 scheme을 `http/https`로 제한합니다.
   - HEAD 실패 후 GET fallback 직전에 host를 다시 resolve/검증합니다.
3. **회귀 테스트 추가**
   - CIDR 차단 정책 단위 테스트 추가
   - unsupported redirect scheme 차단 테스트 추가
   - fallback 시 DNS rebinding 완화 테스트 추가

## 적용 파일

- `crates/scraper/service/src/link_checker/url_safety.rs`
- `crates/scraper/service/src/link_checker/mod.rs`
- `crates/scraper/service/src/link_checker/tests.rs`
- `crates/scraper/service/Cargo.toml`
- `Cargo.toml`

## 검증

```bash
cd hololive-scraper-rs
cargo test -p scraper-service link_checker -- --nocapture
```

실행 결과: PASS (`18 passed, 0 failed`)
