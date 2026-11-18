package handlers

import (
	_ "embed"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
)

type BroadcastClassification struct {
	Type   broadcasttype.Type
	Source string
}

//go:embed broadcast_type_rules.json
var broadcastTypeRulesJSON []byte

var broadcastRules = mustLoadBroadcastRules(broadcastTypeRulesJSON)

type broadcastTitleRule struct {
	Type           broadcasttype.Type `json:"type"`
	Keywords       []string           `json:"keywords"`
	ExactTags      []string           `json:"exact_tags,omitempty"`
	RejectKeywords []string           `json:"reject_keywords,omitempty"`
}

type broadcastGameTagRules struct {
	RejectKeywords []string `json:"reject_keywords"`
	Exact          []string `json:"exact"`
	Contains       []string `json:"contains"`
	TagOnly        []string `json:"tag_only,omitempty"`
}

type broadcastTypeRules struct {
	Version        string                          `json:"version"`
	SourceNotes    []string                        `json:"source_notes,omitempty"`
	Topics         map[string]broadcasttype.Type   `json:"topics"`
	TitleRules     []broadcastTitleRule            `json:"title_rules"`
	Generic        []broadcastTitleRule            `json:"generic_title_rules,omitempty"`
	GameTag        broadcastGameTagRules           `json:"game_title_tag"`
	ReviewedVideos map[string]broadcastVideoReview `json:"reviewed_videos,omitempty"`

	gameTopicTags []string
}

type broadcastTitleStrength int

const (
	broadcastTitleStrengthUnknown broadcastTitleStrength = iota
	broadcastTitleStrengthGeneric
	broadcastTitleStrengthLead
	broadcastTitleStrengthStrong
)

type broadcastTitleClassification struct {
	Type     broadcasttype.Type
	Strength broadcastTitleStrength
}

var broadcastTitleAlwaysOverrides = map[broadcasttype.Type]struct{}{
	broadcasttype.ASMR:        {},
	broadcasttype.Membership:  {},
	broadcasttype.HorseRacing: {},
	broadcasttype.Watchalong:  {},
}

var broadcastTitleOverrideStrengths = map[broadcasttype.Type]map[broadcastTitleStrength]struct{}{
	broadcasttype.Singing: {broadcastTitleStrengthStrong: {}},
	broadcasttype.News:    {broadcastTitleStrengthStrong: {}},
	broadcasttype.Talk:    {broadcastTitleStrengthStrong: {}},
	broadcasttype.Event: {
		broadcastTitleStrengthLead:   {},
		broadcastTitleStrengthStrong: {},
	},
}

func ClassifyBroadcastWithSource(topicID, title string) BroadcastClassification {
	topicType := classifyBroadcastTopic(topicID)
	titleClass := classifyBroadcastTitle(title)

	if broadcastTitleOverridesTopic(titleClass, topicType) {
		return BroadcastClassification{Type: titleClass.Type, Source: "title"}
	}

	if topicType != broadcasttype.Unknown {
		return BroadcastClassification{Type: topicType, Source: "topic"}
	}

	if titleClass.Type != broadcasttype.Unknown {
		return BroadcastClassification{Type: titleClass.Type, Source: "title"}
	}

	return BroadcastClassification{Type: broadcasttype.Unknown, Source: "unknown"}
}

func broadcastTitleOverridesTopic(titleClass broadcastTitleClassification, topicType broadcasttype.Type) bool {
	if titleClass.Type == broadcasttype.Unknown || topicType == broadcasttype.Unknown {
		return false
	}

	if titleClass.Type == broadcasttype.Membership && topicType != broadcasttype.Membership {
		return true
	}

	if !broadcastTopicAcceptsTitleOverride(topicType) {
		return false
	}

	return broadcastTitleClassOverridesTopic(titleClass)
}

func broadcastTopicAcceptsTitleOverride(topicType broadcasttype.Type) bool {
	return topicType == broadcasttype.Game || topicType == broadcasttype.Other
}

func broadcastTitleClassOverridesTopic(titleClass broadcastTitleClassification) bool {
	if _, ok := broadcastTitleAlwaysOverrides[titleClass.Type]; ok {
		return true
	}

	strengths, ok := broadcastTitleOverrideStrengths[titleClass.Type]
	if !ok {
		return false
	}

	_, ok = strengths[titleClass.Strength]

	return ok
}

func classifyBroadcastTopic(topicID string) broadcasttype.Type {
	topics := broadcastTopics(topicID)
	for _, topic := range topics {
		if typ, ok := broadcastRules.Topics[topic]; ok {
			return typ
		}
	}

	return broadcasttype.Unknown
}

func classifyBroadcastTitle(title string) broadcastTitleClassification {
	normalized := normalizeBroadcastText(title)
	leadTag := normalizeBroadcastTitleTag(firstBroadcastTitleTag(title))
	rejectScope := leadTag

	if rejectScope == "" {
		rejectScope = normalized
	}

	if typ, ok := classifyBroadcastTitleByKeyword(normalized, rejectScope, leadTag, broadcastRules.TitleRules); ok {
		return broadcastTitleClassification{Type: typ, Strength: broadcastTitleStrengthStrong}
	}

	if leadTag != "" {
		if typ, ok := classifyBroadcastTitleByKeyword(leadTag, rejectScope, leadTag, broadcastRules.Generic); ok {
			return broadcastTitleClassification{Type: typ, Strength: broadcastTitleStrengthLead}
		}
	}

	if titleLooksLikeGameBroadcast(normalized, leadTag) {
		return broadcastTitleClassification{Type: broadcasttype.Game, Strength: broadcastTitleStrengthStrong}
	}

	if typ, ok := classifyBroadcastTitleByKeyword(normalized, normalized, leadTag, broadcastRules.Generic); ok {
		return broadcastTitleClassification{Type: typ, Strength: broadcastTitleStrengthGeneric}
	}

	return broadcastTitleClassification{Type: broadcasttype.Unknown, Strength: broadcastTitleStrengthUnknown}
}

func classifyBroadcastTitleByKeyword(normalized, rejectScope, leadTag string, rules []broadcastTitleRule) (broadcasttype.Type, bool) {
	for _, rule := range rules {
		if broadcastRejectScopeMatches(rejectScope, rule.RejectKeywords) {
			continue
		}

		if containsAnyBroadcastKeyword(normalized, rule.Keywords) ||
			(leadTag != "" && containsExactBroadcastKeyword(leadTag, rule.ExactTags)) {
			return rule.Type, true
		}
	}

	return broadcasttype.Unknown, false
}

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
		(containsAnyBroadcastKeyword(leadTag, broadcastRules.GameTag.Contains) ||
			containsAnyBroadcastKeyword(leadTag, broadcastRules.GameTag.TagOnly) ||
			hasAnyBroadcastKeywordPrefix(leadTag, broadcastRules.gameTopicTags)) {
		return true
	}

	return containsAnyBroadcastKeyword(normalized, broadcastRules.GameTag.Contains)
}

func hasAnyBroadcastKeywordPrefix(value string, keywords []string) bool {
	for _, keyword := range keywords {
		if keyword != "" && strings.HasPrefix(value, keyword) && hasBroadcastKeywordBoundary(value, 0, len(keyword)) {
			return true
		}
	}

	return false
}

var broadcastTitleTagDelims = [][2]string{
	{"【", "】"},
	{"〖", "〗"},
	{"≪", "≫"},
	{"◤", "◢"},
}

func firstBroadcastTitleTag(title string) string {
	tag := ""
	tagIdx := -1

	for _, delim := range broadcastTitleTagDelims {
		open := strings.Index(title, delim[0])
		if open < 0 || (tagIdx >= 0 && open >= tagIdx) {
			continue
		}

		body, _, ok := strings.Cut(title[open+len(delim[0]):], delim[1])
		if !ok {
			continue
		}

		tagIdx = open
		tag = strings.TrimSpace(body)
	}

	return tag
}

func containsExactBroadcastKeyword(value string, keywords []string) bool {
	return slices.Contains(keywords, value)
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
		if isBroadcastWordRune(r) && !unicode.IsDigit(r) {
			return false
		}
	}

	return true
}

func isBroadcastWordRune(r rune) bool {
	return r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))
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

	return slices.Contains(broadcastTopics(topicID), wanted)
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
		case '\u2019':
			return '\''
		default:
			return r
		}
	}, value)
	value = strings.ToLower(strings.TrimSpace(value))

	return strings.Join(strings.Fields(value), " ")
}
