# Go↔Rust 계약 SSOT 초안 (Queue/Key/Trigger)

> 작성일: 2026-03-02  
> 상태: M1 설계 초안 + 1차 코드 반영

## 1) 목적

Go/Rust 양측에 흩어진 계약(Queue Key, Claim Key, Trigger API)을 단일 관리 대상으로 고정해 drift를 차단한다.

## 2) As-Is 계약 스냅샷 (2026-03-02)

### 2-1. Queue 계약

- **Queue Key**: `alarm:dispatch:queue`
  - Rust producer: `hololive/hololive-rs/crates/alarm/service/src/queue.rs`
  - Rust consumer default: `hololive/hololive-rs/crates/shared/services/notification/src/lib.rs`
  - Dispatcher config default: `hololive/hololive-rs/crates/dispatcher/app/src/config.rs`

### 2-2. Envelope DTO 계약

- Go: `hololive/hololive-shared/pkg/domain/alarm.go::AlarmQueueEnvelope`
- Rust: `hololive/hololive-rs/crates/shared/core/src/model/alarm.rs::AlarmQueueEnvelope`

필드 계약(v1):
1. `notification` (object)
2. `claim_keys` (`[]string`)
3. `enqueued_at` (RFC3339 string)
4. `version` (`uint8`, 현재 `1`)

### 2-3. Key Prefix 계약

- `notified:claim:`
  - Go: `hololive/hololive-shared/pkg/service/notification/alarm_types.go`
  - Rust: `hololive/hololive-rs/crates/alarm/core/src/keys.rs`
- `notified:claim:event:`
  - Go/Rust 동일 파일군에서 동시 정의

### 2-4. Trigger API 계약

- Base path: `/internal/trigger`
- Header: `X-API-Key` (설정 시 필수)
- Endpoints:
  - `POST /majorevent-weekly`
  - `POST /majorevent-monthly`
  - `POST /membernews-weekly`
- Handler: `hololive/hololive-shared/pkg/server/trigger.go`
- Caller proxy: `hololive/hololive-admin/internal/service/trigger/client.go`

상태 코드 계약:
- `200`: 성공
- `409`: 이미 실행 중 (`majorevent.ErrNotificationInProgress`)
- `5xx`: 내부 오류

## 3) 현재 drift 방지 장치

- Go 호환 테스트: `hololive/hololive-shared/pkg/domain/alarm_test.go`
- Rust 호환 테스트: `hololive/hololive-rs/crates/shared/core/src/model/alarm.rs` (fixture roundtrip)
- Trigger client 테스트: `hololive/hololive-admin/internal/service/trigger/client_test.go`

## 3-1) 1차 코드 반영 (2026-03-02)

1. Go 계약 패키지 신설
   - `hololive/hololive-shared/pkg/contracts/alarm`
   - `hololive/hololive-shared/pkg/contracts/trigger`
2. Trigger path 하드코딩 제거
   - server/client 모두 contracts 상수 사용
3. Notify claim key prefix 하드코딩 제거
   - notification 서비스에서 contracts 상수 참조
4. Rust queue key 하드코딩 축소
   - `shared-core::keys::ALARM_DISPATCH_QUEUE_KEY` 추가
   - alarm-service/shared-notification/dispatcher-app default에 공통 상수 적용
5. 계약 정합 스크립트 추가
   - `scripts/architecture/check-go-rust-alarm-contracts.sh`
6. Trigger 경로 하드코딩 금지 게이트 추가
   - `scripts/architecture/check-go-trigger-route-hardcoding.sh`
7. Envelope 버전 상수 parity 추가
   - Go `QueueEnvelopeVersionV1` ↔ Rust `ALARM_QUEUE_ENVELOPE_VERSION_V1` 비교

## 4) M1 SSOT 목표 구조

1. Go 계약 패키지 신설: `hololive-shared/pkg/contracts/alarm`
2. Rust 계약 crate 신설: `hololive-rs/crates/shared-contracts` (또는 동등 crate)
3. Queue Key / Prefix / DTO를 SSOT 모듈로 이동
4. 기존 정의는 thin wrapper 또는 re-export로 1단계 호환 유지

## 5) M1 실행 순서(제안)

1. 계약 상수/DTO를 SSOT 위치에 신규 정의 (기존 코드 미삭제)
2. Go/Rust 각 호출부를 SSOT 참조로 전환
3. 교차 fixture 파이프라인 통합 (Go↔Rust 동일 fixture)
4. 기존 중복 정의 제거

## 6) 리스크/주의

1. 문자열 상수 직접 참조(하드코딩) 누락 시 런타임 큐 단절 가능
2. `version` 필드 변경 시 dual-read 전략 없이 바로 변경 금지
3. Trigger API는 헤더/상태코드 계약 변경 시 admin proxy와 동시 반영 필요
