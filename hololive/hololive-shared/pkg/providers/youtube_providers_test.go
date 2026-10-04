package providers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

// 조립된 Holodex 서비스는 /users/live 실패를 그대로 돌려주고 공식 일정 서비스로 live-status를 채우지 않는다
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

	var officialRequests atomic.Int32

	officialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		http.Error(w, "unexpected official schedule request", http.StatusInternalServerError)
	}))
	t.Cleanup(officialServer.Close)

	official := settings.OfficialScheduleRuntimeConfig{
		OfficialSchedule:     settings.DefaultOfficialScheduleConfig(),
		MaxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
	}

	official.OfficialSchedule.BaseURL = officialServer.URL

	scraperService, err := ProvideScraperServiceWithOfficialSchedule(nil, slog.New(slog.DiscardHandler), official)
	if err != nil {
		t.Fatalf("ProvideScraperServiceWithOfficialSchedule() error = %v", err)
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

	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0", got)
	}
}
