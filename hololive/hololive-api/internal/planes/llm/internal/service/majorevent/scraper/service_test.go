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

package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const newsFeedPath = "/news"

type fakeEventRepository struct {
	mu        sync.Mutex
	known     map[string]domain.MajorEventType
	failures  map[string]error
	lookupErr error
	lookups   [][]string
	attempts  []string
	upserted  []*domain.MajorEvent
}

func (f *fakeEventRepository) GetExistingExternalIDs(_ context.Context, eventType domain.MajorEventType, externalIDs []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lookups = append(f.lookups, slices.Clone(externalIDs))
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}

	var existing []string

	for _, id := range externalIDs {
		if f.known[id] == eventType {
			existing = append(existing, id)
		}
	}

	return existing, nil
}

func (f *fakeEventRepository) UpsertEvent(_ context.Context, event *domain.MajorEvent) error {
	f.attempts = append(f.attempts, event.ExternalID)
	if err := f.failures[event.ExternalID]; err != nil {
		return err
	}

	if f.known == nil {
		f.known = make(map[string]domain.MajorEventType)
	}

	f.known[event.ExternalID] = event.Type
	f.upserted = append(f.upserted, event)

	return nil
}

func TestServiceScrape_StoresOnlyNewEvents(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(sampleRSS)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	repository := &fakeEventRepository{
		known: map[string]domain.MajorEventType{
			"https://hololive.hololivepro.com/events/superexpo2026/": domain.MajorEventTypeEvent,
		},
	}
	parser := NewRSSParser()
	fetcher := NewFeedFetcher(server.Client(), DefaultFeedFetcherConfig())
	logger := slog.New(slog.DiscardHandler)

	service, err := NewService(repository, fetcher, parser, ServiceConfig{
		Sources: []FeedSource{
			{
				Name:      "event",
				EventType: domain.MajorEventTypeEvent,
				FeedURL:   server.URL,
			},
		},
		FeedConcurrency:  1,
		IncrementalLimit: 50,
	}, logger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, scrapeErr := service.Scrape(t.Context())
	if scrapeErr != nil {
		t.Fatalf("Scrape() error = %v", scrapeErr)
	}

	if result.StoredEvents != 0 {
		t.Fatalf("Scrape() stored = %d, want 0", result.StoredEvents)
	}

	if result.SkippedKnown != 1 {
		t.Fatalf("Scrape() skipped = %d, want 1", result.SkippedKnown)
	}
}

const (
	olderEventID = "https://hololive.hololivepro.com/news/older/"
	newerEventID = "https://hololive.hololivepro.com/news/newer/"
)

func rssTestItem(id, date string) string {
	return fmt.Sprintf("<item><title>News</title><guid>%s</guid><link>%s</link><pubDate>%s</pubDate></item>", id, id, date)
}

func rssTestFeed(items ...string) string {
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>News</title>` + strings.Join(items, "") + `</channel></rss>`
}

func newTestScrapeService(t *testing.T, repository eventRepository, feeds map[string]string, limit int) *Service {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feed, ok := feeds[r.URL.Path]
		if !ok {
			http.NotFound(w, r)

			return
		}

		if _, err := w.Write([]byte(feed)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	sources := make([]FeedSource, 0, len(feeds))
	for path := range feeds {
		sources = append(sources, FeedSource{Name: path, EventType: domain.MajorEventTypeNews, FeedURL: server.URL + path})
	}

	service, err := NewService(repository, NewFeedFetcher(server.Client(), DefaultFeedFetcherConfig()), NewRSSParser(), ServiceConfig{
		Sources: sources, FeedConcurrency: 1, IncrementalLimit: limit,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	return service
}

func TestServiceScrape_PartialPersistenceRecoversMissingOlderID(t *testing.T) {
	t.Parallel()

	failure := errors.New("older event save failed")
	repository := &fakeEventRepository{failures: map[string]error{olderEventID: failure}}
	service := newTestScrapeService(t, repository, map[string]string{newsFeedPath: rssTestFeed(
		rssTestItem(olderEventID, "Thu, 09 Jan 2025 05:00:00 +0000"),
		rssTestItem(newerEventID, "Fri, 10 Jan 2025 05:00:00 +0000"),
	)}, 50)

	result, err := service.Scrape(t.Context())
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), olderEventID) {
		t.Fatalf("Scrape error = %v, want wrapped failure and external ID", err)
	}

	if result.StoredEvents != 1 || result.FailedEvents != 1 || result.FeedsFailed != 0 || result.ParsedEvents != 2 {
		t.Fatalf("partial Scrape result = %+v", result)
	}

	delete(repository.failures, olderEventID)

	result, err = service.Scrape(t.Context())
	if err != nil || result.StoredEvents != 1 || result.FailedEvents != 0 || result.SkippedKnown != 1 {
		t.Fatalf("next Scrape = %+v, %v; want older saved, newer skipped", result, err)
	}

	if !slices.Equal(repository.attempts, []string{olderEventID, newerEventID, olderEventID}) {
		t.Fatalf("upsert attempts = %v; newer event must not be saved again", repository.attempts)
	}
}

func TestServiceScrape_AllPersistenceFailuresPreserved(t *testing.T) {
	t.Parallel()

	olderFailure, newerFailure := errors.New("older save failed"), errors.New("newer save failed")
	repository := &fakeEventRepository{failures: map[string]error{olderEventID: olderFailure, newerEventID: newerFailure}}
	service := newTestScrapeService(t, repository, map[string]string{newsFeedPath: rssTestFeed(
		rssTestItem(olderEventID, "Thu, 09 Jan 2025 05:00:00 +0000"),
		rssTestItem(newerEventID, "Fri, 10 Jan 2025 05:00:00 +0000"),
	)}, 50)

	result, err := service.Scrape(t.Context())
	if !errors.Is(err, olderFailure) || !errors.Is(err, newerFailure) {
		t.Fatalf("Scrape error = %v, want both persistence failures", err)
	}

	if result.StoredEvents != 0 || result.FailedEvents != 2 || result.FeedsFailed != 0 || result.ParsedEvents != 2 {
		t.Fatalf("failed Scrape result = %+v", result)
	}
}

func TestServiceScrape_LateIDAcrossSameTypeFeeds(t *testing.T) {
	t.Parallel()

	repository := &fakeEventRepository{}
	news := newTestScrapeService(t, repository, map[string]string{newsFeedPath: rssTestFeed(
		rssTestItem(newerEventID, "Fri, 10 Jan 2025 05:00:00 +0000"),
	)}, 50)

	if result, err := news.Scrape(t.Context()); err != nil || result.StoredEvents != 1 {
		t.Fatalf("initial news Scrape = %+v, %v", result, err)
	}

	service := newTestScrapeService(t, repository, map[string]string{
		newsFeedPath: rssTestFeed(rssTestItem(newerEventID, "Fri, 10 Jan 2025 05:00:00 +0000")),
		"/en-news":   rssTestFeed(rssTestItem(olderEventID, "Thu, 09 Jan 2025 05:00:00 +0000")),
	}, 50)
	result, err := service.Scrape(t.Context())

	if err != nil || result.StoredEvents != 1 || result.SkippedKnown != 1 || result.FeedsAttempted != 2 {
		t.Fatalf("late feed Scrape = %+v, %v", result, err)
	}

	if !slices.Equal(repository.attempts, []string{newerEventID, olderEventID}) {
		t.Fatalf("upsert attempts = %v", repository.attempts)
	}
}

func TestServiceScrape_KnownIDsAcrossBoundedChunks(t *testing.T) {
	t.Parallel()

	repository := &fakeEventRepository{known: map[string]domain.MajorEventType{}}
	items := make([]string, 0, 5)

	for i := range 5 {
		id := fmt.Sprintf("https://hololive.hololivepro.com/news/%d/", i)

		items = append(items, rssTestItem(id, "Thu, 09 Jan 2025 05:00:00 +0000"))

		if i != 3 {
			repository.known[id] = domain.MajorEventTypeNews
		}
	}

	service := newTestScrapeService(t, repository, map[string]string{newsFeedPath: rssTestFeed(items...)}, 2)
	result, err := service.Scrape(t.Context())

	if err != nil || result.StoredEvents != 1 || result.SkippedKnown != 4 {
		t.Fatalf("chunked Scrape = %+v, %v", result, err)
	}

	if len(repository.lookups) != 3 {
		t.Fatalf("lookup count = %d, want 3 bounded queries", len(repository.lookups))
	}

	for _, ids := range repository.lookups {
		if len(ids) > 2 {
			t.Fatalf("lookup size = %d, want at most 2", len(ids))
		}
	}

	if !slices.Equal(repository.attempts, []string{"https://hololive.hololivepro.com/news/3/"}) {
		t.Fatalf("upsert attempts = %v", repository.attempts)
	}
}

func TestServiceScrape_LookupFailureDoesNotPersistUnclassifiedIDs(t *testing.T) {
	t.Parallel()

	repository := &fakeEventRepository{lookupErr: errors.New("lookup failed")}
	service := newTestScrapeService(t, repository, map[string]string{newsFeedPath: sampleRSS}, 50)
	result, err := service.Scrape(t.Context())

	if err == nil || result.FeedsFailed != 1 || len(repository.attempts) != 0 {
		t.Fatalf("lookup failure Scrape = %+v, %v, attempts = %v", result, err, repository.attempts)
	}
}

func TestFeedScheduler_PersistenceFailureUsesExistingRetrySchedule(t *testing.T) {
	t.Parallel()

	repository := &fakeEventRepository{failures: map[string]error{olderEventID: errors.New("save failed")}}
	service := newTestScrapeService(t, repository, map[string]string{newsFeedPath: rssTestFeed(
		rssTestItem(olderEventID, "Thu, 09 Jan 2025 05:00:00 +0000"),
		rssTestItem(newerEventID, "Fri, 10 Jan 2025 05:00:00 +0000"),
	)}, 50)
	scheduledAt := time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)

	scheduler, err := NewFeedScheduler(service, FeedScheduleConfig{
		RunTimeout: time.Minute, RetryDelays: []time.Duration{time.Hour, 2 * time.Hour},
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewFeedScheduler: %v", err)
	}

	scheduler.nowFn = func() time.Time { return scheduledAt.Add(time.Minute) }
	scheduler.executeCycle(t.Context(), scrapeTriggerRegular, scheduledAt)

	if scheduler.retryRunCount() != 2 || !scheduler.peekRetryRun().Equal(scheduledAt.Add(time.Hour)) {
		t.Fatalf("retry runs after partial save = %v", scheduler.retryRuns)
	}

	retryAt := scheduler.peekRetryRun()
	scheduler.popRetryRun()
	scheduler.executeCycle(t.Context(), scrapeTriggerRetry, retryAt)

	if scheduler.retryRunCount() != 1 || !scheduler.peekRetryRun().Equal(scheduledAt.Add(2*time.Hour)) {
		t.Fatalf("failed retry must retain remaining schedule: %v", scheduler.retryRuns)
	}

	delete(repository.failures, olderEventID)

	retryAt = scheduler.peekRetryRun()
	scheduler.popRetryRun()
	scheduler.executeCycle(t.Context(), scrapeTriggerRetry, retryAt)

	if scheduler.retryRunCount() != 0 || len(repository.upserted) != 2 {
		t.Fatalf("successful retry: remaining = %d, stored = %d", scheduler.retryRunCount(), len(repository.upserted))
	}
}
