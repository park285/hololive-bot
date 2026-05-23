package htmlscraper

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func TestNewServiceWithYouTubeProducerUsesProvidedClient(t *testing.T) {
	client := scraper.NewClient(scraper.WithRateLimiter(scraper.NewRateLimiter(0)))

	service := NewServiceWithYouTubeProducer(nil, nil, client, slog.Default())

	if service.youtubeProducer != client {
		t.Fatal("NewServiceWithYouTubeProducer did not keep provided scraper client")
	}
}
