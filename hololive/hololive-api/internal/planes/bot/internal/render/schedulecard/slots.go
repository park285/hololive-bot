package schedulecard

import (
	"cmp"
	"fmt"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	daysPerWeek = 7

	maxNameRunes  = 40
	maxTitleRunes = 120
	maxWithRunes  = 30
	maxPlaceRunes = 60
	maxNoteRunes  = 80
	maxTagRunes   = 20

	maxWithShown = 6
	maxTagsShown = 3

	maxSlotsPerDay = 5
	maxFoldDays    = 2

	othersMarker = "그 외 참가자"

	noTime = 30 * 60
)

type view struct {
	name, period, updated string
	days                  []day
	photo                 image.Image
}

type day struct {
	date  time.Time
	slots []slot
}

func (d day) off() bool { return len(d.slots) == 1 && d.slots[0].status == StatusOff }

type slot struct {
	start   string
	minutes int
	status  Status
	main    string
	sub     string
	tags    []string
	more    int
	from    string
}

func prepare(card Card, drawable func(rune) bool) (view, error) {
	start, end := civil(card.WeekStart), civil(card.WeekEnd)
	if card.WeekStart.IsZero() || card.WeekEnd.IsZero() || end.Before(start) || end.After(start.AddDate(0, 0, daysPerWeek-1)) {
		return view{}, fmt.Errorf("%w: week %s..%s", ErrInvalidCard, start.Format(time.DateOnly), end.Format(time.DateOnly))
	}

	if card.ExtractedAt.IsZero() {
		return view{}, fmt.Errorf("%w: missing extracted time", ErrInvalidCard)
	}

	if len(card.Items) > MaxItems {
		return view{}, fmt.Errorf("%w: %d items exceed %d", ErrInvalidCard, len(card.Items), MaxItems)
	}

	days := make([]day, daysPerWeek)
	for i := range days {
		days[i].date = start.AddDate(0, 0, i)
	}

	for i := range card.Items {
		it := &card.Items[i]

		edge, ok := cardDay(it, start, end)
		if !ok {
			continue
		}

		s, err := toSlot(it, drawable)
		if err != nil {
			return view{}, fmt.Errorf("%w: item %d: %w", ErrInvalidCard, i+1, err)
		}

		if d := civil(it.Day); !d.Equal(edge) {
			s.start = dowKO[weekday(d)] + " " + s.start

			s.minutes += int(d.Sub(edge).Minutes())
		}

		idx := int(edge.Sub(start).Hours() / 24)

		days[idx].slots = append(days[idx].slots, s)
	}

	for i := range days {
		days[i].slots = capSlots(mergeOff(sortSlots(days[i].slots)), maxSlotsPerDay)
	}

	return view{
		name:    cleanText(card.MemberName, maxNameRunes, drawable),
		period:  start.Format("01.02") + " – " + end.Format("01.02"),
		updated: card.ExtractedAt.In(jst).Format("2006.01.02 15:04") + " 업데이트",
		days:    days,
		photo:   usablePhoto(card.Photo),
	}, nil
}

// cardDay는 주 범위 밖의 변환 항목을 가까운 끝 날 칸에 둔다. 요일을 붙일 시각이 없는 항목은 다른 날 칸에서 그날 일정으로 읽히므로 버린다.
// 변환과 창 이동이 각각 하루씩 밀 수 있어 창에서 이틀 안의 항목만 접고, 더 먼 항목은 요일이 겹쳐 끝 날 일정으로 읽히므로 버린다.
func cardDay(it *Item, start, end time.Time) (time.Time, bool) {
	d := civil(it.Day)

	switch {
	case !d.Before(start) && !d.After(end):
		return d, true
	case !it.Converted || it.Start == "" || it.Status == StatusOff:
		return time.Time{}, false
	case d.Before(start) && !d.Before(start.AddDate(0, 0, -maxFoldDays)):
		return start, true
	case d.After(end) && !d.After(end.AddDate(0, 0, maxFoldDays)):
		return end, true
	default:
		return time.Time{}, false
	}
}

func civil(t time.Time) time.Time {
	y, m, d := t.Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func usablePhoto(img image.Image) image.Image {
	if img == nil || img.Bounds().Empty() {
		return nil
	}

	if p, ok := img.(*image.Paletted); ok && len(p.Palette) == 0 {
		return nil
	}

	return img
}

func toSlot(it *Item, drawable func(rune) bool) (slot, error) {
	switch it.Status {
	case StatusPlanned, StatusTentative, StatusCancelled, StatusOff:
	default:
		return slot{}, fmt.Errorf("unknown status %q", it.Status)
	}

	start, minutes, err := clockText(it.Start)
	if err != nil {
		return slot{}, err
	}

	if _, _, err = clockText(it.End); err != nil {
		return slot{}, err
	}

	prev, _, err := clockText(it.PrevStart)
	if err != nil {
		return slot{}, fmt.Errorf("previous start: %w", err)
	}

	s := slot{start: start, minutes: minutes, status: it.Status, main: cleanText(it.Title, maxTitleRunes, drawable)}

	switch {
	case prev != "" && prev != start:
		s.from = prev
	case !it.MovedFrom.IsZero() && !civil(it.MovedFrom).Equal(civil(it.Day)):
		s.from = dowKO[weekday(it.MovedFrom)]
	}

	if s.main == "" {
		s.main = "제목 미정"

		if it.Status == StatusOff {
			s.main = "휴방"
		}
	}

	s.sub = subtitle(it, drawable)
	s.tags = shownTags(it.Tags, s.main+" "+s.sub, drawable)

	return s, nil
}

// clockText의 시 상한 29는 저장 검증(ValidateRows, migration 019 CHECK)과 같다. 25:00 같은 심야 표기를 그대로 둔다.
func clockText(v string) (text string, minutes int, err error) {
	if v == "" {
		return "", noTime, nil
	}

	h, m, ok := strings.Cut(v, ":")
	if !ok || len(h) < 1 || len(h) > 2 || len(m) != 2 || !isDigits(h) || !isDigits(m) {
		return "", 0, fmt.Errorf("invalid time %q", v)
	}

	hour, err := strconv.Atoi(h)
	if err != nil {
		return "", 0, fmt.Errorf("parse hour %q: %w", v, err)
	}

	minute, err := strconv.Atoi(m)
	if err != nil {
		return "", 0, fmt.Errorf("parse minute %q: %w", v, err)
	}

	if hour > 29 || minute > 59 {
		return "", 0, fmt.Errorf("invalid time %q", v)
	}

	return fmt.Sprintf("%02d:%02d", hour, minute), hour*60 + minute, nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

func subtitle(it *Item, drawable func(rune) bool) string {
	var parts []string

	if w := withPhrase(it.With, drawable); w != "" {
		parts = append(parts, w)
	}

	if p := cleanText(it.Place, maxPlaceRunes, drawable); p != "" {
		parts = append(parts, p+"에서")
	}

	if n := cleanText(it.Note, maxNoteRunes, drawable); n != "" {
		parts = append(parts, n)
	}

	if len(parts) == 0 {
		return ""
	}

	return "(" + strings.Join(parts, " · ") + ")"
}

func withPhrase(with []string, drawable func(rune) bool) string {
	names := make([]string, 0, min(len(with), maxWithShown+1))
	others := false

	for _, w := range with {
		if n := cleanText(w, maxWithRunes, drawable); n != "" {
			names = append(names, n)
		}
	}

	if len(names) > 1 && names[len(names)-1] == othersMarker {
		names, others = names[:len(names)-1], true
	}

	if len(names) > maxWithShown {
		names, others = names[:maxWithShown], true
	}

	if len(names) == 0 {
		return ""
	}

	joined := strings.Join(names, " · ")

	if others {
		return joined + " 등과 함께"
	}

	return joined + withParticle(names[len(names)-1]) + " 함께"
}

func withParticle(name string) string {
	var word []rune

	inWord := false

	for _, r := range stripRoles(name) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			inWord = false

			continue
		}

		if !inWord {
			word = word[:0]
		}

		word, inWord = append(word, unicode.ToLower(r)), true
	}

	last := ' '

	if len(word) > 0 {
		last = word[len(word)-1]
	}

	tail := string(word)

	switch {
	case last >= '가' && last <= '힣':
		if (last-'가')%28 != 0 {
			return "과"
		}
	case strings.ContainsRune("013678", last):
		return "과"
	case strings.ContainsRune("lmnr", last),
		strings.HasSuffix(tail, "ng"), strings.HasSuffix(tail, "ck"),
		len(word) > 2 && (strings.HasSuffix(tail, "me") || strings.HasSuffix(tail, "ne") || strings.HasSuffix(tail, "le")):
		return "과"
	}

	return "와"
}

func stripRoles(name string) string {
	for {
		name = strings.TrimSpace(name)

		rest, ok := strings.CutSuffix(name, ")")
		if !ok {
			return name
		}

		before, _, found := strings.CutLast(rest, "(")
		if !found || strings.TrimSpace(before) == "" {
			return name
		}

		name = before
	}
}

func shownTags(tags []string, title string, drawable func(rune) bool) []string {
	var out []string

	for _, t := range tags {
		t = cleanText(t, maxTagRunes, drawable)
		if t == "" || titleStates(title, t) || slices.Contains(out, t) {
			continue
		}

		out = append(out, t)
		if len(out) == maxTagsShown {
			break
		}
	}

	return out
}

func titleStates(title, tag string) bool {
	for from := 0; ; {
		i := strings.Index(title[from:], tag)
		if i < 0 {
			return false
		}

		start := from + i
		end := start + len(tag)
		joinedBefore := start > 0 && asciiWord(tag[0]) && asciiWord(title[start-1])
		joinedAfter := end < len(title) && asciiWord(tag[len(tag)-1]) && asciiWord(title[end])

		if !joinedBefore && !joinedAfter {
			return true
		}

		_, size := utf8.DecodeRuneInString(title[start:])

		from = start + size
	}
}

func asciiWord(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func cleanText(s string, maxRunes int, drawable func(rune) bool) string {
	if limit := maxRunes * utf8.UTFMax * 2; len(s) > limit {
		s = s[:limit]
	}

	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return ' '
		case unicode.Is(unicode.Cf, r), r == utf8.RuneError:
			return -1
		case !unicode.IsSpace(r) && !drawable(r):
			return -1
		}

		return r
	}, norm.NFC.String(strings.ToValidUTF8(s, "")))
	s = strings.Join(strings.Fields(s), " ")

	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	runes := []rune(s)

	return strings.TrimRight(string(runes[:maxRunes-1]), " ") + "…"
}

func sortSlots(slots []slot) []slot {
	slices.SortStableFunc(slots, func(a, b slot) int {
		return cmp.Compare(a.minutes, b.minutes)
	})

	return slots
}

func mergeOff(slots []slot) []slot {
	for _, s := range slots {
		if s.status != StatusOff {
			return slots
		}
	}

	return slots[:min(len(slots), 1)]
}

func capSlots(slots []slot, limit int) []slot {
	if len(slots) <= limit {
		return slots
	}

	kept := slots[: limit-1 : limit-1]
	rest := 0

	for _, s := range slots[limit-1:] {
		rest += max(1, s.more)
	}

	return append(kept, slot{minutes: noTime, status: StatusPlanned, main: fmt.Sprintf("외 %d건", rest), more: rest})
}

func foldDays(days []day, limit int) []day {
	out := make([]day, len(days))
	copy(out, days)

	for i := range out {
		out[i].slots = capSlots(out[i].slots, limit)
	}

	return out
}

func fitFolded[T any](days []day, try func([]day) (T, bool)) (T, []day) {
	for limit := maxSlotsPerDay; ; limit-- {
		folded := foldDays(days, limit)
		if r, ok := try(folded); ok || limit == 1 {
			return r, folded
		}
	}
}
