# YouTube Outbox per-room 전달 정합성 개선안 (PR-2)

> 작성일: 2026-03-05  
> 범위: `youtube_notification_outbox` 기반 fan-out 전달 정합성

## 1) 문제 정의

현재 outbox row는 `content` 단위 1행이며, 실제 발송은 여러 room으로 fan-out 된다.

- room A 성공 + room B 실패가 동시에 발생해도, row 단위 상태(`SENT/PENDING/FAILED`)만 존재
- 결과적으로 **room 단위 재시도/관측/장애복구 정합성**이 부족하다.

## 2) 목표

1. 전달 상태를 room 단위로 추적
2. 재시도 단위를 room 단위로 분리
3. 기존 poller/outbox 흐름과 점진적 호환(빅뱅 전환 금지)

## 3) 제안 모델

## 3-1. 테이블 분리

기존 `youtube_notification_outbox`는 콘텐츠 이벤트 마스터(논리적 root)로 유지하고,
room fan-out은 신규 테이블로 분리한다.

예시:

- `youtube_notification_delivery`
  - `id` BIGSERIAL PK
  - `outbox_id` BIGINT NOT NULL FK (`youtube_notification_outbox.id`)
  - `room_id` VARCHAR(...) NOT NULL
  - `status` VARCHAR(20) NOT NULL DEFAULT `PENDING`
  - `attempt_count` INT NOT NULL DEFAULT 0
  - `next_attempt_at` TIMESTAMPTZ NOT NULL DEFAULT NOW()
  - `locked_at` TIMESTAMPTZ NULL
  - `sent_at` TIMESTAMPTZ NULL
  - `error` TEXT NULL
  - `created_at` TIMESTAMPTZ NOT NULL DEFAULT NOW()

권장 인덱스:

- unique `(outbox_id, room_id)` (중복 fan-out 방지)
- partial index `(next_attempt_at, created_at) WHERE status='PENDING'`
- partial index `(sent_at) WHERE status='SENT'`

## 3-2. 상태 집계

`youtube_notification_outbox.status`는 아래처럼 집계 상태로 운용:

- 모든 delivery가 `SENT`면 outbox `SENT`
- 하나 이상 `PENDING`이면 outbox `PENDING`
- 모두 terminal 실패(`FAILED`)면 outbox `FAILED`

집계는 트랜잭션 내 UPDATE 또는 비동기 reconciler 중 하나를 선택한다.

## 4) 처리 흐름 (개선 후)

1. fetchAndLock는 outbox가 아니라 delivery row(PENDING)를 claim
2. `SendMessage(room_id)` 성공 시 해당 delivery만 `SENT`
3. 실패 시 해당 delivery만 retry/backoff
4. 주기적으로 outbox aggregate status 동기화

## 5) 마이그레이션/롤아웃 전략

## Phase A (스키마 추가)

- 신규 delivery 테이블 + 인덱스 추가
- 기존 코드 경로 유지 (읽기/쓰기 영향 최소화)

## Phase B (이중 기록)

- 신규 outbox 생성 시 room fan-out delivery도 같이 생성
- 구 경로와 병행 운영, 메트릭 비교

## Phase C (소비 전환)

- dispatcher claim 대상을 delivery로 전환
- aggregate status 동기화 활성화

## Phase D (정리)

- 구 outbox 단일 상태 의존 로직 제거
- 운영 지표/알람 기준을 delivery 중심으로 전환

## 6) 리스크/트레이드오프

- 장점: 재시도 정밀도, 장애 분석성, 부분 실패 복구 능력 향상
- 단점: 테이블/인덱스 증가, write 증대, 운영 복잡도 증가
- 완화: 단계적 롤아웃 + 메트릭 기반 전환 게이트

## 7) 검증 지표

- room 단위 성공률/재시도율/최종 실패율
- outbox 집계 상태와 delivery 상태 불일치 건수
- claim 쿼리 latency 및 dead tuple 증가 추이

## 8) 구현 분리 권장

- PR-2A: 스키마 + 모델 + repository
- PR-2B: dispatcher 경로 전환
- PR-2C: aggregate reconciler + 관측(로그/메트릭)

