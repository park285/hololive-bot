# Operations Runbook

## 상황 1. 403/429가 증가합니다

### 확인

```promql
sum by (reason) (rate(youtube_scraper_channel_failures_total{reason=~"rate_limited|forbidden"}[10m]))
```

### 판단

- global hard cooldown이 작동하는지 봅니다.
- RPM budget이 초과됐는지 봅니다.
- 최근 poll interval 변경이 있었는지 봅니다.
- API fallback도 함께 증가하는지 봅니다.

### 조치

1. channel health보다 global rate limit을 먼저 봅니다.
2. poll interval을 늘립니다.
3. fallback이 quota를 급격히 쓰면 API fallback trigger를 점검합니다.
4. browser diagnostic을 실행하지 않습니다.

## 상황 2. parser_drift가 증가합니다

### 확인

```promql
sum by (operation, source) (rate(youtube_scraper_channel_failures_total{reason="parser_drift"}[10m]))
```

### 조치

1. snapshot enabled 여부 확인
2. snapshot artifact 확인
3. fixture로 승격
4. parser test 작성
5. parser 수정
6. snapshot cleanup

## 상황 3. timeout/transport가 증가합니다

### 확인

```promql
sum by (reason) (rate(youtube_scraper_channel_failures_total{reason=~"timeout|transport"}[10m]))
```

### 조치

1. 프록시 상태 확인
2. DNS/network 상태 확인
3. scraper HTTP timeout 확인
4. channel health enforce가 latency를 악화시키는지 확인
5. timeout/transport backoff max를 낮춤

## 상황 4. API fallback quota가 증가합니다

### 확인

- API fallback recovery metric
- quota used
- scraper failure reason

### 조치

1. parser drift인지 403/429인지 구분
2. events==0이 실패 처리되는지 확인
3. failedIDs만 fallback 대상인지 확인
4. 필요하면 API fallback temporarily disable 또는 quota safety margin 증가

## 상황 5. snapshot 디스크가 찹니다

### 조치

```bash
SCRAPER_SNAPSHOT_ENABLED=false
find ./artifacts/youtube-scraper -type f -mtime +3 -delete
```

### 재발 방지

- max body bytes 감소
- min interval 증가
- allowed reason 축소

## 상황 6. browser diagnostic이 과도하게 실행됩니다

### 조치

```bash
SCRAPER_BROWSER_DIAGNOSTIC_ENABLED=false
```

### 확인

- browser diagnostic metric
- parser drift threshold
- max per hour limiter
- manual endpoint 호출 로그

## 상황 7. live/upcoming 감지 지연

### 확인

- channel health skip count
- active/warm/cold tier
- scheduler job count
- poll duration

### 조치

1. active channel health delay 축소
2. parser drift만 enforce
3. timeout/transport enforce off
4. active tier interval 단축
5. cold tier starvation 확인
