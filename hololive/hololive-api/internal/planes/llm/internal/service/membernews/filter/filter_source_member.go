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

	"github.com/park285/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func formatMemberText(matchedMembers []string) string {
	if len(matchedMembers) == 1 {
		return matchedMembers[0]
	}
	return strings.Join(matchedMembers, ", ")
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

func matchMembers(candidate *model.Candidate, profiles []memberProfile) []string {
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
