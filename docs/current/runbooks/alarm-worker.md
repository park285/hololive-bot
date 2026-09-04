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
| `YOUTUBE_OUTBOX_V3_HANDOFF_MODE` | `off`, `shadow`, `cutover`; v1 delivery rows를 v3 ledger로 넘기는 모드 | no; default `off` |
| `BOT_MARKDOWN_REPLIES` | 확인된 오픈채팅의 기존 Markdown message lane | no |
| `ALARM_SHORT_LINK_BASE_URL` | grouped message path의 YouTube short-link origin | no |
| `BIRTHDAY_STREAM_RUNNER_ENABLED` | matching birthday greeting이 sent인 방에만 birthday stream event를 생산 | production policy |
| `BIRTHDAY_STREAM_POLL_INTERVAL_MS` | birthday stream session 평가 주기; 기본 30분 | no |
| `BIRTHDAY_STREAM_SESSION_FRESHNESS_MS` | stale UPCOMING/LIVE 제외 창; 기본 30분 | no |
| `CACHE_*` | Valkey connection | yes |
| `POSTGRES_*` | DB connection | yes |

## Notification egress

Alarm-worker는 방 유형과 알림 compatibility를 모두 확인한 뒤 전송 경로를 선택합니다.

| Room / notification | Egress |
|---|---|
| 확인된 일반채팅 + YouTube target이 있는 broadcast/video/Shorts/community | Karing content-list |
| 확인된 일반채팅 + YouTube와 Chzzk 통합 방송 | YouTube target의 Karing content-list |
| 오픈채팅 | `BOT_MARKDOWN_REPLIES=true`이면 기존 Markdown, 아니면 일반 텍스트 |
| 방 유형 미확인 | 일반 텍스트 |
| Twitch-only, Chzzk-only, celebration, delivery digest, YouTube milestone, generic notification delivery | 방 유형에 따른 기존 message path |

일반 텍스트는 `kakaoformat.Render`를 거칩니다. 지원되는 Karing 알림의 build, admission 또는 handoff가 실패해도 Markdown이나 일반 텍스트로 fallback하지 않습니다. 오픈채팅과 미확인 방은 Karing 호출 전에 message path로 결정되므로 fallback이 아닙니다.

Karing live send의 `202 Accepted`는 성공이 아닙니다. Alarm-worker는 응답의 exact `requestId`로 Iris `/reply-status/{requestId}`를 조회하고 `handoff_completed`를 확인한 뒤에만 dispatch/outbox를 성공 처리합니다. `failed`는 확정 실패이고 `outcome_unknown`, 알 수 없는 상태, request ID 불일치, 빈 응답과 확인 deadline 소진은 결과 불명확입니다. 결과 불명확 상태는 alarm dispatch에서 quarantine되고 YouTube outbox에서는 `SENDING`으로 남아 stale sweeper가 처리하며 다시 post하지 않습니다.

`YOUTUBE_OUTBOX_KARING_ENABLED`와 `ALARM_DISPATCH_KARING_ENABLED`는 퇴역했습니다. 값이 비어 있어도 runtime file에 key가 존재하면 startup이 실패합니다. `ALARM_SHORT_LINK_BASE_URL`은 기존 grouped message path를 위해 유지되며, `hololive-api`의 `127.0.0.1:30101` listener와 중앙·Seoul ingress도 계속 유지합니다.

`ALARM_SHORT_LINK_BASE_URL=https://short.holoshi.com`을 사용하면 두 개 이상의 message-path 방송 알림에서 YouTube URL만 `/l/<videoID>`로 바뀝니다. Provider-first로 listener, 중앙 ingress, Seoul public route와 `scripts/deploy/shortlink-smoke.sh`를 검증한 뒤 consumer를 재기동합니다.

승인된 production 전환은 alarm-worker 중지, static-secret master와 host runtime file의 두 퇴역 flag 제거, exact arm64 artifact의 no-build deploy, replica 1/readiness 확인 순서로 수행합니다. 실제 Karing smoke는 명시적으로 승인된 일반채팅 test room에만 보내고 `handoff_completed`와 lifecycle 단일 완료를 함께 확인합니다. 오픈채팅 test room에서는 기존 Markdown lane이 유지되는지도 별도 승인 아래 확인합니다.

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

- `hololive_youtube_outbox_v3_handoff_total{mode,result}`: v1→v3 handoff delivery row 수.
- `hololive_delivery_outbox_v3_handoff_total{mode,result}`: v2→v3 handoff delivery row 수.

## Outbox v3 handoff

`shadow`는 v3에 `shadowed` row를 기록한 뒤 기존 v1/v2 direct egress를 유지합니다. Shadow write 실패는 기존 발송을 막지 않으며 위 metric의 `result="failure"`와 로그로 드러납니다. `cutover`는 v3 `pending` 저장 성공을 legacy outbox의 완료 기준으로 사용하고 외부 발송은 alarm-dispatch consumer만 수행합니다.

운영 전환은 다음 조건을 모두 확인한 뒤 별도 승인으로 진행합니다.

1. migration 141~143이 적용되고 `alarm_dispatch.executor.enabled=true`인 profile이 배포되어 있습니다.
2. `shadow` 기간 동안 handoff failure가 0이고 legacy 대상 수와 v3 `shadowed` 대상 수가 일치합니다.
3. v1은 `youtube_delivery.executor.enabled=true`를 유지한 채 `YOUTUBE_OUTBOX_V3_HANDOFF_MODE=cutover`로 바꿉니다. 이 executor가 claim과 handoff를 함께 소유합니다.
4. v2는 producer인 hololive-api에서 `DELIVERY_OUTBOX_V3_HANDOFF_MODE=cutover`를 설정하고, 기존 `notification_delivery_outbox` backlog가 0이 된 뒤 승인된 새 profile에서만 `notification_delivery.executor.enabled=false`로 전환합니다.

Rollback 시 새 producer handoff mode를 먼저 `off`로 되돌립니다. 이미 v3 `pending`/`sending`인 delivery가 있으면 legacy direct egress를 다시 켜기 전에 drain 또는 명시적 quarantine 여부를 판단해야 중복 발송을 피할 수 있습니다.

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

Symptoms:
- Alarm advance minutes remains stale.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 hololive-alarm-worker
```

Mitigation:
- Verify `config:update` subscriber wiring and perform source-of-truth refresh if available.

Rollback:
- Roll back settings publisher/consumer change.

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

### 4. Karing admission 이후 delivery가 완료되지 않음

Symptoms:
- Iris `/karing/content-list`는 `202 Accepted`를 반환했지만 alarm delivery가 `sent`로 전이되지 않습니다.
- 로그에 Karing handoff failure 또는 outcome unknown이 있고 alarm dispatch는 quarantine되거나 YouTube delivery가 `SENDING`에 남습니다.

Diagnosis:
- Raw `requestId`를 로그나 응답에 복사하지 않고 bounded alarm-worker/Iris 로그에서 status state와 오류 class만 확인합니다.
- `queued`, `preparing`, `prepared`, `sending`이 caller deadline까지 계속되었는지, `failed` 또는 `outcome_unknown`으로 끝났는지 확인합니다.
- `handoff_completed`가 없는데 DB row만 수동으로 `sent` 처리하지 않습니다.

Mitigation:
- Iris reply delivery worker와 Kakao bridge를 먼저 복구합니다.
- Outcome unknown인 alarm을 Karing 또는 일반 텍스트로 재발송하지 않습니다. YouTube `SENDING` row는 기존 stale sweeper 계약에 맡깁니다.
- 퇴역 환경변수로 plain-text 경로를 다시 켜거나 startup guard를 우회하지 않습니다.

Rollback:
- Karing post 뒤 결과가 불명확한 delivery가 있으면 이전 alarm-worker image를 시작하지 않습니다. Exact pending/sending 범위와 prior artifact의 egress 차이를 제시하고 별도 승인을 받습니다.

### 5. 생일축하는 갔지만 생일 방송 알람이 생성되지 않음

Diagnosis:
- `celebration:birthday:{channelID}:{date}` event와 그 delivery의 `status`, `sent_at`을 확인합니다. `sent`가 아닌 방은 의도적으로 대상이 아닙니다.
- 같은 `channelID`의 당일 `youtube_live_sessions`가 `UPCOMING` 또는 `LIVE`이고 `last_seen_at` freshness 안에 있는지 확인합니다.
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
- Stack Worker Contract v1 이전 image는 현재 profile/config와 호환되지 않습니다. 승인된
  backup에 기록된 release/profile/config 전체를 한 쌍으로 복원해야 하며, current tree의
  설정을 유지한 채 이전 image만 재기동하는 rollback은 지원하지 않습니다.
- paired rollback이 준비되지 않았거나 send-unit schema 호환성이 확인되지 않으면 이전
  image로 전환하지 않고 current revision을 fix-forward합니다.
- Preserve and inspect `alarm:dispatch:*` queues before replaying or deleting queue data.

## Related contracts

- `../contracts/alarm.md`
- `../contracts/karing-kakaolink.md`
- `../contracts/shortlink.md`
- `../contracts/settings.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
