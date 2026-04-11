# YouTube Scraper 운영 Runbook

> 마지막 업데이트: 2026-03-09
> 대상 서비스: `youtube-scraper` (포트 `30005`)

## 1) 목적

현재 Docker Compose 기준 YouTube 수집/스크래핑/아웃박스 런타임을 `youtube-scraper` 단일 서비스로 운영합니다.

포함 책임:
- YouTube ingestion scheduler
- YouTube scraper scheduler
- YouTube outbox dispatcher
- `config:update` 구독 (`scraper_proxy` 적용, `alarm_advance_minutes` 무시)

제외 책임:
- Holodex photo sync (`stream-ingester`, `30004`)

## 2) 배포 구성

`docker-compose.prod.yml` 기준:
- `stream-ingester`: `YOUTUBE_INGESTION_ENABLED=false`, `PHOTO_SYNC_ENABLED=true`, `YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED=false`, `SERVER_PORT=30004`
- `youtube-scraper`: `YOUTUBE_INGESTION_ENABLED=true`, `PHOTO_SYNC_ENABLED=false`, `YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED=true`, `SERVER_PORT=30005`
- `shared_go_workspace`: 기본값 `./shared-go` (필요 시 `SHARED_GO_WORKSPACE_PATH`로 override 가능)

운영 기준:
- YouTube 커뮤니티/쇼츠 알람 라우팅은 `youtube-scraper` outbox dispatcher로 고정합니다.
- canary/legacy 선택 플래그 없이 전체 운영 채널에 동일 경로를 적용합니다.

스크래퍼 튜닝 env:
- `SCRAPER_WORKER_COUNT` 기본값 `2`
- `SCRAPER_VIDEOS_SECONDS` 기본값 `300`
- `SCRAPER_SHORTS_SECONDS` 기본값 `600`
- `SCRAPER_COMMUNITY_SECONDS` 기본값 `600`
- `SCRAPER_STATS_SECONDS` 기본값 `21600`
- `SCRAPER_LIVE_SECONDS` 기본값 `300`

재배포:

```bash
./scripts/deploy/compose-redeploy-service.sh youtube-scraper
```

## 3) 헬스체크

```bash
docker compose -f docker-compose.prod.yml ps youtube-scraper
curl -fsS http://127.0.0.1:30005/health
docker logs --tail 200 hololive-youtube-scraper
```

정상 기준:
- `youtube-scraper` 컨테이너 `healthy`
- `/health` 200
- `YouTube ingestion scheduler started`
- `Scraper scheduler started`
- `YouTube outbox dispatcher started`
- 분산 락 획득 로그 확인 (`event=ingestion_lease_acquired`, `role=youtube-scraper`)

## 4) 컷오버 체크리스트

1. `docker compose ... ps`에서 `youtube-scraper`가 `healthy`
2. `/health`가 200을 반환
3. 로그에서 `event=ingestion_runtime_configured`와 `runtime=youtube-scraper` 확인
4. 로그에서 `event=ingestion_lease_acquired`, `role=youtube-scraper` 확인
5. 10~15분 관찰 시 중복 수집/중복 알림/락 상실 로그 없음
6. 배포 직후 24시간 검증은 `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_POST_DEPLOY_24H_VERIFICATION.md` 기준으로 진행

## 5) 장애 대응

### A. youtube-scraper 헬스 실패

```bash
docker logs --tail 300 hololive-youtube-scraper
docker compose -f docker-compose.prod.yml up -d --build youtube-scraper
curl -fsS http://127.0.0.1:30005/health
```

### B. 분산 limiter 과차단 의심

1. Valkey 상태 확인
2. `DISTRIBUTED_RATE_LIMITING.md` 기준으로 bucket/지연 증가 여부 확인
3. 필요 시 `youtube-scraper`만 재배포 후 재확인

### C. ingestion 분산 락 경합/상실 감지

- 주요 이벤트 로그:
  - `event=ingestion_lease_acquired`
  - `event=ingestion_lease_released`
  - `event=ingestion_lease_lost`
  - `event=ingestion_lease_renew_failed`
- 운영 규칙:
  - `ingestion_lease_lost` 1회라도 발생하면 즉시 점검
  - `ingestion_lease_renew_failed`가 연속 발생하면 Valkey 연결 상태를 점검
  - 동일 시점에 `stream-ingester`가 락을 잡으려는 로그가 보이면 compose/env 분리를 재확인

예시 확인 명령:

```bash
docker logs --since 15m hololive-youtube-scraper | grep "ingestion_lease"
```

## 6) 장애 대응 원칙

- YouTube ingestion/scraper/outbox 장애는 `youtube-scraper` 복구로 먼저 대응합니다.
- photo sync 장애와 혼동하지 말고 `stream-ingester`와 분리해서 판단합니다.
- `hololive-bot`은 ingestion 런타임을 포함하지 않습니다.

## 7) 수동 점검 항목

- `scraper_proxy` 설정 변경 시 `youtube-scraper` 로그에서 반영 여부 확인
- poll interval 또는 worker count 변경 시 `youtube-scraper`만 재배포하고 10~15분 동안 backlog/지연 로그 증가 여부 확인
- outbox 처리량 증가/정체 여부 확인
- 스케줄러 시작 로그가 모두 남는지 확인
- 커뮤니티/쇼츠 big-bang 배포 직후에는 첫 24시간 동안 `detected/success/unsent/pending/duplicate/latency` 지표를 전용 runbook 기준으로 재확인

## 8) 관련 문서

- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_POST_DEPLOY_24H_VERIFICATION.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md`
- `docs/current/YOUTUBE_COMMUNITY_SHORTS_CHANNEL_ROUTE_VERIFICATION.md`
- `hololive/hololive-kakao-bot-go/docs/STREAM_INGESTER_RUNBOOK.md`
- `docs/SERVICE_DECOMPOSITION_ROADMAP.md`
- `docs/DISTRIBUTED_RATE_LIMITING.md`
