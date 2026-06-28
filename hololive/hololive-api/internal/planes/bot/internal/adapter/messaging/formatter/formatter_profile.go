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
	"context"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/park285/shared-go/pkg/stringutil"
)

type profileTemplateData struct {
	Names       []string
	Catchphrase string
	Summary     string
	Highlights  []string
	DataRows    []profileDataRow
	SocialLinks []profileSocialLink
	OfficialURL string
}

type profileDataRow struct {
	Label     string
	Value     string
	Multiline bool
}

type profileSocialLink struct {
	Label string
	URL   string
}

func getTranslatedText(translatedVal, rawVal string) string {
	if trimmed := stringutil.TrimSpace(translatedVal); trimmed != "" {
		return trimmed
	}

	return stringutil.TrimSpace(rawVal)
}

func profileCatchphrase(raw *domain.TalentProfile, translated *domain.Translated) string {
	if translated != nil {
		return getTranslatedText(translated.Catchphrase, raw.Catchphrase)
	}

	if raw != nil {
		return stringutil.TrimSpace(raw.Catchphrase)
	}

	return ""
}

func profileSummary(raw *domain.TalentProfile, translated *domain.Translated) string {
	if translated != nil {
		return getTranslatedText(translated.Summary, raw.Description)
	}

	if raw != nil {
		return stringutil.TrimSpace(raw.Description)
	}

	return ""
}

func profileHighlights(translated *domain.Translated) []string {
	if translated == nil || len(translated.Highlights) == 0 {
		return nil
	}

	highlights := make([]string, 0, len(translated.Highlights))

	for _, highlight := range translated.Highlights {
		if trimmed := stringutil.TrimSpace(highlight); trimmed != "" {
			highlights = append(highlights, trimmed)
		}
	}

	return highlights
}

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

func profileDataRows(raw *domain.TalentProfile, translated *domain.Translated) []profileDataRow {
	entries := getProfileDataEntries(raw, translated)
	if len(entries) == 0 {
		return nil
	}

	maxRows := min(len(entries), 8)
	rows := make([]profileDataRow, 0, maxRows)

	for i := range maxRows {
		label := stringutil.TrimSpace(entries[i].Label)

		value := stringutil.TrimSpace(entries[i].Value)
		if label == "" || value == "" {
			continue
		}

		if strings.Contains(value, "\n") {
			rows = append(rows, profileDataRow{
				Label:     label,
				Value:     "  " + strings.ReplaceAll(value, "\n", "\n  "),
				Multiline: true,
			})

			continue
		}

		rows = append(rows, profileDataRow{Label: label, Value: value})
	}

	return rows
}

func (f *ResponseFormatter) profileSocialLinks(ctx context.Context, raw *domain.TalentProfile) []profileSocialLink {
	if raw == nil || len(raw.SocialLinks) == 0 {
		return nil
	}

	maxLinks := min(len(raw.SocialLinks), 4)
	links := make([]profileSocialLink, 0, maxLinks)

	for i := range maxLinks {
		link := raw.SocialLinks[i]
		if stringutil.TrimSpace(link.Label) == "" || stringutil.TrimSpace(link.URL) == "" {
			continue
		}

		links = append(links, profileSocialLink{
			Label: f.socialLinkLabel(ctx, link.Label),
			URL:   stringutil.TrimSpace(link.URL),
		})
	}

	return links
}

func profileOfficialURL(raw *domain.TalentProfile) string {
	if raw == nil {
		return ""
	}

	return stringutil.TrimSpace(raw.OfficialURL)
}

func (f *ResponseFormatter) FormatTalentProfile(ctx context.Context, raw *domain.TalentProfile, translated *domain.Translated) string {
	if raw == nil {
		return messagestrings.FallbackSentinel
	}

	data := profileTemplateData{
		Names:       talentDisplayNames(raw, translated),
		Catchphrase: profileCatchphrase(raw, translated),
		Summary:     profileSummary(raw, translated),
		Highlights:  profileHighlights(translated),
		DataRows:    profileDataRows(raw, translated),
		SocialLinks: f.profileSocialLinks(ctx, raw),
		OfficialURL: profileOfficialURL(raw),
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdProfile, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) socialLinkLabel(ctx context.Context, label string) string {
	if translated := f.messageStrings.GetContext(ctx, messagestrings.NamespaceSocial, label); translated != "" {
		return translated
	}

	return label
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
