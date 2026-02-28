package adapter

import (
	"fmt"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

// 번역된 값 우선 사용, 없으면 원본 반환
func getTranslatedText(translatedVal, rawVal string) string {
	if trimmed := stringutil.TrimSpace(translatedVal); trimmed != "" {
		return trimmed
	}
	return stringutil.TrimSpace(rawVal)
}

// 캐치프레이즈 섹션 포맷팅
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
	return fmt.Sprintf("%s %s\n", DefaultEmoji.Speech, catchphrase)
}

// 요약 섹션 포맷팅
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

// 하이라이트 섹션 포맷팅
func formatProfileHighlights(translated *domain.Translated) string {
	if translated == nil || len(translated.Highlights) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s 하이라이트\n", DefaultEmoji.Highlight)
	for _, highlight := range translated.Highlights {
		if trimmed := stringutil.TrimSpace(highlight); trimmed != "" {
			fmt.Fprintf(&sb, "- %s\n", trimmed)
		}
	}
	return sb.String()
}

// 번역된 데이터 또는 원본 데이터 반환
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

// 프로필 데이터 섹션 포맷팅 (최대 8개)
func formatProfileDataEntries(raw *domain.TalentProfile, translated *domain.Translated) string {
	dataEntries := getProfileDataEntries(raw, translated)
	if len(dataEntries) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s 프로필 데이터\n", DefaultEmoji.Data)

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

// 소셜 링크 섹션 포맷팅 (최대 4개)
func formatProfileSocialLinks(raw *domain.TalentProfile) string {
	if raw == nil || len(raw.SocialLinks) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s 링크\n", DefaultEmoji.Link)

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

// 공식 URL 섹션 포맷팅
func formatProfileOfficialURL(raw *domain.TalentProfile) string {
	if raw == nil || stringutil.TrimSpace(raw.OfficialURL) == "" {
		return ""
	}
	return fmt.Sprintf("\n%s 공식 프로필: %s", DefaultEmoji.Web, stringutil.TrimSpace(raw.OfficialURL))
}

// FormatTalentProfile: 탤런트 프로필 정보를 포맷팅하여 메시지 문자열을 생성합니다.
func (f *ResponseFormatter) FormatTalentProfile(raw *domain.TalentProfile, translated *domain.Translated) string {
	if raw == nil {
		return ErrorMessage(ErrDisplayProfileDataFailed)
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
		instructionBase = DefaultEmoji.Member + " 멤버 정보"
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
	return MemberHeader(names)
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

	var rawParts []string

	openIdx := strings.Index(display, "(")
	closeIdx := strings.LastIndex(display, ")")
	if openIdx != -1 && closeIdx != -1 && closeIdx > openIdx {
		before := stringutil.TrimSpace(display[:openIdx])
		inside := stringutil.TrimSpace(display[openIdx+1 : closeIdx])
		after := stringutil.TrimSpace(display[closeIdx+1:])

		if before != "" {
			rawParts = append(rawParts, before)
		}
		if inside != "" {
			rawParts = append(rawParts, inside)
		}
		if after != "" {
			rawParts = append(rawParts, after)
		}
	} else {
		rawParts = append(rawParts, display)
	}

	var result []string
	for _, part := range rawParts {
		segments := strings.SplitSeq(part, "/")
		for segment := range segments {
			candidate := stringutil.TrimSpace(segment)
			if candidate != "" {
				result = append(result, candidate)
			}
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
