package matcher

import (
	"runtime"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newBenchMatcher(tb testing.TB) *Matcher {
	tb.Helper()

	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "UC-ch1", Name: "sora"},
		{ChannelID: "UC-ch2", Name: "miko"},
	})
	mm := NewMatcher(provider, nil, newMatcherTestLogger())

	if _, _, err := mm.FindBestMatch(tb.Context(), "sora"); err != nil {
		tb.Fatalf("warmup FindBestMatch: %v", err)
	}

	return mm
}

func TestFindBestMatchCacheHitAllocationBudget(t *testing.T) {
	ctx := t.Context()
	mm := newBenchMatcher(t)

	runtime.GC()

	allocs := testing.AllocsPerRun(1000, func() {
		channel, found, err := mm.FindBestMatch(ctx, "sora")
		if err != nil || !found {
			t.Fatalf("FindBestMatch = (%v, %v), want cached channel", channel, err)
		}
	})
	if allocs > 2 {
		t.Errorf("FindBestMatch cache hit allocs/op = %.1f, want <= 2", allocs)
	}
}

func BenchmarkFindBestMatchCacheHit(b *testing.B) {
	ctx := b.Context()
	mm := newBenchMatcher(b)

	b.ReportAllocs()

	for b.Loop() {
		channel, found, err := mm.FindBestMatch(ctx, "sora")
		if err != nil || !found {
			b.Fatalf("FindBestMatch = (%v, %v), want cached channel", channel, err)
		}
	}
}
