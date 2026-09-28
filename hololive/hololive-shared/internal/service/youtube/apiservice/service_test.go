package apiservice

import (
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

const (
	testChannelID1    = "UC1"
	testChannelID2    = "UC2"
	testFallbackTitle = "@fallback"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestMemberNameFromCacheKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "strips trailing colon segment", key: "ときのそら:UC123", want: "ときのそら"},
		{name: "keeps only first segment before last colon", key: "name:type:UC123", want: "name:type"},
		{name: "no colon returns key unchanged", key: "PlainName", want: "PlainName"},
		{name: "leading colon returns key unchanged", key: ":onlysuffix", want: ":onlysuffix"},
		{name: "empty key returns empty", key: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := memberNameFromCacheKey(tt.key)
			if got != tt.want {
				t.Fatalf("memberNameFromCacheKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestStoreChannelNameMap(t *testing.T) {
	t.Parallel()

	ys := &serviceImpl{
		logger:        discardLogger(),
		channelToName: make(map[string]string),
	}

	ys.storeChannelNameMap(map[string]string{
		"ときのそら:meta": "UC_sora",
		"AZKi:meta":  "UC_azki",
		"empty:meta": "",
		"NoColon":    "UC_nocolon",
	})

	tests := []struct {
		channelID string
		want      string
	}{
		{channelID: "UC_sora", want: "ときのそら"},
		{channelID: "UC_azki", want: "AZKi"},
		{channelID: "UC_nocolon", want: "NoColon"},
	}
	for _, tt := range tests {
		if got := ys.getChannelName(tt.channelID); got != tt.want {
			t.Fatalf("getChannelName(%q) = %q, want %q", tt.channelID, got, tt.want)
		}
	}

	if _, ok := ys.channelToName["empty:meta"]; ok {
		t.Fatal("blank channelID must not be stored under any key")
	}

	if got := ys.getChannelName(""); got != "" {
		t.Fatalf("empty channelID lookup = %q, want empty (blank values are skipped)", got)
	}

	if len(ys.channelToName) != 3 {
		t.Fatalf("channelToName has %d entries, want 3 (blank value skipped)", len(ys.channelToName))
	}
}

func TestStoreChannelNameMap_LastWriteWinsOnChannelCollision(t *testing.T) {
	t.Parallel()

	ys := &serviceImpl{
		logger:        discardLogger(),
		channelToName: make(map[string]string),
	}

	ys.storeChannelNameMap(map[string]string{"OnlyKey:meta": "UC_shared"})

	if got := ys.getChannelName("UC_shared"); got != "OnlyKey" {
		t.Fatalf("getChannelName = %q, want %q", got, "OnlyKey")
	}
}

func TestNew_ReturnsUsableServiceWithNilCache(t *testing.T) {
	t.Parallel()

	svc, err := New(t.Context(), nil, settings.DefaultYouTubeOperationalConfig(), nil, discardLogger())
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	if svc == nil {
		t.Fatal("New() returned nil service")
	}

	got, err := svc.GetChannelStatistics(t.Context(), nil)
	if err != nil {
		t.Fatalf("GetChannelStatistics(nil) unexpected error: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("GetChannelStatistics(nil) len = %d, want 0", len(got))
	}
}

// runtime 설정의 cache 저장·scraper phase timeout이 서비스에 그대로 들어가야 한다(stack audit A3).
func TestNew_UsesInjectedYouTubeTimeouts(t *testing.T) {
	t.Parallel()

	cfg := settings.DefaultYouTubeOperationalConfig()

	cfg.CacheSaveTimeout = 2 * time.Second
	cfg.ScraperPhaseTimeout = 9 * time.Second

	svc, err := New(t.Context(), nil, cfg, nil, discardLogger())
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	ys, ok := svc.(*serviceImpl)
	if !ok {
		t.Fatalf("New() returned %T, want *serviceImpl", svc)
	}

	if ys.cacheSaveTimeout != 2*time.Second || ys.scraperPhaseTimeout != 9*time.Second {
		t.Fatalf("timeouts = (%s, %s), want (2s, 9s)", ys.cacheSaveTimeout, ys.scraperPhaseTimeout)
	}
}
