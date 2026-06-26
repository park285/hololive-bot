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
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type fakeEventRepository struct {
	recentExternalIDs []string
	latestPubDate     *time.Time
	upserted          []*domain.MajorEvent
}

func (f *fakeEventRepository) GetRecentExternalIDs(ctx context.Context, eventType domain.MajorEventType, limit int) ([]string, *time.Time, error) {
	return f.recentExternalIDs, f.latestPubDate, nil
}

func (f *fakeEventRepository) UpsertEvent(ctx context.Context, event *domain.MajorEvent) error {
	f.upserted = append(f.upserted, event)
	return nil
}

func TestServiceScrape_StoresOnlyNewEvents(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(sampleRSS)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	repository := &fakeEventRepository{
		recentExternalIDs: []string{"https://hololive.hololivepro.com/events/superexpo2026/"},
	}
	parser := NewRSSParser()
	fetcher := NewFeedFetcher(server.Client(), DefaultFeedFetcherConfig())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

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

	result, scrapeErr := service.Scrape(context.Background())
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
