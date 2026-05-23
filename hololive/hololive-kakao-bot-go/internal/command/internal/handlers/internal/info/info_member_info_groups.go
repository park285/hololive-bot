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

package info

import (
	"context"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
)

func (c *MemberInfoCommand) memberGroups(ctx context.Context, member *domain.Member) []string {
	if member == nil {
		return nil
	}

	profile, translated, err := c.Deps().OfficialProfiles.GetWithTranslation(ctx, member.Name)
	if err != nil {
		c.log().Debug("Failed to load profile for directory",
			slog.String("member", member.Name),
			slog.Any("error", err),
		)

		return nil
	}

	rawValues := ExtractUnitValues(profile, translated)
	if len(rawValues) == 0 {
		return nil
	}

	return normalizedMemberGroups(rawValues)
}

func normalizedMemberGroups(rawValues []string) []string {
	normalized := make([]string, 0, len(rawValues))
	seen := make(map[string]bool)

	for _, raw := range rawValues {
		for _, token := range SplitGroupTokens(raw) {
			normalized = appendMemberGroup(normalized, seen, token)
		}
	}

	return normalized
}

func appendMemberGroup(groups []string, seen map[string]bool, token string) []string {
	name := NormalizeMemberGroup(token)
	if name == "" || seen[name] {
		return groups
	}

	seen[name] = true
	return append(groups, name)
}

func ExtractUnitValues(profile *domain.TalentProfile, translated *domain.Translated) []string {
	values := make([]string, 0, 2)

	if value, ok := translatedUnitValue(translated); ok {
		values = append(values, value)
	}

	if len(values) == 0 {
		values = append(values, profileUnitValues(profile)...)
	}

	return values
}

func translatedUnitValue(translated *domain.Translated) (string, bool) {
	if translated == nil {
		return "", false
	}

	for _, row := range translated.Data {
		if strings.Contains(row.Label, "유닛") && stringutil.TrimSpace(row.Value) != "" {
			return row.Value, true
		}
	}

	return "", false
}

func profileUnitValues(profile *domain.TalentProfile) []string {
	if profile == nil {
		return nil
	}

	for _, entry := range profile.DataEntries {
		if profileUnitLabel(entry.Label) {
			return nonEmptyUnitValue(entry.Value)
		}
	}

	return nil
}

func profileUnitLabel(label string) bool {
	return strings.Contains(label, "ユニット") || strings.Contains(label, "Unit")
}

func nonEmptyUnitValue(value string) []string {
	if stringutil.TrimSpace(value) == "" {
		return nil
	}

	return []string{value}
}

func SplitGroupTokens(raw string) []string {
	clean := strings.ReplaceAll(raw, "／", "/")

	clean = strings.ReplaceAll(clean, "、", "/")
	clean = strings.ReplaceAll(clean, "・", "/")

	tokens := strings.Split(clean, "/")
	if len(tokens) == 0 {
		return []string{raw}
	}

	result := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = stringutil.TrimSpace(token)
		if token != "" {
			result = append(result, token)
		}
	}

	if len(result) == 0 {
		return []string{raw}
	}

	return result
}

func NormalizeMemberGroup(name string) string {
	trimmed := stringutil.TrimSpace(name)
	if trimmed == "" {
		return DefaultMemberDirectoryGroup
	}

	trimmed = stripMemberGroupAnnotation(trimmed)

	if mapped, ok := memberDirectoryGroupAliases[trimmed]; ok {
		return mapped
	}

	return normalizeEnglishMemberGroup(trimmed)
}

func stripMemberGroupAnnotation(name string) string {
	if idx := strings.IndexAny(name, "（("); idx != -1 {
		return stringutil.TrimSpace(name[:idx])
	}

	return name
}

func normalizeEnglishMemberGroup(name string) string {
	if suffix, ok := japaneseEnglishMemberGroupSuffix(name); ok {
		return suffix
	}

	if suffix, ok := englishMemberGroupSuffix(name); ok {
		return suffix
	}

	return name
}

func japaneseEnglishMemberGroupSuffix(name string) (string, bool) {
	if !strings.HasPrefix(name, "ホロライブEnglish -") {
		return "", false
	}

	suffix := strings.Trim(name[len("ホロライブEnglish -"):], "-")
	return suffix, suffix != ""
}

func englishMemberGroupSuffix(name string) (string, bool) {
	after, ok := strings.CutPrefix(name, "hololive English")
	if !ok {
		return "", false
	}

	suffix := stringutil.TrimSpace(after)
	suffix = strings.Trim(suffix, "-")
	return suffix, suffix != ""
}

func PrimaryMemberName(member *domain.Member) string {
	if member == nil {
		return ""
	}

	primary := strings.Trim(stringutil.TrimSpace(member.NameKo), ",")
	if primary != "" {
		return primary
	}

	return member.Name
}
