# GoScrapy Fetch Adapter Design

## Goal

Introduce `github.com/tech-engine/goscrapy` into the YouTube HTML scraping path with the smallest safe production change. The first phase improves the request execution layer while preserving existing scraper parsing, poller scheduling, persistence, outbox delivery, proxy toggling, rate limiting, and operational budget controls.

## Current State

The repository already has a YouTube scraping stack under `hololive/hololive-shared/pkg/service/youtube/scraper` and a runtime scheduler under `hololive/hololive-stream-ingester`.

Important existing behavior to preserve:

- `scraper.Client` public methods such as `GetCommunityPosts`, `GetShorts`, `GetRecentVideos`, `GetUpcomingEvents`, `ResolveVideoPublishedAt`, and `ResolveCommunityPostPublishedAt`.
- Existing parsing functions based on `ytInitialData`, `gjson`, RSS fallback, and YouTube structure warnings.
- Shared `scraper.RateLimiter`, distributed limiter buckets, `BackoffState`, hard cooldown on `429/403`, and transient retry behavior.
- Proxy runtime toggle through `SetProxyEnabled` and `ProxyEnabled`.
- `poller.Scheduler`, channel registrations, RPM budget checks, DB writes, and outbox dispatch.

GoScrapy v0.25.0 provides a Scrapy-inspired Go framework with request callbacks, middleware, retry, cookie isolation, selectors, pipelines, optional telemetry, and custom HTTP client support. It is in v0.x development and uses Business Source License 1.1 with an additional production-use grant.

## Recommendation

Use GoScrapy as a **fetch adapter** behind the existing scraper client, not as a full replacement for the scraper runtime.

This keeps the blast radius small:

- One new internal fetch abstraction controls whether a page is fetched by the existing `net/http` path or by GoScrapy.
- Existing parser and persistence behavior remains unchanged.
- A feature flag can disable GoScrapy without changing call sites.
- Tests can compare status handling, body limits, headers, context cancellation, proxy toggle semantics, and fallback behavior.

## Non-Goals

- Do not rewrite current scraper methods as GoScrapy spiders in phase 1.
- Do not replace `poller.Scheduler` with GoScrapy scheduling.
- Do not route scraped records through GoScrapy pipelines in phase 1.
- Do not change database schemas, notification payloads, or outbox semantics.
- Do not deploy or restart production services unless explicitly requested later.

## Architecture

### 1. Fetcher boundary

Add an internal fetch boundary inside the scraper package:

```go
type PageFetcher interface {
    FetchPage(ctx context.Context, req PageFetchRequest) (PageFetchResponse, error)
}

type PageFetchRequest struct {
    URL     string
    Headers http.Header
    Policy  FetchPolicy
    Bucket  string
}

type PageFetchResponse struct {
    StatusCode int
    Header     http.Header
    Body       []byte
}
```

`Client.fetchPageOnce` will become a small coordinator:

1. Check hard cooldown.
2. Wait on existing `RateLimiter` with the existing bucket selection.
3. Build headers from the current `ua.Provider`.
4. Call the active `PageFetcher`.
5. Preserve existing `429`, `403`, non-OK, body-limit, and success handling.

### 2. Existing HTTP fetcher

Move the current `http.Client.Do` logic into `netHTTPPageFetcher`. This is the default and the fallback path.

It reuses existing direct/proxy clients and `CloseIdleConnections` behavior.

### 3. GoScrapy fetcher

Add `goscrapyPageFetcher` as an optional implementation. It should:

- Use `github.com/tech-engine/goscrapy/pkg/gos` with a custom `http.Client` so existing direct/proxy transport stays under repo control.
- Schedule exactly one request per `FetchPage` call.
- Return detached response bytes and headers through a local channel.
- Honor caller context cancellation and timeout.
- Avoid GoScrapy pipelines for phase 1.
- Disable or minimize GoScrapy framework logging unless explicitly enabled.

GoScrapy retry middleware should not replace current retry policy in phase 1. Existing retry behavior is already tied to YouTube-specific `429/403`, transient status, and `BackoffState`. If GoScrapy retry is enabled later, it must be guarded by tests proving no retry amplification beyond the existing RPM budget envelope.

### 4. Runtime feature flag

Add a config field and env var:

- `ScraperConfig.FetcherEngine string`
- `SCRAPER_FETCHER_ENGINE`, default `nethttp`
- Allowed values: `nethttp`, `goscrapy`

If `goscrapy` initialization fails, startup should fail fast in tests and local runs. For production safety, runtime construction can optionally log and fall back only when a separate future `SCRAPER_FETCHER_FALLBACK_ENABLED` flag exists. Phase 1 should keep fallback explicit in code tests but not silently mask config errors.

### 5. Fallback strategy

Per-request fallback should be conservative:

- Default `nethttp` has no fallback.
- `goscrapy` can fall back to `nethttp` only for GoScrapy framework execution errors before an HTTP status is obtained.
- Do not fallback on `403`, `429`, or valid non-OK HTTP statuses, because those must preserve current cooldown/backoff semantics.

### 6. Observability

Add lightweight logs and metrics labels at the scraper package boundary:

- fetcher engine selected at startup.
- fetcher fallback occurrence with URL host/path only, not full query or secrets.
- existing structure warning logs remain unchanged.

## Data Flow

Current poller flow stays the same:

```text
poller.Scheduler
  -> CommunityPoller / ShortsPoller / VideosPoller / LivePoller / ChannelStatsPoller
  -> scraper.Client public method
  -> Client.fetchPage
  -> retry.WithRetry
  -> Client.fetchPageOnce
  -> PageFetcher(nethttp or goscrapy)
  -> existing parser
  -> existing repository/outbox flow
```

## Error Handling

The implementation must preserve current public error behavior:

- `429` maps to `ErrRateLimited` and records hard cooldown.
- `403` maps to `ErrForbidden` and records hard cooldown.
- Retryable 5xx and selected transient transport errors are handled by existing `retry.WithRetry`.
- `context.Canceled` and caller deadlines do not create cooldown pollution.
- Response bodies remain limited by `constants.YouTubeConfig.MaxPageBodyBytes`.

## Testing Plan

Use TDD for implementation.

Minimum tests:

1. Config parsing accepts default `nethttp` and explicit `goscrapy`.
2. Invalid `SCRAPER_FETCHER_ENGINE` fails validation.
3. `buildSharedYouTubeScraperClient` wires the selected fetcher engine.
4. `netHTTPPageFetcher` preserves current status/body/header behavior.
5. `goscrapyPageFetcher` returns body, status, headers for a local `httptest.Server`.
6. `goscrapyPageFetcher` honors context cancellation.
7. GoScrapy framework errors can fallback to `nethttp` only before an HTTP status is obtained.
8. `403/429` from GoScrapy path are not converted into fallback successes.
9. Existing scraper package tests still pass.
10. Stream ingester runtime builder tests still pass.

Verification commands:

```bash
go test ./hololive/hololive-shared/pkg/config/...
go test ./hololive/hololive-shared/pkg/service/youtube/scraper/...
go test ./hololive/hololive-stream-ingester/internal/runtime/...
go test ./hololive/hololive-shared/... ./hololive/hololive-stream-ingester/...
go build ./hololive/hololive-shared/... ./hololive/hololive-stream-ingester/...
```

Full repository verification can later use the root commands from `AGENTS.md` if the scoped checks pass.

## Rollout

1. Merge with default `SCRAPER_FETCHER_ENGINE=nethttp`; production behavior unchanged.
2. Run local and staging-like `httptest`/integration checks with `goscrapy` enabled.
3. Enable `SCRAPER_FETCHER_ENGINE=goscrapy` for `youtube-scraper` only after scoped verification.
4. Watch scraper logs for fallback counts, `403/429`, parser warnings, and outbox delivery latency.
5. Keep rollback as changing env back to `nethttp` and restarting only when explicitly requested.

## Risks

- GoScrapy is v0.x, so its API may change.
- GoScrapy uses BSL 1.1, not MIT. The current use appears compatible with the public additional production-use grant, but license acceptance is an owner decision.
- Running a GoScrapy app per fetch may add overhead. If tests show overhead is material, introduce a small persistent fetch runner or defer GoScrapy to a dedicated runtime in a later phase.
- Existing YouTube-specific cooldown and RPM budget logic must remain authoritative to avoid accidental request amplification.
