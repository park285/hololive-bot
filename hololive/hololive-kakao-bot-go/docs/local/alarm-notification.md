# Hololive Kakao Bot – 알람/묶음 발송 구현 정리

## 개요
- 주기적 티커가 Holodex 스케줄을 조회하고, 타깃 분전(minute) 기준으로 예정 스트림을 필터링합니다.
- 같은 방(room)·같은 시작시각(또는 분전 기준)으로 알림을 묶어 한 번에 발송하고, 재발송 방지를 위해 스트림 단위로 발송 기록을 남깁니다.
- 핵심 컴포넌트: Valkey 기반 `AlarmService`, Bot 티커(트리거/그룹핑/발송), 포맷터(단건/묶음), 설정(advance minutes, interval).

---

## 데이터 모델 (Valkey 키)
- `alarm:<roomID>:<userID>`: 사용자별 구독 채널 집합  
  internal/service/notification/alarm.go:92, 128, 167
- `alarm:registry`: 모든 구독자 레지스트리(`"<roomID>:<userID>"`) 집합  
  internal/service/notification/alarm.go:92
- `alarm:channel_registry`: 구독된 채널 ID 집합(≥1 구독자 보유 채널)  
  internal/service/notification/alarm.go:110
- `alarm:channel_subscribers:<channelID>`: 채널별 구독자 집합(`"<roomID>:<userID>"`)  
  internal/service/notification/alarm.go:106
- `notified:<streamID>`: 발송 기록(스케줄/분전/시각), TTL=24h  
  internal/service/notification/alarm.go:490
- `alarm:next_stream:<channelID>`: 다음 방송 캐시(status/title/video_id/start_scheduled)  
  internal/service/notification/alarm.go:509

- 멤버명 해시: `member_names` (`channelID → memberName`)  
  internal/service/notification/alarm.go:482

---

## 설정/주입
- 분전 타깃: `NOTIFICATION_ADVANCE_MINUTES` → 정제·중복 제거·내림차순 정렬, `1`분 fallback 보장  
  internal/service/notification/alarm.go:46
- 체크 주기: `CHECK_INTERVAL_SECONDS`(기본 60s) → 티커 주기  
  internal/config/config.go:86, 167–168
- Go checker 토글(기본 true):
  - `GO_YOUTUBE_ALARM_CHECKER_ENABLED`
  - `GO_CHZZK_ALARM_CHECKER_ENABLED`
  - `GO_TWITCH_ALARM_CHECKER_ENABLED`
- 서비스 생성: `NewAlarmService(cache, holodex, logger, advanceMinutes)`  
  internal/app/builder.go:103

---

## 알람 체크 티커
- 시작/반복: 봇이 티커를 시작하고 주기적으로 검사 수행  
  internal/bot/bot.go:404 (startAlarmChecker), 427 (performAlarmCheck)
- 동시 실행 방지: 뮤텍스로 중복 실행 차단  
  internal/bot/bot.go:436
- 타임아웃: 각 사이클에 `2m` 타임아웃 컨텍스트  
  internal/bot/bot.go:444

---

## 스케줄 수집·필터링
- 채널 수집: `alarm:channel_registry`에서 채널 목록, 각 채널의 구독자 조회  
  internal/service/notification/alarm.go:218, 288
- 스케줄 조회: Holodex 24h 창 조회  
  internal/service/notification/alarm.go:306
- 필터링 규칙:
  - `IsUpcoming == true` AND `StartScheduled != nil`
  - 남은 분(`MinutesUntilCeil`)이 타깃 분전 목록에 포함될 때만 선택  
    internal/service/notification/alarm.go:321, internal/domain/stream.go:91

---

## 알림 생성·중복 방지
- 유효 구독 재검사: 사용자 알람 키(SISMEMBER)로 구독 상태 검증  
  internal/service/notification/alarm.go:378
- 알림 객체 생성: 방 단위 사용자 리스트를 묶어 `AlarmNotification` 생성  
  internal/service/notification/alarm.go:458, 469
- 재발송 방지 및 스케줄 변경:
  - `notified:<streamID>`에 이전 `start_scheduled`/발송 시각/분전 저장
  - 동일 스케줄이면 스킵, 변경 시 분 차이를 계산해 안내 메시지 추가  
    internal/service/notification/alarm.go:490, 400
- 발송 후 마킹: 성공 건을 24h TTL로 저장  
  internal/bot/bot.go:474, internal/service/notification/alarm.go:490

---

## 묶음(다이제스트) 발송과 포맷팅
- 그룹 키: 같은 방 + 같은 시작시각(분 단위 절삭). 시작시각이 없으면 분전 기준으로 그룹핑  
  internal/bot/bot.go:526
- 그룹핑: `groupAlarmNotifications`  
  internal/bot/bot.go:490
- 포맷터:
  - 단건: `AlarmNotification`  
    internal/adapter/formatter.go:409
  - 다건: `AlarmNotificationGroup`  
    internal/adapter/formatter.go:439
- 발송/로깅/에러: 그룹 메시지 전송, 실패 시 로깅 후 다음 그룹 진행  
  internal/bot/bot.go:454, 465

---

## 명령 처리 파이프라인(사용자면)
- 토큰 정규화/파싱: `!알람 추가/제거/목록/초기화` 및 축약 토큰 지원  
  internal/adapter/message.go:690–691, 298, 593
- 커맨드 실행: `AlarmCommand` 분기 → `AlarmService` 호출  
  internal/command/alarm.go:12, 19, 23, 31
- 서비스 API:
  - `AddAlarm` / `RemoveAlarm` / `GetUserAlarms` / `ClearUserAlarms`  
    internal/service/notification/alarm.go:92, 128, 167, 177

---

## 다음 방송 캐시(Next Stream Cache)
- 상태 머신: `live` / `upcoming` / `no_upcoming` / `time_unknown`로 해시 저장, TTL 갱신  
  internal/service/notification/alarm.go:631, 650, 667, 698
- 보존 정책: 동일 영상이 여전히 upcoming이면 값 유지하고 TTL만 연장(깜빡임 방지)  
  internal/service/notification/alarm.go:677
- 갱신 트리거: 스케줄 조회 시 비동기 갱신  
  internal/service/notification/alarm.go:348, 607

---

## 동시성/성능
- 채널 병렬 처리: goroutine 풀(max 15)로 스케줄 조회 분산  
  internal/service/notification/alarm.go:37, 241
- 실행 중복 방지: 알람 체크 루프 뮤텍스 가드  
  internal/bot/bot.go:436

---

## E2E 흐름 요약
1. 사용자가 채널 알람 등록 → Valkey 구독/레지스트리 반영  
   internal/service/notification/alarm.go:92
2. 티커 트리거 → `CheckUpcomingStreams`가 채널·스케줄 조회 및 타깃 분전 필터  
   internal/bot/bot.go:427, internal/service/notification/alarm.go:218, 321
3. 방 단위 알림 구성 → `notified:<streamID>`로 중복 방지/스케줄 변경 처리  
   internal/service/notification/alarm.go:378, 490, 400
4. 그룹핑/포맷팅(단건/다건) → 발송 → 발송 기록 저장  
   internal/bot/bot.go:490, 454, 474

---

## 참고(상수/TTL)
- 캐시 TTL 묶음(예: ChannelSchedule 5m, NotificationSent 60m)  
  internal/constants/constants.go:16, 20
