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
	"unicode"
	"unicode/utf8"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"golang.org/x/text/unicode/norm"

	_ "embed"
)

type BroadcastType = broadcasttype.Type

type BroadcastClassification struct {
	Type   BroadcastType
	Source string
}

const (
	BroadcastTypeGame        = broadcasttype.Game
	BroadcastTypeTalk        = broadcasttype.Talk
	BroadcastTypeSinging     = broadcasttype.Singing
	BroadcastTypeASMR        = broadcasttype.ASMR
	BroadcastTypeMembership  = broadcasttype.Membership
	BroadcastTypeEvent       = broadcasttype.Event
	BroadcastTypeHorseRacing = broadcasttype.HorseRacing
	BroadcastTypeWatchalong  = broadcasttype.Watchalong
	BroadcastTypeNews        = broadcasttype.News
	BroadcastTypeOther       = broadcasttype.Other
	BroadcastTypeUnknown     = broadcasttype.Unknown
)

//go:embed broadcast_type_rules.json
var broadcastTypeRulesJSON []byte

var broadcastRules = mustLoadBroadcastRules(broadcastTypeRulesJSON)

type broadcastTitleRule struct {
	Type           BroadcastType `json:"type"`
	Keywords       []string      `json:"keywords"`
	RejectKeywords []string      `json:"reject_keywords,omitempty"`
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
	Generic     []broadcastTitleRule     `json:"generic_title_rules,omitempty"`
	GameTag     broadcastGameTagRules    `json:"game_title_tag"`
}

type broadcastTitleStrength int

const (
	broadcastTitleStrengthUnknown broadcastTitleStrength = iota
	broadcastTitleStrengthGeneric
	broadcastTitleStrengthLead
	broadcastTitleStrengthStrong
)

type broadcastTitleClassification struct {
	Type     BroadcastType
	Strength broadcastTitleStrength
}

func ClassifyBroadcast(topicID, title string) BroadcastType {
	return ClassifyBroadcastWithSource(topicID, title).Type
}

func ClassifyBroadcastWithSource(topicID, title string) BroadcastClassification {
	topicType := classifyBroadcastTopic(topicID)
	titleClass := classifyBroadcastTitle(title)
	if broadcastTitleOverridesTopic(titleClass, topicType) {
		return BroadcastClassification{Type: titleClass.Type, Source: "title"}
	}
	if topicType != BroadcastTypeUnknown {
		return BroadcastClassification{Type: topicType, Source: "topic"}
	}
	if titleClass.Type != BroadcastTypeUnknown {
		return BroadcastClassification{Type: titleClass.Type, Source: "title"}
	}
	return BroadcastClassification{Type: BroadcastTypeUnknown, Source: "unknown"}
}

func broadcastTitleOverridesTopic(titleClass broadcastTitleClassification, topicType BroadcastType) bool {
	if titleClass.Type == BroadcastTypeUnknown || topicType == BroadcastTypeUnknown {
		return false
	}
	if !broadcastTopicAcceptsTitleOverride(topicType) {
		return false
	}
	return broadcastTitleClassOverridesTopic(titleClass)
}

func broadcastTopicAcceptsTitleOverride(topicType BroadcastType) bool {
	return topicType == BroadcastTypeGame || topicType == BroadcastTypeOther
}

func broadcastTitleClassOverridesTopic(titleClass broadcastTitleClassification) bool {
	switch titleClass.Type {
	case BroadcastTypeSinging,
		BroadcastTypeASMR,
		BroadcastTypeMembership,
		BroadcastTypeHorseRacing,
		BroadcastTypeWatchalong,
		BroadcastTypeNews:
		return true
	case BroadcastTypeEvent:
		return titleClass.Strength == broadcastTitleStrengthStrong || titleClass.Strength == broadcastTitleStrengthLead
	default:
		return false
	}
}

func ParseBroadcastType(raw string) (BroadcastType, bool) {
	return broadcasttype.Parse(raw)
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

func classifyBroadcastTitle(title string) broadcastTitleClassification {
	normalized := normalizeBroadcastText(title)
	leadTag := normalizeBroadcastTitleTag(firstBroadcastTitleTag(title))
	rejectScope := leadTag
	if rejectScope == "" {
		rejectScope = normalized
	}
	if typ, ok := classifyBroadcastTitleByKeyword(normalized, rejectScope, broadcastRules.TitleRules); ok {
		return broadcastTitleClassification{Type: typ, Strength: broadcastTitleStrengthStrong}
	}
	if leadTag != "" {
		if typ, ok := classifyBroadcastTitleByKeyword(leadTag, rejectScope, broadcastRules.Generic); ok {
			return broadcastTitleClassification{Type: typ, Strength: broadcastTitleStrengthLead}
		}
	}
	if titleLooksLikeGameBroadcast(normalized, leadTag) {
		return broadcastTitleClassification{Type: BroadcastTypeGame, Strength: broadcastTitleStrengthStrong}
	}
	if typ, ok := classifyBroadcastTitleByKeyword(normalized, rejectScope, broadcastRules.Generic); ok {
		return broadcastTitleClassification{Type: typ, Strength: broadcastTitleStrengthGeneric}
	}
	return broadcastTitleClassification{Type: BroadcastTypeUnknown, Strength: broadcastTitleStrengthUnknown}
}

func classifyBroadcastTitleByKeyword(normalized, rejectScope string, rules []broadcastTitleRule) (BroadcastType, bool) {
	for _, rule := range rules {
		if broadcastRejectScopeMatches(rejectScope, rule.RejectKeywords) {
			continue
		}
		if containsAnyBroadcastKeyword(normalized, rule.Keywords) {
			return rule.Type, true
		}
	}
	return BroadcastTypeUnknown, false
}

// reject는 boundary 없는 substring 검사라 «Winning Post10»처럼 숫자가 붙은 표기도 걸러낸다.
func broadcastRejectScopeMatches(rejectScope string, keywords []string) bool {
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(rejectScope, keyword) {
			return true
		}
	}
	return false
}

func titleLooksLikeGameBroadcast(normalized, leadTag string) bool {
	if leadTag != "" && containsExactBroadcastKeyword(leadTag, broadcastRules.GameTag.Exact) {
		return true
	}
	if leadTag != "" &&
		!containsAnyBroadcastKeyword(leadTag, broadcastRules.GameTag.RejectKeywords) &&
		containsAnyBroadcastKeyword(leadTag, broadcastRules.GameTag.Contains) {
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
		if broadcastKeywordMatches(value, keyword) {
			return true
		}
	}
	return false
}

func broadcastKeywordMatches(value, keyword string) bool {
	if keyword == "" {
		return false
	}
	if !isASCIIBroadcastKeyword(keyword) {
		return strings.Contains(value, keyword)
	}
	start := 0
	for start <= len(value) {
		idx := strings.Index(value[start:], keyword)
		if idx < 0 {
			return false
		}
		matchStart := start + idx
		matchEnd := matchStart + len(keyword)
		if hasBroadcastKeywordBoundary(value, matchStart, matchEnd) {
			return true
		}
		start = matchStart + 1
	}
	return false
}

func isASCIIBroadcastKeyword(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func hasBroadcastKeywordBoundary(value string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(value[:start])
		if isBroadcastWordRune(r) {
			return false
		}
	}
	if end < len(value) {
		r, _ := utf8.DecodeRuneInString(value[end:])
		if isBroadcastWordRune(r) {
			return false
		}
	}
	return true
}

func isBroadcastWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
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
	value = normalizeBroadcastText(value)
	return strings.Trim(value, ",")
}

func normalizeBroadcastTitleTag(value string) string {
	return normalizeBroadcastText(value)
}

func normalizeBroadcastText(value string) string {
	value = norm.NFKC.String(value)
	value = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff':
			return -1
		default:
			return r
		}
	}, value)
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Join(strings.Fields(value), " ")
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
	if err := validateBroadcastTopics(rules.Topics); err != nil {
		return err
	}
	if err := validateBroadcastRuleSet("title rule", rules.TitleRules); err != nil {
		return err
	}
	return validateBroadcastRuleSet("generic title rule", rules.Generic)
}

func validateBroadcastTopics(topics map[string]BroadcastType) error {
	for topic, typ := range topics {
		if !knownBroadcastType(typ) {
			return fmt.Errorf("topic %q uses unknown type %q", topic, typ)
		}
	}
	return nil
}

func validateBroadcastRuleSet(kind string, rules []broadcastTitleRule) error {
	for _, rule := range rules {
		if !knownBroadcastType(rule.Type) {
			return fmt.Errorf("%s uses unknown type %q", kind, rule.Type)
		}
		if len(rule.Keywords) == 0 {
			return fmt.Errorf("%s %q has no keywords", kind, rule.Type)
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
		rules.TitleRules[i].RejectKeywords = normalizeBroadcastKeywords(rules.TitleRules[i].RejectKeywords)
	}
	for i := range rules.Generic {
		rules.Generic[i].Keywords = normalizeBroadcastKeywords(rules.Generic[i].Keywords)
		rules.Generic[i].RejectKeywords = normalizeBroadcastKeywords(rules.Generic[i].RejectKeywords)
	}
	rules.GameTag.RejectKeywords = normalizeBroadcastKeywords(rules.GameTag.RejectKeywords)
	rules.GameTag.Exact = normalizeBroadcastTitleTags(rules.GameTag.Exact)
	rules.GameTag.Contains = normalizeBroadcastKeywords(rules.GameTag.Contains)
}

func normalizeBroadcastKeywords(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeBroadcastText(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
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
	return broadcasttype.Known(typ)
}
