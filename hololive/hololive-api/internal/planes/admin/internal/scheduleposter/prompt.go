package scheduleposter

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const glossaryOrg = "Hololive"

const (
	tableSeparator     = "⇒"
	contentPrefix      = "TEXT:"
	decorationPrefix   = "DECOR:"
	maxTableLines      = 120
	maxTableLineRunes  = 200
	maxTranscriptLines = 200
	// 판독과 번역표 지시는 같은 말로 장식을 가리켜야 둘의 판단이 어긋나지 않는다.
	decorationText = "logos, signatures, small lettering inside stamps or badges, tape patterns, watermarks, and other decorative lettering that carries no content"
	textPromptBase = `Read every line of text on this VTuber weekly schedule poster and build a translation table that pairs each line with the Korean line to draw back onto the poster.
- Output only lines of the form "original line ⇒ Korean line", one per line, with no explanation, preamble, or closing remarks.
- List lines top to bottom, left to right. If the same phrase appears in several places, list it every time it appears.
- In the original line, copy everything in that line (or cell) of the poster exactly, including times, dates, numbers, Latin letters, and symbols. Skip only text too small to read.
- Do not list ` + decorationText + ` (including member-name logos) when it contains only Latin letters; leave it as it is on the poster. If you are unsure whether something is decoration, list it unchanged as "X ⇒ X". Always list decoration that contains Japanese or digits.
- In the Korean line, keep times, dates, numbers, symbols, AM/PM, abbreviations such as EN, JP, and JST, and @handles exactly as in the original. Write English weekdays (MON to SUN) as 월, 화, 수, 목, 금, 토, 일.
- Translate Japanese and ordinary English (sentences, notices, headings, and words such as "with", "Collab", and "senpai") into Korean. The Korean line must contain no Japanese characters.
- Terms in the glossary below take precedence over every other rule.
- Write people's names, whether written in Japanese or English, with the Korean spelling from the name table below; if the poster shows only the given name or family name, use only that part. If a decorative font or mixed kana makes a name look one or two characters off from the table, use the closest entry. Never translate the meaning of the kanji in a name. Write names and nicknames that are not in the table by their Japanese reading in Hangul.
- Keep English proper nouns (game, work, and brand names) in English.
- Do not translate the meaning of Japanese proper nouns (game, work, project, segment, and unit names); write their Japanese reading in Hangul (地獄銭湯 → 지고쿠센토, 鬼武者 → 오니무샤). Write katakana loanwords with the Korean spelling of the source-language pronunciation (ダークソウル → 다크 소울). If you are unsure of a reading, confirm it with Google Search.
- Translate stream titles and notices that are not proper nouns naturally, in the wording Korean VTuber fans use (콜라보, 멤버십 한정, 잡담, and so on), without adding or removing content.`
	imagePromptBase = `Create the same poster image with the text of this VTuber weekly schedule poster replaced by the Korean lines of the translation table below.
- For every row of the table, draw the Korean line where the original line was, without omission. Do not alter, drop, or add any character of the Korean line, and do not change a single character of times, numbers, Latin letters, or symbols.
- Leave text that is not in the table exactly as it is on the original.
- Keep the layout, character art, background, colors, decorations, icons, and aspect ratio unchanged. Draw the Korean text clearly with the same size, weight, color, and outline as the original text and a similar font style; if it overflows its box, reduce only the font size.
- Do not add text or decorations that are not in the original.

Translation table:
`
	transcribePrompt = `Transcribe every piece of text visible in this image, one line at a time, exactly as it appears.
- Prefix lines of ` + decorationText + ` (including member-name logos) with "DECOR: " and every other line with "TEXT: ". Output nothing other than these two kinds of lines.
- Do not correct, translate, or guess at text you cannot see. Write Korean, Japanese, digits, Latin letters, and symbols exactly as shown.
- Go top to bottom, left to right, and write the same text every time it appears.
- Skip only text too small to read.`
)

type memberSource interface {
	GetAllMembers(ctx context.Context) ([]*domain.Member, error)
}

// buildTextPrompt는 포스터 주인 멤버를 대응표보다 먼저 적어 다른 멤버 이름과 섞이지 않게 한다.
func buildTextPrompt(channelID string, members []*domain.Member, glossary map[string]string) string {
	var prompt strings.Builder

	prompt.WriteString(textPromptBase)

	named := make([]*domain.Member, 0, len(members))

	for _, member := range members {
		if member == nil || member.NameJa == "" || member.NameKo == "" {
			continue
		}

		if member.ChannelID == channelID {
			fmt.Fprintf(&prompt, "\n\nPoster owner: %s", memberLine(member))
		}

		if !member.IsGraduated && member.GetOrg() == glossaryOrg {
			named = append(named, member)
		}
	}

	slices.SortFunc(named, func(a, b *domain.Member) int { return cmp.Compare(a.NameJa, b.NameJa) })

	if len(named) > 0 {
		prompt.WriteString("\n\nName table (Japanese (romaji) → Korean):")

		for _, member := range named {
			prompt.WriteString("\n" + memberLine(member))
		}
	}

	if len(glossary) > 0 {
		prompt.WriteString("\n\nGlossary (original → Korean):")

		for _, term := range slices.Sorted(maps.Keys(glossary)) {
			prompt.WriteString("\n" + term + " → " + glossary[term])
		}
	}

	return prompt.String()
}

func memberLine(member *domain.Member) string {
	if member.Name == "" {
		return member.NameJa + " → " + member.NameKo
	}

	return member.NameJa + " (" + member.Name + ") → " + member.NameKo
}

func buildImagePrompt(table translationTable, problems []string) string {
	return withProblems(imagePromptBase+table.String(), problems)
}

// parseTranslationTable은 형식에 맞는 줄만 이미지 프롬프트로 넘긴다. 검색 결과에서 온 설명문이 이미지 지시로 섞이지 않게 한다.
func parseTranslationTable(text string) (translationTable, error) {
	var table translationTable

	for line := range strings.Lines(text) {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))

		original, translated, ok := strings.Cut(line, tableSeparator)

		original, translated = strings.TrimSpace(original), strings.TrimSpace(translated)

		if !ok || original == "" || translated == "" || strings.ContainsFunc(line, unicode.IsControl) {
			continue
		}

		if utf8.RuneCountInString(line) > maxTableLineRunes {
			return nil, fmt.Errorf("poster translation table line exceeds %d characters", maxTableLineRunes)
		}

		table = append(table, tableRow{original: original, korean: translated})
	}

	if len(table) == 0 {
		return nil, errors.New("poster translation table is empty")
	}

	if len(table) > maxTableLines {
		return nil, fmt.Errorf("poster translation table exceeds %d lines", maxTableLines)
	}

	return table, nil
}

// parseTranscript는 "TEXT:"와 "DECOR:"로 시작하는 줄만 판독으로 받는다. 머리말 같은 설명문이 대조에 섞이지 않게 한다.
func parseTranscript(text string) (transcript, error) {
	var read transcript

	for line := range strings.Lines(text) {
		line = strings.TrimPrefix(strings.TrimSpace(line), "- ")

		target := &read.content

		body, ok := strings.CutPrefix(line, contentPrefix)
		if !ok {
			target = &read.decoration
			body, ok = strings.CutPrefix(line, decorationPrefix)
		}

		if body = strings.TrimSpace(body); !ok || body == "" || strings.ContainsFunc(body, unicode.IsControl) {
			continue
		}

		if utf8.RuneCountInString(body) > maxTableLineRunes {
			return transcript{}, fmt.Errorf("poster transcript line exceeds %d characters", maxTableLineRunes)
		}

		*target = append(*target, body)
	}

	switch total := len(read.content) + len(read.decoration); {
	case total == 0:
		return transcript{}, errors.New("poster transcript is empty")
	case total > maxTranscriptLines:
		return transcript{}, fmt.Errorf("poster transcript exceeds %d lines", maxTranscriptLines)
	}

	return read, nil
}
