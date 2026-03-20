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

package command

import (
	"context"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
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

	rawValues := extractUnitValues(profile, translated)
	if len(rawValues) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(rawValues))
	seen := make(map[string]bool)

	for _, raw := range rawValues {
		for _, token := range splitGroupTokens(raw) {
			name := normalizeMemberGroup(token)
			if name == "" {
				continue
			}

			if !seen[name] {
				normalized = append(normalized, name)
				seen[name] = true
			}
		}
	}

	return normalized
}

func extractUnitValues(profile *domain.TalentProfile, translated *domain.Translated) []string {
	values := make([]string, 0, 2)

	if translated != nil {
		for _, row := range translated.Data {
			if strings.Contains(row.Label, "유닛") && stringutil.TrimSpace(row.Value) != "" {
				values = append(values, row.Value)
				break
			}
		}
	}

	if len(values) == 0 && profile != nil {
		for _, entry := range profile.DataEntries {
			if strings.Contains(entry.Label, "ユニット") || strings.Contains(entry.Label, "Unit") {
				if stringutil.TrimSpace(entry.Value) != "" {
					values = append(values, entry.Value)
				}

				break
			}
		}
	}

	return values
}

func splitGroupTokens(raw string) []string {
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

func normalizeMemberGroup(name string) string {
	trimmed := stringutil.TrimSpace(name)
	if trimmed == "" {
		return defaultMemberDirectoryGroup
	}

	if idx := strings.IndexAny(trimmed, "（("); idx != -1 {
		trimmed = stringutil.TrimSpace(trimmed[:idx])
	}

	if mapped, ok := memberDirectoryGroupAliases[trimmed]; ok {
		return mapped
	}

	if strings.HasPrefix(trimmed, "ホロライブEnglish -") {
		suffix := strings.Trim(trimmed[len("ホロライブEnglish -"):], "-")
		if suffix != "" {
			return suffix
		}
	}

	if after, ok := strings.CutPrefix(trimmed, "hololive English"); ok {
		suffix := stringutil.TrimSpace(after)

		suffix = strings.Trim(suffix, "-")
		if suffix != "" {
			return suffix
		}
	}

	return trimmed
}

func primaryMemberName(member *domain.Member) string {
	if member == nil {
		return ""
	}

	primary := strings.Trim(stringutil.TrimSpace(member.NameKo), ",")
	if primary != "" {
		return primary
	}

	return member.Name
}
