# Runbook: alarm-worker

## Role

`hololive-alarm-worker`는 alarm checker/scheduler, dispatch queue publishing, dispatch queue consumption, generic notification delivery outbox consumption, YouTube outbox egress를 담당합니다.
proactive notification egress의 배타성은 별도 lease가 아니라 PostgreSQL row-claim(`FOR UPDATE SKIP LOCKED`) 조율과 compose 단일 인스턴스 배치(`container_name: hololive-alarm-worker`, 고정 host port `127.0.0.1:30007`)가 함께 보장합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `https://127.0.0.1:30007/health` returns success over H3 |
| Ready | `https://127.0.0.1:30007/ready` returns `status=ready`; `https://127.0.0.1:30007/internal/ready` with `X-API-Key` includes dependency and egress flag readiness |
| Logs | scheduler/checker loops run without repeated DB/cache errors |
| Queue | publishes to and consumes from `alarm:dispatch:queue` when alarm events are due |
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
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED` | YouTube outbox egress enablement | production yes |
| `YOUTUBE_OUTBOX_KARING_ENABLED` | YouTube outbox egress uses Karing content-list templates instead of text sends for supported kinds | no |
| `DELIVERY_DISPATCHER_ENABLED` | generic notification delivery outbox egress enablement | production yes |
| `ALARM_DISPATCH_CONSUMER_ENABLED` | alarm dispatch outbox egress enablement | production yes |
| `ALARM_DISPATCH_KARING_ENABLED` | alarm dispatch queue egress uses Karing content-list templates instead of text sends | no |
| `ALARM_SHORT_LINK_BASE_URL` | `https://short.holoshi.com` for thumbnail-free grouped alarm links; blank disables | no |
| `BIRTHDAY_STREAM_RUNNER_ENABLED` | matching birthday greeting이 sent인 방에만 birthday stream event를 생산 | production policy |
| `BIRTHDAY_STREAM_POLL_INTERVAL_MS` | birthday stream session 평가 주기; 기본 30분 | no |
| `BIRTHDAY_STREAM_SESSION_FRESHNESS_MS` | stale UPCOMING/LIVE 제외 창; 기본 30분 | no |
| `CACHE_*` | Valkey connection | yes |
| `POSTGRES_*` | DB connection | yes |

## Grouped alarm short links

여러 방송을 하나의 일반 텍스트 alarm notification으로 묶을 때 `ALARM_SHORT_LINK_BASE_URL=https://short.holoshi.com`을 설정하면 YouTube 링크를 해당 origin의 `/l/<video_id>`로 렌더링합니다. 빈 값 외 다른 origin은 migration 139의 trusted-link 계약과 어긋나므로 startup에서 거부합니다. `hololive-api` bot plane의 `/l/:videoID` route가 일반 사용자는 YouTube로 `302` redirect하고 KakaoTalk scraper의 `kakaotalk-scrap/` User-Agent는 `403`으로 거부합니다.

provider-first 활성화 순서:

1. `hololive-api`의 `127.0.0.1:30101` short-link listener를 활성화하고 검증합니다.
2. 중앙 source-restricted Nginx의 `100.100.1.7:30192` ingress를 활성화합니다.
3. Seoul Nginx에 `deploy/nginx/holoshi-public-shortlink.conf`를 적용해 전용 `short.holoshi.com/l/*`만 `30192`로 전달합니다.
4. 중앙 호스트에서 `scripts/deploy/shortlink-smoke.sh`를 실행해 세 hop의 일반 `302`, Kakao scraper `403`, invalid ID `404`를 모두 확인합니다.
5. `ALARM_DISPATCH_KARING_ENABLED=false`를 확인한 뒤 `/run/hololive-bot/alarm-worker.env`에 `ALARM_SHORT_LINK_BASE_URL=https://short.holoshi.com`을 설정하고 alarm-worker를 재기동합니다. Karing list template은 명시적 thumbnail 계약을 가지므로 두 기능을 동시에 켜면 alarm-worker가 fail closed합니다.

중앙 live-compat Compose는 `/l/*`만 제공하는 host ingress 전용 HTTP backend를 `127.0.0.1:30101`에 publish합니다. 중앙 source-restricted Nginx가 `100.100.1.7:30192`에서 Seoul gateway 요청만 받아 이 listener로 전달하고, Seoul public Nginx는 `short.holoshi.com/l/*`만 해당 upstream으로 분기합니다.

```text
ALARM_SHORT_LINK_BASE_URL=https://short.holoshi.com
ALARM_DISPATCH_KARING_ENABLED=false
```

기능을 되돌리려면 consumer인 `ALARM_SHORT_LINK_BASE_URL`을 먼저 비우고 alarm-worker를 재기동합니다. 이미 발송된 short link가 계속 동작하도록 `30101` listener와 중앙·Seoul ingress는 무기한 유지하며, 명시적으로 승인된 미래 compatibility deprecation 전에는 제거하지 않습니다. 기존 단일 알림과 Karing template 계약은 변경되지 않습니다.

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-alarm-worker
```

## Readiness

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/ready
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck --api-key-env API_SECRET_KEY https://127.0.0.1:30007/internal/ready
```

`/ready` fails closed when PostgreSQL, Valkey, or required production egress flags are unavailable. `/internal/ready` reports `dependencies.postgres`, `dependencies.valkey`, and `egress_flags.*` booleans for diagnosis.

## Metrics

- Alarm checker/publisher metrics: 검토 필요.

## Common failure modes

### 1. Alarm queue stops growing despite due events

Symptoms:
- Expected alarms are not dispatched.
- YouTube outbox dispatcher has no new send errors.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock LLEN alarm:dispatch:queue
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
- Grouped alarm still contains direct YouTube URLs.
- KakaoTalk still renders a YouTube preview.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker printenv ALARM_SHORT_LINK_BASE_URL
./scripts/deploy/shortlink-smoke.sh
```

Expected:
- regular request: `302` with a YouTube `Location` header
- Kakao scraper request: `403` without a `Location` header

Mitigation:
- Confirm the public ingress routes `/l/*` to the bot plane and preserves `User-Agent`.
- Confirm `ALARM_DISPATCH_KARING_ENABLED=false`.
- Restart alarm-worker after updating its env file.

Rollback:
- Clear `ALARM_SHORT_LINK_BASE_URL` and restart alarm-worker first. Keep the listener and both ingress layers indefinitely until an explicitly approved future compatibility deprecation.

### 4. 생일축하는 갔지만 생일 방송 알람이 생성되지 않음

Diagnosis:
- `celebration:birthday:{channelID}:{date}` event와 그 delivery의 `status`, `sent_at`을 확인합니다. `sent`가 아닌 방은 의도적으로 대상이 아닙니다.
- 같은 `channelID`의 당일 `youtube_live_sessions`가 `UPCOMING` 또는 `LIVE`이고 `last_seen_at` freshness 안에 있는지 확인합니다.
- `Birthday stream runner failed` 로그가 있으면 audience SQL 오류를 먼저 해결합니다. 이 경로는 실패 시 전체 방으로 fallback하지 않습니다.
- 이미 `celebration:birthday_stream:{channelID}:{date}:{videoID}` event가 있어도 현재 세션이면 다음 tick에서 재평가됩니다. 새 방 delivery만 outbox dedupe를 통과합니다.

Mitigation:
- producer의 full-roster LIVE discovery부터 복구한 뒤 runner를 재평가합니다.
- birthday greeting delivery를 수동으로 sent 처리하거나 birthday stream을 전체 방에 재전송하지 않습니다.

Rollback:
- 구 alarm-worker image는 전체 방 fan-out 의미를 가지므로 rollback 전에 `BIRTHDAY_STREAM_RUNNER_ENABLED=false`로 runner를 중지합니다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/ready
./scripts/deploy/shortlink-smoke.sh
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `hololive-alarm-worker` image/config.
- Preserve and inspect `alarm:dispatch:*` queues before replaying or deleting queue data.

## Related contracts

- `../contracts/alarm.md`
- `../contracts/karing-kakaolink.md`
- `../contracts/shortlink.md`
- `../contracts/settings.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
