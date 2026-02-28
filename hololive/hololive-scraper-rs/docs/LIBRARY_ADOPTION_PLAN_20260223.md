# hololive-scraper-rs 라이브러리 전환/성능 개선 실행 계획 (현행 구현 반영, 2026-02-23)

## 1) 목적

- `hololive-scraper-rs`의 직접 구현 영역을 `LIB-01`~`LIB-10` 기준으로 재정리합니다.
- 각 항목의 **현재 구현 상태(완료/부분완료/미완료)**, 전환 액션, 기대 이득, backend-guide 규칙 매핑을 한눈에 보이도록 정리합니다.

## 2) 점검 범위/기준

- 범위: `crates/scraper/**`, `crates/alarm/**`
- 기준: 2026-02-23 현재 코드베이스 구현 상태

## 3) 상태 요약

- 완료: **10건** (`LIB-01`~`LIB-10`)
- 부분완료: **0건**
- 미완료: **0건**

> 상태 표기: ✅ 완료 / 🟡 부분완료 / ⏳ 미완료

## 4) LIB-01 ~ LIB-10 전수 목록

| ID | 상태 | 우선순위 | 대상 모듈 | 전환 액션(현행 반영) | 기대 성능·신뢰성 이득 | backend-guide 규칙 |
|---|---|---|---|---|---|---|
| LIB-01 | ✅ | P0 | `crates/scraper/service/src/link_checker/{mod.rs,url_safety.rs}` | `trust-dns-resolver` 기반 host resolve + `ipnet` CIDR denylist 적용, redirect hop별 host 재검증 및 scheme 재검증 반영 | SSRF/DNS rebinding 우회 가능성 축소, host/IP 검증 일관성 향상 | `S3`, `S5`, `R4` |
| LIB-02 | ✅ | P0 | `crates/alarm/infra/src/circuit_breaker.rs` + 외부 API client 호출부 | 커스텀 breaker를 `failsafe` 기반(`consecutive_failures + constant backoff`)으로 전환, `StateObserver` instrumentation으로 상태 공개 API 유지 | 상태전이/복구 정책 표준화, 동시성 안전성 확보, 유지보수 복잡도 감소 | `R4`, `A4` |
| LIB-03 | ✅ | P0 | `crates/alarm/service/src/{chzzk_checker.rs,twitch_checker.rs}` | GET→SET 분리 제거, `set_nx`(`SET NX EX`) 단일 claim으로 원자 dedup 적용 | dedup race 완화, RTT 감소(2-step→1-step), 중복 알림 억제 정확도 향상 | `R1`, `Q1`, `R4` |
| LIB-04 | ✅ | P0 | `crates/scraper/service/src/scheduler.rs` | retry queue를 `VecDeque`로 전환(`pop_front`)하고, 정시 트리거를 `cron::Schedule` 기반 계산으로 표준화 | queue dequeue O(1) 확보 + 정시 실행 계산 신뢰성/가독성 향상 | `R4`, `A4`, `Q5` |
| LIB-05 | ✅ | P0 | `crates/scraper/service/src/scraper/mod.rs` | 수동 retry loop를 `backoff::future::retry_notify` 경로로 통합, transient/permanent 경계(`is_retryable + max_retries`) 명시 | 재시도 정책 일관화, 관측성(재시도 로그) 유지, 회복성 향상 | `R3`, `R4`, `Q2` |
| LIB-06 | ✅ | P1 | `crates/alarm/service/src/scheduler.rs`, `crates/alarm/infra/src/valkey.rs` | `ValkeyClient::smembers_multi` 추가 + `fred` pipeline(`pipeline().smembers...all`) 적용으로 다건 구독자 조회 배치화 | Valkey 왕복 횟수 감소, polling loop latency 개선 | `R4`, `Q2` |
| LIB-07 | ✅ | P1 | `crates/alarm/service/src/checker/mod.rs` | 채널별 반복 스캔 대신 `channel_id -> Vec<Stream>` 인덱스(`group_streams_by_channel`) 구성 후 재사용 | O(N*M) 반복 비용 감소, CPU 효율 개선 | `Q5` |
| LIB-08 | ✅ | P1 | `crates/alarm/service/src/chzzk_checker.rs` | 채널별 Chzzk 조회를 `buffer_unordered` bounded concurrency로 병렬화 | 채널 수 증가 시 전체 조회 지연 단축 | `R4`, `Q5` |
| LIB-09 | ✅ | P1 | `crates/alarm/infra/src/holodex.rs` | Holodex 배치 실패 폴백을 `buffer_unordered` bounded concurrency로 전환 | 장애 상황 복구 시간 단축, 폴백 처리량 향상 | `R4`, `Q5` |
| LIB-10 | ✅ | P1 | `crates/alarm/service/src/dedup/mod.rs` | 로컬 dedup fallback을 `moka::sync::Cache`(TTL + max_capacity)로 표준화 | 만료 정리 비용 완화, fallback 경로 동시성/안정성 개선 | `R4`, `Q5` |

## 5) 권장 실행 순서 (Wave)

### Wave 1 (완료)
- `LIB-01` → `LIB-03` → `LIB-05`
- 보안/중복/재시도 기반 안정화 축 완료

### Wave 2 (완료)
- `LIB-06` → `LIB-07` → `LIB-08` → `LIB-09` → `LIB-10`
- Valkey I/O 및 조회 병목 최적화 축 완료

### Wave 3 (완료)
- `LIB-02` circuit breaker 라이브러리 공용화 완료
- `LIB-04` cron 기반 정시 스케줄 계산 전환 완료
- 단위/통합 회귀 테스트로 dedup·scheduler 경로 검증 완료

## 6) 검증 기준 (LIB 공통 게이트)

필수 게이트:
1. `cargo fmt --all --check`
2. `cargo clippy --workspace -- -D warnings`
3. `cargo test --workspace`
4. `/health` 정상 응답
5. `/ready` 정상(또는 의도된 degraded/shadow 상태) 응답

문서 갱신 시점(2026-02-23) 확인 결과:
- `cargo fmt --all --check`: PASS
- `cargo clippy --workspace -- -D warnings`: PASS
- `cargo test --workspace`: PASS
- `/health`: PASS (`http://localhost:30010/health`, `http://localhost:30011/health`)
- `/ready`: PASS (`http://localhost:30011/ready`)
