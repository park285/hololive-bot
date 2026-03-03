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

// FilterCandidates: 기간/멤버/카테고리/출처 검증을 모두 적용합니다.
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
		matchedMembers := matchMembers(item.candidate, profiles)
		if len(matchedMembers) == 0 {
			continue
		}

		sourceURL := strings.TrimSpace(item.candidate.SourceURL)
		if sourceURL == "" {
			continue
		}

		tier := model.SourceTierCommunity
		normalizedURL := sourceURL
		if sourceValidator != nil {
			validatedTier, validatedURL, validateErr := sourceValidator.ValidateSourceURL(sourceURL)
			if validateErr != nil {
				continue
			}
			tier = validatedTier
			normalizedURL = validatedURL
			if tier == model.SourceTierCommunity && !sourceValidator.HasCorroboration(item.candidate.Description) {
				continue
			}
		}

		memberText := matchedMembers[0]
		if len(matchedMembers) > 1 {
			memberText = strings.Join(matchedMembers, ", ")
		}

		result = append(result, model.FilteredCandidate{
			Candidate:      item.candidate,
			EffectiveDate:  item.date,
			MatchedMembers: matchedMembers,
			MemberText:     memberText,
			Category:       classifyCategory(item.candidate),
			SourceTier:     tier,
			SourceURL:      normalizedURL,
		})
	}

	sort.SliceStable(result, func(i, j int) bool {
		left := result[i]
		right := result[j]

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
	})

	return result
}

func applyPeriodFilter(candidates []model.Candidate, period model.Period, now time.Time) []datedCandidate {
	normalizedPeriod := model.NormalizePeriod(period)
	nowKST := now.In(kst)

	var (
		rangeStart time.Time
		rangeEnd   time.Time
	)

	switch normalizedPeriod {
	case model.PeriodMonthly:
		rangeStart = time.Date(nowKST.Year(), nowKST.Month(), 1, 0, 0, 0, 0, kst)
		rangeEnd = rangeStart.AddDate(0, 1, 0).Add(-time.Nanosecond)
	default:
		rangeStart = beginningOfDay(nowKST.AddDate(0, 0, -7))
		rangeEnd = endOfDay(nowKST.AddDate(0, 0, 21))
	}

	result := make([]datedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidateDate, ok := resolveCandidateDate(candidate)
		if !ok {
			continue
		}
		candidateDate = candidateDate.In(kst)
		if candidateDate.Before(rangeStart) || candidateDate.After(rangeEnd) {
			continue
		}

		result = append(result, datedCandidate{candidate: candidate, date: candidateDate})
	}

	return result
}

func resolveCandidateDate(candidate model.Candidate) (time.Time, bool) {
	switch candidate.Type {
	case domain.MajorEventTypeNews:
		if candidate.PubDate != nil {
			return *candidate.PubDate, true
		}
		if candidate.EventStartDate != nil {
			return *candidate.EventStartDate, true
		}
	default:
		if candidate.EventStartDate != nil {
			return *candidate.EventStartDate, true
		}
		if candidate.PubDate != nil {
			return *candidate.PubDate, true
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

		if membersData != nil {
			member := membersData.FindMemberByName(display)
			if member == nil {
				member = membersData.FindMemberByAlias(display)
			}
			if member != nil {
				if member.NameKo != "" {
					display = member.NameKo
				} else if member.Name != "" {
					display = member.Name
				}
				appendToken(member.Name)
				appendToken(member.NameKo)
				appendToken(member.NameJa)
				if member.Aliases != nil {
					for _, alias := range member.Aliases.Ko {
						appendToken(alias)
					}
					for _, alias := range member.Aliases.Ja {
						appendToken(alias)
					}
				}
			}
		}

		tokens := make([]string, 0, len(tokenSet))
		for token := range tokenSet {
			tokens = append(tokens, token)
		}
		sort.Strings(tokens)
		profiles = append(profiles, memberProfile{display: display, tokens: tokens})
	}

	return profiles
}

func matchMembers(candidate model.Candidate, profiles []memberProfile) []string {
	if len(profiles) == 0 {
		return nil
	}

	normalizedBody := stringutil.NormalizeKey(candidate.Title + " " + candidate.Description)
	memberTokenSet := make(map[string]struct{}, len(candidate.Members))
	for _, member := range candidate.Members {
		normalized := stringutil.NormalizeKey(member)
		if normalized == "" {
			continue
		}
		memberTokenSet[normalized] = struct{}{}
	}

	matched := make([]string, 0)
	matchedSet := make(map[string]struct{})
	for _, profile := range profiles {
		isMatched := false
		for _, token := range profile.tokens {
			if token == "" {
				continue
			}
			if _, ok := memberTokenSet[token]; ok {
				isMatched = true
				break
			}
			if strings.Contains(normalizedBody, token) {
				isMatched = true
				break
			}
		}
		if !isMatched {
			continue
		}

		if _, exists := matchedSet[profile.display]; exists {
			continue
		}
		matchedSet[profile.display] = struct{}{}
		matched = append(matched, profile.display)
	}

	return matched
}

func classifyCategory(candidate model.Candidate) model.Category {
	text := strings.ToLower(candidate.Title + " " + candidate.Description)

	if strings.Contains(text, "生誕") || strings.Contains(text, "생일") || strings.Contains(text, "birthday") {
		return model.CategoryBirthdayLive
	}
	if strings.Contains(text, "ソロライブ") || strings.Contains(text, "solo live") || strings.Contains(text, "단독 라이브") {
		return model.CategorySoloLive
	}
	if strings.Contains(text, "コラボ") || strings.Contains(text, "콜라보") || strings.Contains(text, "collaboration") {
		return model.CategoryCollab
	}
	if strings.Contains(text, "グッズ") || strings.Contains(text, "굿즈") || strings.Contains(text, "merchandise") {
		return model.CategoryGoods
	}
	if strings.Contains(text, "fes") || strings.Contains(text, "expo") || strings.Contains(text, "live") || strings.Contains(text, "concert") || strings.Contains(text, "event") {
		return model.CategoryEvent
	}
	return model.CategoryOther
}
