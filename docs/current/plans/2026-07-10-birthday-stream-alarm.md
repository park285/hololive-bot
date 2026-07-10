# 생일 방송 와꾸 알람 구현 계획 (birthday_stream) — v2

작성: 2026-07-10. 독립 설계안 3건의 교차 반박으로 수렴한 안에 별도 적대 리뷰(HOLD 판정, HIGH 3건)를 반영한 v2.
선행 문서: `docs/agent-workflows/plans/2026-05-26-celebration-alarm.md` (로컬 전용 워크플로 문서; 자정 축하 러너 — 본 계획은 그 연장선의 별도 러너).

## 목표

멤버 생일(KST) 당일, 해당 멤버의 예정/진행 방송(와꾸)을 30분 주기로 감지해 발견 즉시 전체 알람 방에 알림을 보낸다. 방송기록 기능이 `youtube_live_sessions`를 `status='ENDED'`로 읽는 것과 동일한 패턴을 `UPCOMING/LIVE`로 미러링한다.

## 합의된 결정 (8개 쟁점)

| # | 쟁점 | 판정 |
|---|---|---|
| 1 | 폴링 호스트 | 신규 전용 러너 `birthdayStreamRunner` (workerapp, celebrationRunner 인터벌 루프 클론). celebrationRunner 인터벌 모드는 env로 도달 불가(`parseNonNegativeIntEnv`), 60s checker는 30분 lookahead라 부적합 |
| 2 | 데이터 소스 | DB `youtube_live_sessions` (UPCOMING+LIVE). Holodex 직접 호출 배제 — 쿼터/외부 실패 모드 불필요, 30분 주기에 지연 이득 없음 |
| 3 | 와꾸 정의 | 생일 멤버 채널의 세션 중 상태별 시각이 해당 생일일 KST `[00:00, 24:00)` 내: UPCOMING→`scheduled_start_time`, LIVE→`COALESCE(started_at, scheduled_start_time)`. 제목 키워드 매칭 없음(다국어 취약, 생일 당일 본인 채널 방송은 전부 유효). `last_seen_at` freshness 가드 필수(삭제된 대기실 오발송 방지, 기본 30분 — 실측 live poll 주기 2분×cold tier 6배=12분 대비 2.5배 여유) |
| 4 | 메시지 형태 | 자정 축하와 별도의 와꾸 알람. 프레임(video)당 1건. **멤버·일자당 발송 상한 3건은 outbox 기존 event 수 기준으로 러너에서 집행** (SQL 스냅샷 `ROW_NUMBER` 캡은 적대 리뷰에서 기각 — 아래 참조) |
| 5 | dedup | per-video identity: `celebration:birthday_stream:{channelID}:{date}:{videoID}`. `Identity()`가 VideoID 비어있지 않을 때만 `:videoID` 추가(기존 kind 키 불변). 러너는 무상태 — 매 틱 blind republish, outbox preflight(`repository_event_preflight.go`, hash 충돌 시 repoint — 리스케줄 재발송 없음 검증됨)가 유일한 보장. 취소 후 재게시(새 video_id)는 1회 추가 발송 — 의도된 동작. UPCOMING→LIVE 전이도 동일 video_id(PK) 행 update라 dedup 유지 확인됨 |
| 6 | 발송 대상 | 전체 방 `GetAllDistinctRoomIDs` (자정 축하와 일관, 선행 계획의 "전체 구독 방" 의도 계승). 구독방은 checker LIVE/임박 알람을 어차피 받으므로 구독방 한정은 기능을 중복으로 퇴화시킴 |
| 7 | 라우팅 | `SourceKind: celebration` + `AlarmType: BIRTHDAY` + 신규 `CelebrationKind "birthday_stream"`. `validateCelebrationDispatch`가 이미 통과시킴(검증됨) — LIVE validator 확장 금지 |
| 8 | 롤아웃 | `BIRTHDAY_STREAM_RUNNER_ENABLED` (기본 false) + 템플릿 시드 마이그레이션 1개. 스키마 DDL 없음. 롤백 = flag off (동일 이미지에서만 — 아래 rollback 주의) |

교차 검토에서 기각된 대안: 구독방 한정 타게팅(철회), 멤버·일자당 1회 발송 + 인메모리 sent set(철회 — 늦게 올라오는 본방 생誕祭 와꾸를 구조적으로 영구 누락, sent set은 outbox preflight와 중복 상태).

## 적대 리뷰 HOLD 사유와 수용 내역

**HIGH — 전부 수용:**
1. **자정 말미 blind window**: 마지막 당일 틱(예: 23:31) 이후 삽입된 세션은 다음 틱(00:01)이 다음 날짜 생일만 조회하므로 영구 누락. → **매 틱 이중 날짜 평가**: `date(now KST)`와 `date(now - tickInterval KST)`가 다르면 어제 날짜도 함께 평가(어제 생일 멤버 + 어제 날짜 윈도우 + 어제 date의 identity). dedup identity에 date가 포함되므로 중복 없음.
2. **`ROW_NUMBER() <= 3`은 hard cap이 아님**: 이미 발송된 video가 스냅샷 상위 3자리를 계속 점유해 늦은 본방(rn=4)을 영구 차단하고, `started_at` 정렬 이동만으로 4번째 고유 발송도 가능, 동률 시 비결정적. → SQL 캡 제거. 러너가 `SELECT count(*) FROM alarm_dispatch_events WHERE event_key LIKE 'celebration:birthday_stream:{channelID}:{date}:%'`로 기발송 video 수를 세고, 신규 video를 시작시각 ASC + `video_id` ASC(동률 tie-break) 순으로 `3 - 기발송수`개까지만 publish.
3. **`exhaustive` 린트 게이트 파손**: 신규 `CelebrationKind` 상수가 `hololive-api` calendar의 switch 2곳(`calendar.go:170-178`, `:233-239`)을 깨뜨림(blocking gate, `.golangci.yml:26-29`). → Task 목록에 해당 switch 처리 추가, 검증에 lint 추가.

**MEDIUM — 수용:**
- 템플릿 없이 flag 활성화 시 3회 retry 후 **영구 DLQ**(delivery dedupe key `DO NOTHING`이라 이후 틱이 복구 불가). → 마이그레이션 107 적용 확인을 flag 활성화의 hard precondition으로 격상. 표준 배포 스크립트(`compose-redeploy-service.sh`)는 migration 선행이라 경로 준수 시 안전.
- freshness 2h는 취소된 UPCOMING(producer는 사라진 LIVE만 ENDED 처리, UPCOMING은 방치)을 최대 2시간 오발송 후보로 남김. → 기본 30분으로 단축(`BIRTHDAY_STREAM_SESSION_FRESHNESS_MS`, 배포 전 실제 폴 cadence 확인).
- **binary rollback 비호환**: 구 이미지의 renderer는 unknown kind를 거부 → 새 kind pending/retry 행이 남은 상태에서 이미지 rollback 시 전량 DLQ. → rollback 규칙: 동일 이미지에서 flag off가 기본; 이미지 rollback은 `birthday_stream` pending/retry 행 드레인 확인 후.
- TemplateKey 3원 원자성: key 상수 + `GetAllTemplateKeys` 목록/sample map + 시드 migration이 함께 landing해야 seed-render gate 테스트 통과. Task 3·4에 명시.
- all-room fan-out 물량(`V × R`, 자정 축하와 겹치면 멤버당 최대 4R burst)이 무계측. → 활성화 전 `SELECT count(DISTINCT room_id) FROM alarms` 실측을 체크리스트에 추가(egress claim batch 50이 자연 pacing).

**LOW — 수용:**
- `title` nullable → SQL에서 `COALESCE(title, '')`.
- YouTube `channel_id` 없는 생일 멤버(Chzzk/Twitch 전용)는 무음 제외 → 러너에서 empty channel_id skip + debug 로그, 한계 문서화.

## Task 목록

1. **`hololive/hololive-shared/pkg/domain/event_celebration.go`** — `CelebrationKindBirthdayStream = "birthday_stream"`; `CelebrationDispatchPayload`에 `VideoID`, `StreamTitle`, `StreamURL`, `ScheduledStartKST` (전부 `omitempty`, wire 구조체 수정 불필요 — payload 통짜 marshal); `Identity()`에 VideoID 조건부 suffix. 기존 kind identity 불변 테스트 포함.
2. **`hololive/hololive-shared/pkg/domain/alarm_dispatch_source.go`** — `validateCelebrationDispatch`에 birthday_stream VideoID 필수 검사 + 테스트 (빈 VideoID는 identity가 day-key로 붕괴해 per-video dedup 무력화).
3. **`hololive/hololive-shared/pkg/domain/notification_template.go` + `template_sample_data.go` + `template_sample_data_core.go`** — `TemplateKeyCelebrationBirthdayStream = "CELEBRATION_BIRTHDAY_STREAM"` + `GetAllTemplateKeys` 목록 + 샘플 payload를 **한 커밋에** 등록(seed-render gate가 3원 일치를 강제).
4. **`hololive/hololive-api/scripts/migrations/107_seed_notification_birthday_stream_template.sql`** + `manifest.txt` — 077 패턴(`ON CONFLICT ... DO UPDATE`)으로 시드. 본문 예: `🎂 {{.MemberName}} 생일 방송 일정이 잡혔습니다!` + `{{.StreamTitle}}` + `⏰ {{.ScheduledStartKST}}` + `{{.StreamURL}}`. (107 번호는 현 작업 트리 기준 비어 있음 — 구현 시점 재확인.)
5. **`hololive-api` calendar switch 2곳** (`internal/planes/.../calendar.go:170-178`, `:233-239`) — 신규 kind case 처리 (`exhaustive` blocking gate).
6. **`hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_group.go`** — celebration group key에 `Celebration.VideoID` segment 추가(비어있지 않을 때) + `alarm_dispatch_group_test.go`. (현재 키는 `roomID|celebration|kind|channelID`뿐이고 렌더러는 `envelopes[0]`만 렌더 + `MarkDispatched`는 그룹 전체 마킹 — 같은 배치 프레임 2개 중 하나가 무음 유실되는 correctness 버그.)
7. **`hololive/hololive-alarm-worker/internal/app/workerapp/celebration_message.go`** — `case domain.CelebrationKindBirthdayStream` → `TemplateKeyCelebrationBirthdayStream` 렌더 + 테스트.
8. **`hololive/hololive-alarm-worker/internal/app/workerapp/queries/birthday_stream_runner_XXXX_01.sql`** (workerapp `mustSQL` 임베드) —
   ```sql
   SELECT video_id, channel_id, COALESCE(title, '') AS title, status,
          scheduled_start_time, started_at
   FROM youtube_live_sessions
   WHERE channel_id = ANY($1)
     AND ((status = 'UPCOMING' AND scheduled_start_time >= $2 AND scheduled_start_time < $3)
       OR (status = 'LIVE' AND COALESCE(started_at, scheduled_start_time) >= $2 AND COALESCE(started_at, scheduled_start_time) < $3))
     AND last_seen_at >= $4
   ORDER BY COALESCE(started_at, scheduled_start_time), video_id
   ```
   (캡 없음 — 캡은 러너가 outbox 기준으로 집행. 동률 tie-break `video_id`.)
   보조 쿼리: `SELECT count(*) FROM alarm_dispatch_events WHERE event_key LIKE $1` (prefix `celebration:birthday_stream:{channelID}:{date}:`).
9. **`hololive/hololive-alarm-worker/internal/app/workerapp/birthday_stream_runner.go`** (신규) + `_test.go` — celebrationRunner 의존성 미러(memberRepo `FindMembersWithBirthdayOn`, alarmRepo `GetAllDistinctRoomIDs`, pgx pool, `celebrationPublisher`, 주입 가능한 `now`/`sleep`). `Start` = 단순 인터벌 루프. `RunOnce`:
   - 평가 날짜 집합 = `{date(now KST)}` ∪ `{date(now - tickInterval KST)}` (자정 직후 틱은 어제도 평가 — blind window 마감).
   - 날짜별: `FindMembersWithBirthdayOn(month, day)` (없으면 skip — 연중 대부분 인덱스 쿼리 1~2회로 종료) → empty `channel_id` 멤버 skip(debug 로그) → 해당 날짜 KST 경계 UTC 변환 → 세션 쿼리 → 멤버별 기발송 event 수 조회 → 신규 video를 정렬순으로 `3 - 기발송수`개까지 (프레임 × 방) envelope 빌드(`AlarmType: BIRTHDAY`, `SourceKind: celebration`, Kind `birthday_stream`, 해당 날짜의 `Date`, `resolveCelebrationMemberName` 재사용, `domain.YouTubeWatchURL`) → `PublishDispatchBatch` → inserted/duplicate 카운트 로깅. 로컬 상태 없음.
   - 테스트: KST 경계 포함/제외, 자정 직후 어제-평가로 늦은 프레임 잡힘, 생일 없음 no-op, LIVE 상태 포함, 같은 틱 프레임 2개 별도 그룹(group-key fix), 틱 N+K 늦은 프레임 정확히 1건 신규, 리스케줄 무발송, 빈 VideoID validator 거부, outbox 기준 캡(기발송 3건이면 신규 0건 / 기발송 1건이면 신규 최대 2건), 동률 tie-break 결정성, empty channel_id skip.
10. **`build_runtime.go` + `runtime_alarm_worker.go` + `runtime_alarm_worker_runner.go`** — `buildBirthdayStreamRunnerScheduler` (`buildCelebrationRunnerScheduler` 패턴); env: `BIRTHDAY_STREAM_RUNNER_ENABLED`(기본 false), `BIRTHDAY_STREAM_POLL_INTERVAL_MS`(기본 1800000=30분), `BIRTHDAY_STREAM_SESSION_FRESHNESS_MS`(기본 1800000=30분). `BirthdayStreamRunner runtimeAlarmScheduler` 필드 + `panicguard.GoE(..., "alarm-worker-birthday-stream", ...)` 등록. **주의: 이 두 runtime 파일에는 현재 uncommitted 변경이 있음 — 구현은 현 작업 트리 위에서 진행.**

## 검증

```bash
go build ./hololive/hololive-shared/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-api/...
go test ./hololive/hololive-shared/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-api/...
golangci-lint run ./hololive/...   # exhaustive gate — calendar switch 확인
./scripts/architecture/check-migration-manifest.sh
```
마이그레이션 적용 후:
```sql
SELECT template_key FROM notification_templates WHERE template_key = 'CELEBRATION_BIRTHDAY_STREAM';
```
staging에서 synthetic UPCOMING row + flag on으로:
```sql
SELECT event_key, status FROM alarm_dispatch_events WHERE event_key LIKE 'celebration:birthday_stream:%';
```

## 활성화 전제 체크리스트 (hard preconditions)

1. 마이그레이션 107 적용 확인 (템플릿 없이 flag on → 3회 retry 후 **영구 DLQ**, 이후 틱 복구 불가).
2. 운영 DB `members.birthday` 채움 확인 (`manual/seed_member_celebration_dates.sql`은 자동 마이그레이션 미포함).
3. `SELECT count(DISTINCT room_id) FROM alarms` 실측 — fan-out 물량(V×R, 자정 축하 중첩 시 멤버당 최대 4R) 판단.
4. producer live poll cadence 실측 → freshness 기본 30분 적정성 확인.

## Rollback

- 기본: **동일 이미지에서 flag off**. 기존 outbox 행은 정상 드레인, 템플릿은 무해.
- **이미지 rollback은 `birthday_stream` pending/retry 행이 드레인된 후에만** — 구 binary의 renderer는 unknown kind를 거부해 잔여 행 전량이 DLQ로 감.

## 한계 (문서화)

- 커버리지 = "봇 어딘가에 구독이 있는 채널 ∩ 운영 채널"(`youtube_poll_targets.go:54-91`). 구독 0 멤버는 자정 축하는 받아도 와꾸 알람은 발화 불가. 전 로스터 커버리지로 광고하지 말 것.
- YouTube `channel_id` 없는 멤버(Chzzk/Twitch 전용)는 무음 제외.
- 감지 지연 = producer 폴 주기 + 러너 틱(≤30분). UPCOMING 없이 시작하는 게릴라 단명 프레임은 놓칠 수 있음(LIVE 알람이 커버).
- 취소된 UPCOMING은 freshness 창(30분) 동안 오발송 후보로 남을 수 있음.
- 러너는 lease 미보호 — 다중 replica 안전성은 `event_key`/delivery 유니크 제약에 의존(중복 외부 발송은 방어됨, DB 작업량만 replica 수만큼 증가).
- 취소 후 재게시 churn 시 멤버당 발송이 3건을 근소 초과 가능(캡은 outbox event 수 기준 — 취소된 video의 event도 계수에 포함되므로 실제로는 초과보다 미달 방향; 수용).
