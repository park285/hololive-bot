package pollers

import (
	"context"
	"testing"

	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
)

func TestMetadataResolveBudgetAllowsOneRequestPerPoll(t *testing.T) {
	ctx := withMetadataResolveBudget(context.Background(), "shorts", nil)
	if !takeMetadataResolve(ctx) {
		t.Fatal("first metadata resolve was deferred")
	}
	if takeMetadataResolve(ctx) {
		t.Fatal("second metadata resolve exceeded the per-poll allowance")
	}
}

func TestBudgetDeferralDoesNotConsumeFreshnessFailureAttempt(t *testing.T) {
	deferrals := newFreshnessDeferrals()
	for range 5 {
		deferrals.recordBudgetDeferral("channel", "video")
	}
	if attempt := deferrals.recordFailure("channel", "video"); attempt != 1 {
		t.Fatalf("first real resolution failure attempt = %d, want 1", attempt)
	}
}

func TestMetadataRequestUnitsMatchRuntimeAllowance(t *testing.T) {
	wantVideos := float64(scraper.FetchPageMaxAttempts * (2 + metadataResolvePerPoll))
	if got := VideosWorstCaseRequestUnits(); got != wantVideos {
		t.Fatalf("video units = %v, want %v", got, wantVideos)
	}
	wantShorts := float64(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts * (1 + metadataResolvePerPoll))
	if got := ShortsWorstCaseRequestUnits(); got != wantShorts {
		t.Fatalf("short units = %v, want %v", got, wantShorts)
	}
}
