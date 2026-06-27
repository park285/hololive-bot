# Runbook: hololive-api

## Role

`hololive-api`는 bot/admin/llm plane을 한 프로세스(단일 compose service `hololive-api`)에서 호스팅하는 통합 runtime입니다.

- Bot plane: Kakao/Iris webhook ingress, 사용자 명령 routing, reply orchestration (port `30001`).
- LLM plane: major event/member news scheduling, LLM digest 생성, internal subscription/trigger 제공자 (port `30003`).
- Admin plane: dashboard-facing admin HTTP control plane, trigger client facade, alarm HTTP 호환 facade (port `30006`).

## Normal status

| Check | Expected |
|---|---|
| Health (bot) | `http://127.0.0.1:30001/health` returns success |
| Health (llm) | `http://127.0.0.1:30003/health` returns success |
| Ready (llm) | `http://127.0.0.1:30003/ready` returns success |
| Health (admin) | `http://127.0.0.1:30006/health` returns success |
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

- 검토 필요.

## Common failure modes

### 1. Health check fails

Symptoms:
- Compose marks `hololive-api` unhealthy.
- Webhook replies, admin dashboard calls, or scheduler triggers stop.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps hololive-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 hololive-api
curl http://127.0.0.1:30001/health
curl http://127.0.0.1:30003/health
curl http://127.0.0.1:30006/health
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
curl http://127.0.0.1:30003/health
```

Mitigation:
- Validate `LLM_SCHEDULER_INTERNAL_URL`, CLIPROXY, and member news/major event source state.
- For `409`, wait for the active run to finish; investigate a stuck scheduler if the conflict persists.

Rollback:
- Roll back the plane/contract/config change that introduced failures.

## Smoke test

```bash
curl http://127.0.0.1:30001/health
curl http://127.0.0.1:30003/health
curl http://127.0.0.1:30003/ready
curl http://127.0.0.1:30006/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `hololive-api` image/config.
- Recheck Iris webhook/reply, scheduler-dependent commands, manual triggers, and dashboard health after rollback.

## Post-deploy monitoring (unified runtime)

bot/admin/llm을 한 프로세스에 묶었으므로 평균값보다 동시 spike(예: LLM weekly digest + admin stats 조회 + bot 이미지 렌더링 동시 발생)가 중요하다. 컷오버 후 최소 24시간 관찰:

- Go RSS / heap inuse·idle, GC pause·GC CPU 비중 (GOMEMLIMIT 1024MiB, 컨테이너 limit 1280m 대비 여유)
- PostgreSQL active/idle connection 수 — plane별 pool 합산(bot/admin/llm 각 max 4 = 최대 12) + alarm-worker/youtube-producer/migration 포함 전체 budget
- pgx acquire latency, Valkey command latency, H3 handshake error rate
- bot webhook p95/p99, admin API p95/p99, LLM scheduler job lag
- deunhealth 재시작 빈도 — healthcheck(30001/health·30003/ready·30006/health)는 process liveness 기준이므로, 잦은 재시작은 H3 listener hang/5s 타임아웃/GC pause를 의심

## Related contracts

- `../contracts/iris-boundary.md`
- `../contracts/membernews.md`
- `../contracts/majorevent.md`
- `../contracts/trigger.md`
- `../contracts/settings.md`
- `../contracts/alarm.md`
