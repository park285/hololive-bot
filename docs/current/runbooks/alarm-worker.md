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
| `STACK_WORKER_PROFILE_FILE` | strict `hololive/alarm-worker` profile containing `alarm_dispatch`, `notification_delivery`, `youtube_delivery`. `alarm_dispatch.quarantine_threshold_ms`가 `lease_ms`보다 작으면 기동에 실패하므로 배포 전에 운영 프로필 값을 확인합니다 | yes |
| `BOT_MARKDOWN_REPLIES` | 확인된 오픈채팅의 카카오 네이티브 Markdown 전송 여부; 기본값 `false`. 명령 응답과 알림에 공통 적용 | no |
| `BOT_SEE_MORE_FOLD` | worker 렌더에는 쓰지 않음. 공통 설정 로딩이 bool로 검증하므로 잘못된 값이면 worker도 기동 실패; 기본 `true` | no |
| `ALARM_SHORT_LINK_BASE_URL` | grouped message path의 YouTube short-link origin | no |
| `BIRTHDAY_STREAM_RUNNER_ENABLED` | matching birthday greeting이 sent인 방에만 birthday stream event를 생산 | production policy |
| `BIRTHDAY_STREAM_POLL_INTERVAL_MS` | birthday stream session 평가 주기; 기본 30분 | no |
| `BIRTHDAY_STREAM_SESSION_FRESHNESS_MS` | stale UPCOMING/LIVE 제외 창; 기본 30분 | no |
| `CACHE_*` | Valkey connection | yes |
| `POSTGRES_*` | DB connection | yes |

`ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH`(양의 정수, 기본 1000), `CELEBRATION_CHECK_HOUR_KST`(0–23, 기본 0), `CELEBRATION_RUN_INTERVAL_MS`·`BIRTHDAY_STREAM_*_MS`(양의 밀리초)는 키가 없으면 기본값을 씁니다. 키가 있는데 비어 있거나 공백뿐이면, 또는 정수가 아니거나 범위를 벗어나면 기본값으로 바꾸지 않고 기동에 실패합니다. `ALARM_DISPATCH_RETENTION_*` 키와 같은 규칙입니다. 배포 전에 alarm-worker env에 `KEY=` 형태의 빈 값이 없는지 확인합니다. celebration·birthday stream 키는 해당 runner가 켜졌을 때만 읽습니다.

## Notification egress

Alarm-worker는 기본적으로 오픈채팅과 일반채팅 모두 기존 `kakaoformat.Render` 변환 렌더러로 전송합니다. 카카오톡 자체 Markdown 렌더링에 의존하지 않도록 운영 설정도 `BOT_MARKDOWN_REPLIES=false`로 맞춥니다. 기존 환경에 명시한 `true`는 기본값 변경보다 우선하므로 배포 시 확인해야 합니다. Alarm-worker는 Karing template을 보내지 않습니다(`DEC-20260926-hololive-karing-egress-disposition`, `DEC-20260904-hololive-karing-regular-chat-egress` 대체).

| Room / notification | Egress |
|---|---|
| 일반채팅의 broadcast/video/Shorts/community | `kakaoformat.Render` 일반 텍스트 |
| 오픈채팅 + `BOT_MARKDOWN_REPLIES=true` | Markdown 원문 (`[title](url)` 링크 유지) |
| 오픈채팅 + `BOT_MARKDOWN_REPLIES=false` | `kakaoformat.Render` 일반 텍스트 |
| 방 유형 미확인 | 일반 텍스트 |
| celebration, delivery digest, YouTube milestone, generic notification delivery | 위 방 유형 규칙 적용 |

일반 텍스트는 `kakaoformat.Render`를 거칩니다. Markdown resolver는 오픈채팅 여부만 제공합니다. Karing 선택 분기, chunk planner, Karing sender는 삭제했습니다. 텍스트·Markdown sender는 Iris `202` admission만 확인하고(증명하지 못하면 `sendoutcome.ErrAdmissionOutcomeUnknown`) `/reply-status`를 기다리지 않으므로, sender 성공은 durable admission만 뜻합니다. handoff 확인은 alarm dispatch·notification·youtube handoff reconciler만 합니다.

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
- `alarm_dispatch_pg_backlog_snapshot_last_success_timestamp_seconds`: 마지막으로 성공한 발송·격리 집계 조회의 Unix 시각. 실패하면 갱신하지 않으므로, 경과가 관측 주기보다 크게 벌어지면 건수·경과를 현재 상태로 해석하지 않는다.
- `alarm_dispatch_pg_transition_partial_total{action,reason}`: alarm dispatch 소유자 전이가 일부 또는 전체 대상에서 fence 미스로 끝난 횟수. `action`은 `begin_send_request`·`mark_sent`·`route_failures`·`route_sending_failures`·`requeue_pre_send`·`release_leased`·`move_to_dlq`·`quarantine`·`load_send_request`·`pin_send_request`·`reissue_send_request`, `reason`은 `sendfence` 원인이며 한 문장에서 원인마다 한 번 센다. `mark_sent`의 증가는 외부 발송 뒤 행이 이미 다른 상태라는 뜻이므로 격리 검토 대상이다.
- `hololive_db_pool_*{pool="alarm_worker"}`: DB pool의 획득 횟수·대기·빈 pool 대기와 상태별 연결 수, 최대 연결 수. 이름과 해석은 [hololive-api runbook](hololive-api.md#db-poolyoutube-plane-db-슬롯-지표)과 같다.
- `hololive_http_client_requests_total{client="holodex"}`, `hololive_http_client_request_duration_seconds{client="holodex"}`: Holodex HTTP 요청의 상태 코드별 수(응답 없음은 `code="error"`)와 응답 헤더까지의 지연. 해석은 [hololive-api runbook](hololive-api.md#외부-http-호출-지표)과 같다.

발송 backlog(`alarm_dispatch_pg_backlog_rows`, `alarm_dispatch_pg_oldest_*_age_seconds`)·격리 gauge와 두 snapshot 지표는 retention 주기(`ALARM_DISPATCH_RETENTION_INTERVAL_MS`)와 별개로 60초마다(조회 시한 10초) 갱신한다. 조회 실패는 runner를 멈추지 않고 `alarm_dispatch_pg_backlog_observation_failed_total`과 경고 로그로 남는다.

격리 현황은 Grafana Bot Drilldown에서 확인한다. 검토 대상 건수 경보는 전송 증거 검토를 위한 알림이며 자동 재발송·삭제 권한을 부여하지 않는다. closeout receipt는 보존 총량을 줄이지 않으며 기존 보존 기간과 처분 계약을 유지한다.

## Outbox 파이프라인 소유

v1 YouTube 알림(`youtube_delivery`, `youtube_notification_delivery`)과 v2 digest(`notification_delivery`, `notification_delivery_outbox`)는 v3 alarm-dispatch ledger로 넘기지 않는 정본 파이프라인이고 각 executor가 direct egress를 소유합니다(`DEC-20260926-hololive-outbox-v3-convergence`). v3 handoff(`off`/`shadow`/`cutover`), 비교 전용 `shadowed` 상태, handoff metric은 삭제했습니다.

### YouTube 알람 상태 선점 열 삭제(migration 331)

migration 331은 `youtube_community_shorts_alarm_states`의 발송 전 선점 열 `authorized_at`, 그 부분 색인
`idx_ycsas_authorized_at`, `delivery_status`의 `ENQUEUED` 어휘를 지우고, 남은 `ENQUEUED` 행을 `alarm_sent_at`에
따라 `DETECTED`·`SENT`로 맞춥니다. 이 표를 쓰는 binary는 `hololive-api`(canonicalwrite upsert)와
`hololive-alarm-worker`(발송 성공 전이의 발송 표시)입니다. 열이 사라지면 이 열을 참조하는 binary의 문장이 실패합니다.

| binary | `authorized_at` 참조 | 331 뒤 |
|---|---|---|
| post 단위 claim 제거 이전 API·worker | upsert 열 목록, claim 획득·해제, 발송 표시 | upsert·발송이 실패 |
| claim 제거 릴리스(CHANGELOG "YouTube community·shorts 발송 전 post 단위 claim 제거")의 API·worker | 없음 | 정상 |
| 331을 포함한 릴리스의 API·worker | 없음 | 정상 |

`db-migrate`는 앱 교체보다 먼저 돌기 때문에 331을 claim 제거와 같은 릴리스에 넣지 않습니다.

- 적용 조건: claim 제거 릴리스가 `hololive-api`와 `hololive-alarm-worker` 모두에 운영 반영되고, 그 이전 image로
  도는 컨테이너가 0개일 때만 별도 승인을 받아 적용합니다.
- 순서: [릴리즈 runbook](release.md#3-런타임-호스트-no-build-cutover)의 일반 순서를 따릅니다. (1) 331을 포함한
  manifest로 `hololive-db-migrate` one-shot을 실행하고 종료 코드 0을 확인합니다. (2) 331을 포함한 릴리스의 worker와
  API를 `up -d --no-deps`로 올립니다. migrate가 실패하면 새 image를 올려 계속하지 않고, 원인을 확인한 뒤 migrate부터
  다시 실행합니다. migrate와 앱 교체 사이에 도는 claim 제거 릴리스의 API·worker는 이 열을 참조하지 않으므로 정상
  동작합니다.
- 331은 표를 3초 `lock_timeout`으로 잠그는 짧은 트랜잭션 뒤에 어휘 CHECK 검증을 별도 문장으로 실행합니다. 어느
  단계에서 실패해도 적용 기록이 남지 않으므로 같은 파일을 다시 적용할 수 있습니다. 트랜잭션이 롤백됐다면 재적용에서
  `ENQUEUED`를 정규화하고, 이미 commit됐다면 정규화할 행이 없어 행 값은 그대로인 채 CHECK를 다시 검증합니다.
- rollback: 331 적용 뒤에는 worker·API 모두 claim 제거 릴리스까지 되돌릴 수 있습니다. 그보다 이전이 필요하면
  fix-forward합니다.

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

### 4. Iris admission 이후 handoff가 확인되지 않음

Symptoms:
- Iris `202`로 admission된 alarm dispatch 단위가 `sent`인데 `alarm_dispatch_send_units.handoff_next_check_at`이 지평(`alarm_dispatch` executor `attempt_timeout`)을 넘겨 남아 있거나, 멤버가 `handoff_*` 코드로 격리·DLQ됩니다. notification·youtube도 같은 확인 대기 열이 남거나 `error`가 `handoff_*`로 시작합니다.
- `202`가 admission을 증명하지 못한 결과 불명(`iris reply admission outcome unknown`)이 로그에 있고 alarm dispatch는 quarantine되거나, notification이 `QUARANTINED`가 되거나, YouTube delivery가 `SENDING`에 남습니다.

Diagnosis:
- Raw `requestId`를 로그나 응답에 복사하지 않고 bounded alarm-worker/Iris 로그에서 status state와 오류 class만 확인합니다.
- `queued`, `preparing`, `prepared`, `sending`이 확인 지평까지 계속되었는지, `failed` 또는 `outcome_unknown`으로 끝났는지 확인합니다.
- `handoff_completed`가 없는데 DB row만 수동으로 `sent` 처리하지 않습니다.
- alarm dispatch의 `sent` 단위는 `alarm-dispatch-handoff-reconciler`가 `alarm_dispatch_send_units.handoff_next_check_at`마다 `/reply-status`를 확인합니다. 확인 대기 단위는 `handoff_request_id`가 있고 `handoff_checked_at`이 없는 행입니다. 지평(`alarm_dispatch` executor `attempt_timeout`)을 넘긴 단위가 남아 있으면 reconciler 로그와 Iris 상태 조회 오류를 먼저 확인합니다.
- 확인 결과 코드는 `last_error_code`의 `handoff_failed`(세대·시도 예산 안이면 `:r1`/`:r2` 재발송, 아니면 DLQ), `handoff_outcome_unknown`, `handoff_status_missing`(404), `handoff_unconfirmed`(지평 미확인)입니다. 뒤의 세 코드는 격리이며 재발송하지 않습니다.
- `alarm dispatch handoff check closed without transition` 오류 로그는 토큰·요청은 맞지만 운영 SQL·관리 조치·retention 삭제로 멤버가 더 이상 모두 `sent`가 아니어서(멤버가 모두 지워진 단위 포함) 확인이 영구히 닫힌 단위입니다. 다시 확인하지 않으므로 로그의 `observed_action`·`reason`(관측한 Iris 결과)과 멤버 상태를 맞춰 보고 수동 처리를 정합니다. `lost its claim or send unit` 경고는 다음 확인이나 다른 정산이 이어받는 소유 상실이고, `settlement exceeded its budget` 경고는 정산 슬롯 대기·연결 획득·문장이 정산 예산 5s를 넘겨 soft claim 뒤 다음 확인이 다시 판정하는 지연이라 조치가 필요 없습니다. 같은 경고가 주기마다 이어지면 DB 지연을 먼저 봅니다.
- `alarm dispatch handoff check failed` 오류 로그의 오류가 `handoff dlq committed but claim keys not released`를 담으면 DLQ 전이는 기록됐지만 미발송 멤버의 dedup claim 키 해제(정산과 별도인 예산 5s)가 실패한 단위입니다. 확인이 닫혀 다시 해제하지 않으므로 키는 TTL까지 남습니다. Valkey 상태를 먼저 확인합니다.
- notification delivery의 확인 대기 행은 `notification_delivery_outbox`에서 `status='SENT'`이고 `handoff_request_id`가 있으며 `handoff_checked_at`이 없는 행이고, migration 330 CHECK상 `handoff_next_check_at`이 있는 행과 같습니다. `notification-delivery-handoff-reconciler`가 `handoff_next_check_at`마다 확인하며(첫 확인은 admission 뒤 min(5s, `attempt_timeout`)), 확인이 끝날 때까지 같은 방의 후행 notification은 발송되지 않습니다. 지평(`notification_delivery` executor `attempt_timeout`)을 넘긴 확인 대기 행이 남아 있으면 reconciler 로그와 Iris 상태 조회 오류를 먼저 확인합니다. 확인 결과 코드는 `last_error_code` 대신 `error`의 앞부분(`handoff_failed: …` 등)에 남습니다. `handoff_failed`는 세대·시도 예산 안이면 `PENDING`으로 돌아가 `:r1`/`:r2`로 재발송되고, 아니면 종료 `FAILED`(producer rearm 대상 아님)입니다. 나머지 세 코드는 `QUARANTINED`이며 재발송하지 않습니다.
- `notification delivery handoff check lost its claim; not applied` 경고는 soft claim 토큰·요청이 바뀌어 아무것도 적용하지 않은 확인으로, 다음 확인이 다시 판정합니다. `notification delivery handoff settlement exceeded its budget; the next check decides again` 경고도 같습니다. `notification delivery handoff failed; requeued with the next request generation` 경고는 재발급입니다. `notification delivery handoff outcome unknown; quarantined`·`notification delivery handoff failed; reissue budget exhausted` 오류 로그는 행을 `QUARANTINED`·`FAILED`로 끝낸 확인입니다. 확인 대기 행을 수동으로 `handoff_checked_at`만 채우지 않습니다. migration 330 CHECK가 확인 시각과 상태의 조합을 강제합니다.
- youtube delivery의 확인 대기 요청은 `youtube_notification_send_request`에서 `handoff_request_id`가 있고 `handoff_checked_at`이 없는 행이며, migration 336 CHECK상 `handoff_next_check_at`이 있는 행과 같습니다. `youtube-delivery-handoff-reconciler`가 `handoff_next_check_at`마다 확인합니다(첫 확인은 admission 뒤 min(5s, `youtube_delivery` executor `attempt_timeout`)). 지평을 넘긴 확인 대기 요청이 남아 있으면 reconciler 로그와 Iris 상태 조회 오류를 먼저 확인합니다. 결과 코드는 멤버 delivery `error`의 앞부분(`handoff_failed: …` 등)에 남습니다. `handoff_failed`는 세대·시도 예산 안이면 논리 그룹의 SENT 행이 `PENDING`으로 돌아가 같은 고정 본문을 `:r1`/`:r2`로 재발송하고, 아니면 `FAILED`입니다. 나머지 세 코드는 `QUARANTINED`(같은 출처의 QUARANTINED 원장)이며 재발송하지 않습니다. 부모 `youtube_notification_outbox` 집계는 같은 정산 트랜잭션에서 바뀌므로 delivery와 따로 어긋나지 않습니다. 부모 `sent_at`은 지우지 않아 FAILED·PENDING 부모에도 처음 SENT 시각이 남습니다. post 단위 tracking·알람 발송 표시는 되돌리지 않으므로, 재발송이 실패로 끝난 post의 표시는 수동으로 판단합니다.
- `youtube delivery handoff check closed; frozen members changed since admission` 오류 로그는 토큰·요청은 맞지만 멤버 row_version·상태, 논리 그룹(SENDING·QUARANTINED 행, 한도 초과) 또는 원장 출처가 admission 때와 달라 전이 없이 확인을 영구히 닫은 요청입니다. 로그의 `delivery_ids`·`observed_action`·`reason`으로 멤버와 원장을 맞춰 보고 수동 처리를 정합니다. `youtube delivery handoff check lost its claim or logical group changed; not applied`·`settlement exceeded its budget` 경고는 다음 확인이 다시 판정합니다. 앞의 경고 중 오류에 `membership_changed`가 담긴 것은 정산이 부모 outbox를 미리 잠근 뒤 같은 논리 키의 follower·outbox가 들어와 부모 집합이 늘어난 경우로, 아무것도 바꾸지 않고 확인을 열어 두었으므로 soft claim(15s) 뒤 다음 확인이 늘어난 집합으로 정산합니다. `youtube delivery handoff check failed` 오류 로그 중 commit 응답 유실이 아닌 것(부모 outbox 집계 실패 포함)은 정산 트랜잭션 전체가 되돌아가 claim 토큰이 남은 확인이며, soft claim(15s) 뒤 다음 확인이 다시 정산합니다. 같은 요청에서 반복되면 오류 원문의 DB 오류를 먼저 해결합니다. 로그에는 Iris requestId가 남지 않으므로 필요하면 요청의 `handoff_request_id`를 DB에서 읽습니다.

Mitigation:
- Iris reply delivery worker와 Kakao bridge를 먼저 복구합니다.
- Outcome unknown인 alarm을 일반 텍스트로 재발송하지 않습니다. YouTube `SENDING` row는 기존 stale sweeper 계약에 맡깁니다.
- 퇴역 환경변수로 다른 경로를 다시 켜거나 startup guard를 우회하지 않습니다.

Rollback:
- Markdown post 뒤 결과가 불명확한 delivery가 있으면 이전 alarm-worker image를 시작하지 않습니다. Exact pending/sending 범위와 prior artifact의 egress 차이를 제시하고 별도 승인을 받습니다.

### 5. 생일축하는 갔지만 생일 방송 알람이 생성되지 않음

Diagnosis:
- `celebration:birthday:member-{memberID}:{date}` event와 그 delivery의 `status`, `sent_at`을 확인합니다. `sent`가 아닌 방은 의도적으로 대상이 아닙니다.
- 같은 `channelID`의 `youtube_live_sessions`가 판정 창(KST 생일 전날 21:00부터 다음 날 17:00 전까지) 안에서 `UPCOMING` 또는 `LIVE`인지 확인합니다. 생일 날짜는 KST 0시부터 평가하고 다음 날 17시까지 계속 평가하므로, 전날 시작한 방송은 생일축하 발송 뒤에도 예정·진행 상태여야 합니다. 다른 KST 날짜에 시작하는 방송은 `01/02 15:04`로 표시합니다. 제목에 생일 키워드(`birthday`·`bday`·`b-day`·`誕生日`·`誕生祭`·`誕生会`·`生誕`·`たんじょうび`·`バースデー`·`バースデイ`·`バースディ`·`생일`·`ulang tahun`·`ultah`, NFKC·대소문자 무시)가 없으면 생일 방송으로 보지 않습니다. UPCOMING은 `schedule_observed_at`, LIVE는 `status_observed_at`이 runner의 freshness 하한부터 현재 시각 사이에 있어야 합니다. NULL·미래 관측은 제외하며 `last_seen_at`이나 예정 시작 시각으로 최신성을 판단하지 않습니다.
- `Birthday stream runner failed` 로그가 있으면 audience SQL 오류를 먼저 해결합니다. 이 경로는 실패 시 전체 방으로 fallback하지 않습니다. `list published birthday stream events` 오류는 그 멤버의 기발행 생일 방송 event payload가 decode·검증을 통과하지 못한 것이며, 같은 생일 날짜의 다른 멤버 처리도 멈추므로 해당 `event_key`의 payload를 먼저 확인합니다.
- 이미 `celebration:birthday_stream:member-{memberID}:{date}:{videoID}` event가 있어도 현재 세션이면 다음 tick에서 재평가됩니다. 이때 최초 event의 canonical payload를 재사용하므로 이후 title·photo·schedule 표시값 변경은 event payload를 바꾸지 않습니다. 그 event를 이미 `sent`로 받은 방은 envelope에서 빼고, 늦게 생일축하를 받은 방(과 pending·failed 방)만 발행해 outbox dedupe에 맡깁니다. 이 조회가 실패하면 전체 방으로 재발행하지 않고 `Birthday stream runner failed`로 끝납니다. 새 event·delivery가 없는 실행의 `Birthday stream runner published`는 Debug 로그입니다. 멤버·일자 상한 3건과 이 재평가는 저장 제목이 생일 키워드를 포함하는 event만 셉니다. 저장 제목에 생일 키워드가 없는 기발행 event는 현재 제목이 바뀌어도 다시 보내지 않습니다.

Mitigation:
- producer의 full-roster LIVE discovery부터 복구한 뒤 runner를 재평가합니다.
- birthday greeting delivery를 수동으로 sent 처리하거나 birthday stream을 전체 방에 재전송하지 않습니다.

Rollback:
- 생일축하 sent 방 audience 도입 전의 구 alarm-worker image는 전체 방 fan-out 의미를 가지므로 rollback 전에 `BIRTHDAY_STREAM_RUNNER_ENABLED=false`로 runner를 중지합니다.
- 9.2.0 이하 worker로 되돌리면 제목 판정과 확장 판정 창이 사라집니다. 구 worker는 기발행 event 수로 상한을 세므로 저장 제목이 생일과 무관한 event가 그날의 상한과 재평가 자리를 다시 차지하고, 다음 KST 날짜로 넘어간 방송의 event는 더 이상 재평가되지 않습니다.

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

### 7. 최초공개가 발견 시 알려지거나 공개 전 알림이 빠짐

Symptoms:
- 공개 전에 발견된 최초공개에 `N분 후 공개 예정`(`NEW_VIDEO`)이 나갔다.
- 또는 공개 전에 발견된 최초공개에 `선행공개 N분 전`도 `선행공개 시작`도 없다.
- 또는 `선행공개 N분 전`을 받은 방에 같은 `stream_id`의 시작 알림이 또 나갔다.

Diagnosis:
- 예정 최초공개의 `NEW_VIDEO`는 계약 위반이다. `youtube_notification_outbox`의 `created_at`이 v9.3.0 hololive-api 교체 이후인지 보고, 이후라면 `refreshNotifications`의 예정 Premiere 제외가 빠진 revision인지 확인한다. 교체 전에 만들어진 행은 이전 계약의 정상 결과다.
- 분 전 알림이 없으면 `alarm_upcoming_candidates`의 해당 `stream_id` outcome을 본다. 후보가 아예 없으면 Holodex/저장 세션의 `UPCOMING` 예정 시각이 target 창 안에 있었는지 확인한다. 가장 늦은 target 이후에 발견됐다면 시작 알림이 대신 나가야 한다.
- 시작 알림 중복은 live catchup 억제 marker(`LiveCatchupSuppressWindow`) 조회 실패·만료 경로를 본다(`contracts/alarm.md`의 Live catchup 억제 marker의 실패 처리).
- `is_premiere`가 아직 분류되지 않은 상태에서 선정된 후보는 일반 방송 문구(`방송 5분 전`)로 나갈 수 있다. 남은 공백이며 계약 위반이 아니다.

Mitigation:
- 계약 위반이 확인되면 수정 revision으로 fix-forward한다. 남은 공백에 해당하는 단건 문구 차이·중복은 재발송·삭제하지 않는다.

Rollback:
- v9.3.0 이전 hololive-api로 rollback하면 예정 최초공개의 발견 시 `NEW_VIDEO`가 다시 생긴다. alarm-worker만 9.2.2 이하로 rollback하면 발견 시 알림 없이 분 전 target을 지난 최초공개의 시작 알림이 빠진다. 이 증상만으로는 rollback하지 않는다.

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
