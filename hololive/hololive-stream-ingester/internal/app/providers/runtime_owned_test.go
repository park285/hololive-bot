package providers

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

func TestProvideYouTubeStack_QuotaDisabledReturnsStatsOnly(t *testing.T) {
	t.Parallel()

	statsRepo := &stats.StatsRepository{}

	got := ProvideYouTubeStack(context.Background(), config.YouTubeConfig{EnableQuotaBuilding: false}, config.ScraperConfig{}, nil, nil, nil, statsRepo, nil, nil, nil, nil, slog.New(slog.DiscardHandler))
	if got == nil {
		t.Fatal("ProvideYouTubeStack() returned nil")
	}
	if got.Service != nil {
		t.Fatal("ProvideYouTubeStack().Service = non-nil, want nil")
	}
	if got.Scheduler != nil {
		t.Fatal("ProvideYouTubeStack().Scheduler = non-nil, want nil")
	}
	if got.StatsRepo != statsRepo {
		t.Fatalf("ProvideYouTubeStack().StatsRepo = %#v, want %#v", got.StatsRepo, statsRepo)
	}

	var _ *sharedproviders.YouTubeStack = got
}
