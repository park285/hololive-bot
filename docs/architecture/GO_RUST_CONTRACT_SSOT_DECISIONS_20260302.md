# Go↔Rust 계약 SSOT 의사결정 기록 (2026-03-02)

> 업데이트: 2026-03-03  
> 변경 사항: `AlarmQueueEnvelope` 호환 alias 정책 종료, contracts 직접 정의로 전환

## 배경

M1 진행 중 Queue/Key/Trigger 계약을 SSOT로 모으는 과정에서, Go의 `domain` 모델(`AlarmQueueEnvelope`)과 신규 `contracts` 패키지 사이에 순환 의존 위험이 확인되었다.

## 결정

### D1. 단기(현재): Go Envelope 모델 소스는 `domain` 유지

- 이유:
  1. `domain.AlarmNotification`이 `Channel/Stream` 도메인 타입에 의존
  2. 이를 contracts로 즉시 이동하면 대규모 타입 분리가 필요
  3. 현재 단계 목표는 **하드코딩/분산 상수 제거 + 경로 SSOT화**이므로 범위를 넘지 않음

- 적용(2026-03-03 갱신):
  - `pkg/contracts/alarm`에 `AlarmQueueEnvelope`를 직접 정의
  - `pkg/domain/alarm.go`의 중복 envelope 정의 제거

### D2. 중기: Rust는 `shared-core::keys`를 계약 상수 소스로 사용

- 이유:
  1. alarm-service/shared-notification/dispatcher에서 공통 참조 가능
  2. 중복 리터럴 제거 효과가 즉시 큼

### D3. 게이트 우선

- 이유:
  1. 빅뱅 이전에 회귀 차단 장치가 먼저 필요
  2. M0/M1 게이트가 있어야 이후 리팩터링 안전성이 확보됨

## 트레이드오프

1. 장점
   - 변경 범위를 작게 유지하면서 drift 리스크를 빠르게 낮춤
   - 기존 런타임 동작/타입 계약을 깨지 않음
2. 단점
   - Go DTO SSOT가 완전 단일 소스는 아님(단기 타협)
   - 최종 단계에서 domain/contracts 분리 재작업 필요

## 다음 결정 포인트 (M1 3차)

1. Go DTO를 완전 contracts 소스로 옮길지 여부
2. Rust `shared-contracts` 전용 crate를 분리할지(현재 `shared-core` 유지 vs 분리)
3. Fixture/Schema 기반 자동 검증(codegen 포함) 도입 범위
