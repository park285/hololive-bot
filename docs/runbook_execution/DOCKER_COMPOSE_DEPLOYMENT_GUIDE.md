# Hololive Docker Compose Deployment Guide

## 목적

단일 호스트 `docker compose` 기반으로 hololive runtime을 운영하기 위한 기본 절차입니다.

대상 서비스:
- `hololive-bot` (`30001`)
- `dispatcher-go` (`30020`)
- `llm-scheduler` (`30003`)
- `stream-ingester` (`30004`)
- `holo-postgres` (`5433`)
- `valkey-cache` (`6379`)

## 사전 준비

1. `.env` 작성
   ```bash
   cp .env.example .env
   ```
2. 필수 시크릿/접속값 채우기
   - `DB_PASSWORD`
   - `IRIS_SHARED_TOKEN`
   - `HOLODEX_API_KEY_*`
   - 필요 시 `NORDVPN_*` (VPN profile 사용 시만)
3. Docker Compose 사용 가능 여부 확인
   ```bash
   docker compose version
   ```

## 기본 기동

```bash
./build-all.sh --no-bump
```

또는:

```bash
docker compose -f docker-compose.prod.yml up -d --build
```

## 단일 서비스 재배포

```bash
./scripts/deploy/compose-redeploy-service.sh hololive-bot
./scripts/deploy/compose-redeploy-service.sh llm-scheduler
./scripts/deploy/compose-redeploy-service.sh stream-ingester
./scripts/deploy/compose-redeploy-service.sh dispatcher-go
```

## 상태 확인

```bash
docker compose -f docker-compose.prod.yml ps
curl -fsS http://127.0.0.1:30001/health
curl -fsS http://127.0.0.1:30003/health
curl -fsS http://127.0.0.1:30004/health
curl -fsS http://127.0.0.1:30020/ready
```

## 로그 확인

기본 정책은 **애플리케이션 stdout/stderr를 SSOT**로 두고 `docker compose logs`를 직접 조회하는 것입니다.

```bash
docker compose -f docker-compose.prod.yml logs -f hololive-bot
docker compose -f docker-compose.prod.yml logs -f llm-scheduler
docker compose -f docker-compose.prod.yml logs -f stream-ingester
docker compose -f docker-compose.prod.yml logs -f dispatcher-go
```

보조 스크립트:

```bash
./scripts/logs/query.sh bot --since 1h --limit 1000
./scripts/logs/tail.sh dispatcher --since 30m
./scripts/logs/backfill.sh llm --since 24h
./scripts/logs/stream.sh start
./scripts/logs/prune.sh
```

- compose 런타임에서는 `LOG_DIR=/app/logs/<service>`로 설정해 host `./logs/hololive-bot/`, `./logs/dispatcher-go/`, `./logs/llm-scheduler/`, `./logs/stream-ingester/`에 파일 미러링합니다.
- 앱 파일 로그 로테이션 기본값은 `100MB`, `5 backups`, `30일`, `gzip 압축`입니다.
- Docker Compose `json-file` 드라이버 로테이션 기본값은 `10MB`, `3 files`입니다.
- `logs/mirror/*.log`는 `stream.sh` 또는 `dump.sh`로 만든 **선택적 로컬 미러링**이며 운영 SSOT가 아닙니다.
- `logs/backfill/*.log`는 일회성 스냅샷이며 기본 7일 보관입니다.
- `logs/canary/`, `logs/cron/`, `logs/runtime/pids/`는 보조 운영 산출물입니다.
- 보조 로그 정리는 `./scripts/logs/prune.sh` 기준으로 수행합니다.
- 운영 판단의 기준은 `docker compose logs`이며, 별도 log aggregation 전제를 두지 않습니다.

## DB migration

초기화/마이그레이션은 `hololive-db-migrate`가 담당합니다.

```bash
docker compose -f docker-compose.prod.yml up --build hololive-db-migrate
```

## VPN scraper (선택)

기본 스택에는 포함되지 않습니다. 필요 시 profile로만 실행하세요.

```bash
docker compose -f docker-compose.prod.yml --profile vpn up -d vpn-scraper-proxy
```

## 정지 / 재기동

```bash
docker compose -f docker-compose.prod.yml stop
docker compose -f docker-compose.prod.yml start
```

## 완전 종료

```bash
docker compose -f docker-compose.prod.yml down
```
