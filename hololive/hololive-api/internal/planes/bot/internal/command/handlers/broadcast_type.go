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

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	_ "embed"
)

type BroadcastType string

type BroadcastClassification struct {
	Type   BroadcastType
	Source string
}

const (
	BroadcastTypeGame        BroadcastType = "game"
	BroadcastTypeTalk        BroadcastType = "talk"
	BroadcastTypeSinging     BroadcastType = "singing"
	BroadcastTypeASMR        BroadcastType = "asmr"
	BroadcastTypeMembership  BroadcastType = "membership"
	BroadcastTypeEvent       BroadcastType = "event"
	BroadcastTypeHorseRacing BroadcastType = "horse_racing"
	BroadcastTypeWatchalong  BroadcastType = "watchalong"
	BroadcastTypeNews        BroadcastType = "news"
	BroadcastTypeOther       BroadcastType = "other"
	BroadcastTypeUnknown     BroadcastType = "unknown"
)

//go:embed broadcast_type_rules.json
var broadcastTypeRulesJSON []byte

var broadcastRules = mustLoadBroadcastRules(broadcastTypeRulesJSON)

var broadcastTypeAliases = map[string]BroadcastType{
	"game": BroadcastTypeGame, "games": BroadcastTypeGame, "gaming": BroadcastTypeGame, "게임": BroadcastTypeGame, "겜": BroadcastTypeGame, "게임방송": BroadcastTypeGame,
	"talk": BroadcastTypeTalk, "zatsudan": BroadcastTypeTalk, "free_talk": BroadcastTypeTalk, "free-talk": BroadcastTypeTalk, "잡담": BroadcastTypeTalk, "토크": BroadcastTypeTalk, "수다": BroadcastTypeTalk,
	"singing": BroadcastTypeSinging, "song": BroadcastTypeSinging, "karaoke": BroadcastTypeSinging, "music": BroadcastTypeSinging, "노래": BroadcastTypeSinging, "노래방": BroadcastTypeSinging, "歌枠": BroadcastTypeSinging, "우타와꾸": BroadcastTypeSinging,
	"asmr":       BroadcastTypeASMR,
	"membership": BroadcastTypeMembership, "member": BroadcastTypeMembership, "members": BroadcastTypeMembership, "membersonly": BroadcastTypeMembership, "memberonly": BroadcastTypeMembership, "멤버십": BroadcastTypeMembership, "멤버": BroadcastTypeMembership, "멤버한정": BroadcastTypeMembership, "멤버전용": BroadcastTypeMembership,
	"event": BroadcastTypeEvent, "events": BroadcastTypeEvent, "birthday": BroadcastTypeEvent, "3d": BroadcastTypeEvent, "outfit": BroadcastTypeEvent, "이벤트": BroadcastTypeEvent, "기념": BroadcastTypeEvent, "생일": BroadcastTypeEvent, "신의상": BroadcastTypeEvent, "3d방송": BroadcastTypeEvent,
	"horse_racing": BroadcastTypeHorseRacing, "horse-racing": BroadcastTypeHorseRacing, "horseracing": BroadcastTypeHorseRacing, "keiba": BroadcastTypeHorseRacing, "경마": BroadcastTypeHorseRacing, "競馬": BroadcastTypeHorseRacing,
	"watchalong": BroadcastTypeWatchalong, "watch-along": BroadcastTypeWatchalong, "watch_party": BroadcastTypeWatchalong, "watchparty": BroadcastTypeWatchalong, "동시시청": BroadcastTypeWatchalong, "같이보기": BroadcastTypeWatchalong,
	"news": BroadcastTypeNews, "notice": BroadcastTypeNews, "announcement": BroadcastTypeNews, "뉴스": BroadcastTypeNews, "공지": BroadcastTypeNews,
	"other": BroadcastTypeOther, "variety": BroadcastTypeOther, "etc": BroadcastTypeOther, "기타": BroadcastTypeOther,
	"unknown": BroadcastTypeUnknown, "미분류": BroadcastTypeUnknown,
}

var broadcastTypeLabels = map[BroadcastType]string{
	BroadcastTypeGame:        "게임",
	BroadcastTypeTalk:        "잡담",
	BroadcastTypeSinging:     "노래",
	BroadcastTypeASMR:        "ASMR",
	BroadcastTypeMembership:  "멤버십",
	BroadcastTypeEvent:       "이벤트",
	BroadcastTypeHorseRacing: "경마",
	BroadcastTypeWatchalong:  "동시시청",
	BroadcastTypeNews:        "뉴스/공지",
	BroadcastTypeOther:       "기타",
}

type broadcastTitleRule struct {
	Type     BroadcastType `json:"type"`
	Keywords []string      `json:"keywords"`
}

type broadcastGameTagRules struct {
	RejectKeywords []string `json:"reject_keywords"`
	Exact          []string `json:"exact"`
	Contains       []string `json:"contains"`
}

type broadcastTypeRules struct {
	Version     string                   `json:"version"`
	SourceNotes []string                 `json:"source_notes,omitempty"`
	Topics      map[string]BroadcastType `json:"topics"`
	TitleRules  []broadcastTitleRule     `json:"title_rules"`
	GameTag     broadcastGameTagRules    `json:"game_title_tag"`
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
		BroadcastTypeHorseRacing,
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
		if typ, ok := broadcastRules.Topics[topic]; ok {
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
	if containsAnyBroadcastKeyword(normalized, broadcastRules.GameTag.Contains) {
		return BroadcastTypeGame
	}
	if titleLooksLikeGameBroadcast(title) {
		return BroadcastTypeGame
	}
	return BroadcastTypeUnknown
}

func classifyBroadcastTitleByKeyword(normalized string) (BroadcastType, bool) {
	for _, rule := range broadcastRules.TitleRules {
		if containsAnyBroadcastKeyword(normalized, rule.Keywords) {
			return rule.Type, true
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
	if containsAnyBroadcastKeyword(normalized, broadcastRules.GameTag.RejectKeywords) {
		return false
	}
	if containsExactBroadcastKeyword(normalized, broadcastRules.GameTag.Exact) {
		return true
	}
	return containsAnyBroadcastKeyword(normalized, broadcastRules.GameTag.Contains)
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

func containsExactBroadcastKeyword(value string, keywords []string) bool {
	for _, keyword := range keywords {
		if value == keyword {
			return true
		}
	}
	return false
}

func containsAnyBroadcastKeyword(value string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(value, keyword) {
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

func mustLoadBroadcastRules(data []byte) broadcastTypeRules {
	var rules broadcastTypeRules
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		panic(fmt.Sprintf("load broadcast type rules: %v", err))
	}
	if err := validateBroadcastRules(&rules); err != nil {
		panic(fmt.Sprintf("validate broadcast type rules: %v", err))
	}
	normalizeBroadcastRules(&rules)
	return rules
}

func validateBroadcastRules(rules *broadcastTypeRules) error {
	if strings.TrimSpace(rules.Version) == "" {
		return fmt.Errorf("missing version")
	}
	for topic, typ := range rules.Topics {
		if !knownBroadcastType(typ) {
			return fmt.Errorf("topic %q uses unknown type %q", topic, typ)
		}
	}
	for _, rule := range rules.TitleRules {
		if !knownBroadcastType(rule.Type) {
			return fmt.Errorf("title rule uses unknown type %q", rule.Type)
		}
		if len(rule.Keywords) == 0 {
			return fmt.Errorf("title rule %q has no keywords", rule.Type)
		}
	}
	return nil
}

func normalizeBroadcastRules(rules *broadcastTypeRules) {
	topics := make(map[string]BroadcastType, len(rules.Topics))
	for topic, typ := range rules.Topics {
		topics[normalizeBroadcastTopic(topic)] = typ
	}
	rules.Topics = topics
	for i := range rules.TitleRules {
		rules.TitleRules[i].Keywords = normalizeBroadcastKeywords(rules.TitleRules[i].Keywords)
	}
	rules.GameTag.RejectKeywords = normalizeBroadcastKeywords(rules.GameTag.RejectKeywords)
	rules.GameTag.Exact = normalizeBroadcastTitleTags(rules.GameTag.Exact)
	rules.GameTag.Contains = normalizeBroadcastKeywords(rules.GameTag.Contains)
}

func normalizeBroadcastKeywords(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func normalizeBroadcastTitleTags(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeBroadcastTitleTag(value)
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func knownBroadcastType(typ BroadcastType) bool {
	switch typ {
	case BroadcastTypeGame,
		BroadcastTypeTalk,
		BroadcastTypeSinging,
		BroadcastTypeASMR,
		BroadcastTypeMembership,
		BroadcastTypeEvent,
		BroadcastTypeHorseRacing,
		BroadcastTypeWatchalong,
		BroadcastTypeNews,
		BroadcastTypeOther,
		BroadcastTypeUnknown:
		return true
	default:
		return false
	}
}
