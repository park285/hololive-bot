package matcher

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	displayTestSoraChannel    = "ch-display-sora"
	displayTestRobocoChannel  = "ch-display-roboco"
	displayTestInaChannel     = "ch-display-ina"
	displayTestMikoHoloChan   = "ch-display-miko-holo"
	displayTestMikoIndieChan  = "ch-display-miko-indie"
	displayTestUnregistered   = "ch-display-unregistered"
	displayTestIndependentOrg = "Independents"
)

// 명령 응답의 멤버 이름은 matcher 결과 하나에서 나오므로, 검색 경로마다 같은 정본 표시명을 돌려줘야 한다.
func newDisplayNameTestMatcher() *Matcher {
	return NewMatcher(newStubMemberProvider([]*domain.Member{
		{ChannelID: displayTestSoraChannel, Name: "Tokino Sora", NameKo: "토키노 소라", ShortKoreanName: "소라", NameJa: "ときのそら", Org: orgHololive},
		{ChannelID: displayTestRobocoChannel, Name: "Roboco", NameKo: "로보코", Org: orgHololive},
		{ChannelID: displayTestInaChannel, Name: "Ninomae Inanis", Org: orgHololive},
		{ChannelID: displayTestMikoHoloChan, Name: "Miko", ShortKoreanName: "미코", Org: orgHololive},
		{ChannelID: displayTestMikoIndieChan, Name: "Miko", NameKo: "인디 미코", Org: displayTestIndependentOrg},
	}), nil, slog.New(slog.DiscardHandler))
}

func assertLookupChannel(t *testing.T, lookup string, channel *domain.Channel, found bool, err error, channelID, name string) {
	t.Helper()

	if err != nil || !found || channel == nil || channel.ID != channelID || channel.Name != name {
		t.Fatalf("%s = %+v, %v, %v; want %s %q", lookup, channel, found, err, channelID, name)
	}
}

func TestMatcherLookupReturnsCanonicalDisplayName(t *testing.T) {
	t.Parallel()

	mm := newDisplayNameTestMatcher()

	for _, tc := range []struct {
		query, channelID, want string
	}{
		{query: "Tokino Sora", channelID: displayTestSoraChannel, want: "소라"},
		{query: "토키노 소라", channelID: displayTestSoraChannel, want: "소라"},
		{query: "ときのそら", channelID: displayTestSoraChannel, want: "소라"},
		{query: "Roboco", channelID: displayTestRobocoChannel, want: "로보코"},
		{query: "Ninomae Inanis", channelID: displayTestInaChannel, want: "Ninomae Inanis"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()

			best, found, err := mm.FindBestMatch(t.Context(), tc.query)
			assertLookupChannel(t, "FindBestMatch("+tc.query+")", best, found, err, tc.channelID, tc.want)

			withCandidates, found, err := mm.FindBestMatchWithCandidates(t.Context(), tc.query)
			assertLookupChannel(t, "FindBestMatchWithCandidates("+tc.query+")", withCandidates, found, err, tc.channelID, tc.want)
		})
	}
}

// 후보의 검색 키는 english_name으로 남아야 동명이인 안내의 복사 예시가 같은 후보로 해석된다.
func TestMatcherAmbiguousCandidatesKeepSearchKey(t *testing.T) {
	t.Parallel()

	mm := newDisplayNameTestMatcher()

	_, _, lookupErr := mm.FindBestMatchWithCandidates(t.Context(), "Miko")

	ambiguous, ok := errors.AsType[*AmbiguousMatchError](lookupErr)
	if !ok || len(ambiguous.Candidates) != 2 {
		t.Fatalf("FindBestMatchWithCandidates(Miko) error = %v, want two ambiguous candidates", lookupErr)
	}

	for _, candidate := range ambiguous.Candidates {
		resolved, found, err := mm.FindBestMatchWithCandidates(t.Context(), candidate.QualifiedName())
		assertLookupChannel(t, "copy example "+candidate.QualifiedName(), resolved, found, err, candidate.ChannelID, candidate.DisplayName())
	}
}

func TestMatcherMemberDisplayNamesOmitsUnregisteredChannels(t *testing.T) {
	t.Parallel()

	names, err := newDisplayNameTestMatcher().MemberDisplayNames(t.Context(), []string{
		displayTestSoraChannel, displayTestRobocoChannel, displayTestInaChannel, displayTestUnregistered,
	})
	if err != nil {
		t.Fatalf("MemberDisplayNames() error = %v", err)
	}

	want := map[string]string{displayTestSoraChannel: "소라", displayTestRobocoChannel: "로보코", displayTestInaChannel: "Ninomae Inanis"}
	if len(names) != len(want) {
		t.Fatalf("MemberDisplayNames() = %v, want %v", names, want)
	}

	for channelID, name := range want {
		if names[channelID] != name {
			t.Fatalf("MemberDisplayNames()[%s] = %q, want %q", channelID, names[channelID], name)
		}
	}
}
