package matcher

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestStaticExactMatchesSurviveDynamicCacheFailure(t *testing.T) {
	cacheFailure := errors.New("synthetic cache unavailable")
	cache := cachemocks.NewLenientClient()

	cache.GetAllMembersFunc = func(context.Context) (map[string]string, error) { return nil, cacheFailure }

	provider := newStubMemberProvider([]*domain.Member{{Name: testMemberAqua, ChannelID: testChannelHolo, Org: orgHololive}})
	matcher := NewMatcher(t.Context(), provider, cache, nil, nil, newMatcherTestLogger())

	for _, query := range []string{testMemberAqua, "Aqua (Hololive)"} {
		channel, found, err := matcher.FindBestMatchWithCandidates(t.Context(), query)
		require.NoError(t, err)
		require.True(t, found)

		if channel == nil {
			t.Fatal("static match must return its channel during a cache outage")
		}

		require.Equal(t, testChannelHolo, channel.ID)
	}

	channel, found, err := matcher.FindBestMatch(t.Context(), testMemberAqua)
	require.NoError(t, err)
	require.True(t, found)

	if channel == nil {
		t.Fatal("static match must return its channel during a cache outage")
	}

	require.Equal(t, testChannelHolo, channel.ID)

	for _, query := range []string{"unknown", "Aq"} {
		channel, found, err = matcher.FindBestMatch(t.Context(), query)
		require.ErrorIs(t, err, cacheFailure)
		require.False(t, found)
		require.Nil(t, channel)

		channel, found, err = matcher.FindBestMatchWithCandidates(t.Context(), query)
		require.ErrorIs(t, err, cacheFailure)
		require.False(t, found)
		require.Nil(t, channel)
	}
}
