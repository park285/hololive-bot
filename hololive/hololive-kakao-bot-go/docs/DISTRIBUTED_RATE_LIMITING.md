# Distributed Rate Limiting 가이드

> 마지막 업데이트: 2026-02-28

## 개요

멀티 인스턴스 환경에서 API/스크래퍼 요청 총량을 제어하기 위해 Valkey 기반 Sliding Window Rate Limiter를 적용합니다.

- 구현: `internal/service/ratelimit/sliding_window.go`
- 저장소: Valkey ZSET (`ratelimit:sliding:*`)

## 적용 대상

### 1) Holodex API 경로
- 설정: `constants.HolodexDistributedRateLimitConfig`
- 연동:
  - `internal/service/holodex/service.go`
  - `internal/service/holodex/api_client.go`
- 기본값:
  - `limit=10`
  - `window=1s`
  - `bucket_base=holodex:api`

### 2) YouTube HTML Scraper 경로
- 설정: `constants.YouTubeScraperDistributedRateLimitConfig`
- 연동:
  - `internal/app/bootstrap.go`
  - `internal/app/bootstrap_admin.go`
  - `internal/app/bootstrap_dispatcher.go`
  - `internal/service/youtube/scraper/client.go`
- 기본값:
  - `limit=1`
  - `window=3s`
  - `bucket_base=youtube:scraper`

## 동작 방식

1. 요청 시 현재 윈도우 밖 항목을 정리합니다.  
2. 현재 카운트가 limit 이상이면 차단하고 `RetryAfter`를 계산합니다.  
3. 허용 시 현재 요청을 ZSET에 기록하고 TTL을 갱신합니다.

## 검증 명령

```bash
go test ./internal/service/ratelimit ./internal/service/holodex ./internal/service/youtube/scraper ./internal/app
go build ./...
```

## 운영 시 확인 포인트

- `llm-valkey-cache` 연결 상태 (`/health`, logs)
- `holodex`/`youtube scraper` 호출 지연 증가 여부
- 과도한 거부(deny) 로그 발생 여부
- `stream-ingester` 단일 ingestion 리스 유지 여부 (`event=ingestion_lease_acquired`)
