# YouTube Producer 운영 Runbook

> 실제 tailnet 주소/호스트는 private ops evidence 참조.

> 마지막 업데이트: 2026-07-11
> 대상 서비스: `youtube-producer` (4-way active-active: osaka `30005`, seoul `30015`, main `30025`, osaka2 `30035`)

## 1) 목적

현재 YouTube 수집/스크래핑/아웃박스 런타임을 `youtube-producer` 서비스의 4-way active-active 인스턴스(osaka `a` + seoul `b` + main `c` + osaka2 `d`)로 운영합니다. seoul `b`·main `c`는 Docker Compose 컨테이너로, osaka `a`·osaka2 `d`는 host-native `systemd` 런타임으로 실행됩니다.

포함 책임:
- YouTube ingestion scheduler
- YouTube producer scheduler
- YouTube outbox row production
- `config:update` 구독 (`scraper_proxy` 적용, `alarm_advance_minutes` 무시)

제외 책임:
- Iris final send and delivery terminal state (`alarm-worker`)

## 2) 배포 구성

`docker-compose.prod.yml` 기준:
- `youtube-producer`: `YOUTUBE_INGESTION_ENABLED=true`, `PHOTO_SYNC_ENABLED=true`, `YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED=true`, `YOUTUBE_PRODUCER_RUNTIME_ALLOWED=false`, `SERVER_PORT=30005`
- `shared_go_workspace`: 기본값 `./shared-go` (필요 시 `SHARED_GO_WORKSPACE_PATH`로 override 가능)

운영 기준:
- YouTube 커뮤니티/쇼츠 알람 라우팅은 `youtube-producer` outbox row production과 `alarm-worker` final egress로 고정합니다.
- canary/legacy 선택 플래그 없이 전체 운영 채널에 동일 경로를 적용합니다.

Remote AP split-host 운영 기준 (4-way active-active):
- 토폴로지: Osaka `youtube-producer-a` (`<osaka-a-host>`, `<tailnet-osaka-a>`, `30005`, host-native `systemd`) + Seoul `youtube-producer-b` (`<tailnet-seoul-b>`, `30015`) + main `youtube-producer-c` (`<tailnet-central>`, `30025`, profile `main-ap`) + Osaka2 `youtube-producer-d` (`<tailnet-osaka2-d>`, `30035`, host-native `systemd`)
- shared state/control 호스트: `kapu` (`<tailnet-central>`)
- 원격 AP에서는 `holo-postgres`, `hololive-db-migrate`, `valkey-cache`를 올리지 않고 `<tailnet-central>:5433`, `<tailnet-central>:6379`, `http://<tailnet-central>:8787/v1`을 사용합니다.
- Seoul `youtube-producer-b`는 `docker-compose.prod.yml`과 `docker-compose.seoul.yml`을 겹친 뒤 `--no-deps`로 시작합니다. Osaka `youtube-producer-a`와 Osaka2 `youtube-producer-d`의 live 배포는 `scripts/deploy/ap-host-native-deploy.sh`를 사용하며, 해당 Compose overlay는 경로·계약 검증과 rollback 대비용으로만 유지합니다.
- `YOUTUBE_PRODUCER_RUNTIME_ALLOWED=true`는 Seoul Compose overlay, Osaka·Osaka2 host-native env, `main-ap` profile에서만 설정합니다. 중앙 host `kapu`의 base `youtube-producer`는 이 값이 false라서 락 획득 전에 종료되어야 합니다.
- env 정본은 OpenBao KV이며, 원격 AP Compose는 OpenBao Agent가 렌더링한 token-free `/run/hololive-bot/ap-compose.env`와 producer 전용 `/run/hololive-bot/youtube-producer.env`를 사용합니다. 중앙 Valkey는 Tailscale IP에 publish되지만 password 인증을 필수로 사용합니다.
- `CACHE_PASSWORD`는 admin-dashboard Redis URL에도 들어가므로 URL-safe hex 값을 권장합니다.

스크래퍼 튜닝 env:
- `SCRAPER_WORKER_COUNT` 기본값 `2`
- `SCRAPER_VIDEOS_SECONDS` 기본값 `300`
- `SCRAPER_SHORTS_SECONDS` 기본값 `60`
- `SCRAPER_COMMUNITY_SECONDS` 기본값 `60`
- `SCRAPER_STATS_SECONDS` 기본값 `21600`
- `SCRAPER_LIVE_SECONDS` 기본값 `300`

원격 AP 재배포 (osaka / seoul) — rsync/build/recreate/검증을 포함한 scoped wrapper를 사용합니다:

```bash
./scripts/deploy/ap-deploy.sh osaka --dry-run
I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh osaka --apply

./scripts/deploy/ap-deploy.sh seoul --dry-run
I_APPROVE_SEOUL_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh seoul --apply
```

## 3) 헬스체크

main 호스트 (`youtube-producer-c`):

```bash
docker ps --filter name=hololive-youtube-producer-c --format '{{.Names}}\t{{.Status}}'
COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.main-ap.yml exec -T youtube-producer-c ./bin/healthcheck https://127.0.0.1:30025/health
docker logs --tail 200 hololive-youtube-producer-c
```

원격 AP 확인:

```bash
./scripts/logs/ap-status.sh osaka
./scripts/logs/ap-status.sh seoul
SINCE=15m TAIL=600 PATTERN='ingestion_lease|outbox|ERR|WRN' ./scripts/logs/ap-logs.sh osaka youtube-producer | tail -n 120
SINCE=15m TAIL=600 PATTERN='ingestion_lease|outbox|ERR|WRN' ./scripts/logs/ap-logs.sh seoul youtube-producer | tail -n 120
```

정상 기준:
- 각 AP 컨테이너(`hololive-youtube-producer-a`/`-b`/`-c`) `healthy`
- `/health` 200
- `YouTube ingestion scheduler started`
- `Scraper scheduler started`
- `YouTube outbox dispatcher disabled`
- 분산 락 획득 로그 확인 (`event=ingestion_lease_acquired`, `role=youtube-producer`)

## 4) 컷오버 체크리스트

1. `./scripts/logs/ap-status.sh osaka`/`seoul`과 main `docker ps --filter name=hololive-youtube-producer-c`에서 모든 AP 컨테이너가 `healthy`
2. `/health`가 200을 반환
3. 로그에서 `event=ingestion_runtime_configured`와 `runtime=youtube-producer` 확인
4. 로그에서 `event=ingestion_lease_acquired`, `role=youtube-producer` 확인
5. 10~15분 관찰 시 중복 수집/중복 알림/락 상실 로그 없음
6. 배포 직후 24시간 검증은 `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`·`docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md` 기준으로 진행

## 5) 장애 대응

### A. youtube-producer 헬스 실패

```bash
./scripts/logs/ap-status.sh osaka
./scripts/logs/ap-status.sh seoul
docker logs --tail 300 hololive-youtube-producer-c
```

복구는 해당 호스트만 재배포합니다 — 원격 AP는 `./scripts/deploy/ap-deploy.sh <host>`, main은 `COMPOSE_FILE=docker-compose.prod.yml:docker-compose.main-ap.yml COMPOSE_PROFILES=main-ap ./scripts/deploy/compose-redeploy-service.sh youtube-producer-c`.

### B. 분산 limiter 과차단 의심

1. Valkey 상태 확인
2. `DISTRIBUTED_RATE_LIMITING.md` 기준으로 bucket/지연 증가 여부 확인
3. 필요 시 `youtube-producer`만 재배포 후 재확인

### C. ingestion 분산 락 경합/상실 감지

- 주요 이벤트 로그:
  - `event=ingestion_lease_acquired`
  - `event=ingestion_lease_released`
  - `event=ingestion_lease_lost`
  - `event=ingestion_lease_renew_failed`
- 운영 규칙:
  - `ingestion_lease_lost` 1회라도 발생하면 즉시 점검
  - `ingestion_lease_renew_failed`가 연속 발생하면 Valkey 연결 상태를 점검
  - 동일 시점에 `youtube-producer`가 락을 잡으려는 로그가 보이면 compose/env 분리를 재확인

예시 확인 명령:

```bash
SINCE=15m TAIL=600 PATTERN='ingestion_lease' ./scripts/logs/ap-logs.sh osaka youtube-producer
SINCE=15m TAIL=600 PATTERN='ingestion_lease' ./scripts/logs/ap-logs.sh seoul youtube-producer
docker logs --since 15m hololive-youtube-producer-c 2>&1 | grep "ingestion_lease"
```

원격 AP rollback — prechange 백업 기반 helper를 사용합니다 (host: `osaka`/`seoul`):

```bash
BACKUP_DIR=backups/osaka-active-active-<timestamp> ./scripts/deploy/ap-rollback.sh osaka --dry-run
I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true BACKUP_DIR=backups/osaka-active-active-<timestamp> ./scripts/deploy/ap-rollback.sh osaka --apply

BACKUP_DIR=backups/seoul-active-active-<timestamp> ./scripts/deploy/ap-rollback.sh seoul --dry-run
I_APPROVE_SEOUL_ACTIVE_ACTIVE_ROLLBACK=true BACKUP_DIR=backups/seoul-active-active-<timestamp> ./scripts/deploy/ap-rollback.sh seoul --apply
```

rollback도 한 번에 한 호스트만 수행합니다. 토폴로지/순서 기준은 `docs/current/runbooks/youtube-producer.md`의 Rollback 섹션을 따릅니다. `youtube-producer`는 outbox row producer이므로 승인된 active-active guard 없이 여러 호스트에 동시 기동하지 않습니다.

## 6) 장애 대응 원칙

- YouTube ingestion/scraper/outbox 장애는 `youtube-producer` 복구로 먼저 대응합니다.
- photo sync 장애와 혼동하지 말고 `youtube-producer`와 분리해서 판단합니다.
- `hololive-bot`은 ingestion 런타임을 포함하지 않습니다.

## 7) 수동 점검 항목

- `scraper_proxy` 설정 변경 시 `youtube-producer` 로그에서 반영 여부 확인
- poll interval 또는 worker count 변경 시 `youtube-producer`만 재배포하고 10~15분 동안 backlog/지연 로그 증가 여부 확인
- outbox 처리량 증가/정체 여부 확인
- 스케줄러 시작 로그가 모두 남는지 확인
- 커뮤니티/쇼츠 big-bang 배포 직후에는 첫 24시간 동안 `detected/success/unsent/pending/duplicate/latency` 지표를 `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`·`YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md` 기준으로 재확인

## 8) 관련 문서

- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_CHANNEL_ROUTE_VERIFICATION.md`
- `hololive/hololive-kakao-bot-go/docs/STREAM_INGESTER_RUNBOOK.md`
- `docs/SERVICE_DECOMPOSITION_ROADMAP.md`
- `docs/DISTRIBUTED_RATE_LIMITING.md`
