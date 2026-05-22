// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package matcher

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMatcherTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewMatcher_Defaults(t *testing.T) {
	t.Parallel()

	var baseCtx context.Context
	provider := newStubMemberProvider([]*domain.Member{{ChannelID: "ch1", Name: "m1"}})
	//nolint:staticcheck // nil context path is the behavior under test
	matcher := NewMatcher(baseCtx, provider, &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}, nil, nil, newMatcherTestLogger())

	require.NotNil(t, matcher)
	assert.Nil(t, matcher.ctx)
	assert.Equal(t, provider, matcher.membersData)
	assert.NotNil(t, matcher.matchCache)
	assert.Len(t, matcher.GetAllMembers(), 1)
}

type trackingMemberProvider struct {
	members    []*domain.Member
	member     *domain.Member
	ctx        context.Context
	ctxCalls   *[]context.Context
	channelIDs []string
}

type matcherTestContextKey struct{}

func newTrackingMemberProvider(members []*domain.Member) *trackingMemberProvider {
	member := &domain.Member{}

	if len(members) > 0 && members[0] != nil {
		member = members[0]
	}

	ctxCalls := make([]context.Context, 0, 4)
	channelIDs := make([]string, 0, 4)

	return &trackingMemberProvider{
		members:    members,
		member:     member,
		ctxCalls:   &ctxCalls,
		channelIDs: channelIDs,
	}
}

func (p *trackingMemberProvider) FindMemberByChannelID(channelID string) *domain.Member {
	p.channelIDs = append(p.channelIDs, channelID)
	return p.member
}

func (p *trackingMemberProvider) FindMemberByName(string) *domain.Member {
	return p.member
}

func (p *trackingMemberProvider) FindMemberByAlias(string) *domain.Member {
	return p.member
}

func (p *trackingMemberProvider) GetChannelIDs() []string {
	return nil
}

func (p *trackingMemberProvider) GetAllMembers() []*domain.Member {
	return p.members
}

func (p *trackingMemberProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	*p.ctxCalls = append(*p.ctxCalls, ctx)

	return &trackingMemberProvider{
		members:    p.members,
		member:     p.member,
		ctx:        ctx,
		ctxCalls:   p.ctxCalls,
		channelIDs: p.channelIDs,
	}
}

func (p *trackingMemberProvider) FindMembersByName(string) []*domain.Member {
	return nil
}

func (p *trackingMemberProvider) FindMembersByAlias(string) []*domain.Member {
	return nil
}

type errorAwareMemberProvider struct {
	*stubMemberProvider

	members   []*domain.Member
	loadErr   error
	failLoads int
	loadCalls int
}

func newErrorAwareMemberProvider(members []*domain.Member, failLoads int, loadErr error) *errorAwareMemberProvider {
	return &errorAwareMemberProvider{
		stubMemberProvider: newStubMemberProvider(members),
		members:            members,
		loadErr:            loadErr,
		failLoads:          failLoads,
	}
}

func (p *errorAwareMemberProvider) LoadAllMembers() ([]*domain.Member, error) {
	p.loadCalls++
	if p.loadCalls <= p.failLoads {
		return nil, p.loadErr
	}

	return p.members, nil
}

func (p *errorAwareMemberProvider) WithContext(context.Context) domain.MemberDataProvider {
	return p
}

func TestGetAllMembers_DoesNotInjectBackgroundContext(t *testing.T) {
	t.Parallel()

	var baseCtx context.Context
	provider := newTrackingMemberProvider([]*domain.Member{{ChannelID: "ch1", Name: "m1"}})
	//nolint:staticcheck // nil context path is the behavior under test
	matcher := NewMatcher(baseCtx, provider, &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}, nil, nil, newMatcherTestLogger())

	members := matcher.GetAllMembers()
	require.Len(t, members, 1)
	assert.Empty(t, *provider.ctxCalls)
}

func TestGetMemberByChannelID_UsesRequestContext(t *testing.T) {
	t.Parallel()

	provider := newTrackingMemberProvider([]*domain.Member{{ChannelID: "ch1", Name: "m1"}})
	var baseCtx context.Context
	//nolint:staticcheck // nil context path is the behavior under test
	matcher := NewMatcher(baseCtx, provider, &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}, nil, nil, newMatcherTestLogger())
	reqCtx := context.WithValue(t.Context(), matcherTestContextKey{}, "request")

	member := matcher.GetMemberByChannelID(reqCtx, "ch1")
	require.NotNil(t, member)
	require.Len(t, *provider.ctxCalls, 1)
	assert.True(t, (*provider.ctxCalls)[0] == reqCtx)
}

func TestTryExactValkeyMatch_PrefersHololiveCandidate(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "ch-niji", Name: "Aqua", Org: "Nijisanji"},
		{ChannelID: "ch-holo", Name: "Aqua", Org: "Hololive"},
	})
	matcher := &Matcher{logger: newMatcherTestLogger()}

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
	matcher := &Matcher{logger: newMatcherTestLogger()}

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

	matcher := &Matcher{logger: newMatcherTestLogger()}

	channel, err := matcher.finalizeCandidate(t.Context(), &matchCandidate{
		memberName: "missing-id",
		source:     "test",
	})
	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestLoadDynamicMembers_ErrorFallback(t *testing.T) {
	t.Parallel()

	matcher := &Matcher{
		logger: newMatcherTestLogger(),
		cache: &cachemocks.Client{
			GetAllMembersFunc: func(context.Context) (map[string]string, error) {
				return nil, errors.New("cache down")
			},
		},
	}

	members := matcher.loadDynamicMembers(t.Context())
	require.NotNil(t, members)
	assert.Empty(t, members)
}

func TestFindBestMatch_UsesDynamicStrategyAndCache(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider(nil)
	cacheCalls := 0
	cache := &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			cacheCalls++
			return map[string]string{"Aqua": "ch-aqua"}, nil
		},
	}
	matcher := NewMatcher(t.Context(), provider, cache, nil, nil, newMatcherTestLogger())

	first, err := matcher.FindBestMatch(t.Context(), "Aqua")
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, "ch-aqua", first.ID)
	assert.Equal(t, "Aqua", first.Name)

	second, err := matcher.FindBestMatch(t.Context(), "Aqua")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, "ch-aqua", second.ID)
	assert.Equal(t, 1, cacheCalls)
}

func TestFindBestMatch_UsesSnapshotAcrossDifferentQueries(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider(nil)
	cacheCalls := 0
	cache := &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			cacheCalls++

			return map[string]string{
				"Aqua:Hololive":   "ch-aqua",
				"Marine:Hololive": "ch-marine",
			}, nil
		},
	}
	matcher := NewMatcher(t.Context(), provider, cache, nil, nil, newMatcherTestLogger())

	first, err := matcher.FindBestMatch(t.Context(), "Aqua")
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, "ch-aqua", first.ID)

	second, err := matcher.FindBestMatch(t.Context(), "Marine")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, "ch-marine", second.ID)

	assert.Equal(t, 1, cacheCalls)
}

func TestFindBestMatch_ProviderLoadErrorIsNotCached(t *testing.T) {
	t.Parallel()

	provider := newErrorAwareMemberProvider([]*domain.Member{
		{ChannelID: "ch-aqua", Name: "Aqua"},
	}, 1, errors.New("member repo down"))
	cache := &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}
	matcher := NewMatcher(t.Context(), provider, cache, nil, nil, newMatcherTestLogger())

	channel, err := matcher.FindBestMatch(t.Context(), "Aqua")
	require.Error(t, err)
	assert.Nil(t, channel)
	assert.Contains(t, err.Error(), "get all members")

	channel, err = matcher.FindBestMatch(t.Context(), "Aqua")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-aqua", channel.ID)
}

func TestFindBestMatch_UsesSnapshotAliasIndex(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{{
		ChannelID: "ch-sora",
		Name:      "Tokino Sora",
		NameJa:    "ときのそら",
		NameKo:    "토키노 소라",
		Aliases:   &domain.Aliases{Ja: []string{"そらちゃん"}},
	}})
	matcher := NewMatcher(t.Context(), provider, &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}, nil, nil, newMatcherTestLogger())

	channel, err := matcher.FindBestMatch(t.Context(), "そらちゃん")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-sora", channel.ID)

	channel, err = matcher.FindBestMatch(t.Context(), "토키노 소라")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-sora", channel.ID)
}

func TestFindBestMatch_PrefersAliasExactBeforeNameExact(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{
		{
			ChannelID: "ch-name",
			Name:      "Suisei",
		},
		{
			ChannelID: "ch-alias",
			Name:      "Hoshimachi Suisei",
			Aliases:   &domain.Aliases{Ja: []string{"Suisei"}},
		},
	})
	matcher := NewMatcher(t.Context(), provider, &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}, nil, nil, newMatcherTestLogger())

	channel, err := matcher.FindBestMatch(t.Context(), "Suisei")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-alias", channel.ID)
}

func TestFindBestMatchWithCandidates_DynamicLoadErrorIsNotSticky(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider(nil)
	cacheCalls := 0
	cache := &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			cacheCalls++
			if cacheCalls == 1 {
				return nil, errors.New("temporary cache error")
			}

			return map[string]string{
				"Aqua:Hololive": "ch-holo",
			}, nil
		},
	}
	matcher := NewMatcher(t.Context(), provider, cache, nil, nil, newMatcherTestLogger())

	channel, err := matcher.FindBestMatchWithCandidates(t.Context(), "Aqua")
	require.Error(t, err)
	assert.Nil(t, channel)
	assert.Contains(t, err.Error(), "get all members")

	channel, err = matcher.FindBestMatchWithCandidates(t.Context(), "Aqua")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-holo", channel.ID)
}

func TestFindBestMatchWithCandidates_AmbiguousAndOrgFilter(t *testing.T) {
	t.Parallel()

	cache := &cachemocks.Client{
		GetAllMembersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{
				"Aqua:Hololive":  "ch-holo",
				"Aqua:Nijisanji": "ch-niji",
			}, nil
		},
	}
	matcher := NewMatcher(t.Context(), newStubMemberProvider(nil), cache, nil, nil, newMatcherTestLogger())

	channel, err := matcher.FindBestMatchWithCandidates(t.Context(), "Aqua")
	require.Error(t, err)
	assert.Nil(t, channel)

	var ambiguous *AmbiguousMatchError
	require.ErrorAs(t, err, &ambiguous)
	require.Len(t, ambiguous.Candidates, 2)

	filtered, err := matcher.FindBestMatchWithCandidates(t.Context(), "Aqua (Hololive)")
	require.NoError(t, err)
	require.NotNil(t, filtered)
	assert.Equal(t, "ch-holo", filtered.ID)

	if assert.NotNil(t, filtered.Org) {
		assert.Equal(t, "Hololive", *filtered.Org)
	}
}

func TestExactNameMembers_FiltersOrg(t *testing.T) {
	t.Parallel()

	matcher := &Matcher{logger: newMatcherTestLogger()}
	snapshot := &matcherSnapshot{
		exactNames: map[string][]*snapshotEntry{
			"aqua": {
				{candidate: &matchCandidate{channelID: "ch-holo", memberName: "Aqua", org: "Hololive"}},
				{candidate: &matchCandidate{channelID: "ch-niji", memberName: "Aqua", org: "Nijisanji"}},
			},
		},
	}

	candidates := matcher.exactNameMembers(snapshot, "aqua", "Hololive")
	require.Len(t, candidates, 1)
	assert.Equal(t, "ch-holo", candidates[0].ChannelID)
	assert.Equal(t, "Hololive", candidates[0].Org)
}

func TestFindBestMatchWithCandidates_FallbackAndErrors(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{{
		ChannelID: "ch-sora",
		Name:      "Tokino Sora",
		Aliases:   &domain.Aliases{Ja: []string{"Sora"}},
	}})

	t.Run("cache error", func(t *testing.T) {
		matcher := NewMatcher(t.Context(), provider, &cachemocks.Client{
			GetAllMembersFunc: func(context.Context) (map[string]string, error) {
				return nil, errors.New("cache error")
			},
		}, nil, nil, newMatcherTestLogger())

		channel, err := matcher.FindBestMatchWithCandidates(t.Context(), "Sora")
		require.Error(t, err)
		assert.Nil(t, channel)
		assert.Contains(t, err.Error(), "get all members")
	})

	t.Run("fallback to FindBestMatch", func(t *testing.T) {
		cache := &cachemocks.Client{
			GetAllMembersFunc: func(context.Context) (map[string]string, error) {
				return map[string]string{}, nil
			},
		}
		matcher := NewMatcher(t.Context(), provider, cache, nil, nil, newMatcherTestLogger())

		channel, err := matcher.FindBestMatchWithCandidates(t.Context(), "Sora")
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, "ch-sora", channel.ID)
	})
}
