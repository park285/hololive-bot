package schedulecard

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
)

const (
	at20   = "20:00"
	at21   = "21:00"
	at22   = "22:00"
	at2130 = "21:30"
	memTag = "멤버십 한정"
)

func date(t *testing.T, s string) time.Time {
	t.Helper()

	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", s, err)
	}

	return d
}

func weekCard(t *testing.T, items ...Item) Card {
	t.Helper()

	return Card{
		ChannelID:   "UC-unknown",
		MemberName:  "소라시나 소피아",
		WeekStart:   date(t, "2026-11-09"),
		WeekEnd:     date(t, "2026-11-15"),
		ExtractedAt: time.Date(2026, time.November, 12, 10, 30, 0, 0, time.UTC),
		Items:       items,
	}
}

func planned(t *testing.T, day, start, title string) Item {
	t.Helper()

	return Item{Day: date(t, day), Start: start, Status: StatusPlanned, Title: title}
}

func slotTitles(slots []slot) []string {
	out := make([]string, 0, len(slots))
	for _, s := range slots {
		out = append(out, s.main)
	}

	return out
}

func everyRune(rune) bool { return true }

func TestPrepareWeek(t *testing.T) {
	v, err := prepare(weekCard(t,
		planned(t, "2026-11-08", at20, "지난 주"),
		planned(t, "2026-11-10", "", "시각 미정"),
		planned(t, "2026-11-10", "25:30", "심야"),
		planned(t, "2026-11-10", "9:05", "아침"),
		planned(t, "2026-11-10", "", "두 번째 미정"),
		planned(t, "2026-11-16", at20, "다음 주"),
	), everyRune)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	if len(v.days) != daysPerWeek {
		t.Fatalf("days = %d, want %d", len(v.days), daysPerWeek)
	}

	for i, d := range v.days {
		if want := date(t, "2026-11-09").AddDate(0, 0, i); !d.date.Equal(want) {
			t.Errorf("days[%d] = %s, want %s", i, d.date.Format(time.DateOnly), want.Format(time.DateOnly))
		}
	}

	for i, d := range v.days {
		if i != 1 && len(d.slots) != 0 {
			t.Errorf("days[%d] slots = %v, want none (out-of-week items must be dropped)", i, slotTitles(d.slots))
		}
	}

	got := strings.Join(slotTitles(v.days[1].slots), ",")
	if want := "아침,심야,시각 미정,두 번째 미정"; got != want {
		t.Errorf("tuesday order = %s, want %s", got, want)
	}

	if v.days[1].slots[0].start != "09:05" {
		t.Errorf("start = %q, want zero-padded 09:05", v.days[1].slots[0].start)
	}

	if v.period != "11.09 – 11.15" {
		t.Errorf("period = %q", v.period)
	}

	if v.updated != "2026.11.12 19:30 업데이트" {
		t.Errorf("updated = %q, want JST time", v.updated)
	}
}

func TestPrepareShortWeekStillDrawsSevenDays(t *testing.T) {
	card := weekCard(t, planned(t, "2026-11-13", at20, "범위 밖"), planned(t, "2026-11-11", at20, "범위 안"))

	card.WeekEnd = date(t, "2026-11-12")

	v, err := prepare(card, everyRune)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	if len(v.days) != daysPerWeek || len(v.days[4].slots) != 0 || len(v.days[2].slots) != 1 {
		t.Fatalf("days = %d, fri slots = %d, wed slots = %d", len(v.days), len(v.days[4].slots), len(v.days[2].slots))
	}

	if v.period != "11.09 – 11.12" {
		t.Errorf("period = %q", v.period)
	}
}

func TestPrepareCapsBusyDay(t *testing.T) {
	hours := []string{at22, "10:00", "12:00", "14:00", "16:00", "18:00", at20}
	items := make([]Item, 0, len(hours))

	for _, h := range hours {
		items = append(items, planned(t, "2026-11-13", h, "방송 "+h))
	}

	v, err := prepare(weekCard(t, items...), everyRune)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	slots := v.days[4].slots
	if len(slots) != maxSlotsPerDay {
		t.Fatalf("slots = %d, want %d", len(slots), maxSlotsPerDay)
	}

	if got := strings.Join(slotTitles(slots[:4]), ","); got != "방송 10:00,방송 12:00,방송 14:00,방송 16:00" {
		t.Errorf("kept = %s", got)
	}

	if last := slots[4]; last.more != 3 || last.main != "외 3건" {
		t.Errorf("summary = %+v, want 외 3건", last)
	}

	v, err = prepare(weekCard(t, items[:5]...), everyRune)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	if n := len(v.days[4].slots); n != 5 || v.days[4].slots[4].more != 0 {
		t.Errorf("five items must all be shown, got %d", n)
	}
}

func TestPrepareSkipsOutOfWeekRowsBeforeValidation(t *testing.T) {
	card := weekCard(t, Item{Day: date(t, "2026-11-16"), Start: "30:00", Status: "moved"}, planned(t, "2026-11-10", at20, "방송"))

	v, err := prepare(card, everyRune)
	if err != nil {
		t.Fatalf("prepare() error = %v, out-of-week rows are never drawn", err)
	}

	if len(v.days[1].slots) != 1 {
		t.Errorf("tuesday slots = %d, want 1", len(v.days[1].slots))
	}
}

func TestPrepareFoldsConvertedRowsOutsideTheWindowIntoTheEdgeDays(t *testing.T) {
	converted := func(day, start, title string, status Status) Item {
		return Item{Day: date(t, day), Start: start, Status: status, Title: title, Converted: true}
	}

	v, err := prepare(weekCard(t,
		planned(t, "2026-11-09", "9:00", "첫날"),
		converted("2026-11-08", "10:00", "앞 주", StatusPlanned),
		converted("2026-11-07", "23:00", "앞앞 미정", StatusTentative),
		converted("2026-11-07", "", "시각 없는 앞앞", StatusPlanned),
		converted("2026-11-06", "10:00", "사흘 앞", StatusPlanned),
		converted("2026-11-23", "10:00", "한 주 뒤", StatusPlanned),
		converted("2026-11-10", at21, "안쪽", StatusPlanned),
		planned(t, "2026-11-15", at22, "끝날"),
		converted("2026-11-16", "25:00", "다음 주", StatusPlanned),
		converted("2026-11-16", at20, "다음 주 취소", StatusCancelled),
		converted("2026-11-16", "", "다음 주 휴방", StatusOff),
		converted("2026-11-16", "21:00", "다음 주 휴방 시각", StatusOff),
		planned(t, "2026-11-16", at20, "변환 안 한 다음 주"),
	), everyRune)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	labels := func(d day) []string {
		out := make([]string, 0, len(d.slots))

		for _, s := range d.slots {
			label, kind := timeLabel(s)

			out = append(out, fmt.Sprintf("%s|%s|%d", s.main, label, kind))
		}

		return out
	}

	if got, want := labels(v.days[0]), []string{
		fmt.Sprintf("앞앞 미정|토 23:00 ?|%d", timeUnsure),
		fmt.Sprintf("앞 주|일 10:00|%d", timePlain),
		fmt.Sprintf("첫날|09:00|%d", timePlain),
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("first day = %q, want %q (earlier rows first, each with its own weekday, untimed and far rows dropped)", got, want)
	}

	if got, want := labels(v.days[1]), []string{fmt.Sprintf("안쪽|21:00|%d", timePlain)}; !reflect.DeepEqual(got, want) {
		t.Errorf("tuesday = %q, want %q (rows inside the window keep their day)", got, want)
	}

	if got, want := labels(v.days[6]), []string{
		fmt.Sprintf("끝날|22:00|%d", timePlain),
		fmt.Sprintf("다음 주 취소|월 20:00|%d", timeCancelled),
		fmt.Sprintf("다음 주|월 25:00|%d", timePlain),
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("last day = %q, want %q (later rows last; unconverted, untimed, off and far rows dropped)", got, want)
	}
}

func TestPrepareMergesOff(t *testing.T) {
	off := Item{Day: date(t, "2026-11-10"), Status: StatusOff}
	postedOff := Item{Day: date(t, "2026-11-10"), Status: StatusOff, FromPost: true}
	canceled := Item{Day: date(t, "2026-11-10"), Start: at21, Status: StatusCancelled, Title: "취소"}
	stream := planned(t, "2026-11-10", at20, "방송")
	added := Item{Day: date(t, "2026-11-10"), Start: at20, Status: StatusPlanned, Title: "게릴라", FromPost: true}
	maybe := Item{Day: date(t, "2026-11-10"), Status: StatusTentative, Title: "게릴라 미정", FromPost: true}

	for name, tc := range map[string]struct {
		items []Item
		want  []string
	}{
		"휴방만 있는 날은 휴방 하나":             {[]Item{off, off}, []string{"휴방"}},
		"포스터가 함께 적은 휴방과 방송은 둘 다 둔다":   {[]Item{off, stream}, []string{"휴방", "방송"}},
		"글이 더한 예정 방송이 있으면 휴방을 뺀다":     {[]Item{off, added}, []string{"게릴라"}},
		"글이 더한 미정 방송이 있어도 휴방을 뺀다":     {[]Item{off, maybe}, []string{"게릴라 미정"}},
		"글이 휴방으로 바꾼 칸은 남은 방송이 있으면 뺀다": {[]Item{postedOff, stream}, []string{"방송"}},
		"취소 칸만 있으면 휴방을 둔다":            {[]Item{off, canceled}, []string{"취소", "휴방"}},
	} {
		v, err := prepare(weekCard(t, tc.items...), everyRune)
		if err != nil {
			t.Fatalf("%s: prepare() error = %v", name, err)
		}

		if got := slotTitles(v.days[1].slots); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: slots = %q, want %q", name, got, tc.want)
		}
	}
}

func TestPrepareChangeMarks(t *testing.T) {
	tue, fri := date(t, "2026-11-10"), date(t, "2026-11-13")

	v, err := prepare(weekCard(t,
		Item{Day: tue, Start: at22, PrevStart: "9:30", MovedFrom: fri, Status: StatusPlanned, Title: "늦춘 방송"},
		Item{Day: tue, Start: at20, MovedFrom: fri, Status: StatusPlanned, Title: "옮긴 방송"},
		Item{Day: tue, Start: "12:00", PrevStart: "12:00", MovedFrom: tue, Status: StatusPlanned, Title: "그대로"},
		Item{Day: tue, Start: "", PrevStart: at21, Status: StatusPlanned, Title: "미정으로"},
		Item{Day: tue, Start: "25:00", PrevStart: "23:00", Status: StatusPlanned, Title: "자정 넘김"},
		Item{Day: date(t, "2026-11-16"), Start: at20, PrevStart: "99:99", Status: StatusPlanned, Title: "다음 주"},
	), everyRune)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	got := make([]string, 0, len(v.days[1].slots))

	for i := range v.days[1].slots {
		label, kind := timeLabel(v.days[1].slots[i])

		got = append(got, fmt.Sprintf("%s|%s|%d", v.days[1].slots[i].main, label, kind))
	}

	want := []string{
		fmt.Sprintf("그대로|12:00|%d", timePlain),
		fmt.Sprintf("옮긴 방송|금 → 20:00|%d", timeChanged),
		fmt.Sprintf("늦춘 방송|09:30 → 22:00|%d", timeChanged),
		fmt.Sprintf("자정 넘김|23:00 → 25:00|%d", timeChanged),
		fmt.Sprintf("미정으로|21:00 → 미정|%d", timeChanged),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tuesday = %q, want %q (sorted by the applied time)", got, want)
	}
}

func TestPrepareFoldsSixChangedItems(t *testing.T) {
	items := changeItems(t, 6)

	v, err := prepare(weekCard(t, items...), everyRune)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	for i, d := range v.days {
		if len(d.slots) != maxSlotsPerDay {
			t.Fatalf("day %d slots = %d, want %d", i, len(d.slots), maxSlotsPerDay)
		}

		if last := d.slots[maxSlotsPerDay-1]; last.main != "외 2건" || last.more != 2 {
			t.Errorf("day %d summary = %q, want 외 2건", i, last.main)
		}
	}
}

func TestFoldDaysKeepsCount(t *testing.T) {
	slots := make([]slot, 0, maxSlotsPerDay+3)
	for i := range maxSlotsPerDay + 3 {
		slots = append(slots, slot{minutes: i, status: StatusPlanned, main: fmt.Sprint(i)})
	}

	days := []day{{slots: capSlots(slots, maxSlotsPerDay)}, {slots: slots[:1]}}

	for limit := maxSlotsPerDay; limit >= 1; limit-- {
		folded := foldDays(days, limit)
		if n := len(folded[0].slots); n != limit {
			t.Errorf("limit %d: slots = %d", limit, n)
		}

		if total := countShown(folded[:1]); total != len(slots) {
			t.Errorf("limit %d: shown + folded = %d, want %d", limit, total, len(slots))
		}

		if len(folded[1].slots) != 1 || folded[1].slots[0].more != 0 {
			t.Errorf("limit %d: single-item day changed to %+v", limit, folded[1].slots)
		}
	}

	if len(days[0].slots) != maxSlotsPerDay {
		t.Error("foldDays modified its input")
	}
}

func TestSubtitle(t *testing.T) {
	tests := []struct {
		name string
		item Item
		want string
	}{
		{"no extras", Item{}, ""},
		{"vowel ending", Item{With: []string{"쿄코"}}, "(쿄코와 함께)"},
		{"batchim ending", Item{With: []string{"메라 선배님"}}, "(메라 선배님과 함께)"},
		{"several people", Item{With: []string{"비쥬 선배", "치하야 선배"}}, "(비쥬 선배 · 치하야 선배와 함께)"},
		{"others marker", Item{With: []string{"쿄코", "그 외 참가자"}}, "(쿄코 등과 함께)"},
		{"only others marker", Item{With: []string{"그 외 참가자"}}, "(그 외 참가자와 함께)"},
		{"latin consonant", Item{With: []string{"IRyS"}}, "(IRyS와 함께)"},
		{"latin liquid", Item{With: []string{"Hakos Baelz", "Gura L"}}, "(Hakos Baelz · Gura L과 함께)"},
		{"digit with batchim", Item{With: []string{"홀로멤 3"}}, "(홀로멤 3과 함께)"},
		{"digit without batchim", Item{With: []string{"팀 2"}}, "(팀 2와 함께)"},
		{"trailing punctuation", Item{With: []string{"아소비!"}}, "(아소비!와 함께)"},
		{"blank names dropped", Item{With: []string{" ", "\t", "쿄코"}}, "(쿄코와 함께)"},
		{"too many people", Item{With: []string{"A", "B", "C", "D", "E", "F", "G"}}, "(A · B · C · D · E · F 등과 함께)"},
		{"place", Item{Place: "메라 채널"}, "(메라 채널에서)"},
		{"note", Item{Note: "휴방 가능성 높음"}, "(휴방 가능성 높음)"},
		{"order with place note", Item{With: []string{"아소비"}, Place: "메라 채널", Note: "멤버십"}, "(아소비와 함께 · 메라 채널에서 · 멤버십)"},
		{"role without batchim ignored", Item{With: []string{"루이 선배 (진행)"}}, "(루이 선배 (진행)와 함께)"},
		{"role with batchim ignored", Item{With: []string{"라덴 (게스트)"}}, "(라덴 (게스트)과 함께)"},
		{"honorific role ignored", Item{With: []string{"메라 (주최자님)"}}, "(메라 (주최자님)와 함께)"},
		{"latin ng", Item{With: []string{"Song"}}, "(Song과 함께)"},
		{"latin ck", Item{With: []string{"Rock"}}, "(Rock과 함께)"},
		{"latin silent e", Item{With: []string{"Elizabeth Rose Bloodflame"}}, "(Elizabeth Rose Bloodflame과 함께)"},
		{"latin vowel e", Item{With: []string{"Rose"}}, "(Rose와 함께)"},
		{"decomposed hangul", Item{With: []string{"\u1106\u1166\u1105\u1161\u11b7"}}, "(메람과 함께)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subtitle(&tt.item, everyRune, maxPlaceRunes, maxNoteRunes); got != tt.want {
				t.Errorf("subtitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShownTags(t *testing.T) {
	got := shownTags([]string{"EN", "멤버십 한정", "실황", "실황", "", "콜라보", "3D", "ASMR"}, "멤버십 한정 방송 (실황 아님)", everyRune)
	if want := []string{"EN", "콜라보", "3D"}; !reflect.DeepEqual(got, want) {
		t.Errorf("shownTags() = %q, want %q", got, want)
	}

	got = shownTags([]string{"EN", "JP", "3D"}, "ENDING까지 달리는 JP 서버 3D 방송", everyRune)
	if want := []string{"EN"}; !reflect.DeepEqual(got, want) {
		t.Errorf("shownTags() = %q, want %q", got, want)
	}

	got = shownTags([]string{"A A", "A-A", "가가"}, "xA A A 1A-A-A 가가가", everyRune)
	if len(got) != 0 {
		t.Errorf("shownTags() = %q, want tags overlapping a joined match still found in the title", got)
	}
}

func TestTimeLabel(t *testing.T) {
	tests := []struct {
		s     slot
		label string
		kind  timeKind
	}{
		{slot{status: StatusPlanned, start: at21}, at21, timePlain},
		{slot{status: StatusPlanned}, "미정", timeUnsure},
		{slot{status: StatusTentative, start: at21}, "21:00 ?", timeUnsure},
		{slot{status: StatusTentative}, "미정", timeUnsure},
		{slot{status: StatusOff, start: at21}, "", timeNone},
		{slot{status: StatusCancelled, start: at21}, at21, timeCancelled},
		{slot{status: StatusCancelled}, "", timeNone},
		{slot{status: StatusPlanned, more: 2}, "", timeNone},
		{slot{status: StatusPlanned, start: at22, from: at2130}, "21:30 → 22:00", timeChanged},
		{slot{status: StatusPlanned, from: at2130}, "21:30 → 미정", timeChanged},
		{slot{status: StatusTentative, start: at22, from: at2130}, "21:30 → 22:00 ?", timeChanged},
		{slot{status: StatusPlanned, start: at20, from: "금"}, "금 → 20:00", timeChanged},
		{slot{status: StatusTentative, from: "금"}, "금 → 미정", timeChanged},
		{slot{status: StatusCancelled, start: at22, from: at2130}, at22, timeCancelled},
		{slot{status: StatusOff, from: at2130}, "", timeNone},
		{slot{status: StatusPlanned, start: at22, from: at2130, more: 2}, "", timeNone},
	}

	for _, tt := range tests {
		if label, kind := timeLabel(tt.s); label != tt.label || kind != tt.kind {
			t.Errorf("timeLabel(%+v) = %q, %d; want %q, %d", tt.s, label, kind, tt.label, tt.kind)
		}
	}
}

func TestToSlotStatusDisplay(t *testing.T) {
	f := testFonts(t)
	drawable := f.drawable()

	off, err := toSlot(&Item{Status: StatusOff}, drawable, cardtemplate.Classic)
	if err != nil || off.main != "휴방" {
		t.Errorf("empty off title = %q, %v; want 휴방", off.main, err)
	}

	for in, want := range map[string]string{
		"\u202e\x00 ":        "제목 미정",
		"🎮🎤":                 "제목 미정",
		"🎤 노래 방송 🎶":          "노래 방송",
		"\u1112\u1161\u11ab": "한",
	} {
		got, gotErr := toSlot(&Item{Status: StatusPlanned, Start: at20, Title: in}, drawable, cardtemplate.Classic)
		if gotErr != nil || got.main != want {
			t.Errorf("title %q = %q, %v; want %q", in, got.main, gotErr, want)
		}
	}

	canceled, err := toSlot(&Item{Status: StatusCancelled, Start: at20, Title: "노래"}, drawable, cardtemplate.Classic)
	if err != nil {
		t.Fatalf("toSlot() error = %v", err)
	}

	if tb := layoutTitle(f, canceled, txt{Size: 30, Face: faceBold}, txt{Size: 20, Face: faceBold}, titleLimits{maxW: 500, maxMain: 2, maxSub: 1}); !tb.struck || !tb.muted {
		t.Errorf("canceled title block struck=%v muted=%v, want both", tb.struck, tb.muted)
	}
}

func TestRenderRejectsInvalidCard(t *testing.T) {
	tooMany := make([]Item, MaxItems+1)
	for i := range tooMany {
		tooMany[i] = Item{Day: date(t, "2026-11-10"), Status: StatusPlanned, Title: "x"}
	}

	tests := []struct {
		name   string
		mutate func(*Card)
		format Format
	}{
		{"unknown format", func(*Card) {}, Format("1x1")},
		{"unknown status", func(c *Card) { c.Items = []Item{{Day: c.WeekStart, Status: "moved"}} }, Format16x9},
		{"minute out of range", func(c *Card) { c.Items = []Item{{Day: c.WeekStart, Status: StatusPlanned, Start: "21:60"}} }, Format16x9},
		{"hour out of range", func(c *Card) { c.Items = []Item{{Day: c.WeekStart, Status: StatusPlanned, Start: "30:00"}} }, Format16x9},
		{"signed hour", func(c *Card) { c.Items = []Item{{Day: c.WeekStart, Status: StatusPlanned, Start: "+1:00"}} }, Format16x9},
		{"short minute", func(c *Card) { c.Items = []Item{{Day: c.WeekStart, Status: StatusPlanned, Start: "9:5"}} }, Format16x9},
		{"bad end time", func(c *Card) { c.Items = []Item{{Day: c.WeekStart, Status: StatusPlanned, End: "late"}} }, Format16x9},
		{"previous start out of range", func(c *Card) {
			c.Items = []Item{{Day: c.WeekStart, Status: StatusPlanned, Start: at20, PrevStart: "30:00"}}
		}, Format16x9},
		{"malformed previous start", func(c *Card) {
			c.Items = []Item{{Day: c.WeekStart, Status: StatusPlanned, Start: at20, PrevStart: "21時"}}
		}, Format16x9},
		{"week reversed", func(c *Card) { c.WeekEnd = c.WeekStart.AddDate(0, 0, -1) }, Format16x9},
		{"week too long", func(c *Card) { c.WeekEnd = c.WeekStart.AddDate(0, 0, 7) }, Format16x9},
		{"missing week", func(c *Card) { c.WeekStart = time.Time{} }, Format16x9},
		{"missing extracted time", func(c *Card) { c.ExtractedAt = time.Time{} }, Format16x9},
		{"too many items", func(c *Card) { c.Items = tooMany }, Format16x9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := weekCard(t)
			tt.mutate(&card)

			if _, err := Render(card, tt.format); !errors.Is(err, ErrInvalidCard) {
				t.Errorf("Render() error = %v, want ErrInvalidCard", err)
			}
		})
	}
}

func TestCleanText(t *testing.T) {
	tests := []struct {
		name, in, want string
		limit          int
	}{
		{"controls become spaces", "노래\n방송\t\x07끝", "노래 방송 끝", 40},
		{"format characters removed", "abc\u202edef\u200b", "abcdef", 40},
		{"invalid utf8 removed", "a\xffb", "ab", 40},
		{"whitespace collapsed", "  a   b  ", "a b", 40},
		{"long text clamped", strings.Repeat("가", 50), strings.Repeat("가", 9) + "…", 10},
		{"decomposed hangul composed", "\u1112\u1161\u11ab\u1100\u1173\u11af", "한글", 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanText(tt.in, tt.limit, everyRune); got != tt.want {
				t.Errorf("cleanText() = %q, want %q", got, tt.want)
			}
		})
	}

	if got := cleanText(strings.Repeat("가", 1<<20), maxTitleRunes, everyRune); utf8.RuneCountInString(got) != maxTitleRunes {
		t.Errorf("huge input rune count = %d, want %d", utf8.RuneCountInString(got), maxTitleRunes)
	}
}

func TestUnusablePhotoIsDropped(t *testing.T) {
	empty := image.NewPaletted(image.Rect(0, 0, 8, 8), nil)

	for name, img := range map[string]image.Image{
		"nil":           nil,
		"zero bounds":   image.NewRGBA(image.Rectangle{}),
		"empty palette": empty,
	} {
		if got := usablePhoto(img); got != nil {
			t.Errorf("%s: usablePhoto() kept the image", name)
		}
	}

	if hue := dominantHue(empty); hue < 0 || hue > 360 {
		t.Errorf("dominantHue(empty palette) = %v", hue)
	}
}

func TestThemeForChannel(t *testing.T) {
	tests := map[string]theme{
		"UCy9mgxB8pn2C4aNK_MPthDQ": renderTsuzuri,
		"UCSjQDxud2HkAO2DVD3lwxmw": renderKyoko,
		"UCROQtXcp2loQEmvpe5rhJzQ": renderSopia,
		"UC8eitCE9Z6EwUCs-VUi1blg": renderMela,
		"UCnotInTheTable":          renderFallback,
		"":                         renderFallback,
	}

	for id, want := range tests {
		if got := themeFor(id); reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
			t.Errorf("themeFor(%q) picked the wrong theme", id)
		}
	}
}

const kyokoChannel = "UCSjQDxud2HkAO2DVD3lwxmw"

func TestVariantsOfferPostersOnlyForPortraitClassic(t *testing.T) {
	tests := []struct {
		channel  string
		format   Format
		template cardtemplate.ID
		want     []string
	}{
		{kyokoChannel, Format3x4, cardtemplate.Classic, []string{originalVariant, "polished-light", "polished-dark"}},
		{"UCROQtXcp2loQEmvpe5rhJzQ", Format3x4, cardtemplate.Classic, []string{originalVariant, "pastel-orbit", "deep-space-hud", "cyber-science-pop"}},
		{"UC8eitCE9Z6EwUCs-VUi1blg", Format3x4, cardtemplate.Classic, []string{originalVariant, "aqua-matsuri", "hikeshi-night", "dual-tone-shrine"}},
		{"UCy9mgxB8pn2C4aNK_MPthDQ", Format3x4, cardtemplate.Classic, []string{originalVariant, "postage-diary", "airmail-envelope", "royal-post-uniform"}},
		{kyokoChannel, Format16x9, cardtemplate.Classic, nil},
		{kyokoChannel, Format4x3, cardtemplate.Classic, nil},
		{kyokoChannel, Format3x4, cardtemplate.Orbit, nil},
		{"UC-unknown", Format3x4, cardtemplate.Classic, nil},
	}

	for _, tt := range tests {
		if got := Variants(tt.channel, tt.format, tt.template); !slices.Equal(got, tt.want) {
			t.Errorf("Variants(%q, %s, %s) = %q, want %q", tt.channel, tt.format, tt.template, got, tt.want)
		}
	}
}

func TestClassicThemeResolvesOnlyPooledVariants(t *testing.T) {
	for variant, want := range map[string]theme{
		"":               renderKyoko,
		originalVariant:  renderKyoko,
		"polished-light": renderKyokoPolishedLight,
		"polished-dark":  renderKyokoPolishedDark,
	} {
		got, err := classicTheme(Card{ChannelID: kyokoChannel, Variant: variant}, Format3x4)
		if err != nil || reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
			t.Errorf("classicTheme(%q) picked the wrong theme, err = %v", variant, err)
		}
	}

	for _, tc := range []struct {
		card   Card
		format Format
	}{
		{Card{ChannelID: kyokoChannel, Variant: "pastel-orbit"}, Format3x4},
		{Card{ChannelID: kyokoChannel, Variant: "polished-dark"}, Format16x9},
		{Card{ChannelID: kyokoChannel, Variant: "unknown"}, Format3x4},
		{Card{ChannelID: "UC-unknown", Variant: originalVariant}, Format3x4},
	} {
		if _, err := classicTheme(tc.card, tc.format); !errors.Is(err, ErrInvalidCard) {
			t.Errorf("classicTheme(%q, %q, %s) error = %v, want ErrInvalidCard", tc.card.ChannelID, tc.card.Variant, tc.format, err)
		}
	}
}

func TestRenderPagesRejectsUnservedVariant(t *testing.T) {
	card := weekCard(t, sampleWeek(t)...)

	card.ChannelID = kyokoChannel
	card.Variant = "polished-dark"

	if _, err := RenderPages(t.Context(), card, Format3x4, cardtemplate.Orbit); !errors.Is(err, ErrInvalidCard) {
		t.Errorf("orbit with variant: error = %v, want ErrInvalidCard", err)
	}

	card.Variant = "unknown"

	if _, err := RenderPages(t.Context(), card, Format3x4, cardtemplate.Classic); !errors.Is(err, ErrInvalidCard) {
		t.Errorf("unknown classic variant: error = %v, want ErrInvalidCard", err)
	}
}

const fitStep = 0.03

func testFonts(t *testing.T) *fontSet {
	t.Helper()

	fs, err := loadFontSet()
	if err != nil {
		t.Fatalf("loadFontSet() error = %v", err)
	}

	if err := renderMu.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { renderMu.Release(1) })

	return fs
}

func TestFitTableLongTitle(t *testing.T) {
	f := testFonts(t)
	days := make([]day, daysPerWeek)

	days[2].slots = []slot{{
		status: StatusPlanned, start: at21, minutes: 21 * 60,
		main: strings.Repeat("The Legend of Zelda Tears of the Kingdom ", 3),
		sub:  "(" + strings.Repeat("스포일러 주의 ", 10) + ")",
	}}

	fit, _ := fitTable(f, days, fallbackRow, tableSpec{innerW: 900, avail: 780, gap: 10, minRow: 78, pad: 13, kMax: 1.12})
	tb := fit.slots[2][0].tb

	if len(tb.main) > 2 || len(tb.sub) > 2 || !tb.cut {
		t.Fatalf("main=%d sub=%d cut=%v, want at most two clamped lines", len(tb.main), len(tb.sub), tb.cut)
	}

	for _, l := range tb.main {
		if w := f.width(tb.mainT, l); w > fit.titleW+0.5 {
			t.Errorf("main line %q width %.1f exceeds %.1f", l, w, fit.titleW)
		}
	}

	for _, l := range tb.sub {
		if w := f.width(tb.subT, l); w > fit.titleW+0.5 {
			t.Errorf("sub line %q width %.1f exceeds %.1f", l, w, fit.titleW)
		}
	}

	if fit.k < softMin-fitStep {
		t.Errorf("k = %.2f, a single long title must not shrink the table below %.2f", fit.k, softMin-fitStep)
	}
}

func TestFitTableBusyWeek(t *testing.T) {
	f := testFonts(t)
	counts := []int{5, 4, 2, 1, 2, 0, 3}
	days := make([]day, daysPerWeek)

	for i, n := range counts {
		for j := range n {
			days[i].slots = append(days[i].slots, slot{
				status: StatusPlanned, start: at21, minutes: 21 * 60,
				main: "콜라보 방송 제목 " + strings.Repeat("가", j*3), sub: "(쿄코 · 메라와 함께)",
			})
		}
	}

	fit, _ := fitTable(f, days, fallbackRow, tableSpec{innerW: 1050, avail: 1030, gap: 10, minRow: 78, pad: 13, kMax: 1.24})
	if fit.k < kMin-fitStep {
		t.Fatalf("k = %.2f, below floor %.2f", fit.k, kMin-fitStep)
	}

	for i, n := range counts {
		if len(fit.slots[i]) != n {
			t.Errorf("day %d slots = %d, want %d", i, len(fit.slots[i]), n)
		}

		if fit.heights[i] < fit.totals[i] {
			t.Errorf("day %d content %.1f overflows row %.1f", i, fit.totals[i], fit.heights[i])
		}
	}
}

func TestFitTableOverfullWeekCollapsesLines(t *testing.T) {
	f := testFonts(t)
	days := make([]day, daysPerWeek)

	for i := range days {
		for range maxSlotsPerDay {
			days[i].slots = append(days[i].slots, slot{status: StatusPlanned, start: at21, main: strings.Repeat("긴 제목 ", 8), sub: "(쿄코와 함께)"})
		}
	}

	fit, _ := fitTable(f, days, fallbackRow, tableSpec{innerW: 900, avail: 780, gap: 10, minRow: 78, pad: 13, kMax: 1.12})
	if fit.k < kMin-fitStep || fit.k > softMin {
		t.Fatalf("k = %.2f, want within [%.2f, %.2f]", fit.k, kMin-fitStep, softMin)
	}

	for i := range days {
		for _, sb := range fit.slots[i] {
			if len(sb.tb.main) != 1 || len(sb.tb.tags) != 0 {
				t.Fatalf("day %d: busy days must collapse to one title line without chips, got %d lines", i, len(sb.tb.main))
			}
		}
	}
}

func TestMelaTitleOneLineFallback(t *testing.T) {
	f := testFonts(t)
	s := slot{status: StatusPlanned, main: strings.Repeat("홀로라이브 리듬 게임 ", 6), sub: "(아소비와 함께 · 메라 채널에서)", tags: []string{memTag}}
	tb := melaTitle(f, s, 600, 50, 1)

	if len(tb.main) != 1 || tb.height() > 50 || f.width(tb.mainT, tb.main[0]) > 600 {
		t.Errorf("main=%q height=%.1f, want one clamped line within the capsule", tb.main, tb.height())
	}
}

func TestMelaTitleKeepsTitleOverChips(t *testing.T) {
	f := testFonts(t)
	s := slot{status: StatusPlanned, main: "The Legend of Zelda: Tears of the Kingdom 첫 플레이 스포일러 주의 엔딩까지 달리는 장시간 방송 제2탄 후반전", tags: []string{memTag}}

	tb := melaTitle(f, s, 682, 88, 1)
	if tb.cut || strings.Join(tb.main, " ") != s.main || tb.height() > 88 {
		t.Errorf("main=%q cut=%v height=%.1f, want the whole title in two lines", tb.main, tb.cut, tb.height())
	}
}

func TestWrapBreaksWhenTrackedWidthOverflows(t *testing.T) {
	f := testFonts(t)

	check := func(st txt, s string, maxW float64) {
		t.Helper()

		lines, _ := f.wrap(st, s, maxW, 2)
		for _, l := range lines {
			if w := f.width(st, l); w > maxW {
				t.Errorf("track %v %q: line %q width %v exceeds %v", st.Track, s, l, w, maxW)
			}
		}
	}

	check(txt{Size: 43.1, Face: faceXBold, Track: 0.4}, "하마ed!B사3하가글다글f자g3파바B타자?아자f한카아", 941.6380859374998)

	for _, track := range []float64{0.1, 0.2, 0.4, 0.8, 1.3, 3} {
		for _, s := range []string{"TheLegendOfZeldaTearsOfTheKingdom", "가나다라마바사아자차카타파하", "ホロライブ配信ホロライブ"} {
			st := txt{Size: 20, Face: faceXBold, Track: track}
			check(st, s, math.Nextafter(f.width(st, s), 0))
		}
	}
}

func TestGlyphCacheBounded(t *testing.T) {
	f := testFonts(t)

	for r := '가'; r < '가'+maxCachedGlyphs+100; r++ {
		f.glyph(faceBold, r)

		if len(f.cache) > maxCachedGlyphs {
			t.Fatalf("cache entries = %d, want at most %d", len(f.cache), maxCachedGlyphs)
		}
	}
}

func TestOffLineFitsAndKeepsReason(t *testing.T) {
	f := testFonts(t)
	mainT, subT := txt{Size: 34, Face: faceXBold, Track: 3}, txt{Size: 24, Face: faceBold}

	long := slot{status: StatusOff, main: strings.Repeat("이벤트 리허설과 수록이 겹쳐 쉬어 갑니다 ", 5), sub: "(수록 때문에)"}
	if l := layoutOff(f, long, mainT, subT, 600); l.width(f) > 600 {
		t.Errorf("long off line width %.1f exceeds 600", l.width(f))
	}

	short := slot{status: StatusOff, main: "휴방", sub: "(병원 진료)"}
	if l := layoutOff(f, short, mainT, subT, 600); l.sub != short.sub || l.width(f) > 600 {
		t.Errorf("off reason = %q, want %q", l.sub, short.sub)
	}

	days := make([]day, daysPerWeek)

	days[0].slots = []slot{long}

	fit, _ := fitTable(f, days, fallbackRow, tableSpec{innerW: 700, avail: 1000, gap: 10, minRow: 78, pad: 13, kMax: 1})
	if tb := fit.slots[0][0].tb; len(tb.main) != 1 || len(tb.sub) != 0 {
		t.Errorf("off day reserved main=%d sub=%d lines, want one line", len(tb.main), len(tb.sub))
	}
}

func busyDays(perDay int) []day {
	titles := []string{"PEAK", "Overwatch 2 랭크", "마인크래프트 건축 이어서", "잡담과 노래 조금", "The Legend of Zelda 첫 플레이"}
	days := make([]day, daysPerWeek)

	for i := range days {
		for j := range perDay {
			s := slot{status: StatusPlanned, start: fmt.Sprintf("%02d:00", 10+2*j), minutes: (10 + 2*j) * 60, main: titles[(i+j)%len(titles)]}
			if j%2 == 0 {
				s.sub = "(쿄코와 함께)"
			}

			if j == perDay-1 {
				s.status, s.start = StatusTentative, ""
			}

			days[i].slots = append(days[i].slots, s)
		}
	}

	return days
}

func changeItems(t *testing.T, perDay int) []Item {
	t.Helper()

	start := date(t, "2026-11-09")
	kinds := []Item{
		{Start: at22, PrevStart: "25:30", Status: StatusPlanned, Title: "Overwatch 2 랭크", With: []string{"쿄코"}},
		{Start: "23:00", PrevStart: at21, Status: StatusTentative, Title: "잡담과 노래 조금", Tags: []string{memTag}},
		{PrevStart: at2130, Status: StatusPlanned, Title: "마인크래프트 건축 이어서"},
		{Start: at20, MovedFrom: start.AddDate(0, 0, 4), Status: StatusPlanned, Title: "The Legend of Zelda 첫 플레이", Place: "메라 채널"},
		{Start: "19:00", Status: StatusCancelled, Title: "노래 방송"},
		{Status: StatusTentative, Title: "PEAK"},
	}
	items := make([]Item, 0, daysPerWeek*perDay)

	for i := range daysPerWeek {
		for j := range perDay {
			it := kinds[(i+j)%len(kinds)]

			it.Day = start.AddDate(0, 0, i)
			items = append(items, it)
		}
	}

	return items
}

func changeDays(t *testing.T, f *fontSet, perDay int) []day {
	t.Helper()

	v, err := prepare(weekCard(t, changeItems(t, perDay)...), f.drawable())
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	return v.days
}

func countShown(days []day) int {
	n := 0

	for _, d := range days {
		for _, s := range d.slots {
			n += max(1, s.more)
		}
	}

	return n
}

func checkTableFit(t *testing.T, f *fontSet, days []day, st rowStyle, sp tableSpec) {
	t.Helper()

	fit, folded := fitWeek(f, days, st, sp)
	used := sp.gap * float64(daysPerWeek-1)

	if fit.k < kMin-fitStep || fit.titleW <= 0 {
		t.Errorf("k = %.2f (floor %.2f), title width %.1f", fit.k, kMin-fitStep, fit.titleW)
	}

	for i := range folded {
		used += fit.heights[i]
		if fit.heights[i]+1e-6 < fit.totals[i]+2*sp.pad {
			t.Errorf("day %d content %.1f + padding overflows row %.1f", i, fit.totals[i], fit.heights[i])
		}
	}

	if used > sp.avail+1e-6 {
		t.Errorf("table height %.1f exceeds %.1f", used, sp.avail)
	}

	if n, want := countShown(folded), countShown(days); n != want {
		t.Errorf("shown + folded = %d, want %d", n, want)
	}
}

func checkKyokoFit(t *testing.T, f *fontSet, c *canvas, days []day) {
	t.Helper()

	g := kyokoGrid(c, kyokoBoard(c))

	plan := kyokoPlanFor(f, days, g, c.tall(), c.kMax())
	if plan.k < kMin-fitStep {
		t.Errorf("k = %.2f, below floor %.2f", plan.k, kMin-fitStep)
	}

	for ci, col := range plan.cells {
		used := kyokoCellGap * float64(len(col)-1)
		for _, cell := range col {
			used += cell.h

			for j := range cell.slots {
				checkKyokoTitle(t, f, plan, &cell.slots[j], cell.stack[j])
			}
		}

		if used > g.H+1e-6 {
			t.Errorf("column %d height %.1f exceeds %.1f", ci, used, g.H)
		}
	}
}

func checkKyokoTitle(t *testing.T, f *fontSet, plan kyokoPlan, sb *slotBox, stacked bool) {
	t.Helper()

	w := kyokoTitleW(f, plan.cellW, plan.k, sb.s, stacked)
	if w <= 0 {
		t.Errorf("title %q width %.1f, want positive", sb.s.main, w)
	}

	for _, l := range sb.tb.main {
		if lw := f.width(sb.tb.mainT, l); lw > w+0.5 {
			t.Errorf("title line %q width %.1f exceeds %.1f", l, lw, w)
		}
	}
}

func checkMelaFit(t *testing.T, f *fontSet, c *canvas, days []day) {
	t.Helper()

	_, tb := melaBoxes(c)
	rows, folded := fitFolded(days, func(d []day) (melaRows, bool) { return melaLines(f, c.kMax(), d, tb) })

	if rows.capH < melaMinCapH {
		t.Fatalf("capsule height %.1f below %.1f", rows.capH, melaMinCapH)
	}

	if n, want := countShown(folded), countShown(days); n != want {
		t.Errorf("shown + folded = %d, want %d", n, want)
	}

	for i, d := range folded {
		for j, s := range d.slots {
			h := rows.capH + rows.pitch*(rows.slotUnits[i][j]-1)
			b := box{tb.X + melaPlateW + 34, tb.Y, melaCapsuleW(tb), h}

			tw := b.right() - 66 - melaTitleX(f, b, rows.capH, s)
			if tw <= 0 {
				t.Fatalf("title %q width %.1f, want positive", s.main, tw)
			}

			title := melaTitle(f, s, tw, h-16, 1)
			if title.height() > h-16 && (len(title.main) != 1 || title.mainT.Size > h-7) {
				t.Errorf("title %q (%d lines, %.1fpx) does not fit capsule %.1f", s.main, len(title.main), title.mainT.Size, h)
			}
		}
	}
}

func TestMelaGrowsCapsuleForLongTitle(t *testing.T) {
	f := testFonts(t)
	long := Item{
		Day: date(t, "2026-11-10"), Start: "19:00", Status: StatusPlanned,
		Title: "홀로 도도부현 쿠키 배틀 아치치 메라 VS 스즈나 츠즈리 오프 콜라보",
		With:  []string{"이로하", "아즈키", "츠즈리"}, Place: "이로하 채널", Note: "정답을 맞히지 못하면 못 먹음",
	}
	card := weekCard(t,
		Item{Day: date(t, "2026-11-09"), Status: StatusOff, Title: "휴방"},
		long,
		planned(t, "2026-11-11", "07:00", "노자 English"),
		planned(t, "2026-11-11", at21, "음주 오프 콜라보"),
		planned(t, "2026-11-12", "19:00", "노래 방송"),
		planned(t, "2026-11-12", at22, "MIMESIS 4인 콜라보"),
		Item{Day: date(t, "2026-11-13"), Status: StatusOff, Title: "휴방"},
		planned(t, "2026-11-14", "13:00", "마인크래프트"),
		planned(t, "2026-11-15", "18:00", "오프 콜라보"),
	)

	v, err := prepare(card, f.drawable())
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	w, h, _ := Format3x4.size()
	c := &canvas{w: float64(w), h: float64(h), f: f}
	_, tb := melaBoxes(c)

	rows, days := fitFolded(v.days, func(d []day) (melaRows, bool) { return melaLines(f, c.kMax(), d, tb) })
	if len(days) != 7 || len(days[1].slots) != 1 {
		t.Fatalf("days = %d, want the whole week without folding", len(days))
	}

	s := days[1].slots[0]
	capH := rows.capH + rows.pitch*(rows.slotUnits[1][0]-1)
	b := box{tb.X + melaPlateW + 34, tb.Y, melaCapsuleW(tb), capH}
	title := melaTitle(f, s, b.right()-66-melaTitleX(f, b, rows.capH, s), capH-16, min(c.kMax(), max(1, rows.capH/84)))

	if title.cut || len(title.sub) == 0 || title.mainT.Size < 37 || title.height() > capH-16 {
		t.Fatalf("title = %d main + %d sub lines at %.1fpx in capsule %.1f, want the whole title and notes at base size", len(title.main), len(title.sub), title.mainT.Size, capH)
	}

	if rows.capH < melaMinCapH || rows.slotUnits[2][0] != 1 {
		t.Fatalf("other capsules = %.1f px, units %v; want the other rows to keep one unit", rows.capH, rows.slotUnits[2])
	}
}

func TestMelaLongTitleKeepsBusyWeekUnfolded(t *testing.T) {
	f := testFonts(t)

	for _, format := range []Format{Format16x9, Format4x3, Format3x4} {
		w, h, _ := format.size()
		c := &canvas{w: float64(w), h: float64(h), f: f}
		_, tb := melaBoxes(c)

		for perDay := 1; perDay <= maxSlotsPerDay; perDay++ {
			lines := func(days []day) []day {
				_, folded := fitFolded(days, func(d []day) (melaRows, bool) { return melaLines(f, c.kMax(), d, tb) })

				return folded
			}

			want := shownSlots(lines(busyDays(perDay)))

			for _, sub := range []string{"", "(이로하 · 아즈키 · 츠즈리와 함께 · 이로하 채널에서 · 정답을 맞히지 못하면 못 먹음)"} {
				long := busyDays(perDay)

				long[1].slots[0].main = "홀로 도도부현 쿠키 배틀 아치치 메라 VS 스즈나 츠즈리 오프 콜라보"
				long[1].slots[0].sub = sub

				if got := shownSlots(lines(long)); got != want {
					t.Errorf("%s/%d per day/sub %q: %d capsules shown with a long title, want %d", format, perDay, sub, got, want)
				}
			}
		}
	}
}

func shownSlots(days []day) int {
	n := 0

	for _, d := range days {
		n += len(d.slots)
	}

	return n
}

func TestThemeLayoutsFitBusyWeeks(t *testing.T) {
	f := testFonts(t)
	tables := map[string]struct {
		st   rowStyle
		spec func(*canvas) tableSpec
	}{
		"tsuzuri":  {tsuzuriRow, func(c *canvas) tableSpec { _, tb := tsuzuriBoxes(c); return tsuzuriSpec(c, tb) }},
		"sopia":    {sopiaRow, func(c *canvas) tableSpec { _, tb := sopiaBoxes(c); return sopiaSpec(c, tb) }},
		"fallback": {fallbackRow, func(c *canvas) tableSpec { return fallbackSpec(c, fallbackTableBox(c)) }},
	}

	for _, format := range []Format{Format16x9, Format4x3, Format3x4} {
		w, h, _ := format.size()
		c := &canvas{w: float64(w), h: float64(h), f: f}

		weeks := map[string][]day{}

		for _, perDay := range []int{1, 3, 4, maxSlotsPerDay} {
			weeks[fmt.Sprintf("busy%d", perDay)] = busyDays(perDay)
		}

		for _, perDay := range []int{4, 5, 6} {
			weeks[fmt.Sprintf("changes%d", perDay)] = changeDays(t, f, perDay)
		}

		for week, days := range weeks {
			for name, tt := range tables {
				t.Run(fmt.Sprintf("%s/%s/%s", format, name, week), func(t *testing.T) {
					checkTableFit(t, f, days, tt.st, tt.spec(c))
				})
			}

			t.Run(fmt.Sprintf("%s/kyoko/%s", format, week), func(t *testing.T) { checkKyokoFit(t, f, c, days) })
			t.Run(fmt.Sprintf("%s/mela/%s", format, week), func(t *testing.T) { checkMelaFit(t, f, c, days) })
		}
	}
}

func TestPlaceChipsInlineOrRow(t *testing.T) {
	f := testFonts(t)
	mainT, subT := txt{Size: 34, Face: faceXBold}, txt{Size: 24, Face: faceBold}
	s := slot{status: StatusPlanned, main: "그림 방송", tags: []string{memTag}}

	inline := layoutTitle(f, s, mainT, subT, titleLimits{maxW: 900, maxMain: 2, maxSub: 1, withTags: true})
	if inline.tagRow || inline.tagLH != 0 || len(inline.tags) != 1 {
		t.Errorf("short title: tagRow=%v tagLH=%.1f tags=%q, want inline chip", inline.tagRow, inline.tagLH, inline.tags)
	}

	s.main = strings.Repeat("가", 30)

	row := layoutTitle(f, s, mainT, subT, titleLimits{maxW: 500, maxMain: 2, maxSub: 1, withTags: true})
	if !row.tagRow || row.tagLH == 0 || len(row.tags) != 1 {
		t.Errorf("full line: tagRow=%v tagLH=%.1f tags=%q, want chip row", row.tagRow, row.tagLH, row.tags)
	}

	off := layoutTitle(f, s, mainT, subT, titleLimits{maxW: 500, maxMain: 1, maxSub: 1})
	if len(off.tags) != 0 {
		t.Errorf("withTags=false kept %q", off.tags)
	}
}

func TestBoldSymbolFallback(t *testing.T) {
	f := testFonts(t)

	for _, r := range "★→–·" {
		if got, want := f.glyph(faceHand, r), f.glyph(faceBold, r); got != want {
			t.Errorf("glyph(faceHand, %q) is not the Pretendard Bold glyph", r)
		}
	}

	if f.glyph(faceHand, '가') == f.glyph(faceBold, '가') {
		t.Error("Hangul must keep the theme face")
	}
}

var themeChannels = []string{
	"UCy9mgxB8pn2C4aNK_MPthDQ",
	"UCSjQDxud2HkAO2DVD3lwxmw",
	"UCROQtXcp2loQEmvpe5rhJzQ",
	"UC8eitCE9Z6EwUCs-VUi1blg",
	"UC-fallback",
}

func sampleWeek(t *testing.T) []Item {
	t.Helper()

	return []Item{
		planned(t, "2026-11-09", "12:00", "PEAK"),
		{Day: date(t, "2026-11-09"), Start: at21, Status: StatusPlanned, Title: "Overwatch", With: []string{"츠즈리"}, Tags: []string{"EN", "콜라보"}},
		{Day: date(t, "2026-11-10"), Status: StatusOff, Title: "휴방"},
		{Day: date(t, "2026-11-11"), Status: StatusTentative, Title: "콜라보 방송"},
		{Day: date(t, "2026-11-12"), Start: at21, Status: StatusTentative, Title: "잡담", Note: "휴방 가능성 높음"},
		{Day: date(t, "2026-11-13"), Start: at20, Status: StatusCancelled, Title: "노래 방송"},
		planned(t, "2026-11-14", "19:00", "일치할 때까지 못 끝내!"),
	}
}

func TestRenderConcurrent(t *testing.T) {
	card := weekCard(t, sampleWeek(t)...)

	card.ChannelID = themeChannels[1]

	want, err := Render(card, Format16x9)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	for i := range 3 {
		wg.Go(func() {
			c := card

			if i%2 == 1 {
				c.ChannelID = themeChannels[(i+2)%len(themeChannels)]
			}

			got, err := Render(c, Format16x9)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err != nil:
				errs = append(errs, err)
			case i%2 == 0 && !bytes.Equal(got, want):
				errs = append(errs, errors.New("concurrent render differs from sequential render"))
			}
		})
	}

	wg.Wait()

	for _, err := range errs {
		t.Error(err)
	}
}
