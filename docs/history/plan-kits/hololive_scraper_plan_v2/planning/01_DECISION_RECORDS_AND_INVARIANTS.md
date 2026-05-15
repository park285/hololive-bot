# Decision Records and Invariants

## ADR-001. Browser는 기본 scraping path가 아니다

### 결정

Chrome/browser rendered HTML fetcher는 `currentPageFetcher()`의 기본 경로로 연결하지 않습니다.

### 이유

- Chrome은 무겁습니다.
- 장애 면적이 큽니다.
- 운영 latency가 예측하기 어렵습니다.
- 차단 우회처럼 보일 수 있습니다.
- HTML parser drift 대응에는 raw HTML fixture가 더 효과적입니다.

### 허용되는 사용

- parser drift가 반복된 채널의 진단
- 운영자가 수동 요청한 channel/page의 snapshot
- 낮은 QPS background diagnostic

### 금지되는 사용

- every poll fallback
- 403/429 직후 browser retry
- CAPTCHA/login wall 우회
- OCR 결과를 운영 데이터로 사용

## ADR-002. 403/429는 전역 hard backoff로 유지한다

### 결정

403/429는 기존 `BackoffState.RecordErrorWithSuggestedCooldown`을 유지합니다.

### 이유

403/429는 채널별 문제가 아니라 요청 경로 또는 YouTube 측 제한일 가능성이 높습니다.

### 결과

- channel/source health는 403/429를 주요 대상으로 삼지 않습니다.
- 403/429 발생 시 fallback/API 사용 증가를 조심해야 합니다.
- 429가 증가하면 polling cadence 또는 request budget부터 봅니다.

## ADR-003. parser drift는 channel/source health로 관리한다

### 결정

`parser_drift`는 channel/source별로 기록합니다.

### 이유

YouTube layout은 채널, 탭, renderer 종류에 따라 달라질 수 있습니다. 한 채널의 `shortsLockupViewModel` drift가 전체 HTML scraping 중단 사유가 되어서는 안 됩니다.

## ADR-004. Snapshot은 default OFF다

### 결정

raw HTML snapshot은 기본 OFF입니다.

### 이유

- HTML body가 큽니다.
- 장애 시 디스크가 빠르게 찰 수 있습니다.
- 개인정보/쿠키/헤더가 저장되지 않게 조심해야 합니다.
- 운영자가 명시적으로 켜야 합니다.

## ADR-005. Metric label에 channel_id를 넣지 않는다

### 결정

Prometheus metric에는 channel_id를 label로 넣지 않습니다.

### 이유

채널 수가 늘면 cardinality가 커지고, Prometheus 성능과 저장 비용에 악영향을 줍니다.

### channel별 정보 저장 위치

- structured log
- state store
- optional admin diagnostic endpoint
- snapshot filename

## ADR-006. Empty upcoming은 실패가 아니다

### 결정

`GetUpcomingEvents`에서 events가 0개인 경우는 성공입니다.

### 이유

채널에 live/upcoming stream이 없는 정상 상태입니다. 이를 실패로 처리하면 API fallback quota를 낭비합니다.

## ADR-007. API fallback은 실패 채널만 대상으로 한다

### 결정

scraper가 실패한 채널만 YouTube Data API fallback 대상으로 유지합니다.

### 이유

- quota 보존
- 기존 구조 유지
- scraper 성공 채널을 API로 중복 조회하지 않음

## ADR-008. Operation guard는 fetcher가 아니라 operation layer에 둔다

### 결정

`fetchChannelSourcePage` 같은 helper는 `GetUpcomingEvents`, `GetShorts`, `GetCommunityPosts` 등 operation에서 호출합니다.

### 이유

fetcher는 URL과 response만 알고, operation/stage/reason을 모릅니다. parser drift snapshot은 operation/stage 정보가 있어야 fixture로 승격할 수 있습니다.

## Invariants

아래 조건은 모든 PR에서 깨지면 안 됩니다.

1. 403/429는 retry loop 안에서 반복 재시도하지 않습니다.
2. `events == 0`은 fallback trigger가 아닙니다.
3. API fallback은 quota check 후 실행됩니다.
4. snapshot 저장 실패는 scraping 실패로 전파하지 않습니다.
5. browser diagnostic 실패는 일반 poller 실패로 전파하지 않습니다.
6. `channel_id`는 metric label에 넣지 않습니다.
7. config default는 운영 안전성을 우선합니다.
8. parser fixture test는 snapshot 기반으로 추가될 수 있어야 합니다.
9. channel health는 stateStore가 없어도 no-op으로 동작해야 합니다.
10. 모든 새 state key는 prefix가 명확해야 합니다.
