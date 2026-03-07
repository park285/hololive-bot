// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package holodex

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	ytscraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func newTestScraper(t *testing.T, html string, memberMap map[string]string) *ScraperService {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lives/hololive" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(server.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &ScraperService{
		httpClient:    server.Client(),
		logger:        logger,
		baseURL:       server.URL,
		memberNameMap: memberMap,
	}
}

func TestScraperFetchAllStreams(t *testing.T) {
	html := `
<div class="container">
  <div class="col-12">
    <div class="navbar-inverse">
      <span class="holodule navbar-text">09/10 (Wed)</span>
    </div>
  </div>
  <div class="col-12">
    <a class="thumbnail" href="https://www.youtube.com/watch?v=video123">
      <div class="datetime">12:34</div>
      <div class="name">Member One</div>
    </a>
  </div>
</div>`

	memberMap := map[string]string{
		stringutil.Normalize("Member One"): "channel-1",
	}
	svc := newTestScraper(t, html, memberMap)

	streams, err := svc.FetchAllStreams(context.Background())
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}

	stream := streams[0]
	if stream.ID != "video123" {
		t.Fatalf("unexpected stream id: %s", stream.ID)
	}
	if stream.ChannelID != "channel-1" {
		t.Fatalf("unexpected channel id: %s", stream.ChannelID)
	}
	if stream.StartScheduled == nil {
		t.Fatalf("expected start time")
	}
}

func TestScraperFetchAllStreamsStructureError(t *testing.T) {
	html := `<div class="container"><div class="col-12"></div></div>`
	svc := newTestScraper(t, html, nil)

	_, err := svc.FetchAllStreams(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsStructureError(err) {
		t.Fatalf("expected structure error, got %v", err)
	}
}

func TestScraperHelpers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &ScraperService{
		logger: logger,
		memberNameMap: map[string]string{
			stringutil.Normalize("Member One"): "channel-1",
		},
	}

	if got := svc.extractVideoID("https://www.youtube.com/watch?v=abc123&feature=y"); got != "abc123" {
		t.Fatalf("unexpected video id: %s", got)
	}
	if got := svc.extractVideoID("invalid"); got != "" {
		t.Fatalf("expected empty video id, got %s", got)
	}

	onclick := "ga('send','event',{'event_category':'Tokino Sora'})"
	if got := svc.extractMemberFromOnClick(onclick); got != "Tokino Sora" {
		t.Fatalf("unexpected member name: %s", got)
	}

	if got := svc.matchMemberToChannel("Member One"); got != "channel-1" {
		t.Fatalf("unexpected match: %s", got)
	}
	if got := svc.matchMemberToChannel("Member"); got != "channel-1" {
		t.Fatalf("unexpected partial match: %s", got)
	}
}

func TestScraperParseDatetimeWithContext(t *testing.T) {
	svc := &ScraperService{}
	if _, err := svc.parseDatetimeWithContext("", ""); err == nil {
		t.Fatalf("expected error for empty date/time")
	}

	jst, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Now().In(jst)
	dateStr := now.Format("01/02")
	timeStr := now.Format("15:04")

	parsed, err := svc.parseDatetimeWithContext(dateStr, timeStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed == nil {
		t.Fatalf("expected parsed time")
	}
	if parsed.Location().String() != "Asia/Tokyo" {
		t.Fatalf("unexpected location: %v", parsed.Location())
	}
	if parsed.Year() != now.Year() {
		t.Fatalf("unexpected year: %d", parsed.Year())
	}
}

func TestScraperFetchChannel_DoesNotFallbackOnEmptyYouTubeResult(t *testing.T) {
	var officialRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialRequests.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	cacheClient := &cachemocks.Client{
		GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) {
			return nil, false
		},
		SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
	}

	svc := &ScraperService{
		httpClient: server.Client(),
		cache:      cacheClient,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL:    server.URL,
		fetchUpcoming: func(context.Context, string) ([]*ytscraper.UpcomingEvent, error) {
			return []*ytscraper.UpcomingEvent{}, nil
		},
	}

	streams, err := svc.FetchChannel(context.Background(), "channel-1")
	if err != nil {
		t.Fatalf("FetchChannel() error = %v", err)
	}
	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0", got)
	}
}

func TestScraperFetchChannel_FallbacksToOfficialOnYouTubeError(t *testing.T) {
	var officialRequests atomic.Int32

	html := `
<div class="container">
  <div class="col-12">
    <div class="navbar-inverse">
      <span class="holodule navbar-text">09/10 (Wed)</span>
    </div>
  </div>
  <div class="col-12">
    <a class="thumbnail" href="https://www.youtube.com/watch?v=video123">
      <div class="datetime">12:34</div>
      <div class="name">Member One</div>
    </a>
  </div>
</div>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialRequests.Add(1)
		if r.URL.Path != "/lives/hololive" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(server.Close)

	cacheClient := &cachemocks.Client{
		GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) {
			return nil, false
		},
		SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
	}

	svc := &ScraperService{
		httpClient: server.Client(),
		cache:      cacheClient,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL:    server.URL,
		memberNameMap: map[string]string{
			stringutil.Normalize("Member One"): "channel-1",
		},
		fetchUpcoming: func(context.Context, string) ([]*ytscraper.UpcomingEvent, error) {
			return nil, context.DeadlineExceeded
		},
	}

	streams, err := svc.FetchChannel(context.Background(), "channel-1")
	if err != nil {
		t.Fatalf("FetchChannel() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}
	if streams[0].ChannelID != "channel-1" {
		t.Fatalf("channel_id = %s, want channel-1", streams[0].ChannelID)
	}
	if got := officialRequests.Load(); got != 1 {
		t.Fatalf("official schedule requests = %d, want 1", got)
	}
}

func TestScraperFetchAllStreams_DeduplicatesConcurrentRequests(t *testing.T) {
	var officialRequests atomic.Int32

	html := `
<div class="container">
  <div class="col-12">
    <div class="navbar-inverse">
      <span class="holodule navbar-text">09/10 (Wed)</span>
    </div>
  </div>
  <div class="col-12">
    <a class="thumbnail" href="https://www.youtube.com/watch?v=video123">
      <div class="datetime">12:34</div>
      <div class="name">Member One</div>
    </a>
  </div>
</div>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialRequests.Add(1)
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(server.Close)

	now := time.Date(2026, 3, 6, 23, 40, 0, 0, time.UTC)
	svc := &ScraperService{
		httpClient: server.Client(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL:    server.URL,
		memberNameMap: map[string]string{
			stringutil.Normalize("Member One"): "channel-1",
		},
		nowFunc: func() time.Time { return now },
	}

	const concurrency = 6
	results := make(chan []*domain.Stream, concurrency)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			streams, err := svc.FetchAllStreams(context.Background())
			if err != nil {
				t.Errorf("FetchAllStreams() error = %v", err)
				return
			}
			results <- streams
		}()
	}

	wg.Wait()
	close(results)

	if got := officialRequests.Load(); got != 1 {
		t.Fatalf("official schedule requests = %d, want 1", got)
	}

	for streams := range results {
		if len(streams) != 1 {
			t.Fatalf("len(streams) = %d, want 1", len(streams))
		}
	}
}

func TestScraperFetchAllStreams_UsesShortTTLCacheAndClonesResults(t *testing.T) {
	var officialRequests atomic.Int32

	html := `
<div class="container">
  <div class="col-12">
    <div class="navbar-inverse">
      <span class="holodule navbar-text">09/10 (Wed)</span>
    </div>
  </div>
  <div class="col-12">
    <a class="thumbnail" href="https://www.youtube.com/watch?v=video123">
      <div class="datetime">12:34</div>
      <div class="name">Member One</div>
    </a>
  </div>
</div>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialRequests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(server.Close)

	currentTime := time.Date(2026, 3, 6, 23, 45, 0, 0, time.UTC)
	svc := &ScraperService{
		httpClient: server.Client(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL:    server.URL,
		memberNameMap: map[string]string{
			stringutil.Normalize("Member One"): "channel-1",
		},
		nowFunc: func() time.Time { return currentTime },
	}

	first, err := svc.FetchAllStreams(context.Background())
	if err != nil {
		t.Fatalf("first FetchAllStreams() error = %v", err)
	}
	first[0].Title = "mutated"

	second, err := svc.FetchAllStreams(context.Background())
	if err != nil {
		t.Fatalf("second FetchAllStreams() error = %v", err)
	}

	if got := officialRequests.Load(); got != 1 {
		t.Fatalf("official schedule requests = %d, want 1", got)
	}
	if second[0].Title == "mutated" {
		t.Fatalf("cached result should be cloned, got mutated title")
	}

	currentTime = currentTime.Add(constants.OfficialScheduleConfig.PageCacheTTL + time.Second)

	third, err := svc.FetchAllStreams(context.Background())
	if err != nil {
		t.Fatalf("third FetchAllStreams() error = %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("len(third) = %d, want 1", len(third))
	}
	if got := officialRequests.Load(); got != 2 {
		t.Fatalf("official schedule requests after TTL expiry = %d, want 2", got)
	}
}
