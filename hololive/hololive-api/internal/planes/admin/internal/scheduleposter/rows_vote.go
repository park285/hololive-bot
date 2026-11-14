package scheduleposter

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const (
	extractRuns   = 3
	extractQuorum = 2
	// 서로 다른 칸으로 남기는 원문 유사도 기준이며 하니스 compare.match_rows와 같다.
	sameDaySimilarity = 0.6
	anyDaySimilarity  = 0.85
	// Python difflib의 autojunk와 같다. 200자 이상인 쪽에서 1%+1번 넘게 나온 글자는 짝을 찾는 데 쓰지 않는다.
	autojunkMinRunes = 200
)

type voteSlot struct {
	firstRun int
	by       []*extractedRow
}

func (s *voteSlot) seen() []*extractedRow {
	return slices.DeleteFunc(slices.Clone(s.by), func(row *extractedRow) bool { return row == nil })
}

// voteExtracted는 하니스 score.vote처럼 결과들의 행을 짝지어 칸마다 다수결로 합친다. 모든 결과가 같으면 첫 결과와 같다.
func voteExtracted(runs [][]extractedRow) ([]extractedRow, error) {
	var kept, dropped []*voteSlot

	for _, slot := range slotRuns(runs) {
		if len(slot.seen()) >= extractQuorum {
			kept = append(kept, slot)
		} else {
			dropped = append(dropped, slot)
		}
	}

	if len(kept) == 0 && len(dropped) > 0 {
		if countEmpty(runs) >= extractQuorum {
			return nil, nil
		}

		return nil, errors.New("extract runs share no schedule row")
	}

	voted := make([]extractedRow, 0, len(kept))

	for _, slot := range kept {
		row := voteRow(slot.seen(), len(runs))
		at := len(voted)

		if slot.firstRun > 0 {
			at = insertionPoint(voted, &row)
		}

		voted = slices.Insert(voted, at, row)
	}

	for _, slot := range dropped {
		row := slot.seen()[0]
		target := &voted[droppedTarget(voted, row)]

		target.Disagreements = append(target.Disagreements, warning("E9 일부 실행에만 있어 뺀 행", droppedDetail(row)))
	}

	return voted, nil
}

func slotRuns(runs [][]extractedRow) []*voteSlot {
	slots := make([]*voteSlot, 0, len(runs[0]))

	for run, rows := range runs {
		reps := make([]*extractedRow, len(slots))
		for i, slot := range slots {
			reps[i] = slot.seen()[0]
		}

		pairs, unmatched := pairRuns(reps, rows)
		for _, pair := range pairs {
			slots[pair[0]].by[run] = &rows[pair[1]]
		}

		for _, j := range unmatched {
			slot := &voteSlot{firstRun: run, by: make([]*extractedRow, len(runs))}

			slot.by[run] = &rows[j]
			slots = append(slots, slot)
		}
	}

	return slots
}

func voteRow(got []*extractedRow, runs int) extractedRow {
	row := *got[0]
	notes := statusWarnings(got)

	row.Status = voteStatus(got)

	fields := []struct {
		name  string
		value func(*extractedRow) string
		set   func(from *extractedRow)
	}{
		{"날짜", func(r *extractedRow) string { return r.Date }, func(from *extractedRow) { row.Date = from.Date }},
		{"시작", func(r *extractedRow) string { return clockShown(r.Start) }, func(from *extractedRow) { row.Start = from.Start }},
		{"끝", func(r *extractedRow) string { return clockShown(r.End) }, func(from *extractedRow) { row.End = from.End }},
		{"시간대", func(r *extractedRow) string { return zoneKey(r.TZ) }, func(from *extractedRow) { row.TZ = from.TZ }},
	}

	for _, field := range fields {
		key, ok := majority(got, field.value)
		if !ok {
			notes = append(notes, warning("E7 실행 간 값 불일치", field.name+" "+shownValues(got, field.value)))

			continue
		}

		field.set(got[slices.IndexFunc(got, func(r *extractedRow) bool { return field.value(r) == key })])
	}

	// 같은 조각(인원수 등)이 원문과 배지를 오가므로, 따로 고르면 조각이 양쪽에서 모두 사라질 수 있다.
	text, agreed := mostCommon(got, textBadgesKey)

	row.Text, row.Badges = text.Text, text.Badges

	if agreed < extractQuorum {
		notes = append(notes, warning("E7 실행 간 값 불일치", "원문·배지 "+shownValues(got, textBadgesShown)))
	}

	if len(got) < runs {
		notes = append(notes, warning("E8 일부 실행에만 있는 행", fmt.Sprintf("%d/%d", len(got), runs)))
	}

	row.Disagreements = notes

	return row
}

func statusSplitsOnAiring(got []*extractedRow) bool {
	first := airs(got[0].Status)

	return slices.ContainsFunc(got, func(row *extractedRow) bool { return airs(row.Status) != first })
}

// voteStatus는 방송 있음·없음이 갈린 칸을 카드가 단정하지 않도록 tentative로 둔다.
func voteStatus(got []*extractedRow) posterstore.RowStatus {
	if statusSplitsOnAiring(got) {
		return posterstore.RowTentative
	}

	if status, ok := majority(got, func(r *extractedRow) posterstore.RowStatus { return r.Status }); ok {
		return status
	}

	if airs(got[0].Status) {
		return posterstore.RowTentative
	}

	return posterstore.RowOff
}

func statusWarnings(got []*extractedRow) []string {
	if !statusSplitsOnAiring(got) {
		return nil
	}

	return []string{warning("E6 실행 간 방송 유무 불일치", shownValues(got, func(r *extractedRow) string { return string(r.Status) }))}
}

func majority[T comparable](got []*extractedRow, value func(*extractedRow) T) (T, bool) {
	for _, row := range got {
		key := value(row)

		if countFunc(got, func(other *extractedRow) bool { return value(other) == key }) >= extractQuorum {
			return key, true
		}
	}

	var zero T

	return zero, false
}

func countFunc(got []*extractedRow, match func(*extractedRow) bool) int {
	count := 0

	for _, row := range got {
		if match(row) {
			count++
		}
	}

	return count
}

// 결과의 실행 순번은 응답이 온 순서와 무관하므로 값을 정렬해 보여 준다.
func shownValues(got []*extractedRow, value func(*extractedRow) string) string {
	shown := make([]string, len(got))
	for i, row := range got {
		shown[i] = value(row)
	}

	slices.Sort(shown)

	return strings.Join(shown, "/")
}

func countEmpty(runs [][]extractedRow) int {
	count := 0

	for _, rows := range runs {
		if len(rows) == 0 {
			count++
		}
	}

	return count
}

func clockShown(clock *string) string {
	return cmp.Or(clockKey(clock), "없음")
}

// zoneKey는 카드가 시간대가 없는 행도 JST로 읽으므로 없음과 JST 표기 차이를 같게 본다.
func zoneKey(tz *string) string {
	if zone := optional(tz); zone != "" && !strings.EqualFold(zone, "JST") {
		return zone
	}

	return "JST"
}

func mostCommon(got []*extractedRow, key func(*extractedRow) string) (*extractedRow, int) {
	best, bestCount := got[0], 0

	for _, row := range got {
		k := key(row)

		if count := countFunc(got, func(other *extractedRow) bool { return key(other) == k }); count > bestCount {
			best, bestCount = row, count
		}
	}

	return best, bestCount
}

func textBadgesShown(row *extractedRow) string {
	if len(row.Badges) == 0 {
		return clip(row.Text)
	}

	return clip(row.Text + " [" + strings.Join(row.Badges, ", ") + "]")
}

func textBadgesKey(row *extractedRow) string {
	forms := make([]string, len(row.Badges))
	for i, badge := range row.Badges {
		forms[i] = badgeForm(badge)
	}

	slices.Sort(forms)

	return string(textKey(row.Text)) + "\x00" + strconv.Itoa(len(forms)) + "\x00" + strings.Join(forms, "\x00")
}

func textKey(text string) []rune {
	folded := cases.Fold().String(norm.NFKC.String(text))

	return []rune(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}

		return -1
	}, folded))
}

func badgeForm(badge string) string {
	return strings.Trim(strings.Join(strings.Fields(cases.Fold().String(norm.NFKC.String(badge))), " "), " #＃")
}

// clockKey는 validateExtracted를 통과한 H:MM·HH:MM 시각만 받는다.
func clockKey(clock *string) string {
	if clock == nil {
		return ""
	}

	if len(*clock) == len("9:00") {
		return "0" + *clock
	}

	return *clock
}

func orderKey(row *extractedRow) string {
	return row.Date + " " + cmp.Or(clockKey(row.Start), "99")
}

func insertionPoint(rows []extractedRow, row *extractedRow) int {
	key := orderKey(row)

	if at := slices.IndexFunc(rows, func(other extractedRow) bool { return orderKey(&other) > key }); at >= 0 {
		return at
	}

	return len(rows)
}

func droppedTarget(rows []extractedRow, row *extractedRow) int {
	if at := slices.IndexFunc(rows, func(other extractedRow) bool { return other.Date == row.Date }); at >= 0 {
		return at
	}

	return max(insertionPoint(rows, row)-1, 0)
}

func droppedDetail(row *extractedRow) string {
	parts := []string{row.Date}
	if row.Start != nil {
		parts = append(parts, *row.Start)
	}

	return strings.Join(append(parts, clip(row.Text)), " ")
}

type runPairing struct {
	a            []*extractedRow
	b            []extractedRow
	keysA, keysB [][]rune
	usedA, usedB []bool
	similarities map[[2]int]float64
	pairs        [][2]int
}

type pairCandidate struct {
	score float64
	i, j  int
}

// pairRuns는 하니스 compare.match_rows와 같은 순서로 a와 b의 행을 짝짓는다. 짝과 짝을 찾지 못한 b의 번호를 돌려준다.
func pairRuns(a []*extractedRow, b []extractedRow) ([][2]int, []int) {
	p := &runPairing{
		a: a, b: b, keysA: make([][]rune, len(a)), keysB: make([][]rune, len(b)),
		usedA: make([]bool, len(a)), usedB: make([]bool, len(b)), similarities: map[[2]int]float64{},
	}

	for i := range a {
		p.keysA[i] = textKey(a[i].Text)
	}

	for j := range b {
		p.keysB[j] = textKey(b[j].Text)
	}

	p.take(func(i, j int) (float64, bool) { return 1, p.sameSlot(i, j) && slices.Equal(p.keysA[i], p.keysB[j]) })
	p.take(func(i, j int) (float64, bool) { return p.similar(i, j), p.sameSlot(i, j) })
	p.take(func(i, j int) (float64, bool) {
		s := p.similar(i, j)

		return s, a[i].Date == b[j].Date && s >= sameDaySimilarity
	})
	p.take(func(i, j int) (float64, bool) {
		s := p.similar(i, j)

		return s, s >= anyDaySimilarity
	})

	var unmatched []int

	for j := range b {
		if !p.usedB[j] {
			unmatched = append(unmatched, j)
		}
	}

	return p.pairs, unmatched
}

func (p *runPairing) sameSlot(i, j int) bool {
	return p.a[i].Date == p.b[j].Date && clockKey(p.a[i].Start) == clockKey(p.b[j].Start)
}

func (p *runPairing) similar(i, j int) float64 {
	key := [2]int{i, j}
	if s, ok := p.similarities[key]; ok {
		return s
	}

	s := similarity(p.keysA[i], p.keysB[j])

	p.similarities[key] = s

	return s
}

func (p *runPairing) take(score func(i, j int) (float64, bool)) {
	var candidates []pairCandidate

	for i := range p.a {
		for j := range p.b {
			if p.usedA[i] || p.usedB[j] {
				continue
			}

			if s, ok := score(i, j); ok {
				candidates = append(candidates, pairCandidate{s, i, j})
			}
		}
	}

	slices.SortFunc(candidates, func(x, y pairCandidate) int {
		return cmp.Or(cmp.Compare(y.score, x.score), cmp.Compare(y.i, x.i), cmp.Compare(y.j, x.j))
	})

	for _, c := range candidates {
		if !p.usedA[c.i] && !p.usedB[c.j] {
			p.usedA[c.i], p.usedB[c.j] = true, true
			p.pairs = append(p.pairs, [2]int{c.i, c.j})
		}
	}
}

// similarity는 Python difflib.SequenceMatcher(None, a, b).ratio()와 같다.
func similarity(a, b []rune) float64 {
	total := len(a) + len(b)
	if total == 0 {
		return 1
	}

	return 2 * float64(matchingRunes(a, b)) / float64(total)
}

type runeSpan struct {
	alo, ahi, blo, bhi int
}

func matchingRunes(a, b []rune) int {
	positions := map[rune][]int{}

	for j, r := range b {
		positions[r] = append(positions[r], j)
	}

	if len(b) >= autojunkMinRunes {
		limit := len(b)/100 + 1

		maps.DeleteFunc(positions, func(_ rune, at []int) bool { return len(at) > limit })
	}

	matched := 0
	queue := []runeSpan{{0, len(a), 0, len(b)}}

	for len(queue) > 0 {
		span := queue[len(queue)-1]

		queue = queue[:len(queue)-1]

		i, j, k := longestMatch(a, b, positions, span)
		if k == 0 {
			continue
		}

		matched += k

		if span.alo < i && span.blo < j {
			queue = append(queue, runeSpan{span.alo, i, span.blo, j})
		}

		if i+k < span.ahi && j+k < span.bhi {
			queue = append(queue, runeSpan{i + k, span.ahi, j + k, span.bhi})
		}
	}

	return matched
}

func longestMatch(a, b []rune, positions map[rune][]int, span runeSpan) (int, int, int) {
	besti, bestj, best := span.alo, span.blo, 0
	lengths := map[int]int{}

	for i := span.alo; i < span.ahi; i++ {
		next := map[int]int{}

		for _, j := range positions[a[i]] {
			if j < span.blo {
				continue
			}

			if j >= span.bhi {
				break
			}

			k := lengths[j-1] + 1

			next[j] = k

			if k > best {
				besti, bestj, best = i-k+1, j-k+1, k
			}
		}

		lengths = next
	}

	// autojunk로 뺀 글자도 찾은 짝의 양옆으로는 이어 붙인다.
	for besti > span.alo && bestj > span.blo && a[besti-1] == b[bestj-1] {
		besti, bestj, best = besti-1, bestj-1, best+1
	}

	for besti+best < span.ahi && bestj+best < span.bhi && a[besti+best] == b[bestj+best] {
		best++
	}

	return besti, bestj, best
}
