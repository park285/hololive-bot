# Runbook: youtube-scraper

## Role

`youtube-scraper`는 YouTube scraping/polling과 outbox production을 담당합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30005/health` returns success |
| Ready | 검토 필요 |
| Logs | no repeated poller, outbox, DB, cache, or proxy errors |
| Queue | produces outbox/tracking state; does not consume alarm dispatch queue |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | polling/outbox persistence fails |
| Valkey | yes | cache/config/coordination degrades |
| Iris | no | final proactive egress is owned by `alarm-worker` |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP health port | yes |
| `YOUTUBE_INGESTION_ENABLED` | must be true for this service | yes |
| `PHOTO_SYNC_ENABLED` | must be false for this service | yes |
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false` | producer-only egress boundary | yes |
| `YOUTUBE_SCRAPER_RUNTIME_ALLOWED=true` | must be true only on the owning Osaka host | yes |
| `SCRAPER_*` | poller intervals/workers | yes |

## Logs

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs -f youtube-scraper
```

## Metrics

- 검토 필요.

## Common failure modes

### 1. Polling stalls

Symptoms:
- No fresh YouTube polling/outbox activity.
- Health may remain up.

Diagnosis:
```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs --tail=300 youtube-scraper
curl http://127.0.0.1:30005/health
```

Mitigation:
- Check scraper interval env, PostgreSQL, Valkey, and YouTube/proxy errors.

Rollback:
- Roll back `youtube-scraper` image/config if poller behavior changed.

### 2. Outbox backlog grows

Symptoms:
- Scraper persists events but downstream delivery is delayed.

Diagnosis:
```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs --tail=300 youtube-scraper
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs --tail=300 dispatcher-go
```

Mitigation:
- Identify whether backlog is scraper, alarm-worker, or dispatcher/Iris side.

Rollback:
- Roll back the runtime that introduced the backlog.

## Smoke test

```bash
curl http://127.0.0.1:30005/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `youtube-scraper` image/config.
- Confirm `YOUTUBE_INGESTION_ENABLED=true` and outbox production state after rollback.

## Related contracts

- `../contracts/alarm.md`
- `../contracts/settings.md`
- `../contracts/iris-boundary.md`
