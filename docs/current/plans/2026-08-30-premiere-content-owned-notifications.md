# 최초공개 알림은 콘텐츠 파이프라인만 소유

**Goal:** 같은 YouTube 최초공개 영상에 `N분 후 공개 예정`과 라이브 `방송 5분 전`/`방송 시작`이 같이 나가지 않게 한다.

**Architecture:** 구독자 최초공개 알림은 `NEW_VIDEO` outbox가 소유한다. `youtube_live_sessions.is_premiere=true`는 라이브 세션·캘린더·생일 방송 발견용 분류로 남기고, alarm-worker YouTube 체커의 upcoming·catchup 후보에서는 제외한다. 체커는 Holodex 스트림이나 `LoadRecentSessions`의 `last_seen` 창이 아니라 이번 틱 video_id 집합으로 확정 최초공개를 읽는다.

**Decisions:** `DEC-20260830-hololive-premiere-content-owned-notifications`

## 제품 계약

- 구독자가 받는 최초공개 알림: 발견 시 `N분 후 공개 예정`, 이미 시작된 채 발견되면 `최초공개`.
- 내지 않는 알림: 같은 video_id의 라이브 upcoming(5/3/1분, 일정 변경 포함)과 live catchup 시작.
- 유지: 생일 방송 celebration, 캘린더/상태용 live session, content→live premiere 투영, `KEEP_EXISTING` 충돌 규칙.
- 하지 않음: 새 테이블/migration, Holodex `type` 파싱, `NEW_VIDEO` 존재만으로 일반 라이브 억제, 5분 전을 outbox 스케줄러로 옮기기.

## 잔여 공백

- content consume 전에 Holodex가 먼저 5분 전 창을 지나면 라이브 알람이 나간 뒤 `NEW_VIDEO`가 이을 수 있다.
- 최초공개 시각이 바뀌어도 라이브 쪽 일정변경 알림은 나가지 않으며 outbox는 재발송하지 않는다.
- `is_premiere=false`로 먼저 확정된 세션은 `KEEP_EXISTING`이라 라이브 알람이 남을 수 있다.

`Fallback delta: none`.
