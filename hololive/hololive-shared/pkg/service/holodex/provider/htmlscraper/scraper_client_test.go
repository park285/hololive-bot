package htmlscraper

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func TestNewServiceWithYouTubeClientUsesProvidedClient(t *testing.T) {
	client := scraper.NewClient(scraper.WithRateLimiter(ratelimiter.New(0)))
	service := NewServiceWithYouTubeClient(nil, nil, client, slog.Default())
	if service.youtubeClient != client {
		t.Fatal("NewServiceWithYouTubeClient did not keep provided scraper client")
	}
}

func TestOfficialScheduleAPINilResponse(t *testing.T) {
	service := NewTestServiceWithHTTPClient(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, nil
		})},
		slog.Default(),
		"https://schedule.example",
		nil,
	)

	_, err := service.fetchOfficialScheduleAPI(t.Context())
	if err == nil {
		t.Fatal("expected error for nil HTTP response")
	}
	if got := err.Error(); !strings.Contains(got, "nil *Response") && !strings.Contains(got, "nil response") {
		t.Fatalf("error = %q, want nil response context", got)
	}
}
