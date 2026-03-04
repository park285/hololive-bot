package matcher

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newMatcherTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewMemberMatcher_Defaults(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{{ChannelID: "ch1", Name: "m1"}})
	matcher := NewMemberMatcher(nil, provider, &cachemocks.Client{}, nil, nil, newMatcherTestLogger())

	require.NotNil(t, matcher)
	require.NotNil(t, matcher.ctx)
	assert.Equal(t, provider, matcher.membersData)
	assert.NotNil(t, matcher.matchCache)
	assert.Equal(t, 1, len(matcher.GetAllMembers()))
}

func TestTryExactValkeyMatch_PrefersHololiveCandidate(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "ch-niji", Name: "Aqua", Org: "Nijisanji"},
		{ChannelID: "ch-holo", Name: "Aqua", Org: "Hololive"},
	})
	matcher := &MemberMatcher{logger: newMatcherTestLogger()}

	candidate := matcher.tryExactValkeyMatch(provider, "Aqua", map[string]string{
		"aqua_main": "ch-niji",
		"AQUA":      "ch-holo",
	})

	require.NotNil(t, candidate)
	assert.Equal(t, "ch-holo", candidate.channelID)
	assert.Equal(t, "Aqua", candidate.memberName)
	assert.Equal(t, "valkey-exact", candidate.source)
}

func TestTryPartialValkeyAndAliasMatch(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "ch-sui", Name: "Hoshimachi Suisei", Aliases: &domain.Aliases{Ko: []string{"스이", "호시마치"}}},
	})
	matcher := &MemberMatcher{logger: newMatcherTestLogger()}

	partialValkey := matcher.tryPartialValkeyMatch(provider, "hoshi", map[string]string{
		"Hoshimachi Suisei": "ch-sui",
	})
	require.NotNil(t, partialValkey)
	assert.Equal(t, "ch-sui", partialValkey.channelID)
	assert.Equal(t, "valkey-partial", partialValkey.source)

	partialAlias := matcher.tryPartialAliasMatch(provider, "호시")
	require.NotNil(t, partialAlias)
	assert.Equal(t, "ch-sui", partialAlias.channelID)
	assert.Equal(t, "alias-partial", partialAlias.source)
}

func TestFinalizeCandidate_EmptyChannelID(t *testing.T) {
	t.Parallel()

	matcher := &MemberMatcher{logger: newMatcherTestLogger()}

	channel, err := matcher.finalizeCandidate(context.Background(), &matchCandidate{
		memberName: "missing-id",
		source:     "test",
	})
	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestLoadDynamicMembers_ErrorFallback(t *testing.T) {
	t.Parallel()

	matcher := &MemberMatcher{
		logger: newMatcherTestLogger(),
		cache: &cachemocks.Client{
			GetAllMembersFunc: func(context.Context) (map[string]string, error) {
				return nil, errors.New("cache down")
			},
		},
	}

	members := matcher.loadDynamicMembers(context.Background())
	require.NotNil(t, members)
	assert.Empty(t, members)
}

func TestFindBestMatch_UsesDynamicStrategyAndCache(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider(nil)
	cacheCalls := 0
	cacheSvc := &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			cacheCalls++
			return map[string]string{"Aqua": "ch-aqua"}, nil
		},
	}
	matcher := NewMemberMatcher(context.Background(), provider, cacheSvc, nil, nil, newMatcherTestLogger())

	first, err := matcher.FindBestMatch(context.Background(), "Aqua")
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, "ch-aqua", first.ID)
	assert.Equal(t, "Aqua", first.Name)

	second, err := matcher.FindBestMatch(context.Background(), "Aqua")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, "ch-aqua", second.ID)
	assert.Equal(t, 1, cacheCalls)
}

func TestFindBestMatchWithCandidates_AmbiguousAndOrgFilter(t *testing.T) {
	t.Parallel()

	cacheSvc := &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{
				"Aqua:Hololive":  "ch-holo",
				"Aqua:Nijisanji": "ch-niji",
			}, nil
		},
	}
	matcher := NewMemberMatcher(context.Background(), newStubMemberProvider(nil), cacheSvc, nil, nil, newMatcherTestLogger())

	channel, err := matcher.FindBestMatchWithCandidates(context.Background(), "Aqua")
	require.Error(t, err)
	assert.Nil(t, channel)
	var ambiguous *AmbiguousMatchError
	require.ErrorAs(t, err, &ambiguous)
	require.Len(t, ambiguous.Candidates, 2)

	filtered, err := matcher.FindBestMatchWithCandidates(context.Background(), "Aqua (Hololive)")
	require.NoError(t, err)
	require.NotNil(t, filtered)
	assert.Equal(t, "ch-holo", filtered.ID)
	if assert.NotNil(t, filtered.Org) {
		assert.Equal(t, "Hololive", *filtered.Org)
	}
}

func TestFindBestMatchWithCandidates_FallbackAndErrors(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{{
		ChannelID: "ch-sora",
		Name:      "Tokino Sora",
		Aliases:   &domain.Aliases{Ja: []string{"Sora"}},
	}})

	t.Run("cache error", func(t *testing.T) {
		matcher := NewMemberMatcher(context.Background(), provider, &cachemocks.Client{
			GetAllMembersFunc: func(context.Context) (map[string]string, error) {
				return nil, errors.New("cache error")
			},
		}, nil, nil, newMatcherTestLogger())

		channel, err := matcher.FindBestMatchWithCandidates(context.Background(), "Sora")
		require.Error(t, err)
		assert.Nil(t, channel)
		assert.Contains(t, err.Error(), "get all members")
	})

	t.Run("fallback to FindBestMatch", func(t *testing.T) {
		cacheSvc := &cachemocks.Client{
			GetAllMembersFunc: func(context.Context) (map[string]string, error) {
				return map[string]string{}, nil
			},
		}
		matcher := NewMemberMatcher(context.Background(), provider, cacheSvc, nil, nil, newMatcherTestLogger())

		channel, err := matcher.FindBestMatchWithCandidates(context.Background(), "Sora")
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, "ch-sora", channel.ID)
	})
}
