package officialidentity

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildRequiresOneDistinctChannel(t *testing.T) {
	t.Parallel()
	index := Build(testMembers{members: []*domain.Member{
		{Name: "Shared", ChannelID: "channel-1", Aliases: &domain.Aliases{Ko: []string{"공유"}}},
		{Name: "Shared", ChannelID: "channel-2"},
		{Name: "Duplicate Same ID", ChannelID: "channel-3", Aliases: &domain.Aliases{Ja: []string{"同じ"}}},
		{Name: "Duplicate Same ID Again", ChannelID: "channel-3", Aliases: &domain.Aliases{Ja: []string{"同じ"}}},
	}})
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
		{Name: "Sakura Miko", NameJa: "さくらみこ", ShortKoreanName: "미코", ChannelID: "ch-miko"},
		{Name: "Hoshimachi Suisei", NameJa: "星街すいせい", ShortKoreanName: "스이세이", ChannelID: "ch-sui"},
	}}
	got := DisplayNames(members, []string{"さくらみこ", "星街すいせい", "Gawr Gura"}, "ch-miko")
	if len(got) != 2 || got[0] != "스이세이" || got[1] != "Gawr Gura" {
		t.Fatalf("DisplayNames = %#v", got)
	}
	if Format(got) != "스이세이, Gawr Gura" {
		t.Fatalf("Format = %q", Format(got))
	}
}

type testMembers struct {
	members []*domain.Member
}

func (m testMembers) GetAllMembers() []*domain.Member { return m.members }
func (m testMembers) FindMemberByChannelID(channelID string) *domain.Member {
	for _, member := range m.members {
		if member != nil && member.ChannelID == channelID {
			return member
		}
	}
	return nil
}
func (testMembers) FindMemberByName(string) *domain.Member                  { return nil }
func (testMembers) FindMemberByAlias(string) *domain.Member                 { return nil }
func (testMembers) GetChannelIDs() []string                                 { return nil }
func (m testMembers) WithContext(context.Context) domain.MemberDataProvider { return m }
func (testMembers) FindMembersByName(string) []*domain.Member               { return nil }
func (testMembers) FindMembersByAlias(string) []*domain.Member              { return nil }
