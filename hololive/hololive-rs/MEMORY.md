# Hololive RS Memory

> 최종 갱신: 2026-03-04

## Crate Structure

현재 Rust workspace는 총 **14개 crate**로 구성됩니다.

- `crates/scraper/*` (4)
  - `scraper/core`
  - `scraper/service`
  - `scraper/infra`
  - `scraper/app`
- `crates/alarm/*` (4)
  - `alarm/core`
  - `alarm/service`
  - `alarm/infra`
  - `alarm/app`
- `crates/dispatcher/*` (4)
  - `dispatcher/app`
  - `dispatcher/formatter`
  - `dispatcher/notification`
  - `dispatcher/template`
- `crates/shared/*` (2)
  - `shared/core`
  - `shared/infra`

Phase 1 재배치 결과로 `formatter/notification/template`는 `shared/services/*`에서 `dispatcher/*`로 이동된 상태를 기준으로 한다.
