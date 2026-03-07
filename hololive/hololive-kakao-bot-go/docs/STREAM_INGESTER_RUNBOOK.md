# Stream Ingester 운영 Runbook

> 마지막 업데이트: 2026-02-27
> 대상 서비스: `stream-ingester` (포트 `30004`)

## 1) 목적

P6 분리 단계에서 YouTube/스크래퍼 수집 런타임을 `hololive-bot`에서 분리해 독립 운영합니다.

포함 책임:
- YouTube ingestion scheduler
- YouTube scraper scheduler
- YouTube outbox dispatcher
- Photo sync
- config:update 구독 (`scraper_proxy`, `alarm_advance_minutes`)

## 2) 배포 구성

`docker-compose.prod.yml` 기준:
- `hololive-bot`: ingestion 미포함
- `stream-ingester`: ingestion 전담, `SERVER_PORT=30004`

## 3) 헬스체크

```bash
docker compose -f docker-compose.prod.yml ps hololive-bot stream-ingester
curl -fsS http://127.0.0.1:30004/health
docker logs --tail 200 hololive-stream-ingester
```

정상 기준:
- `stream-ingester` 컨테이너 `healthy`
- `/health` 200
- 스케줄러 시작 로그 확인
- 분산 락 획득 로그 확인 (`event=ingestion_lease_acquired`, `role=stream-ingester`)

## 4) 컷오버 체크리스트 (운영 전환 시)

1. `docker compose ... ps`에서 `hololive-bot`, `stream-ingester` 모두 `healthy`
2. 환경 변수 확인
   - `hololive-bot`: ingestion 관련 토글 없음
   - `stream-ingester`: ingestion 관련 토글 없음
3. 로그 확인
   - bot에서 ingestion 시작 로그가 없어야 함
   - stream-ingester에서 `event=stream_ingestion_enabled`, `event=ingestion_lease_acquired` 확인
4. 10~15분 관찰 시 중복 수집/중복 알림 로그 없음

## 5) 장애 대응

### A. stream-ingester 헬스 실패
```bash
docker logs --tail 300 hololive-stream-ingester
docker compose -f docker-compose.prod.yml up -d --build stream-ingester
curl -fsS http://127.0.0.1:30004/health
```

### B. 분산 limiter 과차단 의심
1. Valkey 상태 확인
2. `DISTRIBUTED_RATE_LIMITING.md` 기준으로 bucket/지연 증가 여부 확인
3. 필요 시 트래픽 완화 후 재확인

### C. ingestion 분산 락 경합/상실 감지
- 주요 이벤트 로그:
  - `event=ingestion_lease_acquired`
  - `event=ingestion_lease_released`
  - `event=ingestion_lease_lost`
  - `event=ingestion_lease_renew_failed`
- 운영 규칙:
  - `ingestion_lease_lost` 1회라도 발생하면 즉시 점검
  - `ingestion_lease_renew_failed`가 연속 발생하면 Valkey 연결 상태 점검

예시 확인 명령:
```bash
docker logs --since 15m hololive-stream-ingester | grep "ingestion_lease"
```

## 6) 장애 대응 원칙

- ingestion 장애는 `stream-ingester` 복구로만 대응합니다.
- `hololive-bot`은 더 이상 ingestion fallback 경로를 제공하지 않습니다.

## 7) 수동 점검 항목

- `scraper_proxy` 설정 변경 시 stream-ingester 로그에 적용 로그 확인
- outbox 처리량 지표/로그 증가 확인
- photo sync 주기 실행 로그 확인

## 8) 관련 문서

- `docs/SERVICE_DECOMPOSITION_ROADMAP.md`
- `docs/DISTRIBUTED_RATE_LIMITING.md`
