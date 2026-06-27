package matcher

import (
	"context"
	"runtime"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newBenchMatcher(tb testing.TB, ctx context.Context) *Matcher {
	tb.Helper()

	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "UC-ch1", Name: "sora"},
		{ChannelID: "UC-ch2", Name: "miko"},
	})
	mm := NewMatcher(ctx, provider, &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}, nil, nil, newMatcherTestLogger())

	if _, err := mm.FindBestMatch(ctx, "sora"); err != nil {
		tb.Fatalf("warmup FindBestMatch: %v", err)
	}

	return mm
}

func TestFindBestMatchCacheHitAllocationBudget(t *testing.T) {
	ctx := context.Background()
	mm := newBenchMatcher(t, ctx)

	runtime.GC()

	allocs := testing.AllocsPerRun(1000, func() {
		channel, err := mm.FindBestMatch(ctx, "sora")
		if err != nil || channel == nil {
			t.Fatalf("FindBestMatch = (%v, %v), want cached channel", channel, err)
		}
	})
	if allocs > 2 {
		t.Errorf("FindBestMatch cache hit allocs/op = %.1f, want <= 2", allocs)
	}
}

func BenchmarkFindBestMatchCacheHit(b *testing.B) {
	ctx := context.Background()
	mm := newBenchMatcher(b, ctx)

	b.ReportAllocs()
	for b.Loop() {
		channel, err := mm.FindBestMatch(ctx, "sora")
		if err != nil || channel == nil {
			b.Fatalf("FindBestMatch = (%v, %v), want cached channel", channel, err)
		}
	}
}
