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

var (
	spacedColon  = regexp.MustCompile(`(\d)\s*:\s*(\d)`)
	tokenPattern = regexp.MustCompile(`\d+(?::\d+)*|[A-Z]+`)
	weekdays     = map[string]rune{"MON": '월', "TUE": '화', "WED": '수', "THU": '목', "FRI": '금', "SAT": '토', "SUN": '일'}
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
	if _, weekday := weekdays[token]; weekday {
		return false
	}

	return len(token) >= 3 || strings.ContainsAny(token, "0123456789")
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

// originalTokens가 로마자만 있는 줄을 빼는 것은 서명·로고처럼 판독마다 넣고 빼는 장식이 많아서다.
func originalTokens(lines []string) map[string]int {
	counts := map[string]int{}

	for _, line := range lines {
		if !strings.ContainsFunc(line, isJapanese) && !strings.ContainsAny(normalize(line), "0123456789") {
			continue
		}

		for _, token := range tokens(line) {
			if significant(token) {
				counts[token]++
			}
		}
	}

	return counts
}

func significantTokens(lines []string) map[string]int {
	counts := map[string]int{}

	for _, line := range lines {
		for _, token := range tokens(line) {
			if significant(token) {
				counts[token]++
			}
		}
	}

	return counts
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

func checkTable(table translationTable, original []string) []string {
	var problems []string

	for _, row := range table {
		problems = append(problems, checkRow(row)...)
	}

	tableTokens := significantTokens(table.koreanLines())

	for _, token := range sortedKeys(originalTokens(original)) {
		if want, got := originalTokens(original)[token], tableTokens[token]; got < want {
			problems = append(problems, fmt.Sprintf("원본에 %d번 있는 ‘%s’가 번역표의 한국어 줄에는 %d번만 있다", want, token, got))
		}
	}

	for _, run := range missingOriginalRuns(original, table) {
		problems = append(problems, fmt.Sprintf("원본의 ‘%s’ 부분이 번역표에 빠졌다", run))
	}

	return problems
}

func checkRow(row tableRow) []string {
	var problems []string

	korean := map[string]int{}
	for _, token := range tokens(row.korean) {
		korean[token]++
	}

	for _, token := range tokens(row.original) {
		if korean[token] > 0 {
			korean[token]--

			continue
		}

		if day, weekday := weekdays[token]; weekday && strings.ContainsRune(row.korean, day) {
			continue
		}

		problems = append(problems, fmt.Sprintf("번역표 줄 ‘%s’의 한국어 줄에 원문의 ‘%s’가 빠졌다", row.original, token))
	}

	for _, run := range japaneseRuns(row.korean) {
		problems = append(problems, fmt.Sprintf("번역표 줄 ‘%s’의 한국어 줄에 번역하지 않은 ‘%s’가 남았다", row.original, run))
	}

	return problems
}

// missingOriginalRuns는 줄 나눔·순서·한 글자 빠짐이 판독마다 달라 글자 수로 센다. 모자란 글자는 번역표 쪽에 대신 읽은 글자가 있을 때만 다르게 읽은 것으로 넘겨, 실제로 빠진 글자는 번역본 검사까지 가지 않고 여기서 걸린다.
func missingOriginalRuns(original []string, table translationTable) []string {
	seen := countRunes(original, isJapanese)
	have := countRunes(table.originalLines(), isJapanese)
	deficit := map[rune]int{}
	missing, substitutes := 0, 0

	for r, count := range seen {
		if count > have[r] {
			deficit[r] = count - have[r]

			missing += count - have[r]
		}
	}

	for r, count := range have {
		substitutes += max(0, count-seen[r])
	}

	if missing <= min(unmatchedOriginalAllowance, substitutes) {
		return nil
	}

	return runsWithDeficit(original, deficit)
}

// runsWithDeficit은 모자란 글자를 원본 덩어리에 돌려 붙인다. 글자가 모두 모자란 덩어리를 먼저 골라야 흔한 글자(の 등)가 엉뚱한 덩어리를 지목하지 않는다.
func runsWithDeficit(original []string, deficit map[rune]int) []string {
	runs := japaneseRuns(strings.Join(original, "\n"))
	slices.SortStableFunc(runs, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })

	var found []string

	for _, whole := range []bool{true, false} {
		for _, run := range runs {
			need := countRunes([]string{run}, isJapanese)

			matched := whole

			for r, count := range need {
				if whole {
					matched = matched && deficit[r] >= count
				} else {
					matched = matched || deficit[r] > 0
				}
			}

			if !matched {
				continue
			}

			for r, count := range need {
				deficit[r] -= min(count, deficit[r])
			}

			if !slices.Contains(found, run) {
				found = append(found, run)
			}
		}
	}

	return found
}

func checkRender(table translationTable, original, rendered []string) []string {
	var problems []string

	expected := table.koreanLines()
	want := originalTokens(original)

	for token, count := range significantTokens(expected) {
		want[token] = max(want[token], count)
	}

	got := significantTokens(rendered)

	for _, token := range sortedKeys(want) {
		if got[token] < want[token] {
			problems = append(problems, fmt.Sprintf("‘%s’가 %d번 있어야 하는데 번역본에는 %d번 있다", token, want[token], got[token]))
		}
	}

	problems = append(problems, hangulProblems(table, rendered)...)

	leftover := countRunes(rendered, isJapanese)
	for r, count := range countRunes(expected, isJapanese) {
		leftover[r] -= count
	}

	var untranslated []string

	for _, line := range rendered {
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
