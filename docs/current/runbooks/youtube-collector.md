# Runbook: youtube-collector

## Role

`youtube-collector`는 Community vertical slice의 YouTube.js fetch/normalize/`source_observation_outbox` Publish 런타임입니다. Canonical persist와 observation consume는 `youtube-producer`가 담당합니다. 중앙 싱글톤이며 기본 central `docker compose up`에 포함됩니다. AP overlays는 이 서비스를 profile `central-only`로 막아 a/b/d에서 기동하지 않습니다.

Community 알림은 collector Publish와 producer consumer persist가 모두 필요합니다. producer는 `GetCommunityPosts`를 호출하지 않습니다. collector live path의 정본 community fetch는 YouTube.js helper이며 HTML `GetCommunityPosts` fallback은 없습니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `https://127.0.0.1:30045/health` returns success over H3 |
| Ready | `https://127.0.0.1:30045/ready` returns success |
| Logs | startup markers include PostgreSQL and Valkey connection success; repeated community collect errors are absent |
| Outbox | fence가 유효하면(`legacy`/`shadow`/`authoritative`) `source_observation_outbox` insert |
| Canonical tables | collector 프로세스가 `youtube_community_posts` / `youtube_notification_outbox`를 쓰지 않음 |
| Metrics | live-compat `${HOLOLIVE_METRICS_PORT_BIND_IP}:30096` |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes; `verify-full` TLS with `/run/hololive-bot/certs/postgres-ca.pem` | Publish fails |
| Valkey | yes | poll target/coordination degrades |
| `youtube-producer` | consumer persist에 필요 | collector만 기동하면 observation이 PENDING으로 남음 |
| Iris | no | final proactive egress is owned by `alarm-worker` |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT=30045` | HTTP health port | yes |
| `YOUTUBE_INGESTION_ENABLED=true` | collector polling enablement | yes |
| `YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true` | must be true only on the collector host | yes |
| `YOUTUBE_PRODUCER_RUNTIME_ALLOWED=false` | collector binary must not start producer runtime | yes |
| `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=false` | collector is a singleton | yes |
| `PHOTO_SYNC_ENABLED=false` | photo sync stays on producer AP-C | yes |
| `POSTGRES_USER=hololive_scraper` | outbox SELECT/INSERT only | yes |
| `HOLOLIVE_SCRAPER_PASSWORD` | scraper role password used as `POSTGRES_PASSWORD` | yes |
| `POSTGRES_SSLMODE=verify-full` | required client verification | yes |
| `POSTGRES_SSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem` | CA bundle | yes |
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false` | producer-only egress boundary | yes |
| `SCRAPER_POLL_SHORTS_INTERVAL_SECONDS` | collector primary cadence when set (`communityPrimaryPollInterval`) | no |
| `SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS` | collector cadence fallback when shorts interval is unset | no |

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f youtube-collector
```

## Common failure modes

### 1. Fence load failure

Symptoms:
- collector poll errors contain `load authority fence`
- no new `source_observation_outbox` rows

Diagnosis:
- migration `144` 적용 여부와 `source_authority_fences` 행을 확인합니다.
- `POSTGRES_USER=hololive_scraper` SELECT grant를 확인합니다.

Mitigation:
- fail-closed가 맞습니다. fence를 고치기 전에 Publish를 우회하지 않습니다.

Rollback:
- collector를 중지하면 Community 알림 ingest가 멈춥니다. producer consumer는 남은 outbox만 처리합니다.

### 2. Outbox insert permission denied

Symptoms:
- Publish errors on `source_observation_outbox`
- collector uses `hololive_runtime` instead of `hololive_scraper`

Diagnosis:
- rendered `POSTGRES_USER` and `HOLOLIVE_SCRAPER_PASSWORD`를 확인합니다.

Mitigation:
- `POSTGRES_USER=hololive_scraper`와 scraper DB password를 복구합니다.

Rollback:
- collector를 중지합니다. producer는 Community fetch를 하지 않으므로 알림은 재개되지 않습니다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30045/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30045/ready
```

fence가 유효하면 observation insert를 기대합니다. shadow는 canonical/notification write가 없고, persist는 producer consumer의 legacy/authoritative 경로입니다.

## Rollback

- `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml stop youtube-collector` 로 collector만 내립니다. 기본 central `up`은 collector를 다시 시작합니다.
- production fence default는 migration `144`의 `legacy`입니다. 이 문서는 fence flip이나 production migration 적용을 수행하지 않습니다.
- systemd unit `scripts/deploy/lib/hololive-youtube-collector.service`는 템플릿이며 이 변경에서 설치하지 않습니다.
