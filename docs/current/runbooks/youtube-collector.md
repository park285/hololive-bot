# Runbook: youtube-collector

## Role

`youtube-collector`는 AP fleet(`a`/`b`/`c`/`d`)의 외부 수집 런타임입니다. Holodex / Official / YouTube.js fetch, collection lease, checkpoint, observation Publish만 소유합니다. Canonical persist와 notification intent는 `hololive-api` YouTube plane이 담당합니다. `members.photo`는 hololive-api admin PhotoSync가 담당합니다.

## Deploy completion contract

| Host | Service | Runtime | Port | Ready | Env | DB role |
|---|---|---|---:|---|---|---|
| Osaka `a` | `youtube-collector-a` | host-native `hololive-youtube-collector@youtube-collector-a.service` | 30005 | `https://127.0.0.1:30005/ready` | `/etc/stack-secrets/hololive-bot/youtube-collector.env` + `youtube-collector-host.env` | `hololive_scraper` |
| Seoul `b` | `youtube-collector-b` | Compose | 30015 | `https://127.0.0.1:30015/ready` | `HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE` | `hololive_scraper` |
| Central `c` | `youtube-collector` | Compose | 30025 | `https://127.0.0.1:30025/ready` | `HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE` | `hololive_scraper` |
| Osaka2 `d` | `youtube-collector-d` | host-native `hololive-youtube-collector@youtube-collector-d.service` | 30035 | `https://127.0.0.1:30035/ready` | `/etc/stack-secrets/hololive-bot/youtube-collector.env` + `youtube-collector-host.env` | `hololive_scraper` |

Completion is `scripts/deploy/ap-completion-check.sh <host>` for APs and compose `/ready` for central `c`.

## Normal status

| Check | Expected |
|---|---|
| Health | `https://127.0.0.1:<port>/health` returns success over H3 |
| Ready | `https://127.0.0.1:<port>/ready` returns `status=ready` and `instance_id` |
| Logs | startup markers include PostgreSQL and Valkey connection success |
| Observation store | enabled projection target과 collection lease가 유효하면 `source_observations` / `source_observation_queue` insert |
| Canonical tables | collector 프로세스가 canonical/notification tables를 쓰지 않음 |
| Metrics | `:30096` on the host metrics bind |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes; `verify-full` TLS with postgres CA | Publish fails |
| Valkey | optional | DB-only scheduling |
| `hololive-api` YouTube plane | consumer persist에 필요 | collector만 기동하면 observation이 PENDING으로 남음 |
| Iris | no | final proactive egress is owned by `alarm-worker` |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | per-host H3 port | yes |
| `YOUTUBE_INGESTION_ENABLED=true` | collector enablement | yes |
| `YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true` | must be true only on collector hosts | yes |
| `YOUTUBE_COLLECTOR_INSTANCE_ID` | fleet identity `youtube-collector-a/b/c/d` | yes |
| `PHOTO_SYNC_ENABLED=false` | photo product path stays on hololive-api admin | yes |
| `POSTGRES_USER=hololive_scraper` | lease/observation insert only | yes |
| `POSTGRES_SSLMODE=verify-full` | required client verification | yes |
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false` | egress boundary | yes |

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f youtube-collector
```

## Common failure modes

### 1. Projection/lease load failure

Symptoms:
- collector logs contain `phase=candidate_load` or lease acquire failures
- no new `source_observations` / `source_observation_queue` rows

Diagnosis:
- migration `144`–`161` 적용 여부와 collection projection/target/lease 상태를 확인합니다.
- `POSTGRES_USER=hololive_scraper` grant를 확인합니다.

Mitigation:
- fail-closed가 맞습니다. projection이나 lease 검증을 우회하지 않습니다.

### 2. Outbox insert permission denied

Symptoms:
- Publish errors
- collector uses `hololive_runtime` instead of `hololive_scraper`

Diagnosis:
- rendered `POSTGRES_USER` and `HOLOLIVE_SCRAPER_PASSWORD`를 확인합니다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30025/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30025/ready
```

## Rollback

- 이전 `hololive-youtube-collector:rollback-<UTC timestamp>` tag가 있으면 [`rollback.md`](rollback.md#runtime-rollback)의 revision 확인·`prod` 재승격 절차를 사용한 뒤 collector만 무빌드 재생성합니다.

```bash
export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
sudo -n env COMPOSE_ENV_FILE="$COMPOSE_ENV_FILE" ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-build --no-deps --force-recreate youtube-collector
```

- 승인된 stack-secrets 변경 절차로 중앙 host의 `compose.env`에 `HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR=1`을 설정하면 tracked disable overlay가 replicas를 0으로 유지합니다.
