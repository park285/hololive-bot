# Outbox Per-Room 모드 롤아웃 런북

> 작성일: 2026-03-05

## 1. 목적

`youtube_notification_outbox` 단일 상태 모델에서 room 단위 전달 상태 모델로 단계 전환한다.

## 2. 사전 조건

- 배포 기준 커밋에 다음 포함:
  - migration `036_create_youtube_notification_delivery.sql`
  - outbox dispatcher `PerRoomMode` 지원 코드
  - stream-ingester env 토글 연결 (`YOUTUBE_OUTBOX_PER_ROOM_MODE`)

## 3. 단계별 적용

### Phase A: 스키마 적용

1. 마이그레이션 실행
   - `hololive-kakao-bot-go/scripts/migrations/036_create_youtube_notification_delivery.sql`
2. 테이블/인덱스 생성 확인
   - `youtube_notification_delivery`
   - `idx_ynd_outbox_room`, `idx_ynd_pending_next`, `idx_ynd_sent_cleanup`

### Phase B: 코드 배포 (토글 OFF)

1. 애플리케이션 배포
2. 기본값 유지
   - `YOUTUBE_OUTBOX_PER_ROOM_MODE=false`
3. 기존 outbox 처리 지표 정상 여부 확인

### Phase C: canary 활성화 (토글 ON)

1. stream-ingester 1개 인스턴스에서만 토글 활성화
   - `YOUTUBE_OUTBOX_PER_ROOM_MODE=true`
2. 15~30분 관찰
   - delivery row 생성량
   - 실패율/재시도율
   - DB claim 지연
   - per-room 로그 필드
     - `outbox_claimed`, `outbox_enqueued`, `enqueue_failures`, `target_rooms`
     - `delivery_claimed`, `delivery_sent`, `delivery_failed`, `aggregate_failures`
3. 이상 없으면 점진 확대

권장 점검 커맨드:

```bash
./scripts/logs/check-outbox-per-room.sh --since 30m
```

주기 점검(cron) 예시:

```bash
*/10 * * * * /home/kapu/gemini/hololive-bot/scripts/logs/check-outbox-per-room-cron.sh >> /home/kapu/gemini/hololive-bot/logs/outbox-per-room-canary-cron.log 2>&1
```

### Phase D: 전체 전환

1. 모든 stream-ingester에서 토글 ON
2. 기존 경로 대비 전달 성공률/지연 비교
3. 24시간 안정화 모니터링

## 4. 롤백

1. 즉시 토글 OFF
   - `YOUTUBE_OUTBOX_PER_ROOM_MODE=false`
2. 재배포 후 기존 경로 복귀 확인
3. `youtube_notification_delivery` 데이터는 보존(사후 분석용)

## 5. 운영 체크리스트

- [ ] 마이그레이션 성공
- [ ] canary 1차 성공
- [ ] 전체 전환 성공
- [ ] 롤백 절차 리허설 완료
