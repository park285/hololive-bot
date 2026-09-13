package info

import (
	"slices"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const testInfoOrg = "Hololive"

func TestMemberGroupsFromRegisteredUnits(t *testing.T) {
	command := &MemberInfoCommand{}

	for _, tc := range []struct {
		name   string
		member *domain.Member
		want   []string
	}{
		{"복수 기수", &domain.Member{Org: testInfoOrg, Units: []string{"홀로라이브 1기생", "홀로라이브 게이머즈"}}, []string{"홀로라이브 1기생", "홀로라이브 게이머즈"}},
		{"신규 멤버", &domain.Member{Org: testInfoOrg}, []string{"Hololive (기수 미등록)"}},
		{"ID 유닛", &domain.Member{Org: testInfoOrg, Units: []string{"AREA15"}}, []string{"AREA15"}},
		{"기존 그룹", &domain.Member{Org: "mekPark"}, []string{"mekPark"}},
		{"nil", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := command.memberGroups(tc.member); !slices.Equal(got, tc.want) {
				t.Fatalf("groups=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestMemberInfoCandidatesKeepIndividualsAndLiteralQueries(t *testing.T) {
	members := make([]*domain.Member, 0, 4)

	members = append(members, []*domain.Member{
		{ID: 1, Name: "holoAN", ChannelID: "shared", Org: testInfoOrg},
		{ID: 2, Name: "Izuki Michiru", NameKo: "이즈키 미치루", ChannelID: "shared", Org: testInfoOrg, Aliases: &domain.Aliases{Ko: []string{"미치루"}}},
		{ID: 3, Name: "Hanazono Sayaka", ChannelID: "shared", Org: testInfoOrg},
	}...)

	for _, query := range []string{"Michiru", "Izuki", "izuki michiru", "IZUKI  MICHIRU", "미치루쨩", "Michiru (Hololive)"} {
		got := memberInfoCandidates(members, query)
		if len(got) != 1 || got[0].ID != 2 {
			t.Errorf("query=%q matches=%v", query, got)
		}
	}

	for _, query := range []string{"%", "_", "Missing"} {
		if got := memberInfoCandidates(members, query); len(got) != 0 {
			t.Errorf("query=%q matched=%v", query, got)
		}
	}

	members = append(members, &domain.Member{ID: 4, Name: "Other Michiru", ChannelID: "different"})
	if got := memberInfoCandidates(members, "Michiru"); len(got) != 2 {
		t.Fatalf("ambiguous candidates=%v", got)
	}
}
