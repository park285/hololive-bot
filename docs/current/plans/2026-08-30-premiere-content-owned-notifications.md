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

## v9.2.0: 분 전 알림 재허용 (2026-10-12)

위 제품 계약 중 upcoming 제외는 v9.2.0(#622)에서 바뀌었다. 현재 계약은 `docs/current/contracts/alarm.md`가 소유한다.

- **바뀐 계약:** 확정 최초공개도 일반 방송과 같은 분 전 target에서 `선행공개 N분 전`과 일정 변경 알림을 보낸다. 실패하지 않은 `NEW_VIDEO` 안내가 가장 이른 target 시각(예정 − 최대 target 분) 이후에 생성됐을 때만 공개 직전 안내로 보고 생략한다. 대기 후보는 선정 뒤에 안내가 생겼을 때만 `stream_ended`로 끝낸다. live catchup 제외는 유지한다.
- **이유:** 공개 며칠 전에 발견된 최초공개는 공개 직전 알림이 없었다. 과거 최초공개 6건 중 공개 직전(3.3분 전) 발견은 1건이었다.
- **게시:** #622 `7eee4c3dd`. CI 11개 통과, 병합 tree는 검증한 브랜치와 같다. annotated tag `v9.2.0`과 GitHub Release(`publish-release.sh v9.2.0 v9.1.2`)를 게시했다.
- **배포:**
  - `hololive-alarm-worker:release-7eee4c3dd311-r1-arm64`(`sha256:b956debb…`, arm64, 9.2.0)를 원격에 load해 같은 ID·revision을 확인했다.
  - 9.0.0 worker를 `hololive-alarm-worker:rollback-20261012T145705Z`로 보존하고 VERSION 2개를 `deploy-backups/pre-v9.2.0-20261012T145705Z`에 백업했다. 원격 tree에서 바뀌는 파일은 이 2개뿐이다.
  - `change_started_at=2026-10-12T14:57:39Z`에 `up -d --no-build --no-deps hololive-alarm-worker`로 교체했고 health gate를 6초에 통과했다. migration 없음.
- **수용 증거:** worker healthy, 재시작 0회, 시작 로그 `version=9.2.0`, 실행 compose 파일 4개 동일. 교체 뒤 약 3분 파일 로그 26줄은 모두 INFO(WARN·ERROR 0)이며 다른 컨테이너는 재시작되지 않았다. 최초공개 분 전 알림의 실제 발송은 다음 최초공개에서 `alarm_upcoming_candidates`와 발송 기록으로 확인한다.
- **현재 rollback:** `rollback-20261012T145705Z`를 `prod`로 되돌리고 백업 tar로 VERSION 2개를 복원한 뒤 `--no-build --no-deps hololive-alarm-worker`로 다시 띄운다. 최초공개 분 전 알림이 다시 빠진다.
- **남은 공백:** 분류 전에 선정된 후보는 `방송 N분 전` 문구로 나갈 수 있고, collector live 경로가 먼저 분류한 뒤 공개 직전에 안내가 생기면 두 알림이 겹칠 수 있다(runbook `alarm-worker.md` 7). 템플릿 용어(`선행공개`)는 outbox(`최초공개`)와 다르다.
