package holodexprovider

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func TestNewScraperServiceWithYouTubeProducerUsesProvidedClient(t *testing.T) {
	client := scraper.NewClient(scraper.WithRateLimiter(scraper.NewRateLimiter(0)))

	service := NewScraperServiceWithYouTubeProducer(nil, nil, client, slog.Default())

	if service.youtubeProducer != client {
		t.Fatal("NewScraperServiceWithYouTubeProducer did not keep provided scraper client")
	}
}
