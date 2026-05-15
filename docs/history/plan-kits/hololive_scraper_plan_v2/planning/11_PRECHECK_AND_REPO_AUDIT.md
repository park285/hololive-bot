# Precheck and Repo Audit Plan

## 1. 작업 전 확인

LLM 작업자가 실제 코드를 수정하기 전 다음을 확인합니다.

```bash
git status --short
git rev-parse --abbrev-ref HEAD
git log -1 --oneline
```

## 2. 관련 파일 확인

```bash
sed -n '1,220p' hololive/hololive-shared/pkg/service/youtube/scraper/client.go
sed -n '1,220p' hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go
sed -n '1,220p' hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
sed -n '1,220p' hololive/hololive-shared/pkg/service/youtube/service_upcoming.go
sed -n '1,220p' hololive/hololive-shared/pkg/service/youtube/service_upcoming_scrape.go
sed -n '1,260p' hololive/hololive-shared/pkg/service/youtube/service_upcoming_fallback.go
```

## 3. 기존 테스트 확인

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -count=1
go test ./hololive/hololive-shared/pkg/service/youtube -count=1
go test ./hololive/hololive-stream-ingester/internal/runtime -count=1
```

## 4. Diff 적용 전 질문

- 현재 branch가 clean한가?
- upstream main과 차이가 큰가?
- 같은 파일을 수정하는 다른 PR이 있는가?
- generated file 또는 go work sync가 필요한가?
- config env loader에 helper 함수가 있는가?
- cache state mock이 있는가?

## 5. Diff 적용 후 확인

```bash
gofmt -w <changed-go-files>
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -count=1
go test ./hololive/hololive-shared/pkg/service/youtube -count=1
go test ./hololive/hololive-shared/pkg/service/youtube/poller -count=1
go test ./hololive/hololive-stream-ingester/internal/runtime -count=1
```

## 6. 최종 PR 전 확인

```bash
RUN_DEPENDENCY_HYGIENE=false RUN_RACE_TESTS=false ./scripts/ci/local-ci.sh
```

## 7. 예상 compile 이슈와 해결

### 이슈: `stateStore.Get` not-found error 형태가 다름

해결:

- channel health는 Get error를 false로 취급합니다.
- test mock은 실제 cache client behavior를 최대한 맞춥니다.

### 이슈: prometheus metric duplicate registration

해결:

- `sync.Once` 사용
- package init에서 중복 생성하지 않음

### 이슈: `errors.Join` Go version

해결:

- repo Go version이 충분히 최신이면 사용
- 아니면 custom `Unwrap() []error` 구현

### 이슈: env util int type

해결:

- `sharedenv.Int`가 int default만 받는지 확인
- bytes 값은 int 범위 내 기본값 사용

### 이슈: browser endpoint response body base64

해결:

- 초기 구현은 rendered HTML만 사용
- screenshot은 ref/path로 분리
