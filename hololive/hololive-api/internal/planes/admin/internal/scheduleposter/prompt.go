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
	transcriptPrefix   = "TEXT:"
	maxTableLines      = 120
	maxTableLineRunes  = 200
	maxTranscriptLines = 200
	textPromptBase     = `You are translating a VTuber weekly schedule poster into Korean. Read every line of text on the poster and output a translation table that pairs each line with the Korean line to draw in its place.

Output format
- One row per poster line: original line ⇒ Korean line
- Output only these rows, with no explanation, preamble, numbering, or closing remarks.

What to list
- Go top to bottom, left to right. List a phrase every time it appears, even when it repeats.
- Copy the original line exactly as printed, including times, dates, numbers, Latin letters, and symbols. Skip only text too small to read.
- Do not list logos, signatures, small lettering inside stamps or badges, tape patterns, watermarks, or member-name logos when they contain only Latin letters; they stay as printed. If you are unsure whether something is decoration, list it unchanged as "X ⇒ X". Always list text that contains Japanese or digits.

How to write the Korean line, in priority order
1. Terms in the glossary below: use the glossary spelling.
2. People's names, written in Japanese or English: use the Korean spelling from the name table below, and only the given or family name if that is all the poster shows. If a decorative font or mixed kana makes a name look one or two characters off, use the closest entry. Never translate the meaning of the kanji in a name. Write names and nicknames that are not in the table by their Japanese reading in Hangul.
3. English proper nouns (game, work, and brand names): keep them in English.
4. Japanese proper nouns (game, work, project, segment, and unit names): do not translate the meaning; write the Japanese reading in Hangul (地獄銭湯 → 지고쿠센토, 鬼武者 → 오니무샤). Write katakana loanwords with the Korean spelling of the source-language pronunciation (ダークソウル → 다크 소울). Confirm uncertain readings with Google Search.
5. English weekdays MON to SUN: 월, 화, 수, 목, 금, 토, 일.
6. Everything else in Japanese or ordinary English (sentences, notices, headings, and words such as "with", "Collab", and "senpai"): translate naturally into the wording Korean VTuber fans use (콜라보, 멤버십 한정, 잡담, and so on), without adding or removing content.
- Keep times, dates, numbers, symbols, AM/PM, abbreviations such as EN, JP, and JST, and @handles exactly as in the original line.
- The Korean line must contain no Japanese characters.`
	imagePromptHeader = `Edit this VTuber weekly schedule poster. Replace each original text listed below with its Korean text and change nothing else.
- All %d replacements must appear in the edited poster, each in the position of its original text.
- Copy each Korean text exactly. Do not alter, drop, or add characters, and keep its times, numbers, Latin letters, and symbols unchanged.
- Text that is not listed, including the times, dates, and numbers around replaced text, must stay exactly as printed.
- Keep the layout, character art, background, colors, decorations, icons, and aspect ratio unchanged. Match the size, weight, color, and outline of the original text with a similar font style; if the Korean text does not fit, reduce only its font size.
- Do not add any text or decoration.

Replacements (original → Korean):`
	fixPromptHeader = `This is a Korean translation of a VTuber weekly schedule poster. A check of this image found the problems listed below. Edit this image to fix only these problems and change nothing else.
- Write the Korean text of each reference line exactly, and keep its times, numbers, Latin letters, and symbols unchanged.
- Replace any remaining Japanese text with the Korean text of its reference line.
- Keep all other text, the layout, character art, background, colors, decorations, icons, and aspect ratio unchanged.
- Do not add any text or decoration.

Problems to fix:`
	keepProblemsHeader   = "\n\nThese problems appeared in earlier attempts but not in this image. Do not introduce them:"
	referenceLinesHeader = "\n\nReference lines (original → Korean):"
	transcribePrompt     = `Transcribe every piece of text visible in this image, one line at a time, exactly as it appears.
- Prefix every line with "TEXT: " and output nothing else.
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

// buildImagePrompt가 바뀌는 줄만 넘기는 것은 나머지 줄이 원본 그대로 남아 지시만 길게 만들기 때문이다.
func buildImagePrompt(table translationTable) string {
	var replacements strings.Builder

	count := 0

	for _, row := range table {
		if row.original == row.korean {
			continue
		}

		count++

		fmt.Fprintf(&replacements, "\n%d. %q → %q", count, row.original, row.korean)
	}

	return fmt.Sprintf(imagePromptHeader, count) + replacements.String()
}

// buildFixPrompt는 고칠 문제와 관계된 번역표 줄만 넘겨, 이미 맞게 그린 곳을 다시 그리게 하지 않는다.
func buildFixPrompt(table translationTable, fix, keep []renderProblem) string {
	var prompt strings.Builder

	prompt.WriteString(fixPromptHeader)
	writeProblems(&prompt, fix)

	if len(keep) > 0 {
		prompt.WriteString(keepProblemsHeader)
		writeProblems(&prompt, keep)
	}

	var related []tableRow

	for _, row := range table {
		if !slices.Contains(related, row) && slices.ContainsFunc(fix, func(problem renderProblem) bool { return problem.related(row) }) {
			related = append(related, row)
		}
	}

	if len(related) > 0 {
		prompt.WriteString(referenceLinesHeader)

		for i, row := range related {
			fmt.Fprintf(&prompt, "\n%d. %q → %q", i+1, row.original, row.korean)
		}
	}

	return prompt.String()
}

func writeProblems(prompt *strings.Builder, problems []renderProblem) {
	for _, problem := range problems {
		prompt.WriteString("\n- " + problem.String())
	}
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

		row := tableRow{original: original, korean: translated}

		// 저장한 번역표를 다시 읽을 수 있도록 한도는 String()이 쓰는 형태로 잰다.
		if utf8.RuneCountInString(row.String()) > maxTableLineRunes {
			return nil, fmt.Errorf("poster translation table line exceeds %d characters", maxTableLineRunes)
		}

		table = append(table, row)
	}

	if len(table) == 0 {
		return nil, errors.New("poster translation table is empty")
	}

	if len(table) > maxTableLines {
		return nil, fmt.Errorf("poster translation table exceeds %d lines", maxTableLines)
	}

	return table, nil
}

// parseTranscript는 "TEXT:"로 시작하는 줄만 판독으로 받는다. 머리말 같은 설명문이 대조에 섞이지 않게 한다.
func parseTranscript(text string) ([]string, error) {
	var lines []string

	for line := range strings.Lines(text) {
		read, ok := strings.CutPrefix(strings.TrimPrefix(strings.TrimSpace(line), "- "), transcriptPrefix)
		if read = strings.TrimSpace(read); !ok || read == "" || strings.ContainsFunc(read, unicode.IsControl) {
			continue
		}

		if utf8.RuneCountInString(read) > maxTableLineRunes {
			return nil, fmt.Errorf("poster transcript line exceeds %d characters", maxTableLineRunes)
		}

		lines = append(lines, read)
	}

	if len(lines) == 0 {
		return nil, errors.New("poster transcript is empty")
	}

	if len(lines) > maxTranscriptLines {
		return nil, fmt.Errorf("poster transcript exceeds %d lines", maxTranscriptLines)
	}

	return lines, nil
}
