package officialidentity

import (
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// testOfficialMiko는 공식 일정이 쓰는 미코의 공식 표기다.
const testOfficialMiko = "さくらみこ"

func TestBuildRequiresOneDistinctChannel(t *testing.T) {
	t.Parallel()

	index, err := Build(t.Context(), testMembers{members: []*domain.Member{
		{Name: "Shared", ChannelID: "channel-1", Aliases: &domain.Aliases{Ko: []string{"공유"}}},
		{Name: "Shared", ChannelID: "channel-2"},
		{Name: "Duplicate Same ID", ChannelID: "channel-3", Aliases: &domain.Aliases{Ja: []string{"同じ"}}},
		{Name: "Duplicate Same ID Again", ChannelID: "channel-3", Aliases: &domain.Aliases{Ja: []string{"同じ"}}},
	}})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got := index.Resolve("Shared"); got != "" {
		t.Fatalf("ambiguous identity resolved to %q", got)
	}

	if got := index.Resolve("同じ"); got != "channel-3" {
		t.Fatalf("same-channel duplicate resolved to %q", got)
	}

	if got := index.Resolve("unknown"); got != "" {
		t.Fatalf("unknown identity resolved to %q", got)
	}
}

func TestDisplayNamesMapsKoreanAndKeepsUnmapped(t *testing.T) {
	t.Parallel()

	members := testMembers{members: []*domain.Member{
		{Name: "Sakura Miko", NameJa: testOfficialMiko, ShortKoreanName: "미코", ChannelID: "ch-miko"},
		{Name: "Hoshimachi Suisei", NameJa: "星街すいせい", ShortKoreanName: "스이세이", ChannelID: "ch-sui"},
	}}

	got, err := DisplayNames(t.Context(), members, []string{testOfficialMiko, "星街すいせい", "Gawr Gura"}, "ch-miko")
	if err != nil {
		t.Fatalf("DisplayNames() error = %v", err)
	}

	if len(got) != 2 || got[0] != "스이세이" || got[1] != "Gawr Gura" {
		t.Fatalf("DisplayNames = %#v", got)
	}

	if Format(got) != "스이세이, Gawr Gura" {
		t.Fatalf("Format = %q", Format(got))
	}
}

type testMembers struct {
	members   []*domain.Member
	err       error
	lookupErr error
}

// 멤버 적재 실패는 빈 색인이나 원문 표기로 바꾸지 않고 오류다(DEC-20260926-hololive-source-fallbacks-retirement).
func TestDisplayNamesReturnsMemberLoadError(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("member cache unavailable")

	if got, err := DisplayNames(t.Context(), testMembers{err: loadErr}, []string{testOfficialMiko}, ""); !errors.Is(err, loadErr) {
		t.Fatalf("DisplayNames() = (%#v, %v), want member load error", got, err)
	}

	if got, err := DisplayNames(t.Context(), testMembers{err: loadErr}, nil, ""); err != nil || got != nil {
		t.Fatalf("DisplayNames(no names) = (%#v, %v), want no member load", got, err)
	}
}

// 색인된 채널의 대표 조회가 실패하면 공식 표기로 추측하지 않고 오류다. 미존재만 공식 표기를 쓴다.
func TestDisplayNamesSeparatesLookupFailureFromNotFound(t *testing.T) {
	t.Parallel()

	members := []*domain.Member{{Name: "Sakura Miko", NameJa: testOfficialMiko, ShortKoreanName: "미코", ChannelID: "ch-miko"}}
	lookupErr := errors.New("member cache unavailable")

	if got, err := DisplayNames(t.Context(), testMembers{members: members, lookupErr: lookupErr}, []string{testOfficialMiko}, ""); !errors.Is(err, lookupErr) {
		t.Fatalf("DisplayNames(lookup failure) = (%#v, %v), want lookup error", got, err)
	}

	notFound := testMembers{members: members, lookupErr: domain.ErrMemberNotFound}

	got, err := DisplayNames(t.Context(), notFound, []string{testOfficialMiko}, "")
	if err != nil || len(got) != 1 || got[0] != testOfficialMiko {
		t.Fatalf("DisplayNames(not found) = (%#v, %v), want official name", got, err)
	}
}

func (m testMembers) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	return m.members, m.err
}

func (m testMembers) FindMemberByChannelID(_ context.Context, channelID string) (*domain.Member, error) {
	if m.lookupErr != nil {
		return nil, m.lookupErr
	}

	for _, member := range m.members {
		if member != nil && member.ChannelID == channelID {
			return member, nil
		}
	}

	return nil, domain.ErrMemberNotFound
}

func (testMembers) FindMemberByName(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (testMembers) FindMemberByAlias(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (testMembers) GetChannelIDs(context.Context) ([]string, error) { return []string{}, nil }

func (testMembers) FindMembersByName(context.Context, string) ([]*domain.Member, error) {
	return []*domain.Member{}, nil
}

func (testMembers) FindMembersByAlias(context.Context, string) ([]*domain.Member, error) {
	return []*domain.Member{}, nil
}
