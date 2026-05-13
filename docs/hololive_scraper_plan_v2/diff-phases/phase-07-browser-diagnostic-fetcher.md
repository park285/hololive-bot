# Phase 07. Browser diagnostic fetcher

## 목표

Chrome/browser 컨테이너를 붙이되, 운영 수집 기본 경로가 아니라 “진단용 저빈도 fallback”으로 제한합니다.

## 코드 레벨 의사결정

1. `FetcherEngineBrowserSnapshot`을 일반 `currentPageFetcher()` 기본 경로에 넣지 않습니다.
2. browser fetch는 명시적 diagnostic method에서만 호출합니다.
3. 조건:
   - channel/source health에서 parser drift가 일정 횟수 이상 누적됨
   - snapshot feature가 enabled
   - browser endpoint가 configured
   - 낮은 QPS limiter 통과
4. OCR은 기본 구현에 넣지 않습니다.
   - DOM/rendered HTML을 우선 저장합니다.
   - screenshot은 artifact로만 저장합니다.
   - OCR은 사람이 보는 진단 보조 옵션입니다.

## 변경 대상

- `scraper/fetcher.go`
- `scraper/client_http.go`
- `scraper/browser_snapshot_fetcher.go` 신규
- `scraper/browser_diagnostic.go` 신규
- `config/config_types.go` browser config 추가
- `config/config_env_loaders.go` browser env 추가
- runtime wiring

## Diff: fetcher engine 추가

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/fetcher.go b/hololive/hololive-shared/pkg/service/youtube/scraper/fetcher.go
index c49ac97..1117777 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/fetcher.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/fetcher.go
@@
 const (
 	FetcherEngineNetHTTP  FetcherEngine = "nethttp"
 	FetcherEngineGoScrapy FetcherEngine = "goscrapy"
+	FetcherEngineBrowserSnapshot FetcherEngine = "browser_snapshot"
 )
@@
 func normalizeFetcherEngine(engine FetcherEngine) FetcherEngine {
-	if engine == FetcherEngineGoScrapy {
+	switch engine {
+	case FetcherEngineGoScrapy:
 		return FetcherEngineGoScrapy
+	case FetcherEngineBrowserSnapshot:
+		return FetcherEngineBrowserSnapshot
+	default:
+		return FetcherEngineNetHTTP
 	}
-	return FetcherEngineNetHTTP
 }
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go b/hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go
index aaaaaaa..2227777 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/client_http.go
@@
 func (c *Client) currentPageFetcher() pageFetcher {
 	netHTTPFetcher := netHTTPPageFetcher{client: c}
-	if normalizeFetcherEngine(c.fetcherEngine) == FetcherEngineGoScrapy {
+	switch normalizeFetcherEngine(c.fetcherEngine) {
+	case FetcherEngineGoScrapy:
 		return goscrapyPageFetcher{client: c, fallback: netHTTPFetcher}
+	case FetcherEngineBrowserSnapshot:
+		// Browser는 운영 기본 fetcher로 사용하지 않는다.
+		// parser drift 진단 path에서만 명시적으로 호출한다.
+		return netHTTPFetcher
+	default:
+		return netHTTPFetcher
 	}
-	return netHTTPFetcher
 }
```

## Diff: browser snapshot client skeleton

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/browser_snapshot_fetcher.go b/hololive/hololive-shared/pkg/service/youtube/scraper/browser_snapshot_fetcher.go
new file mode 100644
index 0000000..3337777
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/browser_snapshot_fetcher.go
@@
+package scraper
+
+import (
+	"bytes"
+	"context"
+	"encoding/json"
+	"fmt"
+	"net/http"
+	"time"
+
+	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
+)
+
+type BrowserSnapshotConfig struct {
+	Enabled  bool
+	Endpoint string
+	Timeout  time.Duration
+}
+
+type BrowserSnapshotFetcher struct {
+	client   *http.Client
+	endpoint string
+	timeout  time.Duration
+}
+
+type browserSnapshotRequest struct {
+	URL        string      `json:"url"`
+	Headers    http.Header `json:"headers,omitempty"`
+	Screenshot bool        `json:"screenshot"`
+}
+
+type browserSnapshotResponse struct {
+	StatusCode int         `json:"status_code"`
+	HTML       string      `json:"html"`
+	Screenshot []byte      `json:"screenshot,omitempty"`
+	Header     http.Header `json:"header,omitempty"`
+}
+
+func NewBrowserSnapshotFetcher(endpoint string, timeout time.Duration) *BrowserSnapshotFetcher {
+	if timeout <= 0 {
+		timeout = 20 * time.Second
+	}
+	return &BrowserSnapshotFetcher{
+		client:   &http.Client{Timeout: timeout},
+		endpoint: endpoint,
+		timeout:  timeout,
+	}
+}
+
+func (f *BrowserSnapshotFetcher) FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error) {
+	if f == nil || f.endpoint == "" {
+		return pageFetchResponse{}, fmt.Errorf("browser snapshot endpoint is not configured")
+	}
+	payload, err := json.Marshal(browserSnapshotRequest{
+		URL:        req.URL,
+		Headers:    req.Header,
+		Screenshot: true,
+	})
+	if err != nil {
+		return pageFetchResponse{}, fmt.Errorf("marshal browser snapshot request: %w", err)
+	}
+
+	ctx, cancel := context.WithTimeout(ctx, f.timeout)
+	defer cancel()
+
+	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(payload))
+	if err != nil {
+		return pageFetchResponse{}, fmt.Errorf("create browser snapshot request: %w", err)
+	}
+	httpReq.Header.Set("Content-Type", "application/json")
+
+	resp, err := f.client.Do(httpReq)
+	if err != nil {
+		return pageFetchResponse{}, fmt.Errorf("browser snapshot request failed: %w", err)
+	}
+	defer func() { _ = resp.Body.Close() }()
+
+	body, err := jsonutil.ReadAllLimit(resp.Body, 4<<20)
+	if err != nil {
+		return pageFetchResponse{}, fmt.Errorf("read browser snapshot response: %w", err)
+	}
+	if resp.StatusCode != http.StatusOK {
+		return pageFetchResponse{
+			StatusCode: resp.StatusCode,
+			Header:     resp.Header.Clone(),
+		}, fmt.Errorf("browser snapshot unexpected status: %d", resp.StatusCode)
+	}
+
+	var parsed browserSnapshotResponse
+	if err := json.Unmarshal(body, &parsed); err != nil {
+		return pageFetchResponse{}, fmt.Errorf("decode browser snapshot response: %w", err)
+	}
+	return pageFetchResponse{
+		StatusCode: parsed.StatusCode,
+		Header:     parsed.Header,
+		Body:       []byte(parsed.HTML),
+	}, nil
+}
```

## Diff: diagnostic method

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/browser_diagnostic.go b/hololive/hololive-shared/pkg/service/youtube/scraper/browser_diagnostic.go
new file mode 100644
index 0000000..4447777
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/browser_diagnostic.go
@@
+package scraper
+
+import (
+	"context"
+	"fmt"
+	"log/slog"
+	"net/http"
+	"time"
+)
+
+const browserDiagnosticMinParserDriftFailures = 3
+
+func (c *Client) CaptureBrowserDiagnosticSnapshot(ctx context.Context, channelID string, pageURL string) error {
+	if c == nil || c.browserSnapshotFetcher == nil {
+		return nil
+	}
+	if c.channelHealth == nil {
+		return nil
+	}
+	health, ok := c.channelHealth.Get(ctx, channelID, FailureSourceHTML)
+	if !ok {
+		return nil
+	}
+	if health.LastFailureReason != FailureReasonParserDrift {
+		return nil
+	}
+	if health.ConsecutiveFailures < browserDiagnosticMinParserDriftFailures {
+		return nil
+	}
+
+	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
+	if err != nil {
+		return fmt.Errorf("create browser diagnostic request: %w", err)
+	}
+	applyScraperHeaders(req, c.uaProvider.Headers(ctx))
+
+	resp, err := c.browserSnapshotFetcher.FetchPage(ctx, pageFetchRequest{
+		URL:    pageURL,
+		Header: req.Header,
+	})
+	if err != nil {
+		slog.Warn("browser diagnostic snapshot failed",
+			"channel_id", channelID,
+			"url", pageURL,
+			"error", err)
+		return err
+	}
+
+	c.captureSnapshot(ctx, Snapshot{
+		Operation:  "browser_diagnostic",
+		ChannelID:  channelID,
+		URL:        pageURL,
+		Source:     FailureSourceBrowserSnapshot,
+		Reason:     FailureReasonParserDrift,
+		Stage:      "rendered_html",
+		StatusCode: resp.StatusCode,
+		Body:       resp.Body,
+		CapturedAt: time.Now().UTC(),
+	})
+	return nil
+}
```

위 diff를 적용하려면 `Client`에 다음 필드와 option이 필요합니다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go b/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
index aaa4444..5557777 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
@@
 	snapshotSink     SnapshotSink
 	snapshotPolicy   SnapshotPolicy
+	browserSnapshotFetcher *BrowserSnapshotFetcher
@@
 func WithSnapshotPolicy(policy SnapshotPolicy) ClientOption {
@@
 }
+
+func WithBrowserSnapshotFetcher(fetcher *BrowserSnapshotFetcher) ClientOption {
+	return func(c *Client) {
+		c.browserSnapshotFetcher = fetcher
+	}
+}
```

## 운영 제한

browser diagnostic을 호출하는 위치는 두 가지 중 하나로 제한합니다.

1. 운영자가 admin endpoint에서 특정 channelID를 지정해 수동 호출
2. background diagnostic job이 parser drift 3회 이상 채널을 하루 N개 이하로 호출

절대 하지 말아야 할 것:

- every poll마다 browser fallback
- OCR 결과를 운영 데이터 source로 사용
- CAPTCHA/login 페이지 OCR 우회
- 403/429 이후 browser로 즉시 재시도

## 완료 기준

- `SCRAPER_FETCHER_ENGINE=browser_snapshot`을 넣어도 기본 poller가 Chrome으로 전환되지 않습니다.
- browser diagnostic은 명시적 method에서만 동작합니다.
- parser drift 누적 조건 없이는 browser snapshot이 실행되지 않습니다.
