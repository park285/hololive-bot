package mekparkhost

import (
	jsonv2 "encoding/json/v2"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	unitBChannel   = "UC3OH5FKQ3qtl4uRme_vZTgA"
	achroraChannel = "UChpRPsAeSZn5DistGacR3iA"
)

type titleCase struct {
	Unit      string   `json:"unit"`
	ChannelID string   `json:"channel_id"`
	VideoID   string   `json:"video_id"`
	Source    string   `json:"source"`
	Title     string   `json:"title"`
	Hosts     []string `json:"hosts"`
	Guests    []string `json:"guests"`
}

func TestIdentifyCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/title_corpus.json")
	require.NoError(t, err)

	var corpus struct {
		Cases []titleCase `json:"cases"`
	}

	require.NoError(t, jsonv2.Unmarshal(data, &corpus))
	require.NotEmpty(t, corpus.Cases)

	identified := make(map[string]int)
	totals := make(map[string]int)

	for _, tc := range corpus.Cases {
		t.Run(tc.Source+"/"+tc.VideoID, func(t *testing.T) {
			result := Identify(tc.ChannelID, tc.Title)
			totals[tc.Source]++

			if len(result.Hosts) > 0 {
				identified[tc.Source]++
			}

			require.Equal(t, tc.Unit, result.Unit)
			require.Equal(t, tc.Hosts, participantIDs(result.Hosts), tc.Title)
			require.Equal(t, tc.Guests, participantIDs(result.Guests), tc.Title)

			for _, person := range append(result.Hosts, result.Guests...) {
				require.NotEmpty(t, person.Name)
				require.NotEmpty(t, person.Evidence)
			}
		})
	}

	t.Logf("title-evidence coverage (not accuracy): live_session=%d/%d video=%d/%d", identified["live_session"], totals["live_session"], identified["video"], totals["video"])
}

func participantIDs(people []Participant) []string {
	ids := make([]string, 0, len(people))
	for _, person := range people {
		ids = append(ids, person.ID)
	}

	return ids
}

func TestIdentifyTitleEvidence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		channel string
		title   string
		label   string
	}{
		{name: "neon", channel: unitBChannel, title: "【 #UNIT_B / #宵凪ネオン 】", label: "네온"},
		{name: "mira", channel: unitBChannel, title: "【 #UNIT_B / # 玲銘ミラ】", label: "미라"},
		{name: "lyra", channel: unitBChannel, title: "【 #UNIT_B / #清澄ライラ 】", label: "라이라"},
		{name: "sayana", channel: achroraChannel, title: "#さやな時間", label: "사야나"},
		{name: "rirara", channel: achroraChannel, title: "#りららだいびんぐ", label: "리라라"},
		{name: "hinami", channel: achroraChannel, title: "#ひなみ通信", label: "히나미"},
		{name: "fullwidth_hash_space", channel: achroraChannel, title: "＃　ひなみ通信", label: "히나미"},
		{name: "zero_width_name", channel: unitBChannel, title: "#玲銘ミ\u200bラ", label: "미라"},
		{name: "halfwidth_name", channel: unitBChannel, title: "#玲銘ﾐﾗ", label: "미라"},
		{name: "series_only", channel: achroraChannel, title: "【#あさやなストレッチ夏】", label: "사야나"},
		{name: "same_unit_guest", channel: achroraChannel, title: "【#由比河ひなみ】#あさやなストレッチ夏 #墨汐さやな", label: "사야나 (게스트: 히나미)"},
		{name: "other_unit_guest", channel: achroraChannel, title: "【#玲銘ミラ】#あさやなストレッチ夏 #墨汐さやな", label: "사야나 (게스트: 미라)"},
		{name: "collab", channel: achroraChannel, title: "#墨汐さやな × #琉海垣りらら", label: "리라라 / 사야나"},
		{name: "perspective", channel: achroraChannel, title: "【ひなみ視点】R.E.P.O.", label: "히나미"},
		{name: "debut_part", channel: achroraChannel, title: "【初配信】さやなパート", label: "사야나"},
		{name: "given_name_pair", channel: unitBChannel, title: "【延長戦】りらら×ミラのアソビ大全!!【 #UNIT_B / #ACHRORA 】", label: "미라 (게스트: 리라라)"},
		{name: "pair_spacing", channel: unitBChannel, title: "【りらら × ＃ ミラ】", label: "미라 (게스트: 리라라)"},
		{name: "mirror_pair_channel", channel: achroraChannel, title: "【りらら×ミラ】", label: "리라라 (게스트: 미라)"},
		{name: "mira_substring", channel: unitBChannel, title: "リズム天国ミラクルスターズ #宵凪ネオン", label: "네온"},
		{name: "prose_guest", channel: achroraChannel, title: "今日はひなみが一緒 #あさやなストレッチ夏", label: "사야나"},
		{name: "unit_only", channel: unitBChannel, title: "メンバーシップ解禁 #UNIT_B"},
		{name: "unknown_title", channel: achroraChannel, title: "?"},
		{name: "bare_given_names", channel: unitBChannel, title: "ネオン ミラ ライラ PON"},
		{name: "ascii_x", channel: unitBChannel, title: "りらら x ミラ"},
		{name: "pair_word_suffix", channel: unitBChannel, title: "りらら×ミラクル"},
		{name: "pair_word_prefix", channel: unitBChannel, title: "そっくりらら×ミラ"},
		{name: "guest_without_host", channel: unitBChannel, title: "#墨汐さやな"},
		{name: "foreign_channel", channel: "UC_other", title: "#UNIT_B #玲銘ミラ"},
		{name: "no_channel", title: "#UNIT_B #玲銘ミラ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := Identify(tc.channel, tc.title)
			require.Equal(t, tc.label, result.Label())
		})
	}
}

func TestDisplayName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "유닛 B · 미라", DisplayName(unitBChannel, "#玲銘ミラ", "유닛 B"))
	require.Equal(t, "유닛 B", DisplayName(unitBChannel, "?", "유닛 B"))
	require.Equal(t, "다른 채널", DisplayName("UC_other", "#玲銘ミラ", "다른 채널"))
	require.Equal(t, "미라", DisplayName(unitBChannel, "#玲銘ミラ", ""))
	require.Equal(t, []string{"玲銘ミラ"}, Identify(unitBChannel, "#玲銘ミラ").Hosts[0].Evidence)
}
