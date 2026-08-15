package htmlscraper

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestNewServiceWithOfficialScheduleUsesInjectedConfig(t *testing.T) {
	t.Setenv("OFFICIAL_SCHEDULE_BASE_URL", "https://env-should-not-win.example")
	t.Setenv("MAX_RESPONSE_BODY_BYTES", "999")

	official := settings.OfficialScheduleRuntimeConfig{
		OfficialSchedule: settings.OfficialScheduleConfig{
			BaseURL:      "https://schedule.injected.example",
			Timeout:      5 * time.Second,
			CacheExpiry:  time.Minute,
			PageCacheTTL: time.Second,
		},
		MaxResponseBodyBytes: 2048,
	}
	service := NewServiceWithOfficialSchedule(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), official)
	if got := service.OfficialScheduleBaseURLForTest(); got != "https://schedule.injected.example" {
		t.Fatalf("BaseURL = %q, want injected origin", got)
	}
	if got := service.OfficialScheduleMaxResponseBodyBytesForTest(); got != 2048 {
		t.Fatalf("MaxResponseBodyBytes = %d, want 2048", got)
	}
}
