# Stream Ingester 운영 Runbook

> 마지막 업데이트: 2026-03-09
> 대상 서비스: `stream-ingester` (포트 `30004`)

## 1) 목적

현재 Docker Compose 기준 `stream-ingester`는 photo sync 중심 런타임으로 운영합니다.

포함 책임:
- Photo sync
- ingestion-adjacent health/runtime 엔드포인트

제외 책임:
- YouTube ingestion scheduler
- YouTube scraper scheduler
- YouTube outbox dispatcher
- `config:update` 기반 scraper runtime 운영

위 책임은 `youtube-scraper` (`30005`)가 담당합니다. 상세는 `docs/runbook_execution/YOUTUBE_SCRAPER_RUNBOOK.md`를 따르세요.

## 2) 배포 구성

`docker-compose.prod.yml` 기준:
- `hololive-bot`: ingestion 미포함
- `stream-ingester`: `YOUTUBE_INGESTION_ENABLED=false`, `PHOTO_SYNC_ENABLED=true`, `SERVER_PORT=30004`
- `youtube-scraper`: `YOUTUBE_INGESTION_ENABLED=true`, `PHOTO_SYNC_ENABLED=false`, `SERVER_PORT=30005`

운영 기준:
- YouTube 커뮤니티/쇼츠 알람 라우팅은 `stream-ingester`가 아닌 `youtube-scraper` outbox dispatcher가 전담합니다.
- compose 운영 경로에는 canary/legacy fallback 선택 분기를 두지 않습니다.

## 3) 헬스체크

```bash
docker compose -f docker-compose.prod.yml ps stream-ingester
curl -fsS http://127.0.0.1:30004/health
docker logs --tail 200 hololive-stream-ingester
```

정상 기준:
- `stream-ingester` 컨테이너 `healthy`
- `/health` 200
- `Photo sync service started` 로그 확인
- `event=ingestion_lease_acquired`가 **없음** (`stream-ingester`는 YouTube ingestion 락을 잡지 않음)

## 4) 컷오버 체크리스트 (운영 전환 시)

1. `docker compose ... ps`에서 `stream-ingester`가 `healthy`
2. `/health`가 200을 반환
3. 로그에서 `Photo sync service started` 확인
4. 10~15분 관찰 시 photo sync 오류/반복 재시작 로그 없음

## 5) 장애 대응

### A. stream-ingester 헬스 실패
```bash
docker logs --tail 300 hololive-stream-ingester
docker compose -f docker-compose.prod.yml up -d --build stream-ingester
curl -fsS http://127.0.0.1:30004/health
```

### B. photo sync 이상 징후
1. Valkey 상태 확인
2. Holodex API 응답/에러 증가 여부 확인
3. 필요 시 `stream-ingester`만 재배포 후 재확인

### C. ingestion 이슈가 보일 때
- `stream-ingester`가 아니라 `youtube-scraper` 로그/헬스를 먼저 확인합니다.
- 분산 락 이벤트(`event=ingestion_lease_*`)는 `youtube-scraper` 런북 기준으로 대응합니다.

예시 확인 명령:
```bash
docker logs --since 15m hololive-stream-ingester | grep "ingestion_lease"
```

## 6) 장애 대응 원칙

- photo sync 장애는 `stream-ingester` 복구로 대응합니다.
- YouTube ingestion/scraper/outbox 장애는 `youtube-scraper` 런북으로 대응합니다.
- `hololive-bot`은 ingestion 런타임을 포함하지 않습니다.

## 7) 수동 점검 항목

- photo sync 주기 실행 로그 확인
- ingestion 관련 로그가 보이면 `youtube-scraper`에 잘못 배치되지 않았는지 compose/env를 재확인

## 8) 관련 문서

- `docs/runbook_execution/YOUTUBE_SCRAPER_RUNBOOK.md`
- `docs/SERVICE_DECOMPOSITION_ROADMAP.md`
- `docs/DISTRIBUTED_RATE_LIMITING.md`
