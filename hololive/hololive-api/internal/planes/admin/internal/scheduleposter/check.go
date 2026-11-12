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

func (t translationTable) String() string {
	lines := make([]string, 0, len(t))

	for _, row := range t {
		lines = append(lines, row.original+" "+tableSeparator+" "+row.korean)
	}

	return strings.Join(lines, "\n")
}

func (t translationTable) originalLines() []string {
	lines := make([]string, 0, len(t))

	for _, row := range t {
		lines = append(lines, row.original)
	}

	return lines
}

func (t translationTable) koreanLines() []string {
	lines := make([]string, 0, len(t))

	for _, row := range t {
		lines = append(lines, row.korean)
	}

	return lines
}

// transcript의 장식 줄은 로마자만 있으면 번역표에 넣지 않아도 되는 로고·서명 같은 글자라, 영어 낱말 대조에서만 뺀다.
type transcript struct {
	content    []string
	decoration []string
}

func (t transcript) lines() []string {
	return slices.Concat(t.content, t.decoration)
}

var (
	spacedColon  = regexp.MustCompile(`(\d)\s*:\s*(\d)`)
	tokenPattern = regexp.MustCompile(`\d+(?::\d+)*|[A-Z]+`)
	weekdays     = []string{"MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN"}
)

const unmatchedOriginalAllowance = 2

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

func isWord(token string) bool {
	return !isNumber(token)
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

func isHangul(r rune) bool {
	return r >= 0xAC00 && r <= 0xD7A3
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

func countRunes(lines []string, keep func(rune) bool) map[rune]int {
	counts := map[rune]int{}

	for _, line := range lines {
		for _, r := range line {
			if keep(r) {
				counts[r]++
			}
		}
	}

	return counts
}

// checkTable에서 숫자와 일본어는 장식 줄까지 대조한다. 판독이 일정 줄을 장식으로 잘못 나눠도 시각이 빠진 번역표를 받지 않기 위해서다.
func checkTable(table translationTable, original transcript) []string {
	var problems []string

	for _, row := range table {
		problems = append(problems, checkRow(row)...)
	}

	seenNumbers, keptNumbers := countTokens(original.lines(), isNumber), countTokens(table.koreanLines(), isNumber)

	for _, token := range sortedKeys(seenNumbers) {
		if keptNumbers[token] < seenNumbers[token] {
			problems = append(problems, fmt.Sprintf("원본에 %d번 있는 ‘%s’가 번역표의 한국어 줄에는 %d번만 있다", seenNumbers[token], token, keptNumbers[token]))
		}
	}

	seenWords, listedWords := countTokens(original.content, isWord), countTokens(table.originalLines(), isWord)

	for _, word := range sortedKeys(shortfall(seenWords, listedWords)) {
		problems = append(problems, fmt.Sprintf("원본에 %d번 있는 ‘%s’가 번역표의 원문 줄에는 %d번만 있다", seenWords[word], word, listedWords[word]))
	}

	short := shortfall(countRunes(original.lines(), isJapanese), countRunes(table.originalLines(), isJapanese))

	for _, run := range runsWithShortfall(original.lines(), short) {
		problems = append(problems, fmt.Sprintf("원본의 ‘%s’ 부분이 번역표에 빠졌다", run))
	}

	return problems
}

// checkRow는 숫자만 줄마다 대조한다. 영어는 일반 낱말이면 옮기고 고유 명사면 그대로 두므로, 어느 쪽인지는 번역표 지시가 정한다.
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

		problems = append(problems, fmt.Sprintf("번역표 줄 ‘%s’의 한국어 줄에 원문의 ‘%s’가 빠졌다", row.original, token))
	}

	for _, run := range japaneseRuns(row.korean) {
		problems = append(problems, fmt.Sprintf("번역표 줄 ‘%s’의 한국어 줄에 번역하지 않은 ‘%s’가 남았다", row.original, run))
	}

	return problems
}

// shortfall은 줄 나눔·순서·한 글자 빠짐이 판독마다 달라 단위 개수로 센다. 모자란 만큼 번역표 쪽에 대신 읽은 단위가 있을 때만 2개까지 다르게 읽은 것으로 넘겨, 실제로 빠진 것은 걸린다.
func shortfall[K comparable](seen, have map[K]int) map[K]int {
	short := map[K]int{}
	missing, substitutes := 0, 0

	for unit, count := range seen {
		if count > have[unit] {
			short[unit] = count - have[unit]

			missing += count - have[unit]
		}
	}

	for unit, count := range have {
		substitutes += max(0, count-seen[unit])
	}

	if missing <= min(unmatchedOriginalAllowance, substitutes) {
		return nil
	}

	return short
}

// runsWithShortfall은 모자란 글자를 원본 덩어리에 돌려 붙인다. 글자가 모두 모자란 덩어리를 먼저 골라야 흔한 글자(の 등)가 엉뚱한 덩어리를 지목하지 않는다.
func runsWithShortfall(original []string, short map[rune]int) []string {
	if len(short) == 0 {
		return nil
	}

	runs := japaneseRuns(strings.Join(original, "\n"))
	slices.SortStableFunc(runs, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })

	var found []string

	for _, whole := range []bool{true, false} {
		for _, run := range runs {
			need := countRunes([]string{run}, isJapanese)

			matched := whole

			for r, count := range need {
				if whole {
					matched = matched && short[r] >= count
				} else {
					matched = matched || short[r] > 0
				}
			}

			if !matched {
				continue
			}

			for r, count := range need {
				short[r] -= min(count, short[r])
			}

			if !slices.Contains(found, run) {
				found = append(found, run)
			}
		}
	}

	return found
}

// checkRender에서 원본의 영어 낱말은 번역됐을 수 있어 요구하지 않는다. 그대로 둘 영어는 번역표 한국어 줄에 있으므로 그쪽으로 요구된다.
func checkRender(table translationTable, original, rendered transcript) []string {
	var problems []string

	expected := table.koreanLines()
	drawn := rendered.lines()
	want := countTokens(original.lines(), isNumber)

	for token, count := range countTokens(expected, anyToken) {
		want[token] = max(want[token], count)
	}

	got := countTokens(drawn, anyToken)

	for _, token := range sortedKeys(want) {
		if got[token] < want[token] {
			problems = append(problems, fmt.Sprintf("‘%s’가 %d번 있어야 하는데 번역본에는 %d번 있다", token, want[token], got[token]))
		}
	}

	problems = append(problems, hangulProblems(table, drawn)...)

	leftover := countRunes(drawn, isJapanese)
	for r, count := range countRunes(expected, isJapanese) {
		leftover[r] -= count
	}

	var untranslated []string

	for _, line := range drawn {
		for _, run := range japaneseRuns(line) {
			if strings.ContainsFunc(run, func(r rune) bool { return leftover[r] > 0 }) && !slices.Contains(untranslated, run) {
				untranslated = append(untranslated, run)
			}
		}
	}

	for _, run := range untranslated {
		problems = append(problems, fmt.Sprintf("번역되지 않은 원문 글자 ‘%s’가 번역본에 남았다", run))
	}

	return problems
}

// hangulProblems는 통과 여부를 글자 수로 정한다. 띄어쓰기와 줄 나눔이 판독마다 달라서다.
func hangulProblems(table translationTable, rendered []string) []string {
	want := countRunes(table.koreanLines(), isHangul)
	got := countRunes(rendered, isHangul)

	var short, extra []string

	for _, r := range sortedKeys(want) {
		if got[r] < want[r] {
			short = append(short, string(r))
		}
	}

	for _, r := range sortedKeys(got) {
		if got[r] > want[r] {
			extra = append(extra, string(r))
		}
	}

	if len(short) == 0 && len(extra) == 0 {
		return nil
	}

	problems := undrawnRows(table, rendered)

	if len(short) > 0 && len(problems) == 0 {
		problems = append(problems, fmt.Sprintf("번역본에 한국어 글자 ‘%s’가 번역표보다 적다", strings.Join(short, "’, ‘")))
	}

	if len(extra) > 0 {
		problems = append(problems, fmt.Sprintf("번역표에 없는 한국어 글자 ‘%s’가 번역본에 있다", strings.Join(extra, "’, ‘")))
	}

	return problems
}

// undrawnRows는 다음 그리기 지시에 넣을 줄을 고른다. 긴 줄부터 찾아 지워야 요일 같은 한 글자 줄이 긴 줄 안의 같은 글자로 그려진 것처럼 보이지 않는다.
func undrawnRows(table translationTable, rendered []string) []string {
	text := hangulOnly(strings.Join(rendered, ""))
	order := make([]int, len(table))

	for i := range order {
		order[i] = i
	}

	slices.SortStableFunc(order, func(a, b int) int {
		return len([]rune(hangulOnly(table[b].korean))) - len([]rune(hangulOnly(table[a].korean)))
	})

	missing := make([]bool, len(table))

	for _, i := range order {
		row := hangulOnly(table[i].korean)
		if row == "" {
			continue
		}

		at := strings.Index(text, row)
		if at < 0 {
			missing[i] = true

			continue
		}

		text = text[:at] + "|" + text[at+len(row):]
	}

	var problems []string

	for i, row := range table {
		if missing[i] {
			problems = append(problems, fmt.Sprintf("번역표 줄 ‘%s’이 번역본에 다 그려지지 않았다", row.korean))
		}
	}

	return problems
}

func hangulOnly(text string) string {
	return strings.Map(func(r rune) rune {
		if isHangul(r) {
			return r
		}

		return -1
	}, text)
}

func sortedKeys[K string | rune, V any](m map[K]V) []K {
	return slices.Sorted(maps.Keys(m))
}

// withProblems에는 비교기가 만든 문제만 넘긴다. 모델이 쓴 설명문을 다음 요청의 지시로 넣지 않기 위해서다.
func withProblems(prompt string, problems []string) string {
	if len(problems) == 0 {
		return prompt
	}

	return prompt + "\n\n이전 결과에서 대조로 찾은 문제다. 이번에는 모두 고친다:\n- " + strings.Join(problems, "\n- ")
}
