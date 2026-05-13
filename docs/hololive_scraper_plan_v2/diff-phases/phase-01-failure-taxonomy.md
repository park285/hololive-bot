# Phase 01. 실패 taxonomy와 parser drift error 추가

## 목표

현재 scraper 실패는 서비스 레이어에서 대부분 `error` 하나로만 전달됩니다. 이 phase에서는 실패를 다음 reason으로 분류할 수 있게 만듭니다.

- `rate_limited`: 429
- `forbidden`: 403
- `cooldown`: hard/transient/channel cooldown
- `timeout`
- `transport`
- `http_status`
- `parser_drift`
- `empty_response`
- `channel_not_found`
- `channel_unavailable`
- `context_canceled`
- `unknown`

## 코드 레벨 의사결정

1. 실패 분류는 `scraper` 패키지에 둡니다.
   - `httpStatusError`, `CooldownError`, `ErrRateLimited`, `ErrForbidden`이 scraper 내부 타입이므로 서비스 레이어에서 문자열 파싱하지 않습니다.

2. 403/429는 기존처럼 재시도하지 않습니다.
   - 단, `Retry-After`를 잃지 않도록 `httpStatusError{cause: ErrRateLimited}` 형태로 반환합니다.

3. parser drift는 별도 sentinel error로 승격합니다.
   - `extractYtInitialData` 실패와 `parse...InitialData` 실패를 운영상 parser drift로 볼 수 있어야 합니다.

## 변경 대상

- `hololive/hololive-shared/pkg/service/youtube/scraper/failure.go` 신규
- `hololive/hololive-shared/pkg/service/youtube/scraper/parser_error.go` 신규
- `hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go` 수정
- `hololive/hololive-shared/pkg/service/youtube/scraper/videos.go` 일부 수정
- `hololive/hololive-shared/pkg/service/youtube/scraper/failure_test.go` 신규 권장

## Diff

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/failure.go b/hololive/hololive-shared/pkg/service/youtube/scraper/failure.go
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/failure.go
@@
+package scraper
+
+import (
+	"context"
+	"errors"
+	"net"
+	"net/http"
+	"net/url"
+	"strings"
+	"time"
+)
+
+type FailureReason string
+
+const (
+	FailureReasonNone               FailureReason = "none"
+	FailureReasonRateLimited        FailureReason = "rate_limited"
+	FailureReasonForbidden          FailureReason = "forbidden"
+	FailureReasonCooldown           FailureReason = "cooldown"
+	FailureReasonTimeout            FailureReason = "timeout"
+	FailureReasonTransport          FailureReason = "transport"
+	FailureReasonHTTPStatus         FailureReason = "http_status"
+	FailureReasonParserDrift        FailureReason = "parser_drift"
+	FailureReasonEmptyResponse      FailureReason = "empty_response"
+	FailureReasonChannelNotFound    FailureReason = "channel_not_found"
+	FailureReasonChannelUnavailable FailureReason = "channel_unavailable"
+	FailureReasonContextCanceled    FailureReason = "context_canceled"
+	FailureReasonUnknown            FailureReason = "unknown"
+)
+
+type FailureSource string
+
+const (
+	FailureSourceHTML            FailureSource = "html"
+	FailureSourceRSS             FailureSource = "rss"
+	FailureSourceAPI             FailureSource = "api"
+	FailureSourceBrowserSnapshot FailureSource = "browser_snapshot"
+)
+
+type FailureDetail struct {
+	Reason     FailureReason
+	Source     FailureSource
+	StatusCode int
+	RetryAfter time.Duration
+	Message    string
+}
+
+func ClassifyFailure(err error, source FailureSource) FailureDetail {
+	if err == nil {
+		return FailureDetail{Reason: FailureReasonNone, Source: source}
+	}
+
+	detail := FailureDetail{
+		Reason:  FailureReasonUnknown,
+		Source:  source,
+		Message: err.Error(),
+	}
+
+	if errors.Is(err, context.Canceled) {
+		detail.Reason = FailureReasonContextCanceled
+		return detail
+	}
+
+	if errors.Is(err, ErrRateLimited) {
+		detail.Reason = FailureReasonRateLimited
+		detail.StatusCode = http.StatusTooManyRequests
+		detail.RetryAfter = extractHTTPRetryAfter(err)
+		return detail
+	}
+
+	if errors.Is(err, ErrForbidden) {
+		detail.Reason = FailureReasonForbidden
+		detail.StatusCode = http.StatusForbidden
+		detail.RetryAfter = extractHTTPRetryAfter(err)
+		return detail
+	}
+
+	var cooldown *CooldownError
+	if errors.As(err, &cooldown) {
+		detail.Reason = FailureReasonCooldown
+		detail.RetryAfter = cooldown.RetryDelay()
+		return detail
+	}
+
+	if errors.Is(err, ErrChannelNotFound) {
+		detail.Reason = FailureReasonChannelNotFound
+		return detail
+	}
+
+	if errors.Is(err, ErrChannelUnavailable) {
+		detail.Reason = FailureReasonChannelUnavailable
+		return detail
+	}
+
+	var statusErr *httpStatusError
+	if errors.As(err, &statusErr) {
+		detail.Reason = FailureReasonHTTPStatus
+		detail.StatusCode = statusErr.code
+		detail.RetryAfter = statusErr.retryAfter
+		return detail
+	}
+
+	if isTimeoutError(err) {
+		detail.Reason = FailureReasonTimeout
+		return detail
+	}
+
+	if isRetryableTransportError(err) {
+		detail.Reason = FailureReasonTransport
+		return detail
+	}
+
+	if IsParserDriftError(err) {
+		detail.Reason = FailureReasonParserDrift
+		return detail
+	}
+
+	if strings.Contains(strings.ToLower(err.Error()), "empty response") {
+		detail.Reason = FailureReasonEmptyResponse
+		return detail
+	}
+
+	return detail
+}
+
+func isTimeoutError(err error) bool {
+	if err == nil {
+		return false
+	}
+	if errors.Is(err, context.DeadlineExceeded) {
+		return true
+	}
+	var netErr net.Error
+	if errors.As(err, &netErr) && netErr.Timeout() {
+		return true
+	}
+	var urlErr *url.Error
+	if errors.As(err, &urlErr) {
+		if errors.Is(urlErr.Err, context.DeadlineExceeded) {
+			return true
+		}
+		var nestedNetErr net.Error
+		return errors.As(urlErr.Err, &nestedNetErr) && nestedNetErr.Timeout()
+	}
+	return false
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/parser_error.go b/hololive/hololive-shared/pkg/service/youtube/scraper/parser_error.go
new file mode 100644
index 0000000..2222222
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/parser_error.go
@@
+package scraper
+
+import (
+	"errors"
+	"fmt"
+)
+
+var ErrParserDrift = errors.New("youtube parser drift")
+
+type ParserDriftError struct {
+	Operation string
+	Stage     string
+	Cause     error
+}
+
+func (e *ParserDriftError) Error() string {
+	if e == nil {
+		return "youtube parser drift"
+	}
+	if e.Cause == nil {
+		return fmt.Sprintf("%s parser drift at %s", e.Operation, e.Stage)
+	}
+	return fmt.Sprintf("%s parser drift at %s: %v", e.Operation, e.Stage, e.Cause)
+}
+
+func (e *ParserDriftError) Unwrap() error {
+	if e == nil {
+		return nil
+	}
+	return errors.Join(ErrParserDrift, e.Cause)
+}
+
+func NewParserDriftError(operation, stage string, cause error) error {
+	return &ParserDriftError{
+		Operation: operation,
+		Stage:     stage,
+		Cause:     cause,
+	}
+}
+
+func IsParserDriftError(err error) bool {
+	return errors.Is(err, ErrParserDrift)
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go b/hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go
index 4d6ca41..aaaaaaa 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go
@@
 	case http.StatusTooManyRequests:
 		c.backoffState.RecordErrorWithSuggestedCooldown(retryAfter)
 		cooldown := c.backoffState.HardCooldownRemaining()
 		slog.Warn("YouTube rate limit hit, entering cooldown",
 			"url", pageURL,
 			"cooldown", cooldown.Round(time.Second),
 			"retry_after", retryAfter.Round(time.Second))
-		return "", fmt.Errorf("status %d: %w", resp.StatusCode, ErrRateLimited)
+		return "", &httpStatusError{
+			code:       resp.StatusCode,
+			retryAfter: retryAfter,
+			cause:      ErrRateLimited,
+		}

 	case http.StatusForbidden:
 		c.backoffState.RecordErrorWithSuggestedCooldown(retryAfter)
 		slog.Warn("YouTube access forbidden",
 			"url", pageURL,
 			"retry_after", retryAfter.Round(time.Second))
-		return "", fmt.Errorf("status %d: %w", resp.StatusCode, ErrForbidden)
+		return "", &httpStatusError{
+			code:       resp.StatusCode,
+			retryAfter: retryAfter,
+			cause:      ErrForbidden,
+		}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go b/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
index c92fcdb..bbbbbbb 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
@@
 	html, err := c.fetchPage(ctx, url)
 	if err != nil {
 		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
 	}
+	if strings.TrimSpace(html) == "" {
+		return nil, fmt.Errorf("empty response from channel page")
+	}

 	jsonStr, err := extractYtInitialData(html)
 	if err != nil {
 		logStructureWarning("upcoming_events", channelID, "ytInitialData extraction failed", "error", err)
-		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
+		return nil, NewParserDriftError("upcoming_events", "extract_yt_initial_data", err)
 	}

 	data := gjson.Parse(jsonStr)
 	events, err := parseUpcomingEventsFromInitialData(data)
 	if err != nil {
 		logStructureWarning("upcoming_events", channelID, "failed to parse initial data", "error", err)
-		return nil, err
+		return nil, NewParserDriftError("upcoming_events", "parse_initial_data", err)
 	}
```

## 테스트 추가

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/failure_test.go b/hololive/hololive-shared/pkg/service/youtube/scraper/failure_test.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/failure_test.go
@@
+package scraper
+
+import (
+	"errors"
+	"fmt"
+	"net/http"
+	"testing"
+	"time"
+
+	"github.com/stretchr/testify/require"
+)
+
+func TestClassifyFailureRateLimitedWithRetryAfter(t *testing.T) {
+	err := &httpStatusError{
+		code:       http.StatusTooManyRequests,
+		retryAfter: 15 * time.Second,
+		cause:      ErrRateLimited,
+	}
+
+	detail := ClassifyFailure(fmt.Errorf("wrapped: %w", err), FailureSourceHTML)
+
+	require.Equal(t, FailureReasonRateLimited, detail.Reason)
+	require.Equal(t, FailureSourceHTML, detail.Source)
+	require.Equal(t, http.StatusTooManyRequests, detail.StatusCode)
+	require.Equal(t, 15*time.Second, detail.RetryAfter)
+}
+
+func TestClassifyFailureForbidden(t *testing.T) {
+	err := &httpStatusError{
+		code:  http.StatusForbidden,
+		cause: ErrForbidden,
+	}
+
+	detail := ClassifyFailure(err, FailureSourceHTML)
+
+	require.Equal(t, FailureReasonForbidden, detail.Reason)
+	require.Equal(t, http.StatusForbidden, detail.StatusCode)
+}
+
+func TestClassifyFailureParserDrift(t *testing.T) {
+	err := NewParserDriftError("upcoming_events", "extract_yt_initial_data", errors.New("marker missing"))
+
+	detail := ClassifyFailure(err, FailureSourceHTML)
+
+	require.Equal(t, FailureReasonParserDrift, detail.Reason)
+}
+
+func TestClassifyFailureCooldown(t *testing.T) {
+	err := &CooldownError{
+		Kind:  "youtube transient",
+		Delay: 3 * time.Minute,
+		Err:   ErrTransientCooldown,
+	}
+
+	detail := ClassifyFailure(err, FailureSourceHTML)
+
+	require.Equal(t, FailureReasonCooldown, detail.Reason)
+	require.Equal(t, 3*time.Minute, detail.RetryAfter)
+}
```

## 실행

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'TestClassifyFailure|Test.*ParserDrift'
```

## 완료 기준

- 403/429가 `ErrForbidden`/`ErrRateLimited`로 `errors.Is`에 걸립니다.
- 403/429의 `Retry-After`가 `FailureDetail.RetryAfter`로 보존됩니다.
- `extractYtInitialData` 실패가 `parser_drift`로 분류됩니다.
- 기존 retry 정책에서 403/429는 계속 retry되지 않습니다.
