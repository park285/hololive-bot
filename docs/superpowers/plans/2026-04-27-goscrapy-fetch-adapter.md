# GoScrapy Fetch Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional GoScrapy-backed fetch engine for the existing YouTube HTML scraper while keeping `net/http` as the default production behavior.

**Architecture:** Keep the current `scraper.Client` public methods, parsers, retry/backoff, rate limiting, proxy toggle, poller scheduler, DB writes, and outbox flow. Introduce an internal page-fetcher boundary in `hololive-shared/pkg/service/youtube/scraper`, then wire a `SCRAPER_FETCHER_ENGINE` config value through the stream-ingester client builder.

**Tech Stack:** Go 1.26.2, `github.com/tech-engine/goscrapy@v0.25.0`, existing `testing` + `testify`, existing `httptest`, existing Go workspace.

---

## File Structure

- `hololive/hololive-shared/pkg/config/config_types.go`: add fetcher-engine constants, `ScraperConfig.FetcherEngine`, and default/normalization helpers.
- `hololive/hololive-shared/pkg/config/config.go`: load and validate `SCRAPER_FETCHER_ENGINE`.
- `hololive/hololive-shared/pkg/config/config_test.go`: prove default, override, and invalid config behavior.
- `hololive/hololive-shared/pkg/service/youtube/scraper/fetcher.go`: new internal `pageFetcher` boundary plus `netHTTPPageFetcher`.
- `hololive/hololive-shared/pkg/service/youtube/scraper/goscrapy_fetcher.go`: GoScrapy-backed implementation with conservative fallback.
- `hololive/hololive-shared/pkg/service/youtube/scraper/goscrapy_fetcher_test.go`: local HTTP tests for GoScrapy fetch behavior.
- `hololive/hololive-shared/pkg/service/youtube/scraper/client.go`: add fetcher engine option and delegate `fetchPageOnce` to the active fetcher.
- `hololive/hololive-shared/go.mod`, `hololive/hololive-shared/go.sum`, `go.work.sum`: add GoScrapy dependency and sums.
- `hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go`: pass configured fetcher engine into the shared scraper client.
- `hololive/hololive-stream-ingester/internal/runtime/stream_ingester_builders_test.go`: prove builder wiring.

---

### Task 1: Config accepts and validates `SCRAPER_FETCHER_ENGINE`

**Files:**
- Modify: `hololive/hololive-shared/pkg/config/config_types.go`
- Modify: `hololive/hololive-shared/pkg/config/config.go`
- Test: `hololive/hololive-shared/pkg/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append these tests near the other scraper config tests in `hololive/hololive-shared/pkg/config/config_test.go`:

```go
func TestLoad_ScraperFetcherEngineDefault(t *testing.T) {
	setRequiredLoadEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.FetcherEngine != ScraperFetcherEngineNetHTTP {
		t.Fatalf("Scraper.FetcherEngine = %q, want %q", cfg.Scraper.FetcherEngine, ScraperFetcherEngineNetHTTP)
	}
}

func TestLoad_ScraperFetcherEngineEnvOverride(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_FETCHER_ENGINE", "goscrapy")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scraper.FetcherEngine != ScraperFetcherEngineGoScrapy {
		t.Fatalf("Scraper.FetcherEngine = %q, want %q", cfg.Scraper.FetcherEngine, ScraperFetcherEngineGoScrapy)
	}
}

func TestLoad_ScraperFetcherEngineValidation(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SCRAPER_FETCHER_ENGINE", "bad-engine")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "SCRAPER_FETCHER_ENGINE must be one of: nethttp, goscrapy") {
		t.Fatalf("Load() error = %v", err)
	}
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./hololive/hololive-shared/pkg/config/... -run 'TestLoad_ScraperFetcherEngine' -count=1
```

Expected: FAIL because `ScraperConfig.FetcherEngine` and the constants do not exist yet.

- [ ] **Step 3: Implement config types**

In `hololive/hololive-shared/pkg/config/config_types.go`, add these constants and helper near `DefaultScraperWorkerCount` and extend `ScraperConfig`:

```go
const (
	ScraperFetcherEngineNetHTTP  = "nethttp"
	ScraperFetcherEngineGoScrapy = "goscrapy"
)

func DefaultScraperFetcherEngine() string {
	return ScraperFetcherEngineNetHTTP
}

func NormalizeScraperFetcherEngine(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return DefaultScraperFetcherEngine()
	}
	return value
}
```

Add `strings` to the `config_types.go` imports. If the file currently has only a `time` import, change it to:

```go
import (
	"strings"
	"time"
)
```

Update `ScraperConfig` to include:

```go
FetcherEngine string
```

- [ ] **Step 4: Load and validate the env value**

In `hololive/hololive-shared/pkg/config/config.go`, set `FetcherEngine` inside the `ScraperConfig` literal:

```go
FetcherEngine: NormalizeScraperFetcherEngine(sharedenv.String("SCRAPER_FETCHER_ENGINE", DefaultScraperFetcherEngine())),
```

Add validation in `Validate()` after `validateScraperSchedulerConfig`:

```go
if err := validateScraperFetcherEngine(c.Scraper.FetcherEngine); err != nil {
	return err
}
```

Add this helper near the other scraper validation helpers:

```go
func validateScraperFetcherEngine(engine string) error {
	switch NormalizeScraperFetcherEngine(engine) {
	case ScraperFetcherEngineNetHTTP, ScraperFetcherEngineGoScrapy:
		return nil
	default:
		return fmt.Errorf("SCRAPER_FETCHER_ENGINE must be one of: nethttp, goscrapy")
	}
}
```

- [ ] **Step 5: Run the focused test and verify GREEN**

Run:

```bash
go test ./hololive/hololive-shared/pkg/config/... -run 'TestLoad_ScraperFetcherEngine' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add hololive/hololive-shared/pkg/config/config_types.go hololive/hololive-shared/pkg/config/config.go hololive/hololive-shared/pkg/config/config_test.go
git commit -m "feat(config): add scraper fetcher engine setting"
```

---

### Task 2: Add internal page fetcher boundary and keep `nethttp` behavior default

**Files:**
- Create: `hololive/hololive-shared/pkg/service/youtube/scraper/fetcher.go`
- Modify: `hololive/hololive-shared/pkg/service/youtube/scraper/client.go`
- Test: `hololive/hololive-shared/pkg/service/youtube/scraper/retry_test.go`

- [ ] **Step 1: Write the failing fetcher boundary test**

Append this test to `hololive/hololive-shared/pkg/service/youtube/scraper/retry_test.go` near the existing `fetchPageOnce` tests:

```go
func TestNetHTTPPageFetcher_StatusBodyAndHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("X-Test-Header", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	body, err := client.fetchPageOnce(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Contains(t, body, "ytInitialData")
	assert.Equal(t, "test-agent", receivedHeaders.Get("User-Agent"))
	assert.Equal(t, "SOCS=CAI", receivedHeaders.Get("Cookie"))
}
```

- [ ] **Step 2: Run the focused test and verify RED if possible**

Run:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper/... -run TestNetHTTPPageFetcher_StatusBodyAndHeaders -count=1
```

Expected: this may pass before the refactor because current `fetchPageOnce` already supports the behavior. If it passes immediately, keep it as a characterization test and proceed; the next implementation must keep it passing.

- [ ] **Step 3: Create `fetcher.go`**

Create `hololive/hololive-shared/pkg/service/youtube/scraper/fetcher.go`:

```go
package scraper

import (
	"context"
	"fmt"
	"net/http"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type FetcherEngine string

const (
	FetcherEngineNetHTTP  FetcherEngine = "nethttp"
	FetcherEngineGoScrapy FetcherEngine = "goscrapy"
)

type pageFetcher interface {
	FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error)
}

type pageFetchRequest struct {
	URL    string
	Header http.Header
}

type pageFetchResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type netHTTPPageFetcher struct {
	client *Client
}

func normalizeFetcherEngine(engine FetcherEngine) FetcherEngine {
	switch engine {
	case FetcherEngineGoScrapy:
		return FetcherEngineGoScrapy
	default:
		return FetcherEngineNetHTTP
	}
}

func (f netHTTPPageFetcher) FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, http.NoBody)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header = req.Header.Clone()

	resp, err := f.client.currentHTTPClient().Do(httpReq)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("failed to fetch page: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	out := pageFetchResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
	}
	if resp.StatusCode != http.StatusOK {
		drainResponseBody(resp)
		return out, nil
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.YouTubeConfig.MaxPageBodyBytes)
	if err != nil {
		return out, fmt.Errorf("failed to read response body: %w", err)
	}
	out.Body = body
	return out, nil
}
```

- [ ] **Step 4: Add client option and delegate `fetchPageOnce`**

In `hololive/hololive-shared/pkg/service/youtube/scraper/client.go`, add this field to `Client`:

```go
fetcherEngine FetcherEngine
```

Add this option near the other `ClientOption` helpers:

```go
func WithFetcherEngine(engine FetcherEngine) ClientOption {
	return func(c *Client) {
		c.fetcherEngine = normalizeFetcherEngine(engine)
	}
}
```

Set the default in `NewClient`:

```go
fetcherEngine: FetcherEngineNetHTTP,
```

Add this method near `fetchPageOnce`:

```go
func (c *Client) currentPageFetcher() pageFetcher {
	return netHTTPPageFetcher{client: c}
}
```

Update `fetchPageOnce` so the request creation, rate limiting, headers, status handling, cooldown handling, and `RecordSuccess` stay in `client.go`, but the HTTP execution moves through the fetcher:

```go
req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
if err != nil {
	return "", fmt.Errorf("failed to create request: %w", err)
}

snap := c.uaProvider.Headers(ctx)
applyScraperHeaders(req, snap)

resp, err := c.currentPageFetcher().FetchPage(ctx, pageFetchRequest{
	URL:    pageURL,
	Header: req.Header,
})
if err != nil {
	return "", err
}

retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
```

Use `resp.StatusCode`, `resp.Header`, and `resp.Body` for the rest of the existing switch. Remove the old direct `httpClient.Do` call and the old body read from `client.go`.

- [ ] **Step 5: Run focused scraper tests**

Run:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper/... -run 'TestNetHTTPPageFetcher_StatusBodyAndHeaders|TestFetchPage_NoRetryOn429|TestFetchPage_NoRetryOn403|TestFetchPageOnce_ClientHintsHeaders' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add hololive/hololive-shared/pkg/service/youtube/scraper/fetcher.go hololive/hololive-shared/pkg/service/youtube/scraper/client.go hololive/hololive-shared/pkg/service/youtube/scraper/retry_test.go
git commit -m "refactor(scraper): introduce page fetcher boundary"
```

---

### Task 3: Implement GoScrapy page fetcher with conservative fallback

**Files:**
- Create: `hololive/hololive-shared/pkg/service/youtube/scraper/goscrapy_fetcher.go`
- Create: `hololive/hololive-shared/pkg/service/youtube/scraper/goscrapy_fetcher_test.go`
- Modify: `hololive/hololive-shared/pkg/service/youtube/scraper/client.go`
- Modify: `hololive/hololive-shared/go.mod`
- Modify: `hololive/hololive-shared/go.sum`
- Modify: `go.work.sum` if Go tooling updates it

- [ ] **Step 1: Add the dependency**

Run:

```bash
cd hololive/hololive-shared
go get github.com/tech-engine/goscrapy@v0.25.0
cd ../..
```

Expected: `hololive/hololive-shared/go.mod` and `hololive/hololive-shared/go.sum` include GoScrapy and its transitive sums.

- [ ] **Step 2: Write failing GoScrapy tests**

Create `hololive/hololive-shared/pkg/service/youtube/scraper/goscrapy_fetcher_test.go`:

```go
package scraper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingPageFetcher struct {
	err error
}

func (f failingPageFetcher) FetchPage(context.Context, pageFetchRequest) (pageFetchResponse, error) {
	return pageFetchResponse{}, f.err
}

func TestGoScrapyPageFetcher_ReturnsStatusHeadersAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-agent", r.Header.Get("User-Agent"))
		w.Header().Set("X-Goscrapy-Test", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineGoScrapy),
	)

	resp, err := goscrapyPageFetcher{client: client}.FetchPage(context.Background(), pageFetchRequest{
		URL: server.URL,
		Header: http.Header{
			"User-Agent": []string{"test-agent"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", resp.Header.Get("X-Goscrapy-Test"))
	assert.Contains(t, string(resp.Body), "ytInitialData")
}

func TestGoScrapyFetchPageOnce_DoesNotFallbackOn429(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineGoScrapy),
	)

	_, err := client.fetchPage(context.Background(), server.URL)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimited)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestGoScrapyPageFetcher_FallsBackOnlyBeforeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fallback body"))
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
	)
	fetcher := goscrapyPageFetcher{
		client:   client,
		runner:   failingGoscrapyRunner{err: errors.New("framework stopped")},
		fallback: netHTTPPageFetcher{client: client},
	}

	resp, err := fetcher.FetchPage(context.Background(), pageFetchRequest{URL: server.URL, Header: http.Header{}})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "fallback body", string(resp.Body))
}

func TestGoScrapyPageFetcher_HonorsContextCancellation(t *testing.T) {
	client := NewClient(WithRateLimiter(NewRateLimiter(0)), WithFetcherEngine(FetcherEngineGoScrapy))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := goscrapyPageFetcher{client: client}.FetchPage(ctx, pageFetchRequest{
		URL:    "https://example.invalid/",
		Header: http.Header{},
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "canceled") || errors.Is(err, context.Canceled))
}

type failingGoscrapyRunner struct {
	err error
}

func (r failingGoscrapyRunner) Run(context.Context, *Client, pageFetchRequest) (pageFetchResponse, bool, error) {
	return pageFetchResponse{}, false, r.err
}

func TestGoScrapyPageFetcher_TimeoutBeforeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithFetcherEngine(FetcherEngineGoScrapy),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := goscrapyPageFetcher{client: client}.FetchPage(ctx, pageFetchRequest{URL: server.URL, Header: http.Header{}})
	require.Error(t, err)
}
```

- [ ] **Step 3: Run GoScrapy tests and verify RED**

Run:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper/... -run 'TestGoScrapy' -count=1
```

Expected: FAIL because `goscrapyPageFetcher`, `failingGoscrapyRunner`, and GoScrapy wiring do not exist yet.

- [ ] **Step 4: Implement `goscrapy_fetcher.go`**

Create `hololive/hololive-shared/pkg/service/youtube/scraper/goscrapy_fetcher.go`:

```go
package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
	"github.com/tech-engine/goscrapy/pkg/core"
	"github.com/tech-engine/goscrapy/pkg/gos"
	goslogger "github.com/tech-engine/goscrapy/pkg/logger"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type goscrapyRunner interface {
	Run(ctx context.Context, client *Client, req pageFetchRequest) (pageFetchResponse, bool, error)
}

type goscrapyPageFetcher struct {
	client   *Client
	runner   goscrapyRunner
	fallback pageFetcher
}

type defaultGoscrapyRunner struct{}

type goscrapyFetchResult struct {
	response    pageFetchResponse
	gotResponse bool
	err         error
}

func (f goscrapyPageFetcher) FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error) {
	runner := f.runner
	if runner == nil {
		runner = defaultGoscrapyRunner{}
	}

	resp, gotResponse, err := runner.Run(ctx, f.client, req)
	if err != nil && !gotResponse && f.fallback != nil {
		slog.Warn("goscrapy fetch failed before response, falling back to nethttp",
			"url", safeFetchURL(req.URL),
			"error", err.Error())
		return f.fallback.FetchPage(ctx, req)
	}
	return resp, err
}

func (defaultGoscrapyRunner) Run(ctx context.Context, client *Client, req pageFetchRequest) (pageFetchResponse, bool, error) {
	if err := ctx.Err(); err != nil {
		return pageFetchResponse{}, false, fmt.Errorf("goscrapy fetch canceled: %w", err)
	}

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	app := gos.New[struct{}](gos.WithClient(client.currentHTTPClient())).WithLogger(goslogger.NewNoopLogger())
	resultCh := make(chan goscrapyFetchResult, 1)
	errCh := make(chan error, 1)

	appReq := app.Request(appCtx)
	appReq.Url(req.URL).Method(http.MethodGet).Header(req.Header.Clone())
	app.Parse(appReq, func(_ context.Context, resp core.IResponseReader) {
		out, err := readGoScrapyResponse(resp)
		select {
		case resultCh <- goscrapyFetchResult{response: out, gotResponse: true, err: err}:
		default:
		}
		cancel()
	})

	go func() {
		errCh <- app.Start(appCtx)
	}()

	select {
	case result := <-resultCh:
		cancel()
		waitGoScrapyEngine(errCh)
		return result.response, result.gotResponse, result.err
	case err := <-errCh:
		if err == nil {
			err = errors.New("goscrapy stopped before response")
		}
		return pageFetchResponse{}, false, fmt.Errorf("goscrapy fetch page: %w", err)
	case <-ctx.Done():
		cancel()
		waitGoScrapyEngine(errCh)
		return pageFetchResponse{}, false, fmt.Errorf("goscrapy fetch canceled: %w", ctx.Err())
	}
}

func readGoScrapyResponse(resp core.IResponseReader) (pageFetchResponse, error) {
	out := pageFetchResponse{
		StatusCode: resp.StatusCode(),
		Header:     resp.Header().Clone(),
	}
	body := resp.Body()
	if body == nil {
		return out, nil
	}

	if out.StatusCode != http.StatusOK {
		_, _ = jsonutil.ReadAllLimit(body, 4*1024)
		return out, nil
	}

	data, err := jsonutil.ReadAllLimit(body, constants.YouTubeConfig.MaxPageBodyBytes)
	if err != nil {
		return out, fmt.Errorf("failed to read response body: %w", err)
	}
	out.Body = data
	return out, nil
}

func waitGoScrapyEngine(errCh <-chan error) {
	select {
	case <-errCh:
	case <-time.After(100 * time.Millisecond):
	}
}

func safeFetchURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "invalid-url"
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}
```

- [ ] **Step 5: Route `FetcherEngineGoScrapy` from the client**

Update `currentPageFetcher()` in `client.go`:

```go
func (c *Client) currentPageFetcher() pageFetcher {
	netHTTPFetcher := netHTTPPageFetcher{client: c}
	if normalizeFetcherEngine(c.fetcherEngine) == FetcherEngineGoScrapy {
		return goscrapyPageFetcher{client: c, fallback: netHTTPFetcher}
	}
	return netHTTPFetcher
}
```

- [ ] **Step 6: Run GoScrapy tests and verify GREEN**

Run:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper/... -run 'TestGoScrapy' -count=1
```

Expected: PASS.

- [ ] **Step 7: Run focused regression tests**

Run:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper/... -run 'TestFetchPage_NoRetryOn429|TestFetchPage_NoRetryOn403|TestFetchPageOnce_ClientHintsHeaders|TestNetHTTPPageFetcher_StatusBodyAndHeaders' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add hololive/hololive-shared/pkg/service/youtube/scraper/goscrapy_fetcher.go hololive/hololive-shared/pkg/service/youtube/scraper/goscrapy_fetcher_test.go hololive/hololive-shared/pkg/service/youtube/scraper/client.go hololive/hololive-shared/go.mod hololive/hololive-shared/go.sum go.work.sum
git commit -m "feat(scraper): add goscrapy page fetcher"
```

---

### Task 4: Wire config into stream-ingester shared scraper client

**Files:**
- Modify: `hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go`
- Test: `hololive/hololive-stream-ingester/internal/runtime/stream_ingester_builders_test.go`

- [ ] **Step 1: Write the failing runtime wiring test**

Add this helper near `extractScraperRateLimiter` in `hololive/hololive-stream-ingester/internal/runtime/stream_ingester_builders_test.go`:

```go
func extractScraperFetcherEngine(t *testing.T, client *scraper.Client) scraper.FetcherEngine {
	t.Helper()

	value := reflect.ValueOf(client).Elem()
	field := value.FieldByName("fetcherEngine")
	require.True(t, field.IsValid(), "fetcherEngine field must exist")
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	engine, ok := field.Interface().(scraper.FetcherEngine)
	require.True(t, ok, "fetcherEngine field must be scraper.FetcherEngine")
	return engine
}
```

Add this test near `TestPendingPublishedAtResolver_UsesSharedScraperClientProxyState`:

```go
func TestBuildSharedYouTubeScraperClient_UsesConfiguredFetcherEngine(t *testing.T) {
	t.Parallel()

	client := buildSharedYouTubeScraperClient(config.ScraperConfig{
		FetcherEngine: config.ScraperFetcherEngineGoScrapy,
	}, nil, scraper.NewRateLimiter(time.Second))

	require.NotNil(t, client)
	assert.Equal(t, scraper.FetcherEngineGoScrapy, extractScraperFetcherEngine(t, client))
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./hololive/hololive-stream-ingester/internal/runtime/... -run TestBuildSharedYouTubeScraperClient_UsesConfiguredFetcherEngine -count=1
```

Expected: FAIL because the builder does not pass `FetcherEngine` into `scraper.NewClient` yet.

- [ ] **Step 3: Wire the option into the builder**

In `hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go`, update `buildSharedYouTubeScraperClient`:

```go
return scraper.NewClient(
	scraper.WithProxy(proxyConfig),
	scraper.WithRateLimiter(sharedRL),
	scraper.WithStateStore(cacheService),
	scraper.WithFetcherEngine(scraper.FetcherEngine(scraperCfg.FetcherEngine)),
)
```

- [ ] **Step 4: Run runtime focused tests**

Run:

```bash
go test ./hololive/hololive-stream-ingester/internal/runtime/... -run 'TestBuildSharedYouTubeScraperClient_UsesConfiguredFetcherEngine|TestPendingPublishedAtResolver_UsesSharedScraperClientProxyState' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go hololive/hololive-stream-ingester/internal/runtime/stream_ingester_builders_test.go
git commit -m "feat(stream-ingester): wire scraper fetcher engine"
```

---

### Task 5: Final scoped verification and cleanup

**Files:**
- Inspect: all changed files
- Modify only if verification exposes a concrete issue

- [ ] **Step 1: Run scoped config tests**

```bash
go test ./hololive/hololive-shared/pkg/config/...
```

Expected: PASS.

- [ ] **Step 2: Run scoped scraper tests**

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper/...
```

Expected: PASS.

- [ ] **Step 3: Run scoped runtime tests**

```bash
go test ./hololive/hololive-stream-ingester/internal/runtime/...
```

Expected: PASS.

- [ ] **Step 4: Run broader affected checks**

```bash
go test ./hololive/hololive-shared/... ./hololive/hololive-stream-ingester/...
go build ./hololive/hololive-shared/... ./hololive/hololive-stream-ingester/...
```

Expected: PASS.

- [ ] **Step 5: Inspect git status and dependency diffs**

```bash
git status --short
git diff --stat HEAD~4..HEAD
```

Expected: only intended implementation commits and no uncommitted files.

- [ ] **Step 6: Commit fixes only if needed**

If verification required fixes:

```bash
git add <fixed-files>
git commit -m "fix(scraper): stabilize goscrapy fetch adapter"
```

If no fixes are needed, do not create an empty commit.

