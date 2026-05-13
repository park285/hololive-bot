# LLM Task Breakdown

## 사용 방식

각 task는 독립 PR로 작업합니다. LLM 작업자는 한 번에 하나의 task만 수행합니다.

## Task 01. Failure taxonomy

### 입력 문서

- `diff-phases/phase-01-failure-taxonomy.md`
- `planning/01_DECISION_RECORDS_AND_INVARIANTS.md`

### 성공 조건

- `scraper.ClassifyFailure` 구현
- parser drift sentinel 구현
- failure unit test 구현
- 403/429 Retry-After 보존

### 금지

- channel health 구현하지 않음
- snapshot 구현하지 않음
- browser 구현하지 않음

## Task 02. Channel health

### 입력 문서

- `diff-phases/phase-02-channel-health-operation-guard.md`
- `planning/02_PR_SEQUENCE_AND_GATES.md`

### 성공 조건

- channel/source health 구현
- operation guard 구현
- upcoming에만 적용
- stateStore nil no-op

### 금지

- videos/shorts/community로 확장하지 않음
- browser 구현하지 않음

## Task 03. Upcoming observability

### 입력 문서

- `diff-phases/phase-03-upcoming-observability-and-api-fallback.md`

### 성공 조건

- scrape failure detail 기록
- API fallback success/failed IDs 기록
- recovery metric/log 추가
- quota flow 유지

## Task 04. Snapshot

### 입력 문서

- `diff-phases/phase-04-raw-fixture-snapshot-capture.md`

### 성공 조건

- SnapshotSink
- FileSnapshotSink
- interval limiter
- default OFF
- parser drift path에 capture 연결

## Task 05. Config wiring

### 입력 문서

- `diff-phases/phase-05-config-runtime-wiring.md`
- `planning/06_CONFIG_ENV_CONTRACT.md`

### 성공 조건

- config types
- env loader
- runtime scraper client wiring
- default safety

## Task 06. Extend operations

### 입력 문서

- `diff-phases/phase-06-extend-all-scraper-operations.md`

### 성공 조건

- videos HTML/RSS
- shorts
- community
- channel stats/snippet
- source 분리

## Task 07. Browser diagnostic

### 입력 문서

- `diff-phases/phase-07-browser-diagnostic-fetcher.md`
- `planning/07_BROWSER_OCR_POLICY.md`

### 성공 조건

- browser fetcher skeleton
- diagnostic method
- default path disabled
- OCR 없음

## Task 08. Active tiering

### 입력 문서

- `diff-phases/phase-08-active-channel-tiering-rollout-tests.md`

### 성공 조건

- classifier
- tiered registrations
- RPM budget test
- no starvation guard

## 각 Task의 공통 보고 형식

```text
변경 파일:
- ...

테스트:
- 명령:
- 결과:

확인한 invariant:
- ...

남은 위험:
- ...
```
