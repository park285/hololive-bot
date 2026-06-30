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
| Queue | produces `notification_delivery_outbox` rows; does not own dispatch queue draining |

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
- 전체 budget 점검: `hololive-api`(≤12) + `alarm-worker`(max 8) + youtube-producer/migration. PostgreSQL은 단일 컨테이너이므로 `max_connections` 대비 합산을 본다.

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
- Use `docs/current/runbooks/rollback.md` and redeploy the previous `hololive-api` image/config.

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

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck --api-key-env API_SECRET_KEY https://127.0.0.1:30003/internal/ready
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `hololive-api` image/config.
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

- `../contracts/iris-boundary.md`
- `../contracts/membernews.md`
- `../contracts/majorevent.md`
- `../contracts/trigger.md`
- `../contracts/settings.md`
- `../contracts/alarm.md`
