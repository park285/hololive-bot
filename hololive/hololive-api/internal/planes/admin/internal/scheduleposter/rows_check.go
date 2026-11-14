package scheduleposter

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/kapu/hololive-shared/pkg/domain"
	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const (
	maxRowHour      = 29
	maxRowItemChars = 200
	maxRowTerms     = 40
	// DB 배열 상한(migration 019 CHECK)과 같다.
	maxRowWarnings     = 40
	maxErrorValueChars = 120
)

var termBases = []string{"official_ko", "translated", "transliterated"}

// 카드는 시간대를 그리지 않고 시각을 JST로 읽는다.
var errUnsupportedZone = errors.New("extracted row has a time zone other than JST")

var (
	rowClockPattern = regexp.MustCompile(`^([012]?\d):([0-5]\d)$`)
	japanesePattern = regexp.MustCompile(`[\x{3040}-\x{30ff}\x{3400}-\x{9fff}々]`)
	digitsPattern   = regexp.MustCompile(`\p{Nd}+`)
	nameSplitter    = regexp.MustCompile(`[\s★!+]+`)
)

func validateExtracted(rows []extractedRow) ([]posterstore.Row, error) {
	records := make([]posterstore.Row, len(rows))

	for i := range rows {
		row := &rows[i]

		day, err := time.Parse(time.DateOnly, row.Date)
		if err != nil {
			return nil, fmt.Errorf("extracted row %d: date %q is not YYYY-MM-DD", i+1, clip(row.Date))
		}

		for _, clock := range []*string{row.Start, row.End} {
			if clock != nil && !validClock(*clock) {
				return nil, fmt.Errorf("extracted row %d: time %q is not HH:MM up to %d:59", i+1, clip(*clock), maxRowHour)
			}
		}

		if !row.Status.Valid() {
			return nil, fmt.Errorf("extracted row %d: invalid status %q", i+1, clip(string(row.Status)))
		}

		if zone := optional(row.TZ); zone != "" && !strings.EqualFold(zone, "JST") {
			return nil, fmt.Errorf("extracted row %d: zone %q: %w", i+1, clip(zone), errUnsupportedZone)
		}

		if strings.TrimSpace(row.Text) == "" {
			return nil, fmt.Errorf("extracted row %d: text is empty", i+1)
		}

		// text·tz·badges는 번역 지시문의 한 행 줄에 그대로 들어가므로 줄바꿈 같은 제어 문자로 줄을 늘리지 못하게 한다.
		if hasControl(row.Text) || hasControl(optional(row.TZ)) || slices.ContainsFunc(row.Badges, func(badge string) bool {
			return hasControl(badge) || utf8.RuneCountInString(badge) > maxRowItemChars
		}) {
			return nil, fmt.Errorf("extracted row %d: text, tz, or badges has a control character or a badge over %d characters", i+1, maxRowItemChars)
		}

		records[i] = posterstore.Row{
			Day: day, Start: optional(row.Start), End: optional(row.End), TZ: optional(row.TZ), Status: row.Status,
			SourceText: row.Text, Badges: row.Badges,
		}
	}

	if len(records) == 0 {
		return records, nil
	}

	// 번역 전이라 비어 있는 title_ko에 source_text를 넣어 추출이 정하는 칸만 저장 조건으로 본다.
	probe := slices.Clone(records)
	for i := range probe {
		probe[i].TitleKo = probe[i].SourceText
	}

	if err := posterstore.ValidateRows(probe); err != nil {
		return nil, fmt.Errorf("check extracted rows: %w", err)
	}

	return records, nil
}

func hasControl(text string) bool {
	return strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' })
}

func clip(text string) string {
	count := 0

	for i := range text {
		if count == maxErrorValueChars {
			return text[:i] + "…"
		}

		count++
	}

	return text
}

func capWarnings(warnings []string) []string {
	if len(warnings) <= maxRowWarnings {
		return warnings
	}

	kept := maxRowWarnings - 1

	return append(warnings[:kept:kept], fmt.Sprintf("경고 %d개 생략", len(warnings)-kept))
}

func validClock(clock string) bool {
	match := rowClockPattern.FindStringSubmatch(clock)
	if match == nil {
		return false
	}

	hour, err := strconv.Atoi(match[1])

	return err == nil && hour <= maxRowHour
}

func validateTranslated(count int, rows []translatedRow) error {
	if len(rows) != count {
		return fmt.Errorf("translated %d rows for %d input rows", len(rows), count)
	}

	for i := range rows {
		row := &rows[i]
		if row.Index != i+1 {
			return fmt.Errorf("translated row %d has index %d", i+1, row.Index)
		}

		if strings.TrimSpace(row.TitleKo) == "" {
			return fmt.Errorf("translated row %d: title_ko is empty", i+1)
		}

		if slices.ContainsFunc(append(slices.Clone(row.WithKo), row.TagsKo...), func(item string) bool {
			return strings.TrimSpace(item) == "" || utf8.RuneCountInString(item) > maxRowItemChars
		}) {
			return fmt.Errorf("translated row %d: with_ko or tags_ko has an empty item or one over %d characters", i+1, maxRowItemChars)
		}

		if len(row.Terms) > maxRowTerms {
			return fmt.Errorf("translated row %d: %d terms, more than %d", i+1, len(row.Terms), maxRowTerms)
		}

		if slices.ContainsFunc(row.Terms, func(term posterstore.Term) bool {
			return !slices.Contains(termBases, term.Basis) ||
				utf8.RuneCountInString(term.Source) > maxRowItemChars || utf8.RuneCountInString(term.Ko) > maxRowItemChars
		}) {
			return fmt.Errorf("translated row %d: a term has an unknown basis or a text over %d characters", i+1, maxRowItemChars)
		}
	}

	return nil
}

func optional(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func warning(name, detail string) string {
	if detail == "" {
		return name
	}

	return name + ": " + detail
}

func extractWarnings(row *extractedRow, weekStart, weekEnd string) []string {
	var warnings []string

	start := optional(row.Start)

	if row.Date < weekStart || row.Date > weekEnd {
		warnings = append(warnings, warning("E1 날짜", row.Date))
	}

	if row.Status == posterstore.RowOff && start != "" {
		warnings = append(warnings, warning("E3 휴방에 시각", start))
	}

	if start != "" && strings.Contains(strings.ReplaceAll(row.Text, "：", ":"), start) {
		warnings = append(warnings, warning("E5 시각 중복", start))
	}

	return warnings
}

// rowChecker의 경고는 모델 검증에 쓴 하니스 check_translate와 같아야 한다.
type rowChecker struct {
	owner       *domain.Member
	members     []*domain.Member
	fullToShort [][2]string
}

func newRowChecker(owner *domain.Member, named []*domain.Member) rowChecker {
	checker := rowChecker{owner: owner, members: named}

	for _, member := range named {
		full, short := member.NameKo, shortKo(member)
		if full == short || full == owner.NameKo || slices.ContainsFunc(checker.fullToShort, func(pair [2]string) bool { return pair[0] == full }) {
			continue
		}

		checker.fullToShort = append(checker.fullToShort, [2]string{full, short})
	}

	return checker
}

func (c rowChecker) warnings(src *extractedRow, out *translatedRow) []string {
	var warnings []string

	source, korean := rowTexts(src, out)

	people := append(slices.Clone(out.WithKo), out.PlaceKo)

	if japanesePattern.MatchString(korean) || slices.ContainsFunc(out.Terms, func(term posterstore.Term) bool { return japanesePattern.MatchString(term.Ko) }) {
		name := "T2 일본어 잔존"

		if out.DoubtKo != "" {
			name += " (doubt 있음)"
		}

		warnings = append(warnings, warning(name, korean))
	}

	if missing := missingDigits(source, korean); len(missing) > 0 {
		warnings = append(warnings, warning("T3 숫자", strings.Join(missing, ",")))
	}

	if titleHasMarker(out.TitleKo) {
		warnings = append(warnings, warning("T4 제목에 표지", out.TitleKo))
	}

	warnings = append(warnings, c.unlistedPeople(people, source)...)

	if slices.Contains(out.WithKo, shortKo(c.owner)) || slices.Contains(out.WithKo, c.owner.NameKo) {
		warnings = append(warnings, warning("T5 주인이 with에", strings.Join(out.WithKo, ",")))
	}

	for _, person := range people {
		for _, pair := range c.fullToShort {
			if strings.Contains(person, pair[0]) && !strings.HasSuffix(person, "공식 채널") {
				warnings = append(warnings, warning("T6 전체 이름", person))
			}
		}
	}

	for _, term := range out.Terms {
		if !strings.Contains(source, term.Source) {
			warnings = append(warnings, warning("T8 terms 원문 불일치", term.Source))
		}

		if !strings.Contains(korean, term.Ko) {
			warnings = append(warnings, warning("T8 terms 표기 미사용", term.Ko))
		}
	}

	if (src.Status == posterstore.RowOff) != (out.TitleKo == "휴방") {
		warnings = append(warnings, warning("T10 휴방 불일치", "status="+string(src.Status)+" title="+out.TitleKo))
	}

	return warnings
}

func rowTexts(src *extractedRow, out *translatedRow) (source, korean string) {
	source = strings.Join(append([]string{src.Text}, src.Updates...), " ")
	korean = strings.Join(slices.Concat([]string{out.TitleKo}, out.WithKo, []string{out.PlaceKo}, out.TagsKo, []string{out.NoteKo}), " ")

	return source, korean
}

// 저장한 표기는 같은 주 다른 포스터의 Earlier renderings로 퍼지므로 T8에 걸린 표기는 빼고 저장한다.
func verifiedTerms(src *extractedRow, out *translatedRow) []posterstore.Term {
	source, korean := rowTexts(src, out)

	return slices.DeleteFunc(slices.Clone(out.Terms), func(term posterstore.Term) bool {
		return !strings.Contains(source, term.Source) || !strings.Contains(korean, term.Ko)
	})
}

func (c rowChecker) unlistedPeople(people []string, source string) []string {
	var warnings []string

	low := loose(source)

	for _, person := range people {
		for _, member := range c.members {
			if member == c.owner || !startsWithName(person, shortKo(member)) {
				continue
			}

			if !slices.ContainsFunc(memberForms(member), func(form string) bool {
				form = loose(form)

				return form != "" && strings.Contains(low, form)
			}) {
				warnings = append(warnings, warning("T12 이름표에 없는 표기로 정한 사람", person+" ← "+source))
			}
		}
	}

	return warnings
}

func memberForms(member *domain.Member) []string {
	forms := slices.Concat([]string{member.NameJa, member.NameKo, shortKo(member)}, jaAliases(member), koAliases(member))

	for _, token := range nameSplitter.Split(member.Name, -1) {
		if len([]rune(token)) >= 3 {
			forms = append(forms, token)
		}
	}

	ja := []rune(loose(member.NameJa))

	for k := 2; k < len(ja); k++ {
		forms = append(forms, string(ja[:k]))
	}

	for k := 1; k < len(ja)-1; k++ {
		forms = append(forms, string(ja[k:]))
	}

	return forms
}

func startsWithName(text, name string) bool {
	rest, ok := strings.CutPrefix(text, name)

	return ok && (rest == "" || rest[0] == ' ' || rest[0] == '(')
}

func loose(text string) string {
	kept := strings.Map(func(r rune) rune {
		if r == '_' || !isWordRune(r) {
			return -1
		}

		return r
	}, norm.NFKC.String(text))

	return strings.ToLower(kept)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}

func digitRuns(text string) []string {
	var folded strings.Builder

	for _, r := range text {
		switch {
		case r >= '０' && r <= '９':
			folded.WriteRune(r - '０' + '0')
		case r >= '①' && r <= '⑳':
			folded.WriteString(strconv.Itoa(int(r-'①') + 1))
		default:
			folded.WriteRune(r)
		}
	}

	return digitsPattern.FindAllString(folded.String(), -1)
}

func missingDigits(source, korean string) []string {
	present := strings.Join(digitRuns(korean), "")

	var missing []string

	for _, run := range digitRuns(source) {
		if !strings.Contains(present, run) {
			missing = append(missing, run)
		}
	}

	return missing
}

// titleHasMarker는 하니스의 `@|\bwith\b|\bw/|\bch\.`(대소문자 무시)와 같다. Go의 \b는 ASCII 글자만 보므로 유니코드 경계를 직접 잰다.
func titleHasMarker(title string) bool {
	if strings.Contains(title, "@") {
		return true
	}

	runes := []rune(strings.Map(unicode.ToLower, title))

	for _, marker := range []struct {
		text     string
		endsWord bool
	}{{"with", true}, {"w/", false}, {"ch.", false}} {
		needle := []rune(marker.text)

		for i := 0; i+len(needle) <= len(runes); i++ {
			if !slices.Equal(runes[i:i+len(needle)], needle) || i > 0 && isWordRune(runes[i-1]) {
				continue
			}

			if end := i + len(needle); !marker.endsWord || end == len(runes) || !isWordRune(runes[end]) {
				return true
			}
		}
	}

	return false
}
