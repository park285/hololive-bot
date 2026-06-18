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

package htmlscraper

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/pkg/httputil"
	"github.com/park285/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	ytscraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func writeHTMLResponse(t *testing.T, w http.ResponseWriter, html string) {
	t.Helper()
	if _, err := w.Write([]byte(html)); err != nil {
		t.Fatalf("write html response: %v", err)
	}
}

type testMemberDataProvider struct {
	members []*domain.Member
}

func (p testMemberDataProvider) GetAllMembers() []*domain.Member                       { return p.members }
func (p testMemberDataProvider) FindMemberByChannelID(string) *domain.Member           { return nil }
func (p testMemberDataProvider) FindMemberByName(string) *domain.Member                { return nil }
func (p testMemberDataProvider) FindMemberByAlias(string) *domain.Member               { return nil }
func (p testMemberDataProvider) GetChannelIDs() []string                               { return nil }
func (p testMemberDataProvider) WithContext(context.Context) domain.MemberDataProvider { return p }
func (p testMemberDataProvider) FindMembersByName(string) []*domain.Member             { return nil }
func (p testMemberDataProvider) FindMembersByAlias(string) []*domain.Member            { return nil }

func newTestScraper(t *testing.T, html string, memberMap map[string]string) *Service {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lives/hololive" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		writeHTMLResponse(t, w, html)
	}))
	t.Cleanup(server.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Service{
		httpClient:    server.Client(),
		logger:        logger,
		baseURL:       server.URL,
		memberNameMap: memberMap,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errorReadCloser struct {
	err error
}

func (r errorReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r errorReadCloser) Close() error {
	return nil
}

func counterValueByLabels(t *testing.T, labels map[string]string) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, family := range families {
		if family.GetName() != "hololive_holodex_official_schedule_fallback_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			if metricLabelsMatch(metric, labels) {
				if metric.Counter == nil {
					t.Fatalf("metric hololive_holodex_official_schedule_fallback_total with labels %#v is not a counter", labels)
				}
				return metric.Counter.GetValue()
			}
		}
	}

	return 0
}

func metricLabelsMatch(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}

	for _, label := range metric.GetLabel() {
		if labels[label.GetName()] != label.GetValue() {
			return false
		}
	}

	return true
}

func TestNewService_UsesSharedHTTPClientPolicy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(
		nil,
		testMemberDataProvider{members: []*domain.Member{{
			Name:      "Member One",
			NameJa:    "メンバー1",
			ChannelID: "channel-1",
			Aliases: &domain.Aliases{
				Ko: []string{"멤버원"},
				Ja: []string{"メンバー원"},
			},
		}}},
		ytscraper.ProxyConfig{},
		nil,
		logger,
	)
	if service.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if service.httpClient.Timeout != config.DefaultOfficialScheduleConfig().Timeout {
		t.Fatalf("timeout=%s want=%s", service.httpClient.Timeout, config.DefaultOfficialScheduleConfig().Timeout)
	}
	transport, ok := service.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", service.httpClient.Transport)
	}
	shared := httputil.NewExternalAPIClient(config.DefaultOfficialScheduleConfig().Timeout)
	sharedTransport, ok := shared.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("shared transport type = %T, want *http.Transport", shared.Transport)
	}
	if transport.MaxConnsPerHost != sharedTransport.MaxConnsPerHost {
		t.Fatalf("MaxConnsPerHost=%d want=%d", transport.MaxConnsPerHost, sharedTransport.MaxConnsPerHost)
	}
	if transport.MaxIdleConnsPerHost != sharedTransport.MaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost=%d want=%d", transport.MaxIdleConnsPerHost, sharedTransport.MaxIdleConnsPerHost)
	}
	if got := service.memberNameMap[stringutil.Normalize("Member One")]; got != "channel-1" {
		t.Fatalf("member mapping = %q, want channel-1", got)
	}
	if got := service.memberNameMap[stringutil.Normalize("メンバー1")]; got != "channel-1" {
		t.Fatalf("japanese mapping = %q, want channel-1", got)
	}
	if got := service.memberNameMap[stringutil.Normalize("멤버원")]; got != "channel-1" {
		t.Fatalf("alias mapping = %q, want channel-1", got)
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
	service := newTestScraper(t, html, memberMap)

	streams, err := service.FetchAllStreams(context.Background())
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
	service := newTestScraper(t, html, nil)

	_, err := service.FetchAllStreams(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsStructureError(err) {
		t.Fatalf("expected structure error, got %v", err)
	}
}

func TestScraperFetchAllStreams_RecordsOfficialSchedulePageReasonMatched(t *testing.T) {
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

	service := newTestScraper(t, html, map[string]string{
		stringutil.Normalize("Member One"): "channel-1",
	})

	labels := map[string]string{
		"operation": "official_schedule_page",
		"outcome":   "hit",
		"reason":    "matched",
	}
	before := counterValueByLabels(t, labels)

	streams, err := service.FetchAllStreams(context.Background())
	if err != nil {
		t.Fatalf("FetchAllStreams() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}

	after := counterValueByLabels(t, labels)
	if after != before+1 {
		t.Fatalf("fallback metric delta = %v, want 1", after-before)
	}
}

func TestScraperFetchAllStreams_RecordsOfficialSchedulePageReasonStructureDrift(t *testing.T) {
	html := `<div class="container"><div class="col-12"></div></div>`
	service := newTestScraper(t, html, nil)

	labels := map[string]string{
		"operation": "official_schedule_page",
		"outcome":   "error",
		"reason":    "structure_drift",
	}
	before := counterValueByLabels(t, labels)

	_, err := service.FetchAllStreams(context.Background())
	if err == nil {
		t.Fatal("FetchAllStreams() error = nil, want non-nil")
	}

	after := counterValueByLabels(t, labels)
	if after != before+1 {
		t.Fatalf("fallback metric delta = %v, want 1", after-before)
	}
}

func TestScraperFetchAllStreams_RecordsOfficialSchedulePageReasonParseError(t *testing.T) {
	service := &Service{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       errorReadCloser{err: errors.New("broken html stream")},
					Header:     make(http.Header),
				}, nil
			}),
		},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL: "https://example.invalid",
	}

	labels := map[string]string{
		"operation": "official_schedule_page",
		"outcome":   "error",
		"reason":    "parse",
	}
	before := counterValueByLabels(t, labels)

	_, err := service.FetchAllStreams(context.Background())
	if err == nil {
		t.Fatal("FetchAllStreams() error = nil, want non-nil")
	}

	after := counterValueByLabels(t, labels)
	if after != before+1 {
		t.Fatalf("fallback metric delta = %v, want 1", after-before)
	}
}

func TestScraperHelpers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &Service{
		logger: logger,
		memberNameMap: map[string]string{
			stringutil.Normalize("Member One"): "channel-1",
		},
	}

	if got := service.extractVideoID("https://www.youtube.com/watch?v=abc123&feature=y"); got != "abc123" {
		t.Fatalf("unexpected video id: %s", got)
	}
	if got := service.extractVideoID("invalid"); got != "" {
		t.Fatalf("expected empty video id, got %s", got)
	}

	onclick := "ga('send','event',{'event_category':'Tokino Sora'})"
	if got := service.extractMemberFromOnClick(onclick); got != "Tokino Sora" {
		t.Fatalf("unexpected member name: %s", got)
	}

	if got := service.matchMemberToChannel("Member One"); got != "channel-1" {
		t.Fatalf("unexpected match: %s", got)
	}
	if got := service.matchMemberToChannel("Member"); got != "channel-1" {
		t.Fatalf("unexpected partial match: %s", got)
	}
}

func TestScraperParseDatetimeWithContext(t *testing.T) {
	service := &Service{}
	if _, err := service.parseDatetimeWithContext("", ""); err == nil {
		t.Fatalf("expected error for empty date/time")
	}

	jst, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	now := time.Now().In(jst)
	dateStr := now.Format("01/02")
	timeStr := now.Format("15:04")

	parsed, err := service.parseDatetimeWithContext(dateStr, timeStr)
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

	service := &Service{
		httpClient: server.Client(),
		cache:      cacheClient,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL:    server.URL,
		fetchUpcoming: func(context.Context, string) ([]*ytscraper.UpcomingEvent, error) {
			return []*ytscraper.UpcomingEvent{}, nil
		},
	}

	streams, err := service.FetchChannel(context.Background(), "channel-1")
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
		writeHTMLResponse(t, w, html)
	}))
	t.Cleanup(server.Close)

	cacheClient := &cachemocks.Client{
		GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) {
			return nil, false
		},
		SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
	}

	service := &Service{
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

	streams, err := service.FetchChannel(context.Background(), "channel-1")
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

func TestScraperFetchChannel_RecordsOfficialFallbackReasonStructureDrift(t *testing.T) {
	html := `<div class="container"><div class="col-12"></div></div>`
	service := newTestScraper(t, html, nil)
	service.cache = &cachemocks.Client{
		GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) {
			return nil, false
		},
		SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
	}
	service.fetchUpcoming = func(context.Context, string) ([]*ytscraper.UpcomingEvent, error) {
		return nil, context.DeadlineExceeded
	}

	labels := map[string]string{
		"operation": "channel_schedule",
		"outcome":   "error",
		"reason":    "structure_drift",
	}
	before := counterValueByLabels(t, labels)

	_, err := service.FetchChannel(context.Background(), "channel-1")
	if err == nil {
		t.Fatal("FetchChannel() error = nil, want non-nil")
	}

	after := counterValueByLabels(t, labels)
	if after != before+1 {
		t.Fatalf("fallback metric delta = %v, want 1", after-before)
	}
}

func TestScraperFetchChannel_RecordsOfficialFallbackReasonParseError(t *testing.T) {
	service := &Service{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       errorReadCloser{err: errors.New("broken html stream")},
					Header:     make(http.Header),
				}, nil
			}),
		},
		cache: &cachemocks.Client{
			GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) {
				return nil, false
			},
			SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
		},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL: "https://example.invalid",
		fetchUpcoming: func(context.Context, string) ([]*ytscraper.UpcomingEvent, error) {
			return nil, context.DeadlineExceeded
		},
	}

	labels := map[string]string{
		"operation": "channel_schedule",
		"outcome":   "error",
		"reason":    "parse",
	}
	before := counterValueByLabels(t, labels)

	_, err := service.FetchChannel(context.Background(), "channel-1")
	if err == nil {
		t.Fatal("FetchChannel() error = nil, want non-nil")
	}

	after := counterValueByLabels(t, labels)
	if after != before+1 {
		t.Fatalf("fallback metric delta = %v, want 1", after-before)
	}
}

func TestScraperFetchChannel_RecordsOfficialFallbackReasonNetworkError(t *testing.T) {
	service := &Service{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			}),
		},
		cache: &cachemocks.Client{
			GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) {
				return nil, false
			},
			SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
		},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL: "https://example.invalid",
		fetchUpcoming: func(context.Context, string) ([]*ytscraper.UpcomingEvent, error) {
			return nil, context.DeadlineExceeded
		},
	}

	labels := map[string]string{
		"operation": "channel_schedule",
		"outcome":   "error",
		"reason":    "network",
	}
	before := counterValueByLabels(t, labels)

	_, err := service.FetchChannel(context.Background(), "channel-1")
	if err == nil {
		t.Fatal("FetchChannel() error = nil, want non-nil")
	}

	after := counterValueByLabels(t, labels)
	if after != before+1 {
		t.Fatalf("fallback metric delta = %v, want 1", after-before)
	}
}

func TestScraperFetchChannel_RecordsOfficialFallbackReasonEmptyMiss(t *testing.T) {
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
      <div class="name">Other Member</div>
    </a>
  </div>
</div>`

	service := newTestScraper(t, html, map[string]string{
		stringutil.Normalize("Other Member"): "other-channel",
	})
	service.cache = &cachemocks.Client{
		GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) {
			return nil, false
		},
		SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
	}
	service.fetchUpcoming = func(context.Context, string) ([]*ytscraper.UpcomingEvent, error) {
		return nil, context.DeadlineExceeded
	}

	labels := map[string]string{
		"operation": "channel_schedule",
		"outcome":   "miss",
		"reason":    "empty",
	}
	before := counterValueByLabels(t, labels)

	streams, err := service.FetchChannel(context.Background(), "channel-1")
	if err != nil {
		t.Fatalf("FetchChannel() error = %v", err)
	}
	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}

	after := counterValueByLabels(t, labels)
	if after != before+1 {
		t.Fatalf("fallback metric delta = %v, want 1", after-before)
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
		writeHTMLResponse(t, w, html)
	}))
	t.Cleanup(server.Close)

	now := time.Date(2026, 3, 6, 23, 40, 0, 0, time.UTC)
	service := &Service{
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
	for range concurrency {
		wg.Go(func() {
			streams, err := service.FetchAllStreams(context.Background())
			if err != nil {
				t.Errorf("FetchAllStreams() error = %v", err)
				return
			}
			results <- streams
		})
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
		writeHTMLResponse(t, w, html)
	}))
	t.Cleanup(server.Close)

	currentTime := time.Date(2026, 3, 6, 23, 45, 0, 0, time.UTC)
	service := &Service{
		httpClient: server.Client(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		baseURL:    server.URL,
		memberNameMap: map[string]string{
			stringutil.Normalize("Member One"): "channel-1",
		},
		nowFunc: func() time.Time { return currentTime },
	}

	first, err := service.FetchAllStreams(context.Background())
	if err != nil {
		t.Fatalf("first FetchAllStreams() error = %v", err)
	}
	first[0].Title = "mutated"

	second, err := service.FetchAllStreams(context.Background())
	if err != nil {
		t.Fatalf("second FetchAllStreams() error = %v", err)
	}

	if got := officialRequests.Load(); got != 1 {
		t.Fatalf("official schedule requests = %d, want 1", got)
	}
	if second[0].Title == "mutated" {
		t.Fatalf("cached result should be cloned, got mutated title")
	}

	currentTime = currentTime.Add(config.DefaultOfficialScheduleConfig().PageCacheTTL + time.Second)

	third, err := service.FetchAllStreams(context.Background())
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
