# Telemetry and Dashboard Plan

## 1. Metric 설계 원칙

### 허용 label

- `operation`
- `source`
- `reason`
- `recovery_source`
- `failed_source`
- `failed_reason`
- `outcome`
- `tier`

### 금지 label

- `channel_id`
- `video_id`
- `post_id`
- raw URL
- error message

## 2. 추가 metric

### youtube_scraper_channel_failures_total

```text
youtube_scraper_channel_failures_total{
  operation="upcoming_streams",
  source="html",
  reason="parser_drift"
}
```

용도:

- parser drift 증가 감지
- 403/429 증가 감지
- timeout/transport 증가 감지

### youtube_scraper_channel_recoveries_total

```text
youtube_scraper_channel_recoveries_total{
  operation="upcoming_streams",
  failed_source="html",
  failed_reason="parser_drift",
  recovery_source="api"
}
```

용도:

- API fallback이 실제 복구하고 있는지 확인
- fallback 의존도 확인

### youtube_scraper_snapshot_capture_total

권장 추가:

```text
youtube_scraper_snapshot_capture_total{
  operation="upcoming_events",
  source="html",
  reason="parser_drift",
  outcome="success|skipped|failed"
}
```

### youtube_scraper_channel_health_skip_total

권장 추가:

```text
youtube_scraper_channel_health_skip_total{
  source="html",
  reason="parser_drift"
}
```

### youtube_scraper_browser_diagnostic_total

권장 추가:

```text
youtube_scraper_browser_diagnostic_total{
  outcome="success|failed|skipped",
  reason="parser_drift"
}
```

## 3. Dashboard Panel

### Panel 1. Scraper failure reason trend

Query 예시:

```promql
sum by (reason) (rate(youtube_scraper_channel_failures_total[5m]))
```

판단:

- `rate_limited`, `forbidden` 증가: 요청량/IP/YouTube 제한
- `parser_drift` 증가: HTML 구조 변경
- `timeout`, `transport` 증가: 네트워크/프록시 문제

### Panel 2. Source failure trend

```promql
sum by (source, reason) (rate(youtube_scraper_channel_failures_total[5m]))
```

### Panel 3. API fallback recovery ratio

```promql
sum(rate(youtube_scraper_channel_recoveries_total{recovery_source="api"}[5m]))
/
sum(rate(youtube_scraper_channel_failures_total{operation="upcoming_streams"}[5m]))
```

### Panel 4. Fallback primary outcome

기존 fallback metric이 있다면 사용:

```promql
sum by (outcome) (rate(hololive_fallback_primary_total{service="youtube",operation="upcoming_streams"}[5m]))
```

### Panel 5. Snapshot captures

```promql
sum by (operation, reason, outcome) (rate(youtube_scraper_snapshot_capture_total[5m]))
```

### Panel 6. Scheduler job count

기존 poller metric 사용:

```promql
youtube_poller_scheduler_job_count
```

## 4. Alert 기준

### Alert 1. Rate limit spike

조건:

```promql
sum(rate(youtube_scraper_channel_failures_total{reason=~"rate_limited|forbidden"}[10m])) > 0.05
```

대응:

1. global hard cooldown 확인
2. RPM budget 확인
3. 최근 배포에서 polling interval 변화 확인
4. API fallback이 연쇄적으로 quota를 쓰는지 확인

### Alert 2. Parser drift spike

조건:

```promql
sum(rate(youtube_scraper_channel_failures_total{reason="parser_drift"}[10m])) > 0.1
```

대응:

1. snapshot enabled 확인
2. fixture 생성 여부 확인
3. 최근 YouTube layout drift 확인
4. parser test 추가
5. channel health backoff가 정상 작동하는지 확인

### Alert 3. API fallback dependency increase

조건:

```promql
sum(rate(youtube_scraper_channel_recoveries_total{recovery_source="api"}[30m])) > 0.2
```

대응:

1. API quota 사용량 확인
2. parser drift 원인 확인
3. snapshot fixture 확인
4. API fallback timeout/blocked 확인

### Alert 4. Snapshot storm

조건:

```promql
sum(rate(youtube_scraper_snapshot_capture_total{outcome="success"}[10m])) > 0.05
```

대응:

1. snapshot feature off 가능
2. min interval 증가
3. max bytes 감소
4. disk usage 확인

## 5. 로그 필드 표준

### Scraper channel failed

```json
{
  "event": "youtube_upcoming_scraper_channel_failed",
  "channel_id": "...",
  "source": "html",
  "reason": "parser_drift",
  "status_code": 0,
  "retry_after_ms": 0,
  "operation": "upcoming_streams"
}
```

### API fallback recovered

```json
{
  "event": "youtube_upcoming_api_fallback_recovered_channel",
  "channel_id": "...",
  "failed_source": "html",
  "failed_reason": "parser_drift",
  "recovery_source": "api"
}
```

### Snapshot captured

```json
{
  "event": "youtube_scraper_snapshot_captured",
  "channel_id": "...",
  "operation": "upcoming_events",
  "stage": "extract_yt_initial_data",
  "reason": "parser_drift",
  "path": "..."
}
```
