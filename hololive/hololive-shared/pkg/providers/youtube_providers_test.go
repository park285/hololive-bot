package providers

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type providerRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f providerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	out, err := f(req)
	if err != nil {
		return nil, fmt.Errorf("f: %w", err)
	}

	return out, nil
}

// 조립된 Holodex 서비스는 /users/live 실패를 그대로 돌려주고 YouTube scraper로 live-status를 채우지 않는다
// (DEC-20260926-hololive-live-status-scraper-fallback-removal).
func TestProvideHolodexServiceWithConfigHasNoLiveStatusScraperFallback(t *testing.T) {
	t.Parallel()

	holodexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/live" {
			http.NotFound(w, r)

			return
		}

		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(holodexServer.Close)

	var youtubeRequests atomic.Int32

	youtubeClient := scraper.NewClient(
		settings.DefaultYouTubeOperationalConfig(),
		scraper.WithRateLimiter(ratelimiter.New(0)),
		scraper.WithHTTPClient(&http.Client{
			Transport: providerRoundTripFunc(func(*http.Request) (*http.Response, error) {
				youtubeRequests.Add(1)

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("<html></html>")),
				}, nil
			}),
		}),
	)

	scraperService, err := ProvideScraperServiceWithYouTubeClient(nil, youtubeClient, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ProvideScraperServiceWithYouTubeClient() error = %v", err)
	}

	holodexCfg := settings.DefaultHolodexOperationalConfig()

	holodexCfg.BaseURL = holodexServer.URL
	holodexCfg.APIKey = "test-key"
	holodexCfg.DistributedRateLimit.Enabled = false

	service, err := ProvideHolodexServiceWithConfig(&holodexCfg, cachemocks.NewLenientClient(), scraperService, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ProvideHolodexServiceWithConfig() error = %v", err)
	}

	if _, err := service.GetChannelsLiveStatus(t.Context(), []string{"c1", "c2"}); err == nil {
		t.Fatal("GetChannelsLiveStatus() error = nil, want Holodex source failure")
	}

	if got := youtubeRequests.Load(); got != 0 {
		t.Fatalf("youtube requests = %d, want 0", got)
	}
}
