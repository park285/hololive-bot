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

package filter

import (
	"sort"
	"strings"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	kst = model.KST
)

type datedCandidate struct {
	candidate model.Candidate
	date      time.Time
}

type memberProfile struct {
	display string
	tokens  []string
}

var categoryPriority = map[model.Category]int{
	model.CategoryBirthdayLive: 0,
	model.CategorySoloLive:     1,
	model.CategoryCollab:       2,
	model.CategoryEvent:        3,
	model.CategoryGoods:        4,
	model.CategoryOther:        5,
}

var sourceTierPriority = map[model.SourceTier]int{
	model.SourceTierOfficial:  0,
	model.SourceTierMedia:     1,
	model.SourceTierCommunity: 2,
}

func FilterCandidates(
	candidates []model.Candidate,
	period model.Period,
	now time.Time,
	roomMembers []string,
	membersData domain.MemberDataProvider,
	sourceValidator model.SourceURLValidator,
) []model.FilteredCandidate {
	periodCandidates := applyPeriodFilter(candidates, period, now)
	profiles := buildMemberProfiles(roomMembers, membersData)

	result := make([]model.FilteredCandidate, 0, len(periodCandidates))
	for i := range periodCandidates {
		item := &periodCandidates[i]
		filtered, ok := buildFilteredCandidate(*item, profiles, sourceValidator)
		if ok {
			result = append(result, filtered)
		}
	}

	sort.SliceStable(result, func(i, j int) bool {
		return lessFilteredCandidate(result[i], result[j])
	})

	return result
}

func buildFilteredCandidate(
	item datedCandidate,
	profiles []memberProfile,
	sourceValidator model.SourceURLValidator,
) (model.FilteredCandidate, bool) {
	matchedMembers := matchMembers(item.candidate, profiles)
	if len(matchedMembers) == 0 {
		return model.FilteredCandidate{}, false
	}

	tier, normalizedURL, ok := resolveSource(item.candidate, sourceValidator)
	if !ok {
		return model.FilteredCandidate{}, false
	}

	return model.FilteredCandidate{
		Candidate:      item.candidate,
		EffectiveDate:  item.date,
		MatchedMembers: matchedMembers,
		MemberText:     formatMemberText(matchedMembers),
		Category:       classifyCategory(item.candidate),
		SourceTier:     tier,
		SourceURL:      normalizedURL,
	}, true
}

func resolveSource(
	candidate model.Candidate,
	sourceValidator model.SourceURLValidator,
) (model.SourceTier, string, bool) {
	sourceURL := strings.TrimSpace(candidate.SourceURL)
	if sourceURL == "" {
		return model.SourceTierCommunity, "", false
	}
	if sourceValidator == nil {
		return model.SourceTierCommunity, sourceURL, true
	}

	tier, normalizedURL, validateErr := sourceValidator.ValidateSourceURL(sourceURL)
	if validateErr != nil {
		return model.SourceTierCommunity, "", false
	}
	if tier == model.SourceTierCommunity && !sourceValidator.HasCorroboration(candidate.Description) {
		return model.SourceTierCommunity, "", false
	}
	return tier, normalizedURL, true
}

func formatMemberText(matchedMembers []string) string {
	if len(matchedMembers) == 1 {
		return matchedMembers[0]
	}
	return strings.Join(matchedMembers, ", ")
}

func lessFilteredCandidate(left, right model.FilteredCandidate) bool {
	if !left.EffectiveDate.Equal(right.EffectiveDate) {
		return left.EffectiveDate.Before(right.EffectiveDate)
	}

	leftSource := sourceTierPriority[left.SourceTier]
	rightSource := sourceTierPriority[right.SourceTier]
	if leftSource != rightSource {
		return leftSource < rightSource
	}

	leftCategory := categoryPriority[left.Category]
	rightCategory := categoryPriority[right.Category]
	if leftCategory != rightCategory {
		return leftCategory < rightCategory
	}

	return left.Candidate.Title < right.Candidate.Title
}

func applyPeriodFilter(candidates []model.Candidate, period model.Period, now time.Time) []datedCandidate {
	rangeStart, rangeEnd := periodRange(period, now)

	result := make([]datedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidateDate, ok := resolveCandidateDate(candidate)
		if !ok {
			continue
		}
		candidateDate = candidateDate.In(kst)
		if withinRange(candidateDate, rangeStart, rangeEnd) {
			result = append(result, datedCandidate{candidate: candidate, date: candidateDate})
		}
	}

	return result
}

func periodRange(period model.Period, now time.Time) (time.Time, time.Time) {
	nowKST := now.In(kst)
	if model.NormalizePeriod(period) == model.PeriodMonthly {
		rangeStart := time.Date(nowKST.Year(), nowKST.Month(), 1, 0, 0, 0, 0, kst)
		return rangeStart, rangeStart.AddDate(0, 1, 0).Add(-time.Nanosecond)
	}

	return beginningOfDay(nowKST.AddDate(0, 0, -7)), endOfDay(nowKST.AddDate(0, 0, 21))
}

func withinRange(candidateDate, rangeStart, rangeEnd time.Time) bool {
	return !candidateDate.Before(rangeStart) && !candidateDate.After(rangeEnd)
}

func resolveCandidateDate(candidate model.Candidate) (time.Time, bool) {
	if candidate.Type == domain.MajorEventTypeNews {
		return firstAvailableDate(candidate.PubDate, candidate.EventStartDate)
	}
	return firstAvailableDate(candidate.EventStartDate, candidate.PubDate)
}

func firstAvailableDate(dates ...*time.Time) (time.Time, bool) {
	for _, date := range dates {
		if date != nil {
			return *date, true
		}
	}
	return time.Time{}, false
}

func beginningOfDay(t time.Time) time.Time {
	local := t.In(kst)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, kst)
}

func endOfDay(t time.Time) time.Time {
	local := t.In(kst)
	return time.Date(local.Year(), local.Month(), local.Day(), 23, 59, 59, int(time.Second-time.Nanosecond), kst)
}

func buildMemberProfiles(roomMembers []string, membersData domain.MemberDataProvider) []memberProfile {
	profiles := make([]memberProfile, 0, len(roomMembers))
	for _, raw := range roomMembers {
		display := strings.TrimSpace(raw)
		if display == "" {
			continue
		}

		tokenSet := make(map[string]struct{})
		appendToken := func(value string) {
			normalized := stringutil.NormalizeKey(value)
			if normalized == "" {
				return
			}
			tokenSet[normalized] = struct{}{}
		}

		appendToken(display)
		display = enrichMemberProfile(display, membersData, appendToken)

		tokens := make([]string, 0, len(tokenSet))
		for token := range tokenSet {
			tokens = append(tokens, token)
		}
		sort.Strings(tokens)
		profiles = append(profiles, memberProfile{display: display, tokens: tokens})
	}

	return profiles
}

func enrichMemberProfile(display string, membersData domain.MemberDataProvider, appendToken func(string)) string {
	if membersData == nil {
		return display
	}

	member := membersData.FindMemberByName(display)
	if member == nil {
		member = membersData.FindMemberByAlias(display)
	}
	if member == nil {
		return display
	}

	if member.NameKo != "" {
		display = member.NameKo
	} else if member.Name != "" {
		display = member.Name
	}

	appendToken(member.Name)
	appendToken(member.NameKo)
	appendToken(member.NameJa)
	appendMemberAliasTokens(member, appendToken)

	return display
}

func appendMemberAliasTokens(member *domain.Member, appendToken func(string)) {
	if member == nil || member.Aliases == nil {
		return
	}
	for _, alias := range member.Aliases.Ko {
		appendToken(alias)
	}
	for _, alias := range member.Aliases.Ja {
		appendToken(alias)
	}
}

func matchMembers(candidate model.Candidate, profiles []memberProfile) []string {
	if len(profiles) == 0 {
		return nil
	}

	normalizedBody := stringutil.NormalizeKey(candidate.Title + " " + candidate.Description)
	memberTokenSet := buildCandidateMemberTokenSet(candidate.Members)

	matched := make([]string, 0)
	matchedSet := make(map[string]struct{})
	for _, profile := range profiles {
		if profileMatchesCandidate(profile, memberTokenSet, normalizedBody) {
			matched = appendUniqueMatchedMember(matched, matchedSet, profile.display)
		}
	}

	return matched
}

func buildCandidateMemberTokenSet(members []string) map[string]struct{} {
	memberTokenSet := make(map[string]struct{}, len(members))
	for _, member := range members {
		normalized := stringutil.NormalizeKey(member)
		if normalized != "" {
			memberTokenSet[normalized] = struct{}{}
		}
	}
	return memberTokenSet
}

func profileMatchesCandidate(
	profile memberProfile,
	memberTokenSet map[string]struct{},
	normalizedBody string,
) bool {
	for _, token := range profile.tokens {
		if tokenMatchesCandidate(token, memberTokenSet, normalizedBody) {
			return true
		}
	}
	return false
}

func tokenMatchesCandidate(token string, memberTokenSet map[string]struct{}, normalizedBody string) bool {
	if token == "" {
		return false
	}
	if _, ok := memberTokenSet[token]; ok {
		return true
	}
	return strings.Contains(normalizedBody, token)
}

func appendUniqueMatchedMember(
	matched []string,
	matchedSet map[string]struct{},
	display string,
) []string {
	if _, exists := matchedSet[display]; exists {
		return matched
	}
	matchedSet[display] = struct{}{}
	return append(matched, display)
}

func classifyCategory(candidate model.Candidate) model.Category {
	text := strings.ToLower(candidate.Title + " " + candidate.Description)

	keywordRules := []struct {
		category model.Category
		keywords []string
	}{
		{category: model.CategoryBirthdayLive, keywords: []string{"生誕", "생일", "birthday"}},
		{category: model.CategorySoloLive, keywords: []string{"ソロライブ", "solo live", "단독 라이브"}},
		{category: model.CategoryCollab, keywords: []string{"コラボ", "콜라보", "collaboration"}},
		{category: model.CategoryGoods, keywords: []string{"グッズ", "굿즈", "merchandise"}},
		{category: model.CategoryEvent, keywords: []string{"fes", "expo", "live", "concert", "event"}},
	}

	for _, rule := range keywordRules {
		if containsAny(text, rule.keywords) {
			return rule.category
		}
	}

	return model.CategoryOther
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
