package apiservice

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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

	svc, err := New(context.Background(), nil, scraper.ProxyConfig{}, nil, discardLogger())
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("New() returned nil service")
	}

	got, err := svc.GetChannelStatistics(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetChannelStatistics(nil) unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GetChannelStatistics(nil) len = %d, want 0", len(got))
	}
}

func TestScraperProxyToggle_DefaultClientHasNoProxy(t *testing.T) {
	t.Parallel()

	ys := &serviceImpl{
		logger:        discardLogger(),
		scraper:       scraper.NewClient(scraper.WithProxy(scraper.ProxyConfig{})),
		channelToName: make(map[string]string),
	}

	if ys.ScraperProxyEnabled() {
		t.Fatal("ProxyEnabled() = true on a client built without a proxy, want false")
	}

	if ys.SetScraperProxyEnabled(true) {
		t.Fatal("SetScraperProxyEnabled(true) = true without a configured proxy client, want false")
	}
	if ys.ScraperProxyEnabled() {
		t.Fatal("ProxyEnabled() = true after failed enable, want false")
	}

	if !ys.SetScraperProxyEnabled(false) {
		t.Fatal("SetScraperProxyEnabled(false) = false, want true (direct client available)")
	}
	if ys.ScraperProxyEnabled() {
		t.Fatal("ProxyEnabled() = true after disabling, want false")
	}
}

func TestScraperProxyToggle_NilReceiverSafe(t *testing.T) {
	t.Parallel()

	var nilSvc *serviceImpl
	if nilSvc.SetScraperProxyEnabled(true) {
		t.Fatal("SetScraperProxyEnabled on nil receiver = true, want false")
	}
	if nilSvc.ScraperProxyEnabled() {
		t.Fatal("ScraperProxyEnabled on nil receiver = true, want false")
	}

	noScraper := &serviceImpl{logger: discardLogger()}
	if noScraper.SetScraperProxyEnabled(true) {
		t.Fatal("SetScraperProxyEnabled with nil scraper = true, want false")
	}
	if noScraper.ScraperProxyEnabled() {
		t.Fatal("ScraperProxyEnabled with nil scraper = true, want false")
	}
}
