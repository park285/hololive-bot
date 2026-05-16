package holodexprovider

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func TestNewScraperServiceWithYouTubeScraperUsesProvidedClient(t *testing.T) {
	client := scraper.NewClient(scraper.WithRateLimiter(scraper.NewRateLimiter(0)))

	service := NewScraperServiceWithYouTubeScraper(nil, nil, client, slog.Default())

	if service.youtubeScraper != client {
		t.Fatal("NewScraperServiceWithYouTubeScraper did not keep provided scraper client")
	}
}
