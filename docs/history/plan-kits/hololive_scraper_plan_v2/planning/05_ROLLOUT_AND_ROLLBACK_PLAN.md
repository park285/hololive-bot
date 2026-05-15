# Rollout and Rollback Plan

## 1. 배포 원칙

한 번에 모든 기능을 켜지 않습니다. 기능을 넣는 PR과 운영에서 켜는 시점을 분리합니다.

## 2. Stage 0. Baseline 관찰

### 배포 내용

- 코드 변경 없음
- 현재 metric/log baseline 확인

### 확인 항목

- 기존 scraper failure 빈도
- API fallback 빈도
- quota 사용량
- scheduler job count
- RPM budget
- timeout/transport 로그

## 3. Stage 1. Failure taxonomy only

### 켜는 기능

- 실패 reason 분류
- parser drift error wrapping
- metric/log 추가

### 끄는 기능

- channel health
- snapshot
- browser diagnostic
- active tiering

### 성공 기준

- `unknown` reason 비율이 낮음
- 403/429/parser_drift/timeout이 분리되어 보임
- API fallback 빈도가 기존과 크게 다르지 않음

### rollback

- code rollback 또는 metric/log만 제거
- state cleanup 불필요

## 4. Stage 2. Upcoming observability

### 켜는 기능

- upcoming scrape failure detail
- API fallback recovery tracking

### 성공 기준

- recovery metric이 정상 증가
- fallback 대상이 실패 채널로만 제한
- quota 사용량 증가 없음

### rollback

- PR-04 rollback
- state cleanup 불필요

## 5. Stage 3. Channel health dry-run

### 권장 구현

처음에는 channel health를 기록만 하고 skip은 하지 않는 dry-run option이 있으면 가장 안전합니다.

추가 config 제안:

```go
ScraperChannelHealthConfig {
    Enabled bool
    Enforce bool
}
```

- `Enabled=true`, `Enforce=false`: 기록만 함
- `Enabled=true`, `Enforce=true`: skip 적용

### 성공 기준

- parser drift 채널의 next allowed at 계산 정상
- skip 적용 전 예상 delay 확인 가능

### rollback

- `SCRAPER_CHANNEL_HEALTH_ENABLED=false`

## 6. Stage 4. Channel health enforce

### 켜는 기능

- parser drift에만 enforce
- timeout/transport는 아직 dry-run 유지 권장

### 성공 기준

- parser drift 반복 채널의 request 감소
- 전체 failure storm 감소
- live/upcoming latency 악화 없음

### rollback

```bash
SCRAPER_CHANNEL_HEALTH_ENABLED=false
```

또는 parser drift base/max를 낮춤.

## 7. Stage 5. Snapshot capture

### 켜는 기능

```bash
SCRAPER_SNAPSHOT_ENABLED=true
SCRAPER_SNAPSHOT_MAX_BODY_BYTES=524288
SCRAPER_SNAPSHOT_MIN_INTERVAL_SECONDS=1800
```

### 성공 기준

- parser drift 발생 시 snapshot 생성
- snapshot storm 없음
- disk usage 안정

### rollback

```bash
SCRAPER_SNAPSHOT_ENABLED=false
```

### cleanup

```bash
find ./artifacts/youtube-scraper -type f -mtime +7 -delete
```

## 8. Stage 6. Extend all operations

### 켜는 기능

- videos/shorts/community/stats guard
- RSS source health

### 성공 기준

- RSS 실패가 HTML health를 오염시키지 않음
- community missing state 정상
- shorts/recent videos 감지율 유지

### rollback

- PR-07 rollback
- 또는 channel health disabled

## 9. Stage 7. Browser diagnostic manual

### 켜는 기능

- browser endpoint configured
- 수동 diagnostic만 허용

### 성공 기준

- browser diagnostic path 호출 가능
- 기본 poller가 browser를 쓰지 않음
- browser failure가 poller failure로 이어지지 않음

### rollback

```bash
SCRAPER_BROWSER_DIAGNOSTIC_ENABLED=false
```

## 10. Stage 8. Active/warm/cold tiering

### 켜는 기능

- active/warm/cold registration
- cold interval 확대
- `SCRAPER_POLL_TIERING_ENABLED=true`

### 성공 기준

- total RPM 감소
- active channel latency 유지
- cold channel starvation 없음

### rollback

- `SCRAPER_POLL_TIERING_ENABLED=false`
- 기존 registration builder로 복귀

## 11. Rollback 빠른 판단표

| 증상 | 즉시 조치 |
|---|---|
| 403/429 급증 | polling interval 증가, channel health보다 global budget 확인 |
| parser drift 급증 | snapshot enable, browser diagnostic 수동 실행 |
| API quota 급증 | API fallback trigger 확인, events==0 실패 처리 여부 확인 |
| disk usage 급증 | snapshot off, cleanup 실행 |
| live latency 증가 | channel health off 또는 active tier interval 조정 |
| browser CPU 급증 | browser diagnostic off |
| unknown reason 많음 | ClassifyFailure 보강 |
