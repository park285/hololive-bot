# Runbook: alarm-worker

## Role

`hololive-alarm-worker`는 alarm checker/scheduler, dispatch queue publishing, dispatch queue consumption, generic notification delivery outbox consumption, YouTube outbox egress를 담당합니다.
proactive notification egress의 배타성은 별도 lease가 아니라 PostgreSQL row-claim(`FOR UPDATE SKIP LOCKED`) 조율과 compose 단일 인스턴스 배치(`container_name: hololive-alarm-worker`, 고정 host port `127.0.0.1:30007`)가 함께 보장합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `https://127.0.0.1:30007/health` returns success over H3 |
| Ready | `https://127.0.0.1:30007/ready` returns `status=ready`; authenticated `http://127.0.0.1:30097/diagnostics/workers` reports the exact three-worker registry |
| Logs | scheduler/checker loops run without repeated DB/cache errors |
| Queue | publishes to and consumes due rows from `alarm_dispatch_deliveries`; Valkey wakeup tokens are only a polling optimization |
| Delivery outbox | consumes `notification_delivery_outbox` rows for major event/member news proactive sends |
| Egress instance | exactly one `hololive-alarm-worker` container is running; `NOTIFICATION_EGRESS_ROLE=owner` and `NOTIFICATION_SCHEDULER_ROLE=worker` |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | alarm state lookup fails |
| Valkey | yes | dispatch queue/cache/PubSub fail |
| Iris | yes | proactive notification egress |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP health port | yes |
| `NOTIFICATION_SCHEDULER_ROLE` | scheduler enablement | yes |
| `STACK_WORKER_PROFILE_FILE` | strict `hololive/alarm-worker` profile containing `alarm_dispatch`, `notification_delivery`, `youtube_delivery` | yes |
| `BOT_MARKDOWN_REPLIES` | 확인된 오픈채팅의 카카오 네이티브 Markdown 전송 여부; 기본값 `false`. 명령 응답과 알림에 공통 적용 | no |
| `BOT_SEE_MORE_FOLD` | worker 렌더에는 쓰지 않음. 공통 설정 로딩이 bool로 검증하므로 잘못된 값이면 worker도 기동 실패; 기본 `true` | no |
| `ALARM_SHORT_LINK_BASE_URL` | grouped message path의 YouTube short-link origin | no |
| `BIRTHDAY_STREAM_RUNNER_ENABLED` | matching birthday greeting이 sent인 방에만 birthday stream event를 생산 | production policy |
| `BIRTHDAY_STREAM_POLL_INTERVAL_MS` | birthday stream session 평가 주기; 기본 30분 | no |
| `BIRTHDAY_STREAM_SESSION_FRESHNESS_MS` | stale UPCOMING/LIVE 제외 창; 기본 30분 | no |
| `CACHE_*` | Valkey connection | yes |
| `POSTGRES_*` | DB connection | yes |

`ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH`(양의 정수, 기본 1000), `CELEBRATION_CHECK_HOUR_KST`(0–23, 기본 0), `CELEBRATION_RUN_INTERVAL_MS`·`BIRTHDAY_STREAM_*_MS`(양의 밀리초)는 값이 없거나 비어 있으면 기본값을 씁니다. 정수가 아니거나 범위를 벗어나면 기본값으로 바꾸지 않고 기동에 실패합니다. celebration·birthday stream 키는 해당 runner가 켜졌을 때만 읽습니다.

## Notification egress

Alarm-worker는 기본적으로 오픈채팅과 일반채팅 모두 기존 `kakaoformat.Render` 변환 렌더러로 전송합니다. 카카오톡 자체 Markdown 렌더링에 의존하지 않도록 운영 설정도 `BOT_MARKDOWN_REPLIES=false`로 맞춥니다. 기존 환경에 명시한 `true`는 기본값 변경보다 우선하므로 배포 시 확인해야 합니다. Alarm-worker는 Karing template을 보내지 않습니다(`DEC-20260926-hololive-karing-egress-disposition`, `DEC-20260904-hololive-karing-regular-chat-egress` 대체).

| Room / notification | Egress |
|---|---|
| 일반채팅의 broadcast/video/Shorts/community 및 통합 방송 | `kakaoformat.Render` 일반 텍스트 |
| 오픈채팅 + `BOT_MARKDOWN_REPLIES=true` | Markdown 원문 (`[title](url)` 링크 유지) |
| 오픈채팅 + `BOT_MARKDOWN_REPLIES=false` | `kakaoformat.Render` 일반 텍스트 |
| 방 유형 미확인 | 일반 텍스트 |
| Twitch-only, Chzzk-only, celebration, delivery digest, YouTube milestone, generic notification delivery | 위 방 유형 규칙 적용 |

일반 텍스트는 `kakaoformat.Render`를 거칩니다. Markdown resolver는 오픈채팅 여부만 제공합니다. Karing 선택 분기, chunk planner, Karing sender는 삭제했고, Markdown lane이 쓰는 handoff 확인(`sendoutcome.ErrHandoffOutcomeUnknown`, `sendoutcome.ErrHandoffFailed`)만 남아 있습니다.

방송·선행공개·영상·쇼츠·커뮤니티·축하·생일 방송·X 스페이스 알림은 단일·묶음·길이와 무관하게 자동 전체보기 패딩을 넣지 않습니다. `BOT_SEE_MORE_FOLD`는 API의 조회·보고서 렌더 정책에만 쓰이지만, worker도 공통 설정 로딩에서 값 형식을 검증합니다. worker는 사용자 template/채널 override에 직접 들어 있는 패딩과 저장된 예약 `PreRenderedMessage`, 재전송 요청의 본문·route·ID를 보존합니다. 로컬 검증은 실제 DB template→fake Iris 최종 payload의 항목·URL·직접 패딩 보존을 확인합니다. 운영 메시지 발송은 승인된 테스트 방에서 별도로 수행합니다.

이전 Karing 전송의 `outcome_unknown`이나 `SENDING` 기록은 텍스트 전환을 이유로 재발송하지 않습니다. 기존 quarantine 및 stale sweeper 계약을 유지합니다. T18(2026-09-26)에서 v1·v3 원장의 Karing 비종단 행이 0건임을 확인했습니다.

`YOUTUBE_OUTBOX_KARING_ENABLED`와 `ALARM_DISPATCH_KARING_ENABLED`는 퇴역했습니다. 값이 비어 있어도 runtime file에 key가 존재하면 startup이 실패합니다. `ALARM_SHORT_LINK_BASE_URL`은 기존 grouped message path를 위해 유지되며, `hololive-api`의 `127.0.0.1:30101` listener와 중앙·Seoul ingress도 계속 유지합니다.

`ALARM_SHORT_LINK_BASE_URL=https://short.holoshi.com`을 사용하면 두 개 이상의 message-path 방송 알림에서 YouTube URL만 `/l/<videoID>`로 바뀝니다. Provider-first로 listener, 중앙 ingress, Seoul public route와 `scripts/deploy/shortlink-smoke.sh`를 검증한 뒤 consumer를 재기동합니다.

Production 반영은 대상 승인을 받은 뒤 exact arm64 artifact의 no-build deploy와 replica 1/readiness 확인으로 수행합니다. 실제 메시지 smoke는 별도로 승인된 test room에서 오픈채팅 Markdown 원문 또는 일반채팅 일반 텍스트 수신과 lifecycle 단일 완료를 확인합니다.

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-alarm-worker
```

## Readiness

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/ready
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck --api-key-env API_SECRET_KEY https://127.0.0.1:30007/internal/ready
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck --body-api-key-env API_SECRET_KEY http://127.0.0.1:30097/diagnostics/workers
```

`/ready` fails closed when PostgreSQL or Valkey is unavailable. Worker enablement and effective executor/queue state are reported by the authenticated metrics-plane `/diagnostics/workers`; production requires all three profile executors enabled.

## Metrics

- `alarm_dispatch_pg_quarantined_rows`: DB에 보존된 격리 알림 수. 신규 격리 발생 counter와 구분하며, closeout receipt로 검토를 마친 행도 retention 삭제 전까지 포함한다.
- `alarm_dispatch_pg_oldest_quarantined_age_seconds`: 보존된 격리 중 가장 오래된 격리 시점부터의 경과.
- `alarm_dispatch_pg_unreviewed_quarantined_rows`: 처분 검토가 필요한 격리 알림 수. [closeout receipt](alarm-dispatch-quarantine-closeout.md)의 같은 send unit·대상 ID 항목이 행의 현재 `updated_at`·상태·시도 횟수·격리/발송/취소 시각과 정확히 일치할 때만 뺀다. receipt가 있어도 재처리·재격리로 바뀐 행은 다시 센다.
- `alarm_dispatch_pg_oldest_unreviewed_quarantined_age_seconds`: 검토가 필요한 격리 중 가장 오래된 격리 시점부터의 경과.
- `alarm_dispatch_pg_backlog_snapshot_success`: 마지막 발송·격리 집계 조회의 성공 여부. receipt 조회 실패도 실패이며, 0이면 건수·경과의 이전 값을 현재 상태로 해석하지 않는다.

격리 현황은 Grafana Bot Drilldown에서 확인한다. 검토 대상 건수 경보는 전송 증거 검토를 위한 알림이며 자동 재발송·삭제 권한을 부여하지 않는다. closeout receipt는 보존 총량을 줄이지 않으며 기존 보존 기간과 처분 계약을 유지한다.

## Outbox 파이프라인 소유

v1 YouTube 알림(`youtube_delivery`, `youtube_notification_delivery`)과 v2 digest(`notification_delivery`, `notification_delivery_outbox`)는 v3 alarm-dispatch ledger로 넘기지 않는 정본 파이프라인이고 각 executor가 direct egress를 소유합니다(`DEC-20260926-hololive-outbox-v3-convergence`). v3 handoff(`off`/`shadow`/`cutover`), 비교 전용 `shadowed` 상태, handoff metric은 삭제했습니다.

`YOUTUBE_OUTBOX_V3_HANDOFF_MODE`와 `DELIVERY_OUTBOX_V3_HANDOFF_MODE`는 퇴역 키입니다. 빈 값이라도 env에 있으면 alarm-worker와 hololive-api가 기동을 거절합니다. 제거 조건과 재검토 기한은 `hololive/hololive-shared/pkg/config/settings/config_outbox_v3_handoff_retired_env.go`가 소유합니다.

## Common failure modes

### 1. Alarm queue stops growing despite due events

Symptoms:
- Expected alarms are not dispatched.
- YouTube outbox dispatcher has no new send errors.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck --body-api-key-env API_SECRET_KEY http://127.0.0.1:30097/diagnostics/workers
```

Mitigation:
- Check PostgreSQL, Valkey, scheduler role, and alarm state.
- Verify exactly one alarm-worker instance is running: `docker ps --filter name=hololive-alarm-worker --format '{{.Names}}\t{{.Status}}'`.
- Verify PostgreSQL row-claim state is draining and not stuck after its lock expires:

```bash
docker exec -i holo-postgres sh -lc 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
SELECT status,
       count(*),
       min(next_attempt_at) AS oldest_next_attempt,
       min(lock_expires_at) FILTER (WHERE status IN ('leased', 'sending')) AS oldest_lock_expiry
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
SQL
```

Rows left in `leased` or `sending` past `lock_expires_at` mean the consumer died mid-batch; recovery re-claims them on the next recovery interval.

Rollback:
- Roll back the alarm-worker image/config that changed checker or queue publishing behavior.

### 2. Settings update not applied

공개 `alarmAdvanceMinutes` 범위는 `1..1440`입니다. 0과 범위 밖 값은 파일 저장·worker 호출 전에 거절합니다.
저장 실패에서는 기존 Get·disk 값과 worker 미호출을 유지합니다. 저장 뒤 적용 응답이 유실되면 실제 worker 상태는
결과 불명이며, 자동 재시도나 저장 파일 rollback을 하지 않습니다.

Symptoms:
- Alarm advance minutes remains stale.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 hololive-alarm-worker
```

Mitigation:
- Check the admin settings response `runtime.alarm_applied`/`alarm_reason`; the only apply path is `hololive-api` → `PUT /internal/alarm/settings` (no Pub/Sub re-apply).
- Re-submit the setting once alarm-worker is reachable, or restart alarm-worker (it restores target minutes from `settings.json`).

Rollback:
- Roll back the `hololive-api` settings apply change.

### 3. Grouped short links do not suppress previews

Symptoms:
- Grouped message-path alarm에 YouTube 원본 URL이 남습니다.
- KakaoTalk이 YouTube preview를 생성합니다.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker printenv ALARM_SHORT_LINK_BASE_URL
./scripts/deploy/shortlink-smoke.sh
```

Expected:
- regular request: `302` with a YouTube `Location` header
- Kakao scraper request: `403` without a `Location` header

Mitigation:
- Public ingress가 `/l/*`를 bot plane으로 전달하고 `User-Agent`를 보존하는지 확인합니다.
- `ALARM_SHORT_LINK_BASE_URL=https://short.holoshi.com`인지 확인하고 alarm-worker를 재기동합니다.

Rollback:
- `ALARM_SHORT_LINK_BASE_URL`을 비우고 alarm-worker를 재기동합니다. 이미 발송된 URL을 위해 listener와 양쪽 ingress는 유지합니다.

### 4. Markdown admission 이후 delivery가 완료되지 않음

Symptoms:
- Iris Markdown 발송은 `202 Accepted`를 반환했지만 alarm delivery가 `sent`로 전이되지 않습니다.
- 로그에 reply handoff failure 또는 outcome unknown이 있고 alarm dispatch는 quarantine되거나 YouTube delivery가 `SENDING`에 남습니다.

Diagnosis:
- Raw `requestId`를 로그나 응답에 복사하지 않고 bounded alarm-worker/Iris 로그에서 status state와 오류 class만 확인합니다.
- `queued`, `preparing`, `prepared`, `sending`이 caller deadline까지 계속되었는지, `failed` 또는 `outcome_unknown`으로 끝났는지 확인합니다.
- `handoff_completed`가 없는데 DB row만 수동으로 `sent` 처리하지 않습니다.

Mitigation:
- Iris reply delivery worker와 Kakao bridge를 먼저 복구합니다.
- Outcome unknown인 alarm을 일반 텍스트로 재발송하지 않습니다. YouTube `SENDING` row는 기존 stale sweeper 계약에 맡깁니다.
- 퇴역 환경변수로 다른 경로를 다시 켜거나 startup guard를 우회하지 않습니다.

Rollback:
- Markdown post 뒤 결과가 불명확한 delivery가 있으면 이전 alarm-worker image를 시작하지 않습니다. Exact pending/sending 범위와 prior artifact의 egress 차이를 제시하고 별도 승인을 받습니다.

### 5. 생일축하는 갔지만 생일 방송 알람이 생성되지 않음

Diagnosis:
- `celebration:birthday:{channelID}:{date}` event와 그 delivery의 `status`, `sent_at`을 확인합니다. `sent`가 아닌 방은 의도적으로 대상이 아닙니다.
- 같은 `channelID`의 당일 `youtube_live_sessions`가 `UPCOMING` 또는 `LIVE`인지 확인합니다. UPCOMING은 `schedule_observed_at`, LIVE는 `status_observed_at`이 runner의 freshness 하한부터 현재 시각 사이에 있어야 합니다. NULL·미래 관측은 제외하며 `last_seen_at`이나 예정 시작 시각으로 최신성을 판단하지 않습니다.
- `Birthday stream runner failed` 로그가 있으면 audience SQL 오류를 먼저 해결합니다. 이 경로는 실패 시 전체 방으로 fallback하지 않습니다.
- 이미 `celebration:birthday_stream:{channelID}:{date}:{videoID}` event가 있어도 현재 세션이면 다음 tick에서 재평가됩니다. 이때 최초 event의 canonical payload를 재사용하므로 이후 title·photo·schedule 표시값 변경은 event payload를 바꾸지 않고, 새 방 delivery만 outbox dedupe를 통과합니다.

Mitigation:
- producer의 full-roster LIVE discovery부터 복구한 뒤 runner를 재평가합니다.
- birthday greeting delivery를 수동으로 sent 처리하거나 birthday stream을 전체 방에 재전송하지 않습니다.

Rollback:
- 구 alarm-worker image는 전체 방 fan-out 의미를 가지므로 rollback 전에 `BIRTHDAY_STREAM_RUNNER_ENABLED=false`로 runner를 중지합니다.

### 6. 5분 전 알림 뒤에 종료 직후 `방송 시작`이 다시 나감

Symptoms:
- 예정 시각 기준 `방송 5분 전`은 나갔다.
- 실제 시작 시각의 `방송 시작`은 없다.
- 방송이 끝난 직후 같은 영상에 `방송 시작`이 다시 나간다.
- `youtube_notification_outbox`의 `NEW_VIDEO`/`LIVE_STREAM` 행은 없다.

Diagnosis:
- `YOUTUBE_LIVE_CATCHUP_DEDUPE_COLLISION_20260817.md`
- `alarm_dispatch_events`에서 같은 `stream_id`의 첫 행은 `status=upcoming`, `minutes_until=5`이고, 마지막 행은 `status=live`에 `start_actual`이 있으며 `start_scheduled`가 1–2분 이동했는지 본다.
- `youtube_live_sessions.live_first_seen_at`가 실제 시작 근처면 수집 지연이 아니라 checker dedupe 쪽이다.

Mitigation:
- `YOUTUBE_LIVE_CATCHUP_DEDUPE_COLLISION_20260817.md`의 수정이 포함된 alarm-worker image를 배포한다.
- 배포 후 live catchup event category가 `live_catchup`이고, 같은 `stream_id`의 기존 sent room이 예정 시각 보정 뒤에도 다시 선택되지 않는지 확인한다.

Rollback:
- 수정 전 image로 rollback하면 같은 증상이 다시 열리므로, 이 결함만으로는 rollback하지 않고 current revision을 fix-forward한다.

### 7. 최초공개 `공개 예정` 뒤에 같은 영상의 `방송 5분 전`이 다시 나감

Symptoms:
- `youtube_notification_outbox`에 해당 `video_id`의 `NEW_VIDEO`가 있고 문구는 `N분 후 공개 예정` 또는 `최초공개`다.
- 같은 `stream_id`의 `alarm_dispatch_events`에 `alarm_type=LIVE` upcoming 5분 전 또는 live catchup이 있다.

Diagnosis:
- `youtube_live_sessions.is_premiere`가 `true`인데도 live checker가 후보로 삼았는지 본다.
- Holodex live 목록만 보고 `LoadConfirmedPremiereIDs` 분류를 건너뛴 revision이면 이 계약 위반이다.
- `DEC-20260830-hololive-premiere-content-owned-notifications`

Mitigation:
- 현재 alarm-worker revision에서 확정 최초공개 video_id는 upcoming과 live catchup 후보에서 빠지는지 확인한다.

Rollback:
- 수정 전 image로 rollback하면 같은 영상에 영상 알림과 라이브 5분 전이 다시 겹치므로, 이 결함만으로는 rollback하지 않고 current revision을 fix-forward한다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/ready
./scripts/deploy/shortlink-smoke.sh
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- migration 231~233 적용 뒤의 rollback은 image만 되돌리고 `--no-deps`로 `hololive-alarm-worker`·`hololive-api`를
  함께 교체하며 구 `hololive-db-migrate`를 실행하지 않습니다(`rollback.md`의 Valkey 2차 축소 절). 구 worker는 관리자
  방 이름을 Valkey `alarm:room_names`에만 쓰므로 rollback 기간에도 Console 방 이름 변경 동결을 유지합니다.
  retag하면 `hololive-db-migrate`(`hololive-api:prod`)도 구 runner가 되므로, rollback 창에는 중앙 host를 재부팅하지
  않고 `hololive-compose.service`를 disable합니다. 재부팅의 `systemd-compose-up.sh`는 `--no-deps` 없는 `up`이라 구
  runner가 거절되고 API·worker·중앙 collector가 기동하지 못합니다. 이때 복구는 7.0.0 image로 재전진입니다(`rollback.md`).
- 관리자 방 별칭 이관(`HGETALL alarm:room_names` export → migration 232 뒤 차이 값만 `alarm_room_display_names`에
  `ON CONFLICT DO NOTHING`)과 폐기 key 회수 순서는
  [Valkey 축소 계획](../plans/2026-09-28-valkey-dependency-reduction.md#2차-전환-관리자-방-별칭-이관)을 따릅니다.
- Stack Worker Contract v1 이전 image는 현재 profile/config와 호환되지 않습니다. 승인된
  backup에 기록된 release/profile/config 전체를 한 쌍으로 복원해야 하며, current tree의
  설정을 유지한 채 이전 image만 재기동하는 rollback은 지원하지 않습니다.
- paired rollback이 준비되지 않았거나 send-unit schema 호환성이 확인되지 않으면 이전
  image로 전환하지 않고 current revision을 fix-forward합니다.
- delivery telemetry 단일 경로(`DEC-20260926-hololive-delivery-telemetry-single-path`) 이전
  image로 되돌리려면 운영 profile에 `youtube_delivery.telemetry_backfill_batch`를 다시 넣고
  같은 유지보수 단계에서 image를 전환합니다. exact-key 디코더라 키가 없으면 이전 image가
  기동하지 않습니다(`rollback.md`).
- Preserve and inspect `alarm:dispatch:*` queues before replaying or deleting queue data.

## Related contracts

- `../contracts/alarm.md`
- `../contracts/shortlink.md`
- `../contracts/settings.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
