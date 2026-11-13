package scheduleposter

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type tableRow struct {
	original string
	korean   string
}

type translationTable []tableRow

func (r tableRow) String() string {
	return r.original + " " + tableSeparator + " " + r.korean
}

func (t translationTable) String() string {
	lines := make([]string, 0, len(t))

	for _, row := range t {
		lines = append(lines, row.String())
	}

	return strings.Join(lines, "\n")
}

func (t translationTable) koreanLines() []string {
	lines := make([]string, 0, len(t))

	for _, row := range t {
		lines = append(lines, row.korean)
	}

	return lines
}

var (
	spacedColon  = regexp.MustCompile(`(\d)\s*:\s*(\d)`)
	tokenPattern = regexp.MustCompile(`\d+(?::\d+)*|[A-Z]+`)
	weekdays     = []string{"MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN"}
)

func normalize(text string) string {
	var folded strings.Builder

	for _, r := range text {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			folded.WriteRune(r - 0xFEE0)
		case r == '　':
			folded.WriteByte(' ')
		case r == '〜' || r == '∼':
			folded.WriteByte('~')
		case r >= '①' && r <= '⑳':
			folded.WriteString(" " + strconv.Itoa(int(r-'①')+1) + " ")
		default:
			folded.WriteRune(r)
		}
	}

	return spacedColon.ReplaceAllString(strings.ToUpper(folded.String()), "$1:$2")
}

func tokens(text string) []string {
	return tokenPattern.FindAllString(normalize(text), -1)
}

// significant에서 영어 요일을 빼는 것은 번역표가 요일을 한글로 바꾸기 때문이다.
func significant(token string) bool {
	return !slices.Contains(weekdays, token) && (len(token) >= 3 || isNumber(token))
}

func isNumber(token string) bool {
	return strings.ContainsAny(token, "0123456789")
}

func anyToken(string) bool {
	return true
}

func countTokens(lines []string, keep func(string) bool) map[string]int {
	counts := map[string]int{}

	for _, line := range lines {
		for _, token := range tokens(line) {
			if significant(token) && keep(token) {
				counts[token]++
			}
		}
	}

	return counts
}

// isJapanese가 장음 부호(ー, ｰ)를 빼는 것은 한글 번역본의 줄표를 판독이 ー로 읽어도 원문이 남은 것으로 보지 않기 위해서다.
func isJapanese(r rune) bool {
	switch {
	case r == 'ー' || r == 'ｰ':
		return false
	case r >= 0x3041 && r <= 0x3096, r >= 0x309D && r <= 0x309F:
		return true
	case r >= 0x30A1 && r <= 0x30FA, r >= 0x30FC && r <= 0x30FF, r >= 0xFF66 && r <= 0xFF9F:
		return true
	default:
		return r == '々' || unicode.Is(unicode.Han, r)
	}
}

func japaneseRuns(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !isJapanese(r) })
}

// checkTable은 원본 판독과 숫자만 대조한다. 일본어·영어 글자는 두 판독이 서로 다르게 읽는 일이 잦아, 대조하면 맞는 번역표도 거절된다.
func checkTable(table translationTable, original []string) []string {
	var problems []string

	for _, row := range table {
		problems = append(problems, checkRow(row)...)
	}

	seen, kept := countTokens(original, isNumber), countTokens(table.koreanLines(), isNumber)

	for _, token := range sortedKeys(seen) {
		if kept[token] < seen[token] {
			problems = append(problems, fmt.Sprintf("%q appears %d times in the original but only %d times in the Korean lines of the table", token, seen[token], kept[token]))
		}
	}

	return problems
}

func checkRow(row tableRow) []string {
	var problems []string

	kept := countTokens([]string{row.korean}, isNumber)

	for _, token := range tokens(row.original) {
		if !isNumber(token) {
			continue
		}

		if kept[token] > 0 {
			kept[token]--

			continue
		}

		problems = append(problems, fmt.Sprintf("table row %q: the Korean line is missing %q from the original", row.original, token))
	}

	for _, run := range japaneseRuns(row.korean) {
		problems = append(problems, fmt.Sprintf("table row %q: the Korean line still has untranslated %q", row.original, run))
	}

	return problems
}

// renderProblem에는 그 시도의 판독 개수를 넣지 않는다. 시도마다 같은 문제가 같은 값이어야 앞선 시도의 문제와 견줄 수 있다.
type renderProblem struct {
	text         string
	want         int
	untranslated bool
}

func (p renderProblem) String() string {
	if p.untranslated {
		return fmt.Sprintf("untranslated original text %q remains in the translated poster", p.text)
	}

	return fmt.Sprintf("%q must appear %d times in the translated poster but was found fewer times", p.text, p.want)
}

func (p renderProblem) related(row tableRow) bool {
	if p.untranslated {
		return strings.Contains(row.original, p.text)
	}

	return slices.Contains(tokens(row.korean), p.text) || slices.Contains(tokens(row.original), p.text)
}

func problemTexts(problems []renderProblem) []string {
	texts := make([]string, 0, len(problems))

	for _, problem := range problems {
		texts = append(texts, problem.String())
	}

	return texts
}

// checkRender는 판독 오차에 흔들리지 않는 사실만 본다. 한글은 판독이 글꼴에 따라 자주 잘못 읽어 대조하지 않는다.
func checkRender(table translationTable, original, rendered []string) []renderProblem {
	var problems []renderProblem

	want := countTokens(original, isNumber)

	for token, count := range countTokens(table.koreanLines(), anyToken) {
		want[token] = max(want[token], count)
	}

	got := countTokens(rendered, anyToken)

	for _, token := range sortedKeys(want) {
		if got[token] < want[token] {
			problems = append(problems, renderProblem{text: token, want: want[token]})
		}
	}

	var untranslated []string

	for _, line := range rendered {
		for _, run := range japaneseRuns(line) {
			if !slices.Contains(untranslated, run) {
				untranslated = append(untranslated, run)
			}
		}
	}

	for _, run := range untranslated {
		problems = append(problems, renderProblem{text: run, untranslated: true})
	}

	return problems
}

func sortedKeys[K string | rune, V any](m map[K]V) []K {
	return slices.Sorted(maps.Keys(m))
}

// withProblems에는 비교기가 만든 문제만 넘긴다. 모델이 쓴 설명문을 다음 요청의 지시로 넣지 않기 위해서다.
func withProblems(prompt string, problems []string) string {
	if len(problems) == 0 {
		return prompt
	}

	return prompt + "\n\nEarlier attempts were rejected for these problems. Make sure none of them occurs in this result:\n- " + strings.Join(problems, "\n- ")
}
