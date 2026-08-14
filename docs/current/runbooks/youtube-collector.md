# Runbook: youtube-collector

## Role

`youtube-collector`는 Community vertical slice의 YouTube.js fetch/normalize/`source_observation_outbox` Publish 런타임입니다. Canonical persist와 observation consume는 `hololive-api` YouTube plane이 담당합니다. 중앙 싱글톤이며 기본 central `docker compose up`에 포함됩니다. AP overlays는 이 서비스를 profile `central-only`로 막아 a/b/d에서 기동하지 않습니다.

Community 알림은 collector Publish와 API consume persist가 모두 필요합니다. producer는 `GetCommunityPosts`를 호출하지 않고 Community consume도 하지 않습니다. collector live path의 정본 community fetch는 YouTube.js helper이며 HTML `GetCommunityPosts` fallback은 없습니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `https://127.0.0.1:30045/health` returns success over H3 |
| Ready | `https://127.0.0.1:30045/ready` returns success |
| Logs | startup markers include PostgreSQL and Valkey connection success; repeated community collect errors are absent |
| Observation store | enabled projection target과 collection lease가 유효하면 `source_observations` / `source_observation_queue` insert |
| Canonical tables | collector 프로세스가 `youtube_community_posts` / `youtube_notification_outbox`를 쓰지 않음 |
| Metrics | live-compat `${HOLOLIVE_METRICS_PORT_BIND_IP}:30096` |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes; `verify-full` TLS with `/run/hololive-bot/certs/postgres-ca.pem` | Publish fails |
| Valkey | yes | poll target/coordination degrades |
| `hololive-api` YouTube plane | consumer persist에 필요 | collector만 기동하면 observation이 PENDING으로 남음 |
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

### 1. Projection/lease load failure

Symptoms:
- collector logs contain `phase=candidate_load` or lease acquire failures
- no new `source_observations` / `source_observation_queue` rows

Diagnosis:
- migration `144`–`161` 적용 여부와 `youtube_collection_projection_generations`, `youtube_collection_targets`, `youtube_collection_job_leases` 상태를 확인합니다.
- `POSTGRES_USER=hololive_scraper`의 projection/lease/observation store grant를 확인합니다.

Mitigation:
- fail-closed가 맞습니다. projection이나 lease 검증을 우회하지 않습니다. API consumer는 이미 발행된 queue만 처리합니다.

Rollback:
- 아래 `## Rollback` 절차로 이전 image를 복원하거나 collector를 영속적으로 disable합니다. API consumer는 이미 발행된 queue만 처리합니다.

### 2. Outbox insert permission denied

Symptoms:
- Publish errors on `source_observation_outbox`
- collector uses `hololive_runtime` instead of `hololive_scraper`

Diagnosis:
- rendered `POSTGRES_USER` and `HOLOLIVE_SCRAPER_PASSWORD`를 확인합니다.

Mitigation:
- `POSTGRES_USER=hololive_scraper`와 scraper DB password를 복구합니다.

Rollback:
- 아래 `## Rollback` 절차로 이전 image를 복원하거나 collector를 영속적으로 disable합니다. producer는 Community fetch를 하지 않으므로 알림은 재개되지 않습니다.

## Smoke test

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30045/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30045/ready
```

projection target과 collection lease가 유효하면 observation insert를 기대합니다. canonical/notification/application/queue finalize는 `hololive-api` YouTube plane이 한 transaction으로 수행합니다.

## Rollback

- 이전 `hololive-youtube-collector:rollback-<UTC timestamp>` tag가 있으면 [`rollback.md`](rollback.md#runtime-rollback)의 revision 확인·`prod` 재승격 절차를 사용한 뒤 다음 명령으로 collector만 무빌드 재생성합니다.

```bash
export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
sudo -n env COMPOSE_ENV_FILE="$COMPOSE_ENV_FILE" ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-build --no-deps --force-recreate youtube-collector
```

- 최초 배포라 보존 tag가 없으면 단순 `stop`을 사용하지 않습니다. 승인된 stack-secrets 변경 절차로 중앙 host의 `compose.env`에 `HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR=1`을 설정하고 unit을 재시작합니다. 공용 `compose.sh`가 tracked `docker-compose.youtube-collector-disabled.yml`을 자동 추가해 replicas를 0으로 유지하므로 이후 reboot, unit restart, 직접 실행한 기본 central `up`에서도 collector가 다시 생성되지 않습니다. 이 상태에서는 `/health`와 `/ready`가 없는 것이 정상이며 Community ingest가 중단됩니다.
- fix-forward image가 준비되면 먼저 `hololive-youtube-collector:prod` revision을 확인하고 `compose.env`의 flag를 `0`으로 되돌린 뒤 unit을 재시작합니다. `/health`, `/ready`, container revision과 restart count를 확인하기 전에는 복구 완료로 판정하지 않습니다.
- migration `144`–`161`과 collector/API plane rollout은 별도 승인 작업입니다. 이 문서는 production migration 적용을 수행하지 않습니다.
- systemd unit `scripts/deploy/lib/hololive-youtube-collector.service`는 템플릿이며 이 변경에서 설치하지 않습니다.
