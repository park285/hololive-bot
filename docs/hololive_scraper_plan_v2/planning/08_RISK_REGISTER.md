# Risk Register

## R-001. API quota 증가

### 원인

- empty upcoming을 실패로 오분류
- parser drift 증가로 API fallback 급증
- fallback 대상이 전체 채널로 확대

### 영향

- quota exhaustion
- upcoming 알림 누락
- fallback blocked

### 완화

- events==0은 success
- fallback 대상은 failedIDs만
- quota check 유지
- recovery metric 추가

## R-002. Snapshot disk storm

### 원인

- snapshot default ON
- interval limit 없음
- max body bytes 없음
- parser drift storm

### 영향

- disk full
- container crash
- log/artifact 비용 증가

### 완화

- default OFF
- 512KiB default
- 30분 interval
- reason allowlist
- cleanup runbook

## R-003. Browser가 기본 path로 들어감

### 원인

- `SCRAPER_FETCHER_ENGINE=browser_snapshot`을 `currentPageFetcher()`에 직접 연결
- fallback path에서 browser 자동 실행

### 영향

- latency 증가
- CPU/memory 증가
- 차단 우회 리스크
- 장애 면적 증가

### 완화

- `currentPageFetcher()`에서 browser를 no-op/default nethttp로 처리
- explicit diagnostic method만 허용
- max per hour limiter

## R-004. Prometheus cardinality 폭발

### 원인

- channel_id/video_id/post_id를 metric label로 사용

### 영향

- Prometheus memory/storage 증가
- dashboard 지연
- alert 불안정

### 완화

- channel_id는 log/state에만 사용
- metric label allowlist 유지

## R-005. Channel health가 live latency를 악화

### 원인

- timeout/transport를 너무 긴 backoff로 처리
- success decay가 너무 느림
- active channel도 cold처럼 지연

### 영향

- live/upcoming 감지 지연
- 알림 누락

### 완화

- parser drift부터 enforce
- timeout/transport는 dry-run 후 enable
- active channel tier 우선
- max delay 제한

## R-006. Community missing과 parser drift 혼동

### 원인

- posts tab 없음을 parser drift로 기록
- 404를 parser drift로 기록

### 영향

- 불필요한 backoff
- community polling 품질 저하

### 완화

- 기존 communityMissing state 유지
- 404는 channel/source health와 별도 처리
- tab missing은 parser drift가 아님

## R-007. State store 장애가 scraping 장애로 전파

### 원인

- stateStore Get/Set error를 fatal로 처리

### 영향

- cache 장애가 scraper 장애로 확대

### 완화

- stateStore error는 warn 후 no-op
- channel health/snapshot interval은 best-effort

## R-008. Unknown reason이 많음

### 원인

- ClassifyFailure coverage 부족
- wrapping 순서 문제
- errors.Is/As 실패

### 영향

- 운영 판단 어려움

### 완화

- unknown ratio alert
- failure taxonomy test 확대
- representative error fixture 추가

## R-009. Active tiering으로 cold starvation

### 원인

- cold interval 과도하게 증가
- cold channel이 active로 승격되지 않음

### 영향

- 장기 비활성 채널의 갑작스러운 live 감지 지연

### 완화

- cold도 최소 polling 유지
- external signal/RSS로 승격
- active/warm/cold classifier test

## R-010. Parser fixture 승격 누락

### 원인

- snapshot만 쌓이고 test로 만들지 않음

### 영향

- 같은 parser drift 반복
- browser diagnostic 의존 증가

### 완화

- snapshot → fixture → parser test runbook
- parser drift alert에 fixture task 연결
