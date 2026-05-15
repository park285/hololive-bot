# Test Matrix and CI Plan

## 1. Local CI 기준

Repository에는 `scripts/ci/local-ci.sh`가 있습니다. 최종 merge 전에는 이 스크립트를 통과해야 합니다.

권장 최종 명령:

```bash
RUN_DEPENDENCY_HYGIENE=false RUN_RACE_TESTS=false ./scripts/ci/local-ci.sh
```

전체 검증 시:

```bash
RUN_DEPENDENCY_HYGIENE=true RUN_RACE_TESTS=true ./scripts/ci/local-ci.sh
```

## 2. Phase별 최소 테스트

### PR-01 Failure taxonomy

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'TestClassifyFailure|Test.*ParserDrift'
```

확인:

- 429 → `rate_limited`
- 403 → `forbidden`
- `CooldownError` → `cooldown`
- parser drift sentinel → `parser_drift`
- timeout → `timeout`

### PR-02 Parser drift wrapping

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'Upcoming|Parser'
```

확인:

- `extractYtInitialData` 실패가 parser drift
- upcoming empty는 success
- parser drift가 retryable transport로 오분류되지 않음

### PR-03 Channel health

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'TestChannelHealth|TestOperationGuard'
go test ./hololive/hololive-shared/pkg/service/youtube/poller -run 'Reschedule|RetryDelay'
```

확인:

- failure count 증가
- success decay
- next allowed at 계산
- `CooldownError.RetryDelay()` scheduler 반영

### PR-04 Upcoming observability

```bash
go test ./hololive/hololive-shared/pkg/service/youtube -run 'Upcoming|Fallback'
```

확인:

- failure slice 기록
- API success ID 기록
- API failed ID 기록
- recovery metric 호출
- quota consume 유지

### PR-05 Snapshot

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'Snapshot|Capture'
```

확인:

- default OFF
- enabled일 때 저장
- max body bytes
- min interval
- capture error no propagation

### PR-06 Config wiring

```bash
go test ./hololive/hololive-shared/pkg/config
go test ./hololive/hololive-stream-ingester/internal/runtime
```

확인:

- env default
- unknown fetcher engine normalization
- snapshot dir default
- health policy default

### PR-07 All operations

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper
go test ./hololive/hololive-shared/pkg/service/youtube/poller
```

확인:

- videos HTML/RSS source separation
- shorts parser drift
- community missing state
- channel stats/snippet parser drift

### PR-08 Browser diagnostic

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'Browser|Diagnostic'
```

확인:

- default no-op
- parser drift count threshold
- browser endpoint missing no panic
- basic request serialization

### PR-09 Active tiering

```bash
go test ./hololive/hololive-stream-ingester/internal/runtime -run 'Tier|Budget|PollerRegistration'
```

확인:

- active/warm/cold classification
- no duplicate channel/poller registration
- RPM budget reduced
- active channel immediate first run behavior 유지

## 3. Contract tests

### Test: 403/429 no retry

목적:

- `ErrRateLimited`, `ErrForbidden`이 retry loop에서 retry되지 않는지 보장합니다.

### Test: Empty upcoming is success

목적:

- upcoming 없는 채널이 fallback 대상이 되지 않게 보장합니다.

### Test: Snapshot default off

목적:

- 운영 배포 직후 artifact storm이 발생하지 않게 보장합니다.

### Test: Browser default path disabled

목적:

- `SCRAPER_FETCHER_ENGINE=browser_snapshot`을 실수로 넣어도 기본 poller가 Chrome으로 가지 않게 보장합니다.

### Test: Metric label cardinality

목적:

- metric label에 channel_id가 들어가지 않게 code review와 unit test로 확인합니다.

## 4. Fixture 승격 절차

1. snapshot artifact 확인
2. HTML 파일을 `scraper/testdata/...`로 이동
3. parser fixture test 생성
4. 실패 재현 확인
5. parser 수정
6. fixture test pass
7. snapshot artifact 정리

## 5. CI 확장 제안

### 추가 스크립트

```bash
scripts/ci/youtube-scraper-ci.sh
```

내용:

```bash
#!/usr/bin/env bash
set -euo pipefail

go test ./hololive/hololive-shared/pkg/service/youtube/scraper -count=1
go test ./hololive/hololive-shared/pkg/service/youtube -count=1
go test ./hololive/hololive-shared/pkg/service/youtube/poller -count=1
go test ./hololive/hololive-stream-ingester/internal/runtime -count=1
```

### 추가 gate

- snapshot default OFF 확인 test
- browser default path OFF 확인 test
- metric label list snapshot test
