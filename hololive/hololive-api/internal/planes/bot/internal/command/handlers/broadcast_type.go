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

package handlers

import "strings"

type BroadcastType string

type BroadcastClassification struct {
	Type   BroadcastType
	Source string
}

const (
	BroadcastTypeGame       BroadcastType = "game"
	BroadcastTypeTalk       BroadcastType = "talk"
	BroadcastTypeSinging    BroadcastType = "singing"
	BroadcastTypeASMR       BroadcastType = "asmr"
	BroadcastTypeMembership BroadcastType = "membership"
	BroadcastTypeEvent      BroadcastType = "event"
	BroadcastTypeWatchalong BroadcastType = "watchalong"
	BroadcastTypeNews       BroadcastType = "news"
	BroadcastTypeOther      BroadcastType = "other"
	BroadcastTypeUnknown    BroadcastType = "unknown"
)

var observedTopicTypes = map[string]BroadcastType{
	"3d_stream":                              BroadcastTypeEvent,
	"apex":                                   BroadcastTypeGame,
	"arknights:_endfield":                    BroadcastTypeGame,
	"a_taste_of_home":                        BroadcastTypeGame,
	"birthday":                               BroadcastTypeEvent,
	"bunny_garden":                           BroadcastTypeGame,
	"burglin'_gnomes":                        BroadcastTypeGame,
	"chrono_trigger":                         BroadcastTypeGame,
	"clubhouse51":                            BroadcastTypeGame,
	"dorfromantik":                           BroadcastTypeGame,
	"dread_flats":                            BroadcastTypeGame,
	"earthbound":                             BroadcastTypeGame,
	"fallguys":                               BroadcastTypeGame,
	"final_fantasy_online":                   BroadcastTypeGame,
	"final_fantasy":                          BroadcastTypeGame,
	"forza":                                  BroadcastTypeGame,
	"geoguessr":                              BroadcastTypeGame,
	"heartopia":                              BroadcastTypeGame,
	"holovillage":                            BroadcastTypeGame,
	"home_sweet_home":                        BroadcastTypeGame,
	"hyakushakusama":                         BroadcastTypeGame,
	"jump_king":                              BroadcastTypeGame,
	"librarian:_tidy_up_the_arcane_library!": BroadcastTypeGame,
	"mahjong":                                BroadcastTypeGame,
	"mario_64":                               BroadcastTypeGame,
	"meccha_chameleon":                       BroadcastTypeGame,
	"membersonly":                            BroadcastTypeMembership,
	"minecraft":                              BroadcastTypeGame,
	"music_cover":                            BroadcastTypeSinging,
	"news_show":                              BroadcastTypeNews,
	"original_song":                          BroadcastTypeSinging,
	"overwatch":                              BroadcastTypeGame,
	"pokemon":                                BroadcastTypeGame,
	"power_pros":                             BroadcastTypeGame,
	"r.e.p.o.":                               BroadcastTypeGame,
	"residentevil":                           BroadcastTypeGame,
	"rhythm_heaven":                          BroadcastTypeGame,
	"sausage_legend":                         BroadcastTypeGame,
	"school_666":                             BroadcastTypeGame,
	"shadowverse":                            BroadcastTypeGame,
	"singing":                                BroadcastTypeSinging,
	"super_mario":                            BroadcastTypeGame,
	"sword_art_online":                       BroadcastTypeGame,
	"talk":                                   BroadcastTypeTalk,
	"tomodachi_life":                         BroadcastTypeGame,
	"valorant":                               BroadcastTypeGame,
	"vlog":                                   BroadcastTypeOther,
	"watchalong":                             BroadcastTypeWatchalong,
	"wii_fit":                                BroadcastTypeGame,
	"wuthering_waves":                        BroadcastTypeGame,
}

var broadcastTypeAliases = map[string]BroadcastType{
	"game": BroadcastTypeGame, "games": BroadcastTypeGame, "gaming": BroadcastTypeGame, "게임": BroadcastTypeGame, "겜": BroadcastTypeGame, "게임방송": BroadcastTypeGame,
	"talk": BroadcastTypeTalk, "zatsudan": BroadcastTypeTalk, "free_talk": BroadcastTypeTalk, "free-talk": BroadcastTypeTalk, "잡담": BroadcastTypeTalk, "토크": BroadcastTypeTalk, "수다": BroadcastTypeTalk,
	"singing": BroadcastTypeSinging, "song": BroadcastTypeSinging, "karaoke": BroadcastTypeSinging, "music": BroadcastTypeSinging, "노래": BroadcastTypeSinging, "노래방": BroadcastTypeSinging, "歌枠": BroadcastTypeSinging, "우타와꾸": BroadcastTypeSinging,
	"asmr":       BroadcastTypeASMR,
	"membership": BroadcastTypeMembership, "member": BroadcastTypeMembership, "members": BroadcastTypeMembership, "membersonly": BroadcastTypeMembership, "memberonly": BroadcastTypeMembership, "멤버십": BroadcastTypeMembership, "멤버": BroadcastTypeMembership, "멤버한정": BroadcastTypeMembership, "멤버전용": BroadcastTypeMembership,
	"event": BroadcastTypeEvent, "events": BroadcastTypeEvent, "birthday": BroadcastTypeEvent, "3d": BroadcastTypeEvent, "outfit": BroadcastTypeEvent, "이벤트": BroadcastTypeEvent, "기념": BroadcastTypeEvent, "생일": BroadcastTypeEvent, "신의상": BroadcastTypeEvent, "3d방송": BroadcastTypeEvent,
	"watchalong": BroadcastTypeWatchalong, "watch-along": BroadcastTypeWatchalong, "watch_party": BroadcastTypeWatchalong, "watchparty": BroadcastTypeWatchalong, "동시시청": BroadcastTypeWatchalong, "같이보기": BroadcastTypeWatchalong,
	"news": BroadcastTypeNews, "notice": BroadcastTypeNews, "announcement": BroadcastTypeNews, "뉴스": BroadcastTypeNews, "공지": BroadcastTypeNews,
	"other": BroadcastTypeOther, "variety": BroadcastTypeOther, "etc": BroadcastTypeOther, "기타": BroadcastTypeOther,
	"unknown": BroadcastTypeUnknown, "미분류": BroadcastTypeUnknown,
}

var broadcastTypeLabels = map[BroadcastType]string{
	BroadcastTypeGame:       "게임",
	BroadcastTypeTalk:       "잡담",
	BroadcastTypeSinging:    "노래",
	BroadcastTypeASMR:       "ASMR",
	BroadcastTypeMembership: "멤버십",
	BroadcastTypeEvent:      "이벤트",
	BroadcastTypeWatchalong: "동시시청",
	BroadcastTypeNews:       "뉴스/공지",
	BroadcastTypeOther:      "기타",
}

type broadcastTitleRule struct {
	typ      BroadcastType
	keywords []string
}

var broadcastTitleRules = []broadcastTitleRule{
	{typ: BroadcastTypeMembership, keywords: []string{"メン限", "メンバー限定", "メンバー限定配信", "members only", "member only", "membership"}},
	{typ: BroadcastTypeWatchalong, keywords: []string{"同時視聴", "watchalong", "watch along", "watchparty", "watch party", "ウォッチパーティー"}},
	{typ: BroadcastTypeSinging, keywords: []string{"歌枠", "karaoke", "singing stream", "3dカラオケ", "カラオケ", "(cover)", "【cover】", "original song", "オリジナル曲"}},
	{typ: BroadcastTypeASMR, keywords: []string{"asmr"}},
	{typ: BroadcastTypeEvent, keywords: []string{"生誕", "birthday", "3d live", "3dlive", "新衣装", "お披露目"}},
	{typ: BroadcastTypeNews, keywords: []string{"昼ホロ", "朝ミオ", "朝こよ", "ニュース", "お知らせ"}},
	{typ: BroadcastTypeTalk, keywords: []string{"雑談", "free talk", "freetalk", "朝活雑談", "寝る前雑談", "おはすば"}},
	{typ: BroadcastTypeOther, keywords: []string{"【vlog】", "vlog", "カメラ有", "開封", "unboxing", "爆買い"}},
}

func ClassifyBroadcast(topicID, title string) BroadcastType {
	return ClassifyBroadcastWithSource(topicID, title).Type
}

func ClassifyBroadcastWithSource(topicID, title string) BroadcastClassification {
	topicType := classifyBroadcastTopic(topicID)
	titleType := classifyBroadcastTitle(title)
	if broadcastTitleOverridesTopic(titleType, topicType) {
		return BroadcastClassification{Type: titleType, Source: "title"}
	}
	if topicType != BroadcastTypeUnknown {
		return BroadcastClassification{Type: topicType, Source: "topic"}
	}
	if titleType != BroadcastTypeUnknown {
		return BroadcastClassification{Type: titleType, Source: "title"}
	}
	return BroadcastClassification{Type: BroadcastTypeUnknown, Source: "unknown"}
}

func broadcastTitleOverridesTopic(titleType, topicType BroadcastType) bool {
	if titleType == BroadcastTypeUnknown || topicType == BroadcastTypeUnknown {
		return false
	}
	if topicType != BroadcastTypeGame && topicType != BroadcastTypeOther {
		return false
	}
	switch titleType {
	case BroadcastTypeSinging,
		BroadcastTypeASMR,
		BroadcastTypeMembership,
		BroadcastTypeEvent,
		BroadcastTypeWatchalong,
		BroadcastTypeNews:
		return true
	default:
		return false
	}
}

func ParseBroadcastType(raw string) (BroadcastType, bool) {
	typ, ok := broadcastTypeAliases[normalizeBroadcastToken(raw)]
	return typ, ok
}

func (t BroadcastType) Label() string {
	if label, ok := broadcastTypeLabels[t]; ok {
		return label
	}
	return "미분류"
}

func classifyBroadcastTopic(topicID string) BroadcastType {
	topics := broadcastTopics(topicID)
	for _, topic := range topics {
		if typ, ok := observedTopicTypes[topic]; ok {
			return typ
		}
	}
	return BroadcastTypeUnknown
}

func classifyBroadcastTitle(title string) BroadcastType {
	normalized := strings.ToLower(strings.TrimSpace(title))
	if typ, ok := classifyBroadcastTitleByKeyword(normalized); ok {
		return typ
	}
	if titleLooksLikeGameBroadcast(title) {
		return BroadcastTypeGame
	}
	return BroadcastTypeUnknown
}

func classifyBroadcastTitleByKeyword(normalized string) (BroadcastType, bool) {
	for _, rule := range broadcastTitleRules {
		if containsAnyBroadcastKeyword(normalized, rule.keywords) {
			return rule.typ, true
		}
	}
	return BroadcastTypeUnknown, false
}

func titleLooksLikeGameBroadcast(title string) bool {
	tag := firstBroadcastTitleTag(title)
	if tag == "" {
		return false
	}
	normalized := normalizeBroadcastTitleTag(tag)
	if strings.HasPrefix(normalized, "#") {
		return false
	}
	if containsAnyBroadcastKeyword(normalized, []string{"hololive", "ホロライブ", "ぶいすぽ", "vspo", "/", "／", "生スバル", "朝ミオ", "朝こよ", "昼ホロ"}) {
		return false
	}
	if containsAnyBroadcastKeyword(normalized, []string{"雑談", "歌枠", "同時視聴", "メン限", "メンバー", "asmr", "朝活", "お知らせ", "生誕", "birthday", "3d", "live", "cover", "official", "vlog", "カメラ", "開封", "unboxing"}) {
		return false
	}
	if _, ok := observedGameTitleExactMarkers[normalized]; ok {
		return true
	}
	return containsAnyBroadcastKeyword(normalized, observedGameTitleContainsMarkers)
}

var observedGameTitleExactMarkers = map[string]struct{}{
	"ff7":  {},
	"ff10": {},
	"ff14": {},
	"lol":  {},
	"tft":  {},
	"vct":  {},
}

var observedGameTitleContainsMarkers = []string{
	"7 days to die",
	"apex",
	"arknights",
	"backseat drivers",
	"biohazard",
	"cast n chill",
	"counter-strike",
	"danganronpa",
	"dark souls",
	"detroit",
	"dragon quest",
	"escape from tarkov",
	"final fantasy",
	"gta",
	"hytale",
	"league of legends",
	"mario",
	"minecraft",
	"monster hunter",
	"overwatch",
	"pokemon",
	"pokémon",
	"r.e.p.o",
	"reanimal",
	"resident evil",
	"slay the spire",
	"stardew valley",
	"street fighter",
	"teamfight tactics",
	"terraria",
	"valorant",
	"アークナイツ",
	"ウマ娘",
	"スト6",
	"スト６",
	"スーパーマリオ",
	"スレイザスパイア",
	"テラリア",
	"トモコレ",
	"ドラゴンクエスト",
	"ドラゴンボール",
	"バイオハザード",
	"パワプロ",
	"ポケモン",
	"マイクラ",
	"マリオ",
	"モンハン",
	"めっちゃカメレオン",
	"リズム天国",
	"仁王",
	"原神",
	"艦これ",
	"遊戯王",
	"龍が如く",
	"鳴潮",
}

func firstBroadcastTitleTag(title string) string {
	start := strings.Index(title, "【")
	if start < 0 {
		return ""
	}
	rest := title[start+len("【"):]
	end := strings.Index(rest, "】")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func containsAnyBroadcastKeyword(value string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(value, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func broadcastTopics(topicID string) []string {
	parts := strings.Split(topicID, ",")
	topics := make([]string, 0, len(parts))
	for _, part := range parts {
		topic := normalizeBroadcastTopic(part)
		if topic != "" {
			topics = append(topics, topic)
		}
	}
	return topics
}

func broadcastTopicMatches(topicID, wanted string) bool {
	wanted = normalizeBroadcastTopic(wanted)
	if wanted == "" {
		return true
	}
	for _, topic := range broadcastTopics(topicID) {
		if topic == wanted {
			return true
		}
	}
	return false
}

func normalizeBroadcastTopic(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ",")
}

func normalizeBroadcastToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "#")
	return strings.ReplaceAll(value, " ", "_")
}

func normalizeBroadcastTitleTag(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
