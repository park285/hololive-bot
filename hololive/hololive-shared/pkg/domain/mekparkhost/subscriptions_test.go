package mekparkhost

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const subscriptionMiraHostID = "reimei-mira"

func TestFindSubscriptionMember(t *testing.T) {
	for _, name := range []string{"미라", "유닛 B 미라", "UNIT B ミラ", "玲銘ミラ", subscriptionMiraHostID, "玲銘ﾐﾗ"} {
		t.Run(name, func(t *testing.T) {
			channelID, member, ok := FindSubscriptionMember(name)
			require.True(t, ok)
			require.Equal(t, unitBChannel, channelID)
			require.Equal(t, subscriptionMiraHostID, member.ID)
			require.Equal(t, "미라[유닛b]", member.Name)
		})
	}

	for _, name := range []string{"", "유닛 B", "네온 미라", "ミラクル", "히나미", "유닛 B 히나미"} {
		t.Run("not_person_"+name, func(t *testing.T) {
			_, _, ok := FindSubscriptionMember(name)
			require.False(t, ok)
		})
	}
}

func TestMatchesSubscription(t *testing.T) {
	for _, tc := range []struct {
		name, channel, title, host string
		want                       bool
	}{
		{name: "whole_channel", channel: unitBChannel, title: "#宵凪ネオン", want: true},
		{name: "matching_person", channel: unitBChannel, title: "#玲銘ミラ", host: subscriptionMiraHostID, want: true},
		{name: "different_person", channel: unitBChannel, title: "#宵凪ネオン", host: subscriptionMiraHostID},
		{name: "cohosts", channel: unitBChannel, title: "#宵凪ネオン #玲銘ミラ", host: subscriptionMiraHostID, want: true},
		{name: "unknown", channel: unitBChannel, title: "メンバーシップ解禁 #UNIT_B", host: "kiyosumi-lyra", want: true},
		{name: "empty_title", channel: unitBChannel, host: "yoinagi-neon", want: true},
		{name: "foreign_guest_only", channel: unitBChannel, title: "#墨汐さやな", host: "yoinagi-neon", want: true},
		{name: "guest_is_not_host", channel: unitBChannel, title: "りらら×ミラ", host: "kiyosumi-lyra"},
		{name: "invalid_person", channel: unitBChannel, host: "unknown-person"},
		{name: "other_unit", channel: "UChpRPsAeSZn5DistGacR3iA", host: subscriptionMiraHostID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Identify(tc.channel, tc.title).MatchesSubscription(tc.host))
		})
	}
}
