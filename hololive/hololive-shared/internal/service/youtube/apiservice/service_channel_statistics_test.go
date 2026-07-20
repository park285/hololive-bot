package apiservice

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func newTestService(t *testing.T, channelToName map[string]string) *serviceImpl {
	t.Helper()
	if channelToName == nil {
		channelToName = make(map[string]string)
	}
	return &serviceImpl{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		channelToName: channelToName,
	}
}

func TestNonNegativeYouTubeCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     int64
		wantCount uint64
		wantOK    bool
	}{
		{name: "zero is accepted", value: 0, wantCount: 0, wantOK: true},
		{name: "positive is accepted", value: 12345, wantCount: 12345, wantOK: true},
		{name: "max int64 is accepted", value: 1<<62 - 1, wantCount: 1<<62 - 1, wantOK: true},
		{name: "negative one is rejected", value: -1, wantCount: 0, wantOK: false},
		{name: "min int64 is rejected", value: -1 << 62, wantCount: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotCount, gotOK := nonNegativeYouTubeCount(tt.value)
			if gotOK != tt.wantOK {
				t.Fatalf("nonNegativeYouTubeCount(%d) ok = %v, want %v", tt.value, gotOK, tt.wantOK)
			}
			if gotCount != tt.wantCount {
				t.Fatalf("nonNegativeYouTubeCount(%d) count = %d, want %d", tt.value, gotCount, tt.wantCount)
			}
		})
	}
}

func TestValidatedScrapedChannelCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     int64
		wantCount uint64
		wantErr   bool
	}{
		{name: "zero passes", value: 0, wantCount: 0, wantErr: false},
		{name: "positive passes", value: 999, wantCount: 999, wantErr: false},
		{name: "negative fails", value: -5, wantCount: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := validatedScrapedChannelCount("UC_chan", "subscriber", tt.value)
			assertValidatedScrapedChannelCount(t, tt.value, got, err, tt.wantCount, tt.wantErr)
		})
	}
}

func assertValidatedScrapedChannelCount(t *testing.T, value int64, got uint64, err error, wantCount uint64, wantErr bool) {
	t.Helper()

	if wantErr {
		if err == nil {
			t.Fatalf("validatedScrapedChannelCount(_, _, %d) expected error, got nil", value)
		}
		for _, want := range []string{"UC_chan", "subscriber", "-5"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q missing %q", err.Error(), want)
			}
		}
		return
	}
	if err != nil {
		t.Fatalf("validatedScrapedChannelCount(_, _, %d) unexpected error: %v", value, err)
	}
	if got != wantCount {
		t.Fatalf("validatedScrapedChannelCount(_, _, %d) = %d, want %d", value, got, wantCount)
	}
}

func TestValidatedScrapedChannelCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stats   scraper.ChannelStats
		wantSub uint64
		wantVid uint64
		wantViw uint64
		wantErr string
	}{
		{
			name:    "all non-negative pass through",
			stats:   scraper.ChannelStats{SubscriberCount: 100, VideoCount: 20, ViewCount: 3000},
			wantSub: 100, wantVid: 20, wantViw: 3000,
		},
		{
			name:    "all zero pass through",
			stats:   scraper.ChannelStats{},
			wantSub: 0, wantVid: 0, wantViw: 0,
		},
		{
			name:    "negative subscriber rejected first",
			stats:   scraper.ChannelStats{SubscriberCount: -1, VideoCount: -2, ViewCount: -3},
			wantErr: "subscriber",
		},
		{
			name:    "negative video rejected when subscriber ok",
			stats:   scraper.ChannelStats{SubscriberCount: 10, VideoCount: -2, ViewCount: 3},
			wantErr: "video",
		},
		{
			name:    "negative view rejected when others ok",
			stats:   scraper.ChannelStats{SubscriberCount: 10, VideoCount: 2, ViewCount: -3},
			wantErr: "view",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stats := tt.stats
			sub, vid, viw, err := validatedScrapedChannelCounts("UC1", &stats)
			assertValidatedScrapedChannelCounts(t, &tt.stats, sub, vid, viw, err, tt.wantSub, tt.wantVid, tt.wantViw, tt.wantErr)
		})
	}
}

func assertValidatedScrapedChannelCounts(t *testing.T, stats *scraper.ChannelStats, sub, vid, viw uint64, err error, wantSub, wantVid, wantViw uint64, wantErr string) {
	t.Helper()

	if wantErr != "" {
		if err == nil {
			t.Fatalf("validatedScrapedChannelCounts(%+v) expected error containing %q, got nil", *stats, wantErr)
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("error %q does not contain %q", err.Error(), wantErr)
		}
		if sub != 0 || vid != 0 || viw != 0 {
			t.Fatalf("on error counts must be zeroed, got sub=%d vid=%d viw=%d", sub, vid, viw)
		}
		return
	}
	if err != nil {
		t.Fatalf("validatedScrapedChannelCounts(%+v) unexpected error: %v", *stats, err)
	}
	if sub != wantSub || vid != wantVid || viw != wantViw {
		t.Fatalf("counts = (%d,%d,%d), want (%d,%d,%d)", sub, vid, viw, wantSub, wantVid, wantViw)
	}
}

func TestChannelStatsFromScraped_MapsFieldsAndUsesScrapedChannelID(t *testing.T) {
	t.Parallel()

	ys := newTestService(t, nil)
	scraped := &scraper.ChannelStats{
		ChannelID:       "UC_scraped",
		SubscriberCount: 1000,
		VideoCount:      50,
		ViewCount:       1_000_000,
		Handle:          "@handle",
	}

	got, err := ys.channelStatsFromScraped("UC_lookup", scraped)
	if err != nil {
		t.Fatalf("channelStatsFromScraped() unexpected error: %v", err)
	}
	if got.ChannelID != "UC_scraped" {
		t.Fatalf("ChannelID = %q, want %q (must come from scraped stats)", got.ChannelID, "UC_scraped")
	}
	if got.SubscriberCount != 1000 || got.VideoCount != 50 || got.ViewCount != 1_000_000 {
		t.Fatalf("counts = (%d,%d,%d), want (1000,50,1000000)", got.SubscriberCount, got.VideoCount, got.ViewCount)
	}
	if got.Timestamp.IsZero() {
		t.Fatal("Timestamp should be set")
	}
}

func TestChannelStatsFromScraped_FallsBackToHandleWhenChannelNameUnknown(t *testing.T) {
	t.Parallel()

	ys := newTestService(t, nil)
	got, err := ys.channelStatsFromScraped("UC_lookup", &scraper.ChannelStats{Handle: "@onlyhandle"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ChannelTitle != "@onlyhandle" {
		t.Fatalf("ChannelTitle = %q, want fallback handle %q", got.ChannelTitle, "@onlyhandle")
	}
}

func TestChannelStatsFromScraped_PrefersCachedMemberNameOverHandle(t *testing.T) {
	t.Parallel()

	ys := newTestService(t, map[string]string{"UC_lookup": "ときのそら"})
	got, err := ys.channelStatsFromScraped("UC_lookup", &scraper.ChannelStats{Handle: "@handle"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ChannelTitle != "ときのそら" {
		t.Fatalf("ChannelTitle = %q, want cached name %q", got.ChannelTitle, "ときのそら")
	}
}

func TestChannelStatsFromScraped_RejectsNegativeCounts(t *testing.T) {
	t.Parallel()

	ys := newTestService(t, nil)
	got, err := ys.channelStatsFromScraped("UC_lookup", &scraper.ChannelStats{SubscriberCount: -1})
	if err == nil {
		t.Fatalf("expected error for negative subscriber count, got %+v", got)
	}
	if got != nil {
		t.Fatalf("expected nil stats on error, got %+v", got)
	}
}

func TestResolveChannelTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		channelToName map[string]string
		channelID     string
		fallbackTitle string
		want          string
	}{
		{
			name:          "uses cached name when present",
			channelToName: map[string]string{"UC1": "Member A"},
			channelID:     "UC1",
			fallbackTitle: "@fallback",
			want:          "Member A",
		},
		{
			name:          "uses fallback when channel not cached",
			channelToName: map[string]string{"UC2": "Member B"},
			channelID:     "UC1",
			fallbackTitle: "@fallback",
			want:          "@fallback",
		},
		{
			name:          "uses fallback when cached name is empty",
			channelToName: map[string]string{"UC1": ""},
			channelID:     "UC1",
			fallbackTitle: "@fallback",
			want:          "@fallback",
		},
		{
			name:          "empty fallback yields empty title",
			channelToName: map[string]string{},
			channelID:     "UC1",
			fallbackTitle: "",
			want:          "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ys := newTestService(t, tt.channelToName)
			got := ys.resolveChannelTitle(tt.channelID, tt.fallbackTitle)
			if got != tt.want {
				t.Fatalf("resolveChannelTitle(%q, %q) = %q, want %q", tt.channelID, tt.fallbackTitle, got, tt.want)
			}
		})
	}
}

func TestGetChannelStatistics_EmptyChannelIDs(t *testing.T) {
	t.Parallel()

	ys := newTestService(t, nil)

	for _, ids := range [][]string{nil, {}} {
		got, err := ys.GetChannelStatistics(context.Background(), ids)
		if err != nil {
			t.Fatalf("GetChannelStatistics(%v) unexpected error: %v", ids, err)
		}
		if got == nil {
			t.Fatalf("GetChannelStatistics(%v) = nil map, want non-nil empty map", ids)
		}
		if len(got) != 0 {
			t.Fatalf("GetChannelStatistics(%v) len = %d, want 0", ids, len(got))
		}
	}
}
