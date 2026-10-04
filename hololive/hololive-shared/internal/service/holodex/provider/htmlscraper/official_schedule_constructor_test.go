package htmlscraper

import (
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestNewServiceUsesInjectedConfig(t *testing.T) {
	t.Setenv("OFFICIAL_SCHEDULE_BASE_URL", "https://env-should-not-win.example")
	t.Setenv("MAX_RESPONSE_BODY_BYTES", "999")

	official := settings.OfficialScheduleRuntimeConfig{
		OfficialSchedule: settings.OfficialScheduleConfig{
			BaseURL:      "https://schedule.injected.example",
			Timeout:      5 * time.Second,
			PageCacheTTL: time.Second,
		},
		MaxResponseBodyBytes: 2048,
	}

	service, err := NewService(nil, nil, slog.New(slog.DiscardHandler), official)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if got := service.officialSchedule.BaseURL; got != "https://schedule.injected.example" {
		t.Fatalf("BaseURL = %q, want injected origin", got)
	}

	if got := service.maxResponseBodyBytes; got != 2048 {
		t.Fatalf("MaxResponseBodyBytes = %d, want 2048", got)
	}

	if service.httpClient == nil {
		t.Fatal("nil httpClient must be replaced by the official schedule external API client")
	}
}
