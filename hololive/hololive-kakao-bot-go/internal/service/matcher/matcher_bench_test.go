package matcher

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func BenchmarkFindBestMatchCacheHit(b *testing.B) {
	ctx := context.Background()
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
		b.Fatalf("warmup FindBestMatch: %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		channel, err := mm.FindBestMatch(ctx, "sora")
		if err != nil || channel == nil {
			b.Fatalf("FindBestMatch = (%v, %v), want cached channel", channel, err)
		}
	}
}
