package schedulecard

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
)

func orbitFeaturedWeek(t *testing.T, five bool) Card {
	t.Helper()

	card := weekCard(t)

	card.ChannelID = "UCROQtXcp2loQEmvpe5rhJzQ"

	for i, title := range []string{"이과 여자 공감 콜라보", "홀로도리 방송", "콜라보 방송", "술 마시며 이야기 나누기", "", "일치할 때까지 못 끝내!!", "멤버십 한정 방송"} {
		if title == "" {
			continue
		}

		it := Item{Day: card.WeekStart.AddDate(0, 0, i), Status: StatusPlanned, Title: title, Start: at20}
		switch i {
		case 0:
			it.Place = "코요리 선배 채널"
		case 3:
			it.Place = "라덴 채널"
		case 5:
			it.With, it.Place = []string{"아소비"}, "메라 채널"
		case 6:
			it.Tags = []string{memTag}
		}

		card.Items = append(card.Items, it)
	}

	day := card.WeekStart.AddDate(0, 0, 4)
	rocket := Item{Day: day, Status: StatusPlanned, Start: "09:00", Title: "H3 로켓 10호기 · MMX 발사 같이 보기"}
	pico := Item{Day: day, Status: StatusPlanned, Start: "16:00", Title: "PICO PARK 4인 협동", With: []string{"쿄코", "메라", "츠즈리"}}
	race := Item{Day: day, Status: StatusPlanned, Start: at22, Title: "홀로1 GP · 이 레이스엔 필승법이 있다!"}

	if five {
		rocket.Start, race.Start = "04:00", "20:01"
		card.Items = append(card.Items,
			Item{Day: day, Status: StatusPlanned, Start: "11:30", Title: "폐점 사건 · 과학 앞에선 유령도 무력하다"},
			Item{Day: day, Status: StatusPlanned, Start: "27:00", Title: "일본 록 노래방"})
	}

	card.Items = append(card.Items, rocket, pico, race)

	return card
}

func orbitTestPages(t *testing.T, card Card) []orbitPage {
	t.Helper()

	f := testFonts(t)

	v, err := prepareWeek(card, f.drawable(), cardtemplate.Orbit)
	if err != nil {
		t.Fatalf("prepareWeek: %v", err)
	}

	pages := layoutOrbit(orbitPieces(f, v.days))
	for i, p := range pages {
		y := orbitTop

		for _, g := range p.groups {
			if g.y < y-.001 || g.y+g.h > orbitBottom+.001 {
				t.Fatalf("page %d, %v: group %.1f..%.1f outside %.1f..%.1f", i+1, g.date, g.y, g.y+g.h, y, orbitBottom)
			}

			y = g.y
			for _, r := range g.rows {
				if r.y < y-.001 || r.y+r.h > g.y+g.h+.001 {
					t.Fatalf("page %d, %v: row %.1f..%.1f outside %.1f..%.1f", i+1, g.date, r.y, r.y+r.h, y, g.y+g.h)
				}

				checkOrbitTextWidth(t, f, &r)

				y = r.y + r.h
			}

			y = g.y + g.h
		}
	}

	return pages
}

func checkOrbitTextWidth(t *testing.T, f *fontSet, r *orbitRow) {
	t.Helper()

	for j, line := range r.title {
		limit := orbitRight - orbitTitleX

		if j == 0 && r.label != "" {
			limit -= max(151, f.width(orbitClockText, r.label)) + 24
		}

		if w := f.width(orbitTitleText, line); w > limit+.001 {
			t.Fatalf("title %q width %.1f exceeds %.1f with clock %q", line, w, limit, r.label)
		}
	}

	for _, line := range r.detail {
		if w := f.width(orbitDetailText, line); w > orbitRight-orbitTitleX {
			t.Fatalf("detail %q exceeds available width: %.1f", line, w)
		}
	}
}

func orbitGroupsInOrder(pages []orbitPage) []orbitGroup {
	var groups []orbitGroup

	for _, page := range pages {
		groups = append(groups, page.groups...)
	}

	return groups
}

func TestOrbitFeaturedDaysStayTogether(t *testing.T) {
	for _, tc := range []struct{ rows, pages int }{{3, 1}, {5, 2}} {
		t.Run(fmt.Sprint(tc.rows), func(t *testing.T) {
			card := orbitFeaturedWeek(t, tc.rows == 5)
			pages := orbitTestPages(t, card)

			if len(pages) != tc.pages {
				t.Fatalf("got %d pages, want %d", len(pages), tc.pages)
			}

			var days int

			for _, p := range pages {
				for _, g := range p.groups {
					want := card.WeekStart.AddDate(0, 0, days)
					if !g.date.Equal(want) || g.start != 0 || len(g.rows) != g.total {
						t.Fatalf("group %v %d+%d/%d, want whole %v", g.date, g.start, len(g.rows), g.total, want)
					}

					if days == 4 && g.total != tc.rows {
						t.Fatalf("featured date has %d rows, want %d", g.total, tc.rows)
					}

					days++
				}
			}

			if days != daysPerWeek {
				t.Fatalf("got %d dates, want %d", days, daysPerWeek)
			}
		})
	}
}

func TestOrbitBusyDateUsesRemainingSpace(t *testing.T) {
	for _, count := range []int{18, MaxItems} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			card := weekCard(t)
			day := card.WeekStart.AddDate(0, 0, 4)

			for i := range count {
				card.Items = append(card.Items, Item{Day: day, Status: StatusPlanned, Start: at20, Title: fmt.Sprintf("릴레이 %d", i)})
			}

			pages := orbitTestPages(t, card)
			if count == 18 && len(pages) != 2 {
				t.Fatalf("18 broadcasts with four preceding and two following dates used %d pages, want 2", len(pages))
			}

			checkOrbitRelayOrder(t, card, orbitGroupsInOrder(pages))
		})
	}
}

func checkOrbitRelayOrder(t *testing.T, card Card, groups []orbitGroup) {
	t.Helper()

	seen, empty := 0, 0

	for _, g := range groups {
		if !g.date.Equal(card.Items[0].Day) {
			empty++

			continue
		}

		if g.start != seen || g.total != len(card.Items) {
			t.Fatalf("range starts at %d/%d, want %d/%d", g.start, g.total, seen, len(card.Items))
		}

		for _, row := range g.rows {
			if !slices.Equal(row.title, []string{card.Items[seen].Title}) || row.label != at20 {
				t.Fatalf("broadcast %d = %v at %q", seen, row.title, row.label)
			}

			seen++
		}
	}

	if seen != len(card.Items) || empty != 6 {
		t.Fatalf("got %d broadcasts and %d empty dates, want %d and 6", seen, empty, len(card.Items))
	}
}

func TestOrbitLongestBroadcastContinuesWithoutLosingText(t *testing.T) {
	card := weekCard(t)
	name := strings.Repeat("참", maxWithRunes)
	it := Item{
		Day: card.WeekStart.AddDate(0, 0, -1), Start: "29:59", PrevStart: "23:59", Converted: true, Status: StatusTentative,
		Title: strings.Repeat("역", 499) + "중간" + strings.Repeat("끝", 499), With: []string{name, name, name, name, name, name},
		Place: strings.Repeat("터", maxPlaceRunes), Note: strings.Repeat("말", maxNoteRunes),
		Tags: []string{strings.Repeat("별", maxTagRunes), strings.Repeat("달", maxTagRunes), strings.Repeat("해", maxTagRunes)},
	}

	card.Items = []Item{
		{Day: card.WeekStart, Status: StatusPlanned, Start: at22, Title: "밤 방송"},
		it,
		{Day: card.WeekStart, Status: StatusCancelled, Start: "04:00", Title: "새벽 방송"},
	}

	classic, err := prepare(card, everyRune)
	if err != nil {
		t.Fatalf("prepare(classic): %v", err)
	}

	if got, want := classic.days[0].slots[1].main, strings.Repeat("역", 119)+"…"; got != want {
		t.Fatalf("classic title = %q, want %q", got, want)
	}

	rows := orbitRowsFromSplitDay(t, card)
	if len(rows) < 4 {
		t.Fatalf("expected a continued broadcast between two others, got %d rows", len(rows))
	}

	if first := rows[0]; first.ordinal != 0 || first.label != "04:00" || first.kind != timeCancelled || !slices.Equal(first.title, []string{"새벽 방송"}) {
		t.Fatalf("first broadcast: %+v", first)
	}

	if last := rows[len(rows)-1]; last.ordinal != 2 || last.label != at22 || !slices.Equal(last.title, []string{"밤 방송"}) {
		t.Fatalf("last broadcast: %+v", last)
	}

	checkOrbitContinuation(t, rows[1:len(rows)-1], it)
}

func checkOrbitContinuation(t *testing.T, rows []orbitRow, it Item) {
	t.Helper()

	var titles, details []string

	for i, row := range rows {
		if row.ordinal != 1 || row.label != "23:59 → 일 29:59 ?" || row.kind != timeChanged || row.status != StatusTentative {
			t.Fatalf("lost broadcast identity, date, time change or tentative status: %+v", row)
		}

		if i != 0 {
			if len(row.title) == 0 || row.title[0] != orbitContinued {
				t.Fatalf("unmarked continuation title: %q", row.title)
			}

			row.title = row.title[1:]
		}

		titles = append(titles, row.title...)
		details = append(details, row.detail...)
	}

	if strings.Join(titles, "") != it.Title {
		t.Fatalf("title across %d pages: %q", len(rows), titles)
	}

	want := strings.Join(it.With, "·") + "과함께·" + it.Place + "에서·" + it.Note + "·" + strings.Join(it.Tags, "·")
	if got := strings.ReplaceAll(strings.Join(details, ""), " ", ""); got != want {
		t.Fatalf("continued details = %q, want %q", got, want)
	}
}

func orbitRowsFromSplitDay(t *testing.T, card Card) []orbitRow {
	t.Helper()

	var rows []orbitRow

	for _, g := range orbitGroupsInOrder(orbitTestPages(t, card)) {
		if !g.date.Equal(card.WeekStart) {
			continue
		}

		if !g.mixed || g.total != len(card.Items) || len(g.rows) == 0 || g.start != g.rows[0].ordinal {
			t.Fatalf("continued group range = %d+%d/%d, mixed %v", g.start, len(g.rows), g.total, g.mixed)
		}

		rows = append(rows, g.rows...)
	}

	return rows
}

func TestOrbitPosterAndPostStatuses(t *testing.T) {
	card := weekCard(t)
	day := card.WeekStart

	card.Items = []Item{
		{Day: day, Status: StatusPlanned, Title: "방송", Start: at21},
		{Day: day, Status: StatusCancelled, Title: "취소한 방송", Start: at20},
		{Day: day, Status: StatusOff, Title: "휴방"},
		{Day: day.AddDate(0, 0, 1), Status: StatusOff},
		{Day: day.AddDate(0, 0, 1), Status: StatusPlanned, FromPost: true, Title: "추가 방송"},
	}

	pages := orbitTestPages(t, card)
	first, second := pages[0].groups[0], pages[0].groups[1]

	if first.total != 3 || first.rows[0].status != StatusOff || first.rows[0].kind != timeNone || first.rows[1].kind != timeCancelled || first.rows[2].label != at21 {
		t.Fatalf("poster rows lost order or status: %+v", first.rows)
	}

	if second.total != 1 || !slices.Equal(second.rows[0].title, []string{"추가 방송"}) || second.rows[0].label != "미정" {
		t.Fatalf("post did not replace off day: %+v", second.rows)
	}
}

func TestOrbitWrapPreservesClausesAndWords(t *testing.T) {
	f := testFonts(t)

	for _, tc := range []struct{ left, right string }{
		{"H3 로켓 10호기 ·", "MMX 발사 같이 보기"},
		{"Summer", "Festival"},
	} {
		text := tc.left + " " + tc.right
		first := f.width(orbitTitleText, tc.left) + 1
		next := f.width(orbitTitleText, tc.right) + 1

		if got := orbitWrap(f, orbitTitleText, text, first, next); !slices.Equal(got, []string{tc.left, tc.right}) {
			t.Errorf("wrap(%q) = %q, want [%q %q]", text, got, tc.left, tc.right)
		}
	}
}

func TestOrbitRejectsOtherFormats(t *testing.T) {
	for _, format := range []Format{Format16x9, Format4x3, ""} {
		if pages, err := RenderPages(t.Context(), weekCard(t), format, cardtemplate.Orbit); !errors.Is(err, ErrInvalidCard) || len(pages) != 0 {
			t.Fatalf("RenderPages(%q, orbit) returned %d pages, err %v", format, len(pages), err)
		}
	}
}

func TestOrbitUsesPublishedDaysAndCompleteDetails(t *testing.T) {
	card := weekCard(t)

	card.WeekEnd = card.WeekStart

	place, note := strings.Repeat("터", 170)+"완", strings.Repeat("말", 280)+"끝"

	card.Items = []Item{{Day: card.WeekStart, Status: StatusPlanned, Title: "일정", Place: place, Note: note}}

	var detail strings.Builder

	for _, g := range orbitGroupsInOrder(orbitTestPages(t, card)) {
		if !g.date.Equal(card.WeekStart) {
			t.Fatalf("extra day %s", g.date)
		}

		for _, r := range g.rows {
			for _, line := range r.detail {
				detail.WriteString(line)
			}
		}
	}

	if !strings.Contains(detail.String(), place) || !strings.Contains(detail.String(), note) {
		t.Fatalf("missing full place or note: %s", detail.String())
	}
}
