# PR Sequence and Gates

## PR-01. Failure taxonomy

### 목적

scraper failure를 reason/source로 분류합니다.

### 변경

- `scraper/failure.go`
- `scraper/parser_error.go`
- `client_http.go` 403/429 error wrapping 수정
- failure tests

### 선행조건

없음.

### Merge gate

- `go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'TestClassifyFailure|Test.*ParserDrift'`
- 기존 retry test 통과
- 403/429가 retry되지 않는지 확인

### 중단 조건

- 403/429가 retry 대상이 되면 중단
- `errors.Is(err, ErrRateLimited)`가 깨지면 중단
- `Retry-After`가 사라지면 중단

## PR-02. Parser drift wrapping

### 목적

`extractYtInitialData` 실패와 parser 실패를 `parser_drift`로 승격합니다.

### 변경

- `videos.go` upcoming부터 적용
- `shorts.go`, `community.go`, `channel.go`는 아직 건드리지 않음

### Merge gate

- upcoming parser tests 통과
- empty upcoming이 실패로 바뀌지 않음

### 중단 조건

- upcoming 없는 채널이 fallback 대상이 되면 중단
- parser error가 unknown으로만 기록되면 중단

## PR-03. Channel/source health

### 목적

parser drift, timeout, transport, http status에 adaptive backoff를 적용합니다.

### 변경

- `channel_health.go`
- `client_operation_guard.go`
- `client_options.go`
- `state_manager.go`
- upcoming operation guard

### Merge gate

- channel health unit test 통과
- stateStore nil일 때 no-op
- `CooldownError.RetryDelay()`가 scheduler에서 작동

### 중단 조건

- 403/429 channel health가 전역 hard backoff를 대체하면 중단
- channel health가 성공 시 영구 skip 상태를 만들면 중단
- stateStore error가 scraping failure로 전파되면 중단

## PR-04. Upcoming observability and fallback recovery

### 목적

scraper failure와 API recovery를 한 observation window로 연결합니다.

### 변경

- `upcomingScrapeFailure`
- `upcomingScrapeResult.failures`
- `upcomingAPIFallbackResult.successfulIDs/failedIDs/failures`
- scraper failure metric
- recovery metric

### Merge gate

- API fallback 기존 behavior 유지
- quota check 유지
- channel_id metric label 없음

### 중단 조건

- API fallback 대상이 전체 채널로 늘어나면 중단
- events==0 채널이 실패 처리되면 중단
- quota consume timing이 깨지면 중단

## PR-05. Snapshot capture

### 목적

parser drift HTML 일부를 fixture 후보로 저장합니다.

### 변경

- `snapshot.go`
- `snapshot_file_sink.go`
- `snapshot_capture.go`
- `recordParserDrift`에서 capture

### Merge gate

- default OFF
- max body bytes 적용
- interval limiter 적용
- capture 실패가 scraping 실패로 전파되지 않음

### 중단 조건

- snapshot default ON이면 중단
- full 8MiB HTML을 무제한 저장하면 중단
- header/cookie 저장하면 중단

## PR-06. Config/runtime wiring

### 목적

env/config로 channel health와 snapshot을 제어합니다.

### 변경

- `config_types.go`
- `config_env_loaders.go`
- `stream_ingester_youtube_components.go`

### Merge gate

- config tests
- runtime tests
- default snapshot OFF
- channel health default ON 또는 운영 결정에 따라 default OFF

### 중단 조건

- env 이름 충돌
- unknown fetcher engine이 그대로 적용됨
- snapshot dir empty로 panic

## PR-07. Extend operations

### 목적

videos/shorts/community/channel stats/snippet까지 guard 확장.

### Merge gate

- scraper 전체 test 통과
- community missing state 유지
- RSS와 HTML source 분리

### 중단 조건

- community tab missing을 parser drift로 기록하면 중단
- RSS parse 실패가 HTML health를 오염시키면 중단
- shorts/recent videos detection latency 증가가 심하면 rollout 중단

## PR-08. Browser diagnostic

### 목적

Chrome rendered HTML snapshot을 수동/저빈도 진단 path로 추가.

### Merge gate

- default OFF
- 기본 fetcher path로 연결되지 않음
- parser drift 누적 조건 필요
- OCR 없음

### 중단 조건

- every poll browser fallback
- 403/429 후 browser retry
- OCR result를 domain stream/community/shorts로 변환

## PR-09. Active channel tiering

### 목적

최근 활동 채널을 우선 poll하고 cold 채널 cadence를 낮춥니다.

### Merge gate

- tier classifier test
- budget summary 감소
- active channel latency 유지 또는 개선
- cold channel starvation 방지

### 중단 조건

- active channel이 누락됨
- cold channel이 영구 polling 제외됨
- total RPM이 증가함
- registration duplication으로 동일 poller/channel 중복 등록

## PR-10. Runbook/dashboard

### 목적

운영자가 reason별 대응을 할 수 있게 합니다.

### Merge gate

- dashboard query 정리
- alert threshold 정리
- rollback env 정리
- snapshot cleanup policy 정리
