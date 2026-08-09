# Runbook: hololive-api

## Role

`hololive-api`는 bot/admin/llm plane을 한 프로세스(단일 compose service `hololive-api`)에서 호스팅하는 통합 runtime입니다.

- Bot plane: Kakao/Iris webhook ingress, 사용자 명령 routing, reply orchestration (port `30001`).
- LLM plane: major event/member news scheduling, LLM digest 생성, internal subscription/trigger 제공자 (port `30003`).
- Admin plane: dashboard-facing admin HTTP control plane, trigger client facade, alarm HTTP 호환 facade (port `30006`).

## Normal status

| Check | Expected |
|---|---|
| Health (bot) | `https://127.0.0.1:30001/health` returns success through container `./bin/healthcheck` |
| Health (llm) | `https://127.0.0.1:30003/health` returns success through container `./bin/healthcheck` |
| Ready (llm) | `https://127.0.0.1:30003/internal/ready` with `X-API-Key` returns success through container `./bin/healthcheck` |
| Health (admin) | `https://127.0.0.1:30006/health` returns success through container `./bin/healthcheck` |
| Logs | no repeated webhook, Iris, DB, Valkey, LLM, or trigger errors |
| Queue | webhook를 `bot_webhook_inbox`에 commit한 뒤 처리하고 reply를 `bot_reply_outbox`에서 Iris로 전달합니다. `notification_delivery_outbox` dispatch는 소유하지 않습니다. |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | commands, admin reads/writes, subscriptions, summaries, outbox fail |
| Valkey | yes | cache/config/session/PubSub behavior degrades |
| Iris | yes | Kakao ingress/reply fails |
| cliproxy/LLM | partial | digest/summary generation fails where enabled |
| `alarm-worker` | partial | alarm API and proactive delivery drain depend on alarm-worker |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | bot plane HTTP/H3 port (`30001`) | yes |
| `LLM_SCHEDULER_PORT` | llm plane HTTP port (`30003`) | yes |
| `HOLOLIVE_HTTP_TRANSPORTS` | enabled transports | yes |
| `IRIS_*` | Iris URL/certs/tokens | yes |
| `LLM_SCHEDULER_INTERNAL_URL` | internal scheduler/trigger API base | partial |
| `CLIPROXY_*` | LLM proxy | partial |
| `MAJOREVENT_*` | major event scrape/schedule config | partial |
| `DELIVERY_DISPATCHER_ENABLED=false` | producer-only egress boundary (egress owned by `alarm-worker`) | yes |
| `CACHE_*`, `POSTGRES_*` | state dependencies | yes |

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-api
```

## Metrics

통합 단일 프로세스의 자원·연결 상태를 실측하는 운영 명령입니다. 모든 명령은 호스트(central main host)에서 실행합니다.

### Container resource / health

```bash
docker stats --no-stream hololive-api hololive-alarm-worker valkey-cache holo-postgres
docker inspect hololive-api --format '{{.RestartCount}} {{json .State.Health}}'
docker logs --tail=300 hololive-api
docker logs --tail=100 deunhealth        # restart-on-unhealthy 작동 이력
```

### Go runtime metrics (Prometheus)

`/metrics`는 **bot plane만** 평문 HTTP/1.1로 `:30091`에 노출합니다(admin/llm plane은 metrics listener 없음). Go runtime collector가 프로세스 전체(3 plane 합산)를 커버합니다. prod에서는 `API_SECRET_KEY`가 설정되어 있어 `:30091`이 loopback bypass 대상이 아니므로 `X-API-Key` 헤더가 필요합니다.

```bash
# 키 출처는 실행 중 컨테이너의 실제 env(metrics 서버가 검증에 쓰는 값과 동일).
# process substitution으로 키를 호스트 argv/ps에 노출하지 않는다.
curl -s --config <(printf 'header "X-API-Key: %s"\n' "$(docker exec hololive-api printenv API_SECRET_KEY)") \
  http://127.0.0.1:30091/metrics \
  | grep -E 'process_resident_memory_bytes|go_memstats_heap_inuse_bytes|go_memstats_heap_idle_bytes|go_gc_duration_seconds|go_goroutines'
```

- GC 압력은 `go_gc_duration_seconds`(pause 합) 증가율과 `go_memstats_heap_inuse_bytes`가 `GOMEMLIMIT`(1024MiB)에 근접하는지로 본다. GC-CPU 비중 metric의 정확한 이름은 빌드된 client_golang 버전에 따라 다르므로 `curl … | grep go_` 로 실제 노출 항목을 먼저 확인한다(검증 필요).
- pprof는 `:30061`(`HOLOLIVE_API_PPROF_ADDR`)에 있고 동일하게 `X-API-Key`가 필요하다.

### PostgreSQL connections (PG18)

컨테이너 내부 socket-trust로 admin user(`POSTGRES_ADMIN_USER`, 기본 `postgres_admin`)로 접속해 조회합니다.

```bash
docker exec holo-postgres psql -U postgres_admin -d hololive -c \
  "SELECT usename, client_addr, state, count(*) \
     FROM pg_stat_activity WHERE datname='hololive' \
    GROUP BY usename, client_addr, state ORDER BY usename, client_addr, state;"
```

- **중요**: pgx DSN에 `application_name`을 설정하지 않으므로, `hololive-api`의 bot/admin/llm 3 plane은 같은 process·같은 usename(`hololive_runtime`)·같은 `client_addr`(컨테이너 IP 1개)로 보입니다 → **plane 단위 구분은 pg_stat_activity로 불가능**합니다. 구분 가능한 경계는 `client_addr`(hololive-api vs alarm-worker vs migrate) 수준입니다. plane별 budget은 정의값(bot/admin/llm 각 max 4, 합 최대 12)으로 추적합니다.
- 전체 budget은 `scripts/ci/check-postgres-capacity.sh`가 `hololive-api` 12 + `alarm-worker` 8 + producer AP 4개 32 + migrator 1 = 53, `max_connections=60` 대비 reserve 7로 고정합니다.

### Valkey latency / slowlog

비밀번호를 호스트 process list에 노출하지 않도록 컨테이너 내부 env(`CACHE_PASSWORD`)로 인증합니다.

```bash
docker exec valkey-cache sh -c 'REDISCLI_AUTH="$CACHE_PASSWORD" valkey-cli -s /var/run/valkey/valkey-cache.sock slowlog get 25'
docker exec valkey-cache sh -c 'REDISCLI_AUTH="$CACHE_PASSWORD" valkey-cli -s /var/run/valkey/valkey-cache.sock --latency'   # Ctrl-C로 종료
docker exec valkey-cache sh -c 'REDISCLI_AUTH="$CACHE_PASSWORD" valkey-cli -s /var/run/valkey/valkey-cache.sock info commandstats'
```

## Common failure modes

### 1. Health check fails

Symptoms:
- Compose marks `hololive-api` unhealthy.
- Webhook replies, admin dashboard calls, or scheduler triggers stop.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps hololive-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 hololive-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
```

Mitigation:
- Check PostgreSQL, Valkey, Iris env/cert availability.
- Redeploy only after confirming config is correct.

Rollback:
- Use `docs/current/runbooks/rollback.md`. Durable runtime cutover 이후에는 이 문서의 [Rollback](#rollback) backlog preflight 없이 이전 `hololive-api` image/config를 재배포하지 않습니다.

### 2. Member news / major event command or manual trigger fails

Symptoms:
- Bot-plane command path returns scheduler/internal API errors.
- Admin manual trigger endpoint returns failure or `409 notification_in_progress`.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=300 hololive-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
```

Mitigation:
- Validate `LLM_SCHEDULER_INTERNAL_URL`, CLIPROXY, and member news/major event source state.
- For `409`, wait for the active run to finish; investigate a stuck scheduler if the conflict persists.

Rollback:
- Roll back the plane/contract/config change that introduced failures.

### 3. Durable webhook or reply backlog grows

`bot_webhook_inbox`의 `pending`/`retry`와 `bot_reply_outbox`의 `pending`/`retryable_pre_dispatch`가 지속 증가하면 PostgreSQL, command handler, Iris 상태를 함께 확인합니다. `dead`는 poison payload 또는 bounded retry 소진, `outcome_unknown`은 부수 효과 결과를 확정할 수 없어 자동 재실행하지 않은 상태입니다. lease heartbeat가 살아 있는 command는 reclaim 대상이 아닙니다.

Reply outbox의 `outcome_unknown`은 같은 `client_request_id`로 최대 5회, 최초 dispatch인 `first_attempt_at`부터 144시간 미만인 동안에만 durable backoff 후 재확인합니다. attempt 또는 시간 경계에 도달한 행과 accepted lease가 만료된 행은 자동 발송 없이 payload를 보존한 `manual_review`로 이동합니다. `bot_reply_outbox_accepted_reclaimed_total`, `bot_reply_outbox_manual_review_backlog`, `bot_reply_outbox_manual_review_oldest_age_seconds`를 확인하고 Iris 수리 여부와 같은 `client_request_id`의 처리 이력을 조사합니다.

Command claim이 만료되어 `outcome_unknown`으로 닫히면 `bot_durable_command_outcome_unknown_total`이 증가하고 `inspect bot_command_executions status=outcome_unknown` action log가 기록됩니다. 자동 재실행하지 말고 해당 시점의 부수 효과를 조사합니다. Inbox reclaim이 아직 살아 있는 command claim과 만난 경우에는 inbox payload를 완료·scrub하지 않고 command stale cutoff 이후로 미룹니다.

조사 결과 재발송이 필요한 한 행만 골라 아래 operator artifact를 실행합니다. 이 artifact가 replay 가능 시간과 상태 전이의 유일한 소유자이며, 기존 `attempts` 이력을 보존한 채 `operator_replay_grants`를 1 증가시켜 추가 dispatch 한 번만 허용합니다. `operator_actor`는 64자 이하의 계정/handle, `operator_reason`은 256 bytes 이하의 ticket·incident 근거만 사용하며 secret이나 사용자 원문을 넣지 않습니다. 출력은 `replayed`, `cutoff_expired`, `invalid_operator_metadata`, `not_manual_review`, `not_found` 중 하나입니다.

```bash
PGSERVICE=hololive-db-maintenance \
PGPASSFILE=/etc/stack-secrets/hololive-bot/postgres/pgpass \
env -u PGPASSWORD psql -w -X -v ON_ERROR_STOP=1 \
  -v outbox_id='<bot_reply_outbox.id>' \
  -v operator_actor='<operator-handle>' \
  -v operator_reason='<ticket-or-incident-reason>' \
  -f hololive/hololive-api/internal/planes/bot/internal/durability/queries/reply_outbox_replay_manual_review.sql
```

Replay cutoff는 row의 `created_at`부터 144시간입니다. Iris admission retention 168시간보다 24시간 짧게 닫아 operator 판단 뒤 실제 dispatch와 sweep이 지연되더라도 dedup retention 경계를 넘지 않게 합니다. 144시간 경계와 그 이후에는 fail-closed합니다. `cutoff_expired`이면 재발송하지 않습니다. `bot_reply_outbox_replay_audit`에는 grant 시점의 `granted`, 실제 claim 시점의 `replayed` event가 같은 actor/reason과 별도 `recorded_at`으로 append되므로 이후 outbox `updated_at` 변경과 분리해 조사합니다. `invalid_operator_metadata`, `not_manual_review`, `not_found`도 상태를 임의로 고치지 말고 현재 행과 운영 이력을 다시 확인합니다.

Durable runtime binary보다 migration 123~136을 먼저 적용해야 합니다. 실행 순서의 SSOT는 filename 정렬이 아니라 `hololive/hololive-api/scripts/migrations/manifest.txt`이며, replacement due index를 먼저 만드는 127이 기존 index를 제거하는 126보다 앞섭니다. Outbox는 같은 room의 active 선행 행을 직렬화하지만 `manual_review`는 operator 보류 상태이므로 후속 room reply를 막지 않습니다. Migration 133/134 trigger와 terminal writer가 inbox payload와 command 진단을 terminal 전이에서 즉시 scrub합니다. 주기 maintenance는 scrub scan을 반복하지 않고 retention 대상만 찾으며, terminal ledger는 Iris admission retention(7일)보다 긴 8일 뒤 batch 삭제합니다. `manual_review`와 그 replay audit은 판단·처리 이력을 위해 해당 outbox row의 retention 동안 함께 보존합니다.

Migration 133은 runtime cutover 전에 terminal payload scrub trigger를 먼저 설치하고 기존 `dead`/`succeeded` row를 backfill한 뒤 CHECK를 validate합니다. 따라서 이전 runtime의 `inbox_complete` writer가 migration 적용 중이나 cutover 전에 `status`만 `succeeded`로 변경해도 trigger가 `payload`를 `{}`로 scrub하며 CHECK에 거부되지 않습니다.
이 호환 trigger는 모든 legacy writer가 제거됐음을 확인한 뒤에만 별도 migration으로 제거합니다.

Migration 125 이후 runtime은 `bot_webhook_heads`와 `ordering_key` advisory lock을 함께 사용합니다. Schema rollback은 이전 runtime으로 먼저 전환해 writer를 quiesce한 뒤에만 `bot_webhook_heads`/`available_at`을 제거해야 하며, 현재 runtime이 쓰는 동안 migration 125~136을 되돌리면 안 됩니다.

Migration `114_drop_unused_indexes.sql` 적용 전에는 read-only preflight로 rollback artifact를 먼저 생성해야 합니다.

libpq service와 password file을 사용합니다. `PGPASSFILE`은 readable regular file이어야 하고 symlink는 금지합니다. `PGPASSWORD`와 connection URI command argument는 허용하지 않으며 `psql -w`로 interactive password fallback도 차단합니다.

> OpenBao 폐기(2026-08-08) 이후 `stack-secrets` 마스터는 아직 libpq service/pgpass 쌍을 미러하지 않습니다. `libpq-connection.sh`의 file-only 계약(`PGSERVICE`+`PGPASSFILE`, `PGPASSWORD` 금지, symlink 금지)은 그대로 강제되므로, 아래 명령을 쓰려면 `stack-secrets`에 `pg_service.conf`와 pgpass를 먼저 provisioning해 `/etc/stack-secrets/hololive-bot/postgres/`로 미러해야 합니다(approval-gated). 그 전까지 읽기 전용 조사는 `stack-postgres-access` 경로(`docker exec holo-postgres psql -U postgres_admin -d hololive`)를 씁니다.

```bash
PGSERVICE=hololive-db-maintenance \
PGPASSFILE=/etc/stack-secrets/hololive-bot/postgres/pgpass \
  env -u PGPASSWORD hololive/hololive-api/scripts/migrations/preflight-114-restore.sh ./migration-114-rollback.sql
```

preflight가 `MISSING`을 보고하면 migration을 적용하지 않습니다. 생성 artifact는 `BEGIN`/`COMMIT`, `CREATE INDEX IF NOT EXISTS`, 조건부 constraint 복원을 포함해 실패 후 재실행할 수 있습니다. Artifact 실행은 별도 rollback 승인이 필요합니다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck --api-key-env API_SECRET_KEY https://127.0.0.1:30003/internal/ready
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Durable runtime cutover 이후 기본 복구 전략은 현재 image의 fix-forward입니다. 이전 image는 durable queue를 소비하지 않으므로 backlog가 남아 있으면 재배포하지 않습니다.
- 이전 `hololive-api` image/config가 반드시 필요하면 Iris webhook ingress를 먼저 quiesce하고 현재 durable runtime으로 queue를 drain한 뒤 아래 preflight가 성공해야 합니다. 하나라도 0이 아니면 현재 image를 유지하고 roll forward합니다.

  ```bash
  PGSERVICE=hololive-db-maintenance \
  PGPASSFILE=/etc/stack-secrets/hololive-bot/postgres/pgpass \
    env -u PGPASSWORD hololive/hololive-api/scripts/migrations/preflight-durable-runtime-rollback.sh --ingress-quiesced
  ```

- Preflight 성공과 ingress quiescence를 같은 maintenance window에서 유지한 경우에만 이전 image를 재배포합니다. Schema rollback은 계속 [`Migration ordering`](#3-durable-webhook-or-reply-backlog-grows)의 writer quiescence 규칙을 따릅니다.
- Recheck Iris webhook/reply, scheduler-dependent commands, manual triggers, and dashboard health after rollback.

## Post-deploy monitoring (unified runtime)

bot/admin/llm을 한 프로세스에 묶었으므로 평균값보다 동시 spike(예: LLM weekly digest + admin stats 조회 + bot 이미지 렌더링 동시 발생)가 중요하다. 컷오버 후 최소 24시간 관찰:

### 단일 프로세스 blast-radius (먼저 인지할 것)

- 3 plane이 한 프로세스이므로 **한 plane의 자원 폭주(OOM/goroutine leak/GC thrash)가 전체 컨테이너를 끌어내립니다.** healthcheck(`30001/health`·`30003/internal/ready`·`30006/health`)는 각 URL을 순차 검사해 하나라도 실패하면 exit 1 → unhealthy → deunhealth가 컨테이너 전체를 재시작합니다.
- `30003/internal/ready`는 인증된 dependency readiness(PostgreSQL/Valkey)를 포함합니다. 외부에서 접근 가능한 `/ready`는 dependency ping 없이 process health만 반환합니다.

### 경계값 (initial threshold — 실측으로 보정)

아래 수치는 운영 시작 기준선입니다. 실측 baseline 확보 후 조정합니다.

- **RSS**: `docker stats`의 MEM USAGE가 **1.1GiB(limit 1280m 대비)를 5분 이상 초과** 시 경고. limit(1280m)·`pids: 512` 근접 시 OOM/pid-kill 위험 → incident.
- **Heap vs GOMEMLIMIT**: `go_memstats_heap_inuse_bytes`가 `GOMEMLIMIT`(1024MiB)에 지속 근접하면 GC thrash 구간 → `go_gc_duration_seconds` 증가율 동반 확인. GC가 CPU의 ~10%를 지속 점유하면 조사(정확한 GC-CPU metric 이름은 `grep go_`로 확인).
- **bot webhook p99**: Iris webhook 타임아웃(5s) 기준. **p99가 2s를 지속 초과하면 경고, 5s에 근접하면 reply drop → incident.**
- **pgx acquire latency**: plane별 pool max 4라 contention 민감. **acquire p99 > 50ms 지속 → 경고, > 500ms → pool 고갈 임박(incident).**
- **deunhealth restart**: `docker inspect hololive-api`의 `RestartCount ≥ 1` 또는 deunhealth restart 로그 발생 시 **즉시 incident triage**(정상 운영 중에는 0이어야 함).

### 관찰 항목

- Go RSS / heap inuse·idle, GC pause·GC CPU 비중 (GOMEMLIMIT 1024MiB, 컨테이너 limit 1280m·pids 512 대비 여유) — `Metrics` 절 명령 사용
- PostgreSQL connection 수 — plane별 pool 합산(bot/admin/llm 각 max 4 = 최대 12) + alarm-worker(max 8)/youtube-producer/migration 포함 전체 budget. plane 단위 구분은 불가(같은 client_addr/usename — `Metrics` 절 참조)
- pgx acquire latency, Valkey command latency(slowlog/--latency), H3 handshake error rate
- bot webhook p95/p99, admin API p95/p99, LLM scheduler job lag
- deunhealth 재시작 빈도 — 잦은 재시작은 H3 listener hang/5s 타임아웃/GC pause를 의심

## Related contracts

- `member-cache-v2-rollout.md`
- `../contracts/iris-boundary.md`
- `../contracts/membernews.md`
- `../contracts/majorevent.md`
- `../contracts/trigger.md`
- `../contracts/settings.md`
- `../contracts/alarm.md`
