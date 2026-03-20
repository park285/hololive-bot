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

package command

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

func TestCommandConstructorsNameDescription(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		command     Command
		expectName  string
		expectDescr string
	}{
		{name: "help", command: NewHelpCommand(nil), expectName: "help", expectDescr: "도움말을 표시합니다"},
		{name: "live", command: NewLiveCommand(nil), expectName: "live", expectDescr: "현재 방송 중인 스트림 목록"},
		{name: "stats", command: NewStatsCommand(nil), expectName: "stats", expectDescr: "구독자 순위 및 통계 조회"},
		{name: "subscriber", command: NewSubscriberCommand(nil), expectName: string(domain.CommandSubscriber), expectDescr: "특정 멤버의 구독자 수 조회"},
		{name: "member_info", command: NewMemberInfoCommand(nil), expectName: string(domain.CommandMemberInfo), expectDescr: "홀로라이브 멤버 공식 프로필"},
		{name: "major_event", command: NewMajorEventCommand(nil, nil), expectName: "major_event", expectDescr: "행사 알림 관리"},
		{name: "member_news", command: NewMemberNewsCommand(nil), expectName: "member_news", expectDescr: "구독 멤버 뉴스 조회"},
		{name: "news_subscription", command: NewMemberNewsSubscriptionCommand(nil), expectName: "news_subscription", expectDescr: "뉴스 알림 구독 제어"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotNil(t, tc.command)
			assert.Equal(t, tc.expectName, tc.command.Name())
			assert.Equal(t, tc.expectDescr, tc.command.Description())
		})
	}
}

func TestFactoryHelpers(t *testing.T) {
	t.Parallel()

	majorFactory := NewMajorEventFactory(nil)
	require.NotNil(t, majorFactory)

	majorCommand := majorFactory(nil)
	require.NotNil(t, majorCommand)
	assert.Equal(t, "major_event", majorCommand.Name())

	newsFactories := MemberNewsFactories()
	require.Len(t, newsFactories, 2)
	require.NotNil(t, newsFactories[0])
	require.NotNil(t, newsFactories[1])

	newsCommand := newsFactories[0](nil)
	subscriptionCommand := newsFactories[1](nil)

	require.NotNil(t, newsCommand)
	require.NotNil(t, subscriptionCommand)
	assert.Equal(t, "member_news", newsCommand.Name())
	assert.Equal(t, "news_subscription", subscriptionCommand.Name())
}

func TestMemberGroupParsingHelpers(t *testing.T) {
	t.Parallel()

	t.Run("extract unit values prefers translated", func(t *testing.T) {
		profile := &domain.TalentProfile{
			DataEntries: []domain.TalentProfileEntry{{Label: "Unit", Value: "Myth"}},
		}
		translated := &domain.Translated{
			Data: []domain.TranslatedProfileDataRow{{Label: "소속 유닛", Value: "Promise"}},
		}
		values := extractUnitValues(profile, translated)
		assert.Equal(t, []string{"Promise"}, values)
	})

	t.Run("extract unit values falls back to profile", func(t *testing.T) {
		profile := &domain.TalentProfile{
			DataEntries: []domain.TalentProfileEntry{
				{Label: "Birth", Value: "1/1"},
				{Label: "Unit", Value: "Myth"},
			},
		}
		values := extractUnitValues(profile, nil)
		assert.Equal(t, []string{"Myth"}, values)
	})

	t.Run("split group tokens", func(t *testing.T) {
		assert.Equal(t, []string{"Myth", "Promise", "holoX"}, splitGroupTokens("Myth／Promise・holoX"))
		assert.Equal(t, []string{"Raw"}, splitGroupTokens("Raw"))
		assert.Equal(t, []string{" / "}, splitGroupTokens(" / "))
	})

	t.Run("normalize member group", func(t *testing.T) {
		assert.Equal(t, defaultMemberDirectoryGroup, normalizeMemberGroup("  "))
		assert.Equal(t, "홀로라이브 0기생", normalizeMemberGroup("ホロライブ0期生"))
		assert.Equal(t, "Myth", normalizeMemberGroup("Myth（神話）"))
		assert.Equal(t, "Promise", normalizeMemberGroup("hololive English Promise"))
		assert.Equal(t, "Justice", normalizeMemberGroup("ホロライブEnglish -Justice-"))
		assert.Equal(t, "ReGLOSS", normalizeMemberGroup("ReGLOSS"))
	})

	t.Run("primary member name", func(t *testing.T) {
		assert.Empty(t, primaryMemberName(nil))
		assert.Equal(t, "미코", primaryMemberName(&domain.Member{Name: "Sakura Miko", NameKo: ",미코,"}))
		assert.Equal(t, "Sora", primaryMemberName(&domain.Member{Name: "Sora"}))
	})
}

func TestMemberDirectorySortAndFilterHelpers(t *testing.T) {
	t.Parallel()

	cmd := NewMemberInfoCommand(nil)

	members := cmd.filterActiveMembers([]*domain.Member{
		{Name: "active-1", IsGraduated: false},
		nil,
		{Name: "graduated", IsGraduated: true},
		{Name: "active-2", IsGraduated: false},
	})
	require.Len(t, members, 2)
	assert.Equal(t, "active-1", members[0].Name)
	assert.Equal(t, "active-2", members[1].Name)

	group := buildMemberDirectoryGroup("Myth", map[string]adapter.MemberDirectoryEntry{
		"c": {PrimaryName: "C", SecondaryName: "c"},
		"a": {PrimaryName: "A", SecondaryName: "a"},
		"b": {PrimaryName: "B", SecondaryName: "b"},
	})
	require.Len(t, group.Members, 3)
	assert.Equal(t, "A", group.Members[0].PrimaryName)
	assert.Equal(t, "B", group.Members[1].PrimaryName)
	assert.Equal(t, "C", group.Members[2].PrimaryName)

	ordered := cmd.sortGroupsByPreference(map[string]map[string]adapter.MemberDirectoryEntry{
		"기타": {
			"g": {PrimaryName: "G", SecondaryName: "g"},
		},
		"Advent": {
			"a": {PrimaryName: "A", SecondaryName: "a"},
		},
		"Zeta": {
			"z": {PrimaryName: "Z", SecondaryName: "z"},
		},
	})

	require.Len(t, ordered, 3)
	assert.Equal(t, "Advent", ordered[0].GroupName) // preferred order first
	assert.Equal(t, "Zeta", ordered[1].GroupName)   // remaining alphabetical
	assert.Equal(t, "기타", ordered[2].GroupName)
}

func TestRegistryCount(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	assert.Equal(t, 0, registry.Count())

	registry.Register(NewHelpCommand(nil))
	registry.Register(NewLiveCommand(nil))
	assert.Equal(t, 2, registry.Count())
}
