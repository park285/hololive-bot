package htmlscraper

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func TestNewServiceWithYouTubeProducerUsesProvidedClient(t *testing.T) {
	client := scraper.NewClient(scraper.WithRateLimiter(ratelimiter.New(0)))

	service := NewServiceWithYouTubeProducer(nil, nil, client, slog.Default())

	if service.youtubeProducer != client {
		t.Fatal("NewServiceWithYouTubeProducer did not keep provided scraper client")
	}
}

type nilResponseTransport struct{}

func (nilResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestLoadOfficialScheduleDocumentNilResponse(t *testing.T) {
	service := NewTestServiceWithHTTPClient(
		&http.Client{Transport: nilResponseTransport{}},
		slog.Default(),
		"https://schedule.example",
		nil,
	)

	_, err := service.loadOfficialScheduleDocument(t.Context())
	if err == nil {
		t.Fatal("expected error for nil HTTP response")
	}
	if got := err.Error(); !strings.Contains(got, "nil response") {
		t.Fatalf("error = %q, want nil response context", got)
	}
}
