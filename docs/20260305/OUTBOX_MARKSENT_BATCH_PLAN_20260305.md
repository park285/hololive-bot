# YouTube Outbox `markSent` 배치화 사전 설계

> 작성일: 2026-03-05  
> 대상: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go`

## 1. 배경

현재 YouTube outbox dispatcher는 발송 완료 처리 시 `markSent(ctx, id)`를 ID 단위로 반복 호출한다.

- 무구독 fallback 경로: `processOnce` 내 루프에서 단건 UPDATE 반복
- grouped 발송 성공 경로: `processGroupedItems` 내 루프에서 단건 UPDATE 반복

배치 크기/구독 방 수가 커질수록 DB write round-trip이 증가한다.

## 2. 현재 상태 요약 (코드/스키마)

### 2-1. 현재 완료 처리 쿼리

`markSent`는 아래 컬럼만 갱신한다.

- `status = SENT`
- `sent_at = now`
- `locked_at = NULL`
- `error = ''`

대상 테이블: `youtube_notification_outbox`  
PK: `id BIGSERIAL`

### 2-2. 관련 스키마/인덱스

기존 마이그레이션에 이미 필요한 컬럼이 모두 존재한다.

- `scripts/migrations/011-create-youtube-content-tables.sql`
- `scripts/migrations/010-add-alarm-types-and-templates.sql`
- `scripts/migrations/021-optimize-alarm-outbox-query-indexes.sql`

`markSent` 배치화를 위해 **새 컬럼/새 인덱스는 필수 아님**.

## 3. 핵심 판단

## 3-1. I/O 개선 가능성

높음. 단건 UPDATE N회를 `WHERE id IN (...)` 배치 UPDATE로 치환하면 write round-trip를 줄일 수 있다.

## 3-2. DB 스키마 의존

낮음. 기존 PK/상태 컬럼으로 구현 가능하여 **스키마 변경 없이 코드 레벨에서 수행 가능**.

## 3-3. 안전 가드 필요

grouped 경로에서 동일 outbox ID가 room별 group에 중복 포함될 수 있으므로 아래 가드가 필요하다.

1. 배치 대상 ID dedupe
2. `WHERE status = 'PENDING'` 조건으로 idempotent 업데이트

권장 업데이트 조건 예시:

```sql
UPDATE youtube_notification_outbox
SET status='SENT', sent_at=NOW(), locked_at=NULL, error=''
WHERE id IN (...)
  AND status='PENDING';
```

## 4. 리스크/한계 (이번 변경 범위 밖)

현재 모델은 outbox row가 room별 전달 상태를 별도로 저장하지 않는다.

- 즉, `content` 단위 row 1개를 여러 room에 팬아웃 전송
- room A 성공 후 SENT가 되면 room B 실패가 재시도 대상으로 정확히 모델링되지 않음

이 이슈는 `markSent` 배치화와 별개로, per-room delivery 모델(매핑 테이블/전달 상태)이 필요한 구조 개선 과제다.

## 5. PR 분리 전략

## PR-1 (우선, 코드 전용)

목표: I/O 개선만 수행 (스키마 변경 없음)

- `markSentBatch(ctx, ids []int64)` 도입
- `processOnce` fallback 경로/`processGroupedItems` 성공 경로에서 배치 호출
- dedupe + `status='PENDING'` 가드 적용
- 단위 테스트 보강
  - no-subscriber 다건 처리
  - grouped 다건 처리
  - 중복 ID 입력 시 안정성

## PR-2 (선택, 별도 설계)

목표: 전달 정합성 개선 (스키마 변경 가능)

- per-room 전달 상태 추적 모델 설계
- 재시도 단위를 room 수준으로 분리
- 마이그레이션 + backfill + 운영 전환 계획 포함

## 6. 검증 기준 (PR-1)

1. 기능 동등성
   - 성공/실패/재시도 기존 동작 유지
2. 성능 관점
   - 동일 배치에서 UPDATE 실행 횟수 감소
3. 안정성
   - 중복 ID/재진입 상황에서 상태 손상 없음
4. 회귀
   - `outbox` 기존 테스트 + 신규 케이스 통과

