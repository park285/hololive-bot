# Runbook: stream-ingester

## Role

`stream-ingester`는 photo sync와 ingestion-adjacent runtime 기능을 담당합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30004/health` returns success |
| Ready | 검토 필요 |
| Logs | no repeated sync, DB, cache, or external boundary errors |
| Queue | does not own alarm dispatch queue draining |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | sync/ingestion state fails |
| Valkey | yes | cache/config coordination degrades |
| Iris | no | proactive egress is owned by `alarm-worker` |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP health port | yes |
| `PHOTO_SYNC_ENABLED` | runtime ownership switch | yes |
| `YOUTUBE_INGESTION_ENABLED` | must be false for this service | yes |
| `NOTIFICATION_EGRESS_ROLE=producer` | producer-only egress boundary | yes |
| `CACHE_*`, `POSTGRES_*` | state dependencies | yes |

## Logs

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f stream-ingester
```

## Metrics

- 검토 필요.

## Common failure modes

### 1. Photo sync fails or stalls

Symptoms:
- Sync-related logs repeat errors.
- Health may remain up while work is stale.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=300 stream-ingester
curl http://127.0.0.1:30004/health
```

Mitigation:
- Check PostgreSQL/Valkey and runtime ownership env.

Rollback:
- Roll back `stream-ingester` image/config if sync behavior changed.

## Smoke test

```bash
curl http://127.0.0.1:30004/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `stream-ingester` image/config.
- Confirm `YOUTUBE_INGESTION_ENABLED=false` remains unchanged for this runtime.

## Related contracts

- `../contracts/settings.md`
- `../contracts/iris-boundary.md`
