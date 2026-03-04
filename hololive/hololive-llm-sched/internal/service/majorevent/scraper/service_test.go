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
		_, _ = w.Write([]byte(sampleRSS))
	}))
	t.Cleanup(server.Close)

	repo := &fakeEventRepository{
		recentExternalIDs: []string{"https://hololive.hololivepro.com/events/superexpo2026/"},
	}
	parser := NewRSSParser()
	fetcher := NewFeedFetcher(server.Client(), DefaultFeedFetcherConfig())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service, err := NewService(repo, fetcher, parser, ServiceConfig{
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
