package htmlscraper

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func TestNewServiceWithYouTubeClientUsesProvidedClient(t *testing.T) {
	client := scraper.NewClient(settings.DefaultYouTubeOperationalConfig(), scraper.WithRateLimiter(ratelimiter.New(0)))

	service, err := NewServiceWithYouTubeClient(nil, client, slog.Default())
	if err != nil {
		t.Fatalf("NewServiceWithYouTubeClient() error = %v", err)
	}

	if service.youtubeClient != client {
		t.Fatal("NewServiceWithYouTubeClient did not keep provided scraper client")
	}
}

func TestOfficialScheduleAPINilResponse(t *testing.T) {
	service := newTestServiceWithHTTPClient(
		t,
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			//nolint:nilnil // (nil 응답, nil 오류) 조합 자체가 이 테스트의 검증 대상이라 sentinel 오류로 바꿀 수 없다.
			return nil, nil
		})},
		slog.Default(),
		"https://schedule.example",
	)

	_, err := service.fetchOfficialScheduleAPI(t.Context())
	if err == nil {
		t.Fatal("expected error for nil HTTP response")
	}

	if got := err.Error(); !strings.Contains(got, "nil *Response") && !strings.Contains(got, "nil response") {
		t.Fatalf("error = %q, want nil response context", got)
	}
}
