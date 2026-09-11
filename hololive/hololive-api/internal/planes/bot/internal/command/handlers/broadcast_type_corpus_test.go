package handlers

import (
	jsonv2 "encoding/json/v2"
	"os"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
)

const testTypeSourceUnknown = "unknown"

func TestClassifyBroadcastCorpus(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/broadcast_type_corpus.json")
	if err != nil {
		t.Fatal(err)
	}

	var corpus struct {
		Cases []struct {
			VideoID   string             `json:"video_id"`
			ChannelID string             `json:"channel_id"`
			TopicID   string             `json:"topic_id"`
			Title     string             `json:"title"`
			Type      broadcasttype.Type `json:"expected_type"`
		} `json:"cases"`
	}

	if err := jsonv2.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}

	if len(corpus.Cases) == 0 {
		t.Fatal("empty broadcast corpus")
	}

	seen := make(map[string]bool, len(corpus.Cases))
	for _, tc := range corpus.Cases {
		if tc.VideoID == "" || seen[tc.VideoID] || !broadcasttype.Known(tc.Type) {
			t.Fatalf("invalid or duplicate corpus case: %q (%q)", tc.VideoID, tc.Type)
		}

		seen[tc.VideoID] = true

		t.Run(tc.VideoID, func(t *testing.T) {
			t.Parallel()

			if got := ClassifyBroadcastVideo(tc.VideoID, tc.ChannelID, tc.TopicID, tc.Title); got.Type != tc.Type {
				t.Errorf("ClassifyBroadcastVideo(%q, %q) = %q, want %q", tc.TopicID, tc.Title, got.Type, tc.Type)
			}
		})
	}
}

func TestClassifyBroadcastContextBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		topic  string
		title  string
		typ    broadcasttype.Type
		source string
	}{
		{name: "rust prose", title: "removing rust from a chair", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "peak prose", title: "at the peak of my career", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "ark prose", title: "a story about an ark", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "rust word suffix", title: "【RUSTY】", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "rust day tag", title: "【 RUST DAY2 】", typ: broadcasttype.Game, source: testTypeSourceTitle},
		{name: "peak lead", title: "【PEAK】new update!", typ: broadcasttype.Game, source: testTypeSourceTitle},
		{name: "rust watchalong", title: "【RUST 同時視聴】", typ: broadcasttype.Watchalong, source: testTypeSourceTitle},
		{name: "unknown topic", topic: "unreviewed_game", title: "hello", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "watchpa word suffix", title: "【WATCHPANDA】", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "japanese talk abbreviation boundary", title: "【雑貨】新しい棚", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "tournament mentioned during game", topic: "minecraft", title: "【Minecraft】next tournament tomorrow", typ: broadcasttype.Game, source: testTypeSourceTopic},
		{name: "fan name is not membership evidence", topic: "apex", title: "【APEX】wingmen only challenge", typ: broadcasttype.Game, source: testTypeSourceTopic},
		{name: "missing data", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "membership over talk", topic: "talk", title: "【MEMBERS】hello", typ: broadcasttype.Membership, source: testTypeSourceTitle},
		{name: "membership announcement", title: "【祝】メンバーシップ解禁します!【 #UNIT_B 】", typ: broadcasttype.News, source: testTypeSourceTitle},
		{name: "membership news mention", topic: "news_show", title: "【朝こよ】メンバーシップ解禁のニュース！", typ: broadcasttype.News, source: testTypeSourceTopic},
		{name: "membership prose", title: "【雑談】gym membershipの話をする", typ: broadcasttype.Talk, source: testTypeSourceTitle},
		{name: "membership spaced tag", title: "【 ＭＥＭＢＥＲＳＨＩＰ 】karaoke", typ: broadcasttype.Membership, source: testTypeSourceTitle},
		{name: "membership alternate tag", title: "≪ Membership ≫ hello", typ: broadcasttype.Membership, source: testTypeSourceTitle},
		{name: "membership announcement inside private stream", title: "【メン限】メンバーシップ解禁のお礼", typ: broadcasttype.Membership, source: testTypeSourceTitle},
		{name: "membership over singing", topic: "singing", title: "【メン限】歌枠", typ: broadcasttype.Membership, source: testTypeSourceTitle},
		{name: "membership over asmr", topic: "asmr", title: "【メンバー限定】ASMR", typ: broadcasttype.Membership, source: testTypeSourceTitle},
		{name: "membership topic retained", topic: "membersonly", title: "【MEMBERS】hello", typ: broadcasttype.Membership, source: testTypeSourceTopic},
		{name: "game party members", topic: "ark", title: "four party members", typ: broadcasttype.Game, source: testTypeSourceTopic},
		{name: "retrospective over game", topic: "ark", title: "【ARK】打ち上げ&振り返り配信!", typ: broadcasttype.Talk, source: testTypeSourceTitle},
		{name: "game recap mention", topic: "ark", title: "【ARK】昨日を振り返りながら建築", typ: broadcasttype.Game, source: testTypeSourceTopic},
		{name: "announcement aside", topic: "ark", title: "【ARK】告知あり!今日も建築", typ: broadcasttype.Game, source: testTypeSourceTopic},
		{name: "holodreams gameplay", topic: "hololive_Dreams", title: "【ホロドリ】ガチャで全員お迎え", typ: broadcasttype.Game, source: testTypeSourceTopic},
		{name: "holodreams tournament", topic: "hololive_Dreams", title: "【#第1回ポカジャン王決定戦】hololive Dreams", typ: broadcasttype.Event, source: testTypeSourceTitle},
		{name: "holodreams membership", topic: "hololive_Dreams", title: "【メン限】ホロドリ", typ: broadcasttype.Membership, source: testTypeSourceTitle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyBroadcastWithSource(tc.topic, tc.title)
			if got.Type != tc.typ || got.Source != tc.source {
				t.Errorf("ClassifyBroadcastWithSource(%q, %q) = %+v, want %s/%s", tc.topic, tc.title, got, tc.typ, tc.source)
			}
		})
	}
}
