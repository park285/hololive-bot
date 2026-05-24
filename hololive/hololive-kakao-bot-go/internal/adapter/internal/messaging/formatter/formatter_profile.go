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

package formatter

import (
	"fmt"
	"strings"

	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/internal/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
)

// 번역된 값 우선 사용, 없으면 원본 반환.
func getTranslatedText(translatedVal, rawVal string) string {
	if trimmed := stringutil.TrimSpace(translatedVal); trimmed != "" {
		return trimmed
	}

	return stringutil.TrimSpace(rawVal)
}

// 캐치프레이즈 섹션 포맷팅.
func formatProfileCatchphrase(raw *domain.TalentProfile, translated *domain.Translated) string {
	catchphrase := ""

	if translated != nil {
		catchphrase = getTranslatedText(translated.Catchphrase, raw.Catchphrase)
	} else if raw != nil {
		catchphrase = stringutil.TrimSpace(raw.Catchphrase)
	}

	if catchphrase == "" {
		return ""
	}

	return fmt.Sprintf("%s %s\n", msging.DefaultEmoji.Speech, catchphrase)
}

// 요약 섹션 포맷팅.
func formatProfileSummary(raw *domain.TalentProfile, translated *domain.Translated) string {
	summary := ""

	if translated != nil {
		summary = getTranslatedText(translated.Summary, raw.Description)
	} else if raw != nil {
		summary = stringutil.TrimSpace(raw.Description)
	}

	if summary == "" {
		return ""
	}

	return summary + "\n"
}

// 하이라이트 섹션 포맷팅.
func formatProfileHighlights(translated *domain.Translated) string {
	if translated == nil || len(translated.Highlights) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s 하이라이트\n", msging.DefaultEmoji.Highlight)

	for _, highlight := range translated.Highlights {
		if trimmed := stringutil.TrimSpace(highlight); trimmed != "" {
			fmt.Fprintf(&sb, "- %s\n", trimmed)
		}
	}

	return sb.String()
}

// 번역된 데이터 또는 원본 데이터 반환.
func getProfileDataEntries(raw *domain.TalentProfile, translated *domain.Translated) []domain.TranslatedProfileDataRow {
	if translated != nil && len(translated.Data) > 0 {
		return translated.Data
	}

	if raw == nil || len(raw.DataEntries) == 0 {
		return nil
	}

	entries := make([]domain.TranslatedProfileDataRow, 0)

	for _, entry := range raw.DataEntries {
		if stringutil.TrimSpace(entry.Label) == "" || stringutil.TrimSpace(entry.Value) == "" {
			continue
		}

		entries = append(entries, domain.TranslatedProfileDataRow(entry))
	}

	return entries
}

// 프로필 데이터 섹션 포맷팅 (최대 8개).
func formatProfileDataEntries(raw *domain.TalentProfile, translated *domain.Translated) string {
	dataEntries := getProfileDataEntries(raw, translated)
	if len(dataEntries) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s 프로필 데이터\n", msging.DefaultEmoji.Data)

	maxRows := min(len(dataEntries), 8)

	for i := range maxRows {
		row := dataEntries[i]
		label := stringutil.TrimSpace(row.Label)

		value := stringutil.TrimSpace(row.Value)
		if label == "" || value == "" {
			continue
		}

		if strings.Contains(value, "\n") {
			indented := "  " + strings.ReplaceAll(value, "\n", "\n  ")
			fmt.Fprintf(&sb, "- %s:\n%s\n", label, indented)
		} else {
			fmt.Fprintf(&sb, "- %s: %s\n", label, value)
		}
	}

	return sb.String()
}

// 소셜 링크 섹션 포맷팅 (최대 4개).
func formatProfileSocialLinks(raw *domain.TalentProfile) string {
	if raw == nil || len(raw.SocialLinks) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s 링크\n", msging.DefaultEmoji.Link)

	maxLinks := min(len(raw.SocialLinks), 4)

	for i := range maxLinks {
		link := raw.SocialLinks[i]
		if stringutil.TrimSpace(link.Label) == "" || stringutil.TrimSpace(link.URL) == "" {
			continue
		}

		translatedLabel := socialLinkLabel(link.Label)
		fmt.Fprintf(&sb, "- %s: %s\n", translatedLabel, stringutil.TrimSpace(link.URL))
	}

	return sb.String()
}

// 공식 URL 섹션 포맷팅.
func formatProfileOfficialURL(raw *domain.TalentProfile) string {
	if raw == nil || stringutil.TrimSpace(raw.OfficialURL) == "" {
		return ""
	}

	return fmt.Sprintf("\n%s 공식 프로필: %s", msging.DefaultEmoji.Web, stringutil.TrimSpace(raw.OfficialURL))
}

func (f *ResponseFormatter) FormatTalentProfile(raw *domain.TalentProfile, translated *domain.Translated) string {
	if raw == nil {
		return msging.ErrorMessage(msging.ErrDisplayProfileDataFailed)
	}

	var sb strings.Builder

	header := buildTalentHeader(raw, translated)
	sb.WriteString(header)
	sb.WriteString("\n")

	sb.WriteString(formatProfileCatchphrase(raw, translated))
	sb.WriteString(formatProfileSummary(raw, translated))
	sb.WriteString(formatProfileHighlights(translated))
	sb.WriteString(formatProfileDataEntries(raw, translated))
	sb.WriteString(formatProfileSocialLinks(raw))
	sb.WriteString(formatProfileOfficialURL(raw))

	content := stringutil.TrimSpace(sb.String())
	if content == "" {
		return content
	}

	body := stringutil.StripLeadingHeader(content, header)

	body = stringutil.TrimSpace(body)
	if body == "" {
		return content
	}

	instructionBase := stringutil.TrimSpace(header)
	if instructionBase == "" {
		instructionBase = msging.DefaultEmoji.Member + " 멤버 정보"
	}

	return util.ApplyKakaoSeeMorePadding(body, instructionBase)
}

func socialLinkLabel(label string) string {
	translations := map[string]string{
		"歌の再生リスト":   "음악 플레이리스트",
		"公式グッズ":     "공식 굿즈",
		"オフィシャルグッズ": "공식 굿즈",
	}

	if korean, ok := translations[label]; ok {
		return korean
	}

	return label
}

func buildTalentHeader(raw *domain.TalentProfile, translated *domain.Translated) string {
	names := talentDisplayNames(raw, translated)
	return msging.MemberHeader(names)
}

func talentDisplayNames(raw *domain.TalentProfile, translated *domain.Translated) []string {
	var names []string

	english := ""
	japanese := ""

	if raw != nil {
		english = stringutil.TrimSpace(raw.EnglishName)
		japanese = stringutil.TrimSpace(raw.JapaneseName)
	}

	display := ""

	if translated != nil {
		display = stringutil.TrimSpace(translated.DisplayName)
	}

	if english != "" {
		addUniqueName(&names, english)
	}

	for _, candidate := range parseDisplayNameComponents(display) {
		addUniqueName(&names, candidate)
	}

	if japanese != "" {
		addUniqueName(&names, japanese)
	}

	return names
}

func parseDisplayNameComponents(display string) []string {
	display = stringutil.TrimSpace(display)
	if display == "" {
		return nil
	}

	return splitDisplayNameParts(displayNameRawParts(display))
}

func displayNameRawParts(display string) []string {
	openIdx := strings.Index(display, "(")
	closeIdx := strings.LastIndex(display, ")")
	if openIdx == -1 || closeIdx == -1 || closeIdx <= openIdx {
		return []string{display}
	}

	rawParts := make([]string, 0, 3)
	appendDisplayNamePart(&rawParts, display[:openIdx])
	appendDisplayNamePart(&rawParts, display[openIdx+1:closeIdx])
	appendDisplayNamePart(&rawParts, display[closeIdx+1:])
	return rawParts
}

func appendDisplayNamePart(rawParts *[]string, part string) {
	part = stringutil.TrimSpace(part)
	if part != "" {
		*rawParts = append(*rawParts, part)
	}
}

func splitDisplayNameParts(rawParts []string) []string {
	var result []string
	for _, part := range rawParts {
		segments := strings.SplitSeq(part, "/")
		for segment := range segments {
			appendDisplayNamePart(&result, segment)
		}
	}
	return result
}

func addUniqueName(names *[]string, candidate string) {
	candidate = stringutil.TrimSpace(candidate)
	if candidate == "" {
		return
	}

	for _, existing := range *names {
		if strings.EqualFold(existing, candidate) {
			return
		}
	}

	*names = append(*names, candidate)
}
