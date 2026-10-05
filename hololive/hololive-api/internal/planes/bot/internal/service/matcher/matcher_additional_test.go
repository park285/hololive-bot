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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newMatcherTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewMatcher_Defaults(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{{ChannelID: testChannelID1, Name: "m1"}})

	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	require.NotNil(t, matcher)
	assert.Equal(t, provider, matcher.membersData)
	assert.NotNil(t, matcher.matchCache)
}

type trackingMemberProvider struct {
	members    []*domain.Member
	member     *domain.Member
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

func (p *trackingMemberProvider) record(ctx context.Context) {
	*p.ctxCalls = append(*p.ctxCalls, ctx)
}

func (p *trackingMemberProvider) FindMemberByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	p.record(ctx)

	p.channelIDs = append(p.channelIDs, channelID)

	return p.member, nil
}

func (p *trackingMemberProvider) FindMemberByName(ctx context.Context, _ string) (*domain.Member, error) {
	p.record(ctx)

	return p.member, nil
}

func (p *trackingMemberProvider) FindMemberByAlias(ctx context.Context, _ string) (*domain.Member, error) {
	p.record(ctx)

	return p.member, nil
}

func (p *trackingMemberProvider) GetChannelIDs(ctx context.Context) ([]string, error) {
	p.record(ctx)

	return []string{}, nil
}

func (p *trackingMemberProvider) LoadAllMembers(ctx context.Context) ([]*domain.Member, error) {
	p.record(ctx)

	return p.members, nil
}

func (p *trackingMemberProvider) FindMembersByName(ctx context.Context, _ string) ([]*domain.Member, error) {
	p.record(ctx)

	return []*domain.Member{}, nil
}

func (p *trackingMemberProvider) FindMembersByAlias(ctx context.Context, _ string) ([]*domain.Member, error) {
	p.record(ctx)

	return []*domain.Member{}, nil
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

func (p *errorAwareMemberProvider) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	p.loadCalls++
	if p.loadCalls <= p.failLoads {
		return nil, p.loadErr
	}

	return p.members, nil
}

func TestGetMemberByChannelID_UsesRequestContext(t *testing.T) {
	t.Parallel()

	provider := newTrackingMemberProvider([]*domain.Member{{ChannelID: testChannelID1, Name: "m1"}})

	matcher := NewMatcher(provider, nil, newMatcherTestLogger())
	reqCtx := context.WithValue(t.Context(), matcherTestContextKey{}, "request")

	member, err := matcher.GetMemberByChannelID(reqCtx, testChannelID1)
	require.NoError(t, err)
	require.NotNil(t, member)
	require.NotEmpty(t, *provider.ctxCalls)
	assert.Equal(t, reqCtx, (*provider.ctxCalls)[len(*provider.ctxCalls)-1])
}

// 채널 대표 조회는 미존재와 원천 실패를 구분해 돌려준다.
func TestGetMemberByChannelID_SeparatesNotFoundFromFailure(t *testing.T) {
	t.Parallel()

	matcher := NewMatcher(newStubMemberProvider(nil), nil, newMatcherTestLogger())

	if _, err := matcher.GetMemberByChannelID(t.Context(), "missing"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("missing channel error = %v, want domain.ErrMemberNotFound", err)
	}

	cause := errors.New("member cache unavailable")
	failing := NewMatcher(&channelFailureProvider{stubMemberProvider: newStubMemberProvider(nil), err: cause}, nil, newMatcherTestLogger())

	_, err := failing.GetMemberByChannelID(t.Context(), "any")
	if !errors.Is(err, cause) || errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("failing lookup error = %v, want backend cause", err)
	}
}

type channelFailureProvider struct {
	*stubMemberProvider

	err error
}

func (p *channelFailureProvider) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, p.err
}

func TestFinalizeCandidate_EmptyChannelID(t *testing.T) {
	t.Parallel()

	matcher := &Matcher{logger: newMatcherTestLogger()}

	channel := matcher.finalizeCandidate(&matchCandidate{
		memberName: "missing-id",
		source:     "test",
	})
	assert.Nil(t, channel)
}

func TestFindBestMatch_PrefersHololiveOnDuplicateExactName(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "ch-niji", Name: testMemberAqua, Org: "Nijisanji"},
		{ChannelID: testChannelHolo, Name: testMemberAqua, Org: orgHololive},
	})
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	channel, found, err := matcher.FindBestMatch(t.Context(), testMemberAqua)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, channel)
	assert.Equal(t, testChannelHolo, channel.ID)
	require.NotNil(t, channel.Org)
	assert.Equal(t, orgHololive, *channel.Org)
}

func TestFindBestMatch_CachesResultWithoutReloadingProvider(t *testing.T) {
	t.Parallel()

	provider := newErrorAwareMemberProvider([]*domain.Member{
		{ChannelID: "ch-aqua", Name: testMemberAqua, Org: orgHololive},
	}, 0, nil)
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	first, found, err := matcher.FindBestMatch(t.Context(), testMemberAqua)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, first)
	assert.Equal(t, "ch-aqua", first.ID)
	assert.Equal(t, testMemberAqua, first.Name)

	second, found, err := matcher.FindBestMatch(t.Context(), testMemberAqua)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, second)
	assert.Equal(t, "ch-aqua", second.ID)
	assert.Equal(t, 1, provider.loadCalls)
}

func TestFindBestMatch_UsesSnapshotAcrossDifferentQueries(t *testing.T) {
	t.Parallel()

	provider := newErrorAwareMemberProvider([]*domain.Member{
		{ChannelID: "ch-aqua", Name: testMemberAqua, Org: orgHololive},
		{ChannelID: "ch-marine", Name: "Marine", Org: orgHololive},
	}, 0, nil)
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	first, found, err := matcher.FindBestMatch(t.Context(), testMemberAqua)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, first)
	assert.Equal(t, "ch-aqua", first.ID)

	second, found, err := matcher.FindBestMatch(t.Context(), "Marine")
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, second)
	assert.Equal(t, "ch-marine", second.ID)

	assert.Equal(t, 1, provider.loadCalls)
}

func TestFindBestMatch_ProviderLoadErrorIsNotCached(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("member repo down")
	provider := newErrorAwareMemberProvider([]*domain.Member{
		{ChannelID: "ch-aqua", Name: testMemberAqua},
	}, 1, loadErr)
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	channel, found, err := matcher.FindBestMatch(t.Context(), testMemberAqua)
	require.ErrorIs(t, err, loadErr)
	assert.False(t, found)
	assert.Nil(t, channel)

	channel, found, err = matcher.FindBestMatch(t.Context(), testMemberAqua)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-aqua", channel.ID)
	assert.Equal(t, 2, provider.loadCalls)
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
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	channel, found, err := matcher.FindBestMatch(t.Context(), "そらちゃん")
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-sora", channel.ID)

	channel, found, err = matcher.FindBestMatch(t.Context(), "토키노 소라")
	require.NoError(t, err)
	require.True(t, found)
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
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	channel, found, err := matcher.FindBestMatch(t.Context(), "Suisei")
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-alias", channel.ID)
}

func TestFindBestMatchWithCandidates_ProviderLoadErrorIsNotSticky(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("temporary member repo error")
	provider := newErrorAwareMemberProvider([]*domain.Member{
		{ChannelID: testChannelHolo, Name: testMemberAqua, Org: orgHololive},
	}, 1, loadErr)
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	channel, found, err := matcher.FindBestMatchWithCandidates(t.Context(), testMemberAqua)
	require.ErrorIs(t, err, loadErr)
	assert.False(t, found)
	assert.Nil(t, channel)

	channel, found, err = matcher.FindBestMatchWithCandidates(t.Context(), testMemberAqua)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, channel)
	assert.Equal(t, testChannelHolo, channel.ID)
}

func TestFindBestMatchWithCandidates_AmbiguousAndOrgFilter(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: testChannelHolo, Name: testMemberAqua, Org: orgHololive},
		{ChannelID: "ch-niji", Name: testMemberAqua, Org: "Nijisanji"},
	})
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	channel, found, err := matcher.FindBestMatchWithCandidates(t.Context(), testMemberAqua)
	require.Error(t, err)
	assert.False(t, found)
	assert.Nil(t, channel)

	var ambiguous *AmbiguousMatchError

	require.ErrorAs(t, err, &ambiguous)
	require.NotNil(t, ambiguous)
	require.Len(t, ambiguous.Candidates, 2)

	filtered, found, err := matcher.FindBestMatchWithCandidates(t.Context(), "Aqua (Hololive)")
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, filtered)
	assert.Equal(t, testChannelHolo, filtered.ID)

	require.NotNil(t, filtered.Org)
	assert.Equal(t, orgHololive, *filtered.Org)
}

func TestExactNameMembers_FiltersOrg(t *testing.T) {
	t.Parallel()

	matcher := &Matcher{logger: newMatcherTestLogger()}
	snapshot := &matcherSnapshot{
		exactNames: map[string][]*snapshotEntry{
			"aqua": {
				{candidate: &matchCandidate{channelID: testChannelHolo, memberName: testMemberAqua, org: orgHololive}},
				{candidate: &matchCandidate{channelID: "ch-niji", memberName: testMemberAqua, org: "Nijisanji"}},
			},
		},
	}

	candidates := matcher.exactNameMembers(snapshot, "aqua", orgHololive)
	require.Len(t, candidates, 1)
	assert.Equal(t, testChannelHolo, candidates[0].ChannelID)
	assert.Equal(t, orgHololive, candidates[0].Org)
}

func TestFindBestMatchWithCandidates_FallbackAndErrors(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{{
		ChannelID: "ch-sora",
		Name:      "Tokino Sora",
		Aliases:   &domain.Aliases{Ja: []string{"Sora"}},
	}})

	t.Run("provider error", func(t *testing.T) {
		t.Parallel()

		loadErr := errors.New("member repo error")
		matcher := NewMatcher(newErrorAwareMemberProvider(nil, 1, loadErr), nil, newMatcherTestLogger())

		channel, found, err := matcher.FindBestMatchWithCandidates(t.Context(), "Sora")
		require.ErrorIs(t, err, loadErr)
		assert.False(t, found)
		assert.Nil(t, channel)
	})

	t.Run("fallback to FindBestMatch", func(t *testing.T) {
		t.Parallel()

		matcher := NewMatcher(provider, nil, newMatcherTestLogger())

		channel, found, err := matcher.FindBestMatchWithCandidates(t.Context(), "Sora")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "ch-sora", channel.ID)
	})
}
