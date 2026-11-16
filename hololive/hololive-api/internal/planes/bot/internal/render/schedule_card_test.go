package render

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	late23 = "23:00"
	late25 = "25:00"
)

type recordingCardDrawer struct {
	cards   []schedulecard.Card
	formats []schedulecard.Format
	err     error
}

func (d *recordingCardDrawer) draw(card schedulecard.Card, format schedulecard.Format) ([]byte, error) {
	d.cards = append(d.cards, card)
	d.formats = append(d.formats, format)

	if d.err != nil {
		return nil, d.err
	}

	return slices.Concat(pngSignature, []byte(card.ExtractedAt.Format(time.RFC3339Nano))), nil
}

func scheduleCardMember() *domain.Member {
	return &domain.Member{ChannelID: "UCSjQDxud2HkAO2DVD3lwxmw", Name: "Hakuto Kyoko", NameKo: "햐쿠토 쿄코", ShortKoreanName: "쿄코"}
}

func novemberDay(d int) time.Time {
	return time.Date(2026, time.November, d, 0, 0, 0, 0, time.UTC)
}

func scheduleCardSchedule(posterID int64) scheduleposter.PublishedSchedule {
	return scheduleposter.PublishedSchedule{
		PosterID:    posterID,
		WeekStart:   novemberDay(9),
		WeekEnd:     novemberDay(15),
		ExtractedAt: time.Date(2026, time.November, 9, 3, 4, 5, 0, time.UTC),
		Rows: []scheduleposter.Row{
			{Ordinal: 1, Day: novemberDay(9), Start: "21:00", End: late23, TZ: "JST", Status: scheduleposter.RowPlanned, TitleKo: "잡담", WithKo: []string{"미코"}, PlaceKo: "스튜디오", TagsKo: []string{"멤버십 한정"}, NoteKo: "길어질 수 있음"},
			{Ordinal: 2, Day: novemberDay(10), Start: late25, Status: scheduleposter.RowTentative, TitleKo: "게임"},
			{Ordinal: 3, Day: novemberDay(11), Status: scheduleposter.RowOff, TitleKo: "휴방"},
			{Ordinal: 4, Day: novemberDay(12), Start: "20:00", Status: scheduleposter.RowCancelled, TitleKo: "노래"},
			{Ordinal: 5, Day: novemberDay(20), Start: "19:00", Status: scheduleposter.RowPlanned, TitleKo: "다음 주 방송"},
		},
	}
}

func TestScheduleCardRendererCopiesRowsIntoCard(t *testing.T) {
	drawer := &recordingCardDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)
	schedule := scheduleCardSchedule(1)

	data, err := renderer.RenderScheduleCard(t.Context(), scheduleCardMember(), schedule)
	if err != nil || !isPNGData(data) {
		t.Fatalf("RenderScheduleCard() = %q, %v; want PNG", data, err)
	}

	if len(drawer.cards) != 1 || drawer.formats[0] != schedulecard.Format3x4 {
		t.Fatalf("draw calls = %d formats = %v, want one 3:4 draw", len(drawer.cards), drawer.formats)
	}

	card := drawer.cards[0]
	if card.ChannelID != "UCSjQDxud2HkAO2DVD3lwxmw" || card.MemberName != "쿄코" || card.Photo != nil ||
		!card.WeekStart.Equal(schedule.WeekStart) || !card.WeekEnd.Equal(schedule.WeekEnd) || !card.ExtractedAt.Equal(schedule.ExtractedAt) {
		t.Fatalf("card header = %+v, want schedule fields, display name and no photo", card)
	}

	want := []schedulecard.Item{
		{Day: novemberDay(9), Start: "21:00", End: late23, Status: schedulecard.StatusPlanned, Title: "잡담", With: []string{"미코"}, Place: "스튜디오", Tags: []string{"멤버십 한정"}, Note: "길어질 수 있음"},
		{Day: novemberDay(10), Start: late25, Status: schedulecard.StatusTentative, Title: "게임"},
		{Day: novemberDay(11), Status: schedulecard.StatusOff, Title: "휴방"},
		{Day: novemberDay(12), Start: "20:00", Status: schedulecard.StatusCancelled, Title: "노래"},
		{Day: novemberDay(20), Start: "19:00", Status: schedulecard.StatusPlanned, Title: "다음 주 방송"},
	}

	if !slices.EqualFunc(card.Items, want, scheduleItemEqual) {
		t.Fatalf("card items = %+v, want %+v", card.Items, want)
	}
}

func scheduleItemEqual(a, b schedulecard.Item) bool {
	return a.Day.Equal(b.Day) && a.Start == b.Start && a.End == b.End && a.Status == b.Status && a.Title == b.Title &&
		slices.Equal(a.With, b.With) && a.Place == b.Place && slices.Equal(a.Tags, b.Tags) && a.Note == b.Note &&
		a.PrevStart == b.PrevStart && a.MovedFrom.Equal(b.MovedFrom) && a.Converted == b.Converted
}

func TestScheduleCardMovesTheWindowWithTheConvertedPoster(t *testing.T) {
	schedule := scheduleposter.PublishedSchedule{
		PosterID:     9,
		WeekStart:    novemberDay(9),
		WeekEnd:      novemberDay(15),
		ExtractedAt:  novemberDay(9),
		RowsDayShift: 1,
		Rows: []scheduleposter.Row{
			{Ordinal: 1, Day: novemberDay(10), Start: "12:00", TZ: "JST", SourceZone: "PT", Status: scheduleposter.RowPlanned, TitleKo: "잡담"},
			{Ordinal: 2, Day: novemberDay(16), Start: "11:00", TZ: "JST", SourceZone: "PT", Status: scheduleposter.RowPlanned, TitleKo: "노래"},
			{Ordinal: 3, Day: novemberDay(12), Start: "22:30", TZ: "JST", Status: scheduleposter.RowPlanned, TitleKo: "콜라보"},
		},
	}

	card := newScheduleCard(scheduleCardMember(), schedule, nil)
	if !card.WeekStart.Equal(novemberDay(10)) || !card.WeekEnd.Equal(novemberDay(16)) {
		t.Fatalf("card week = %s..%s, want the poster week moved by one day", card.WeekStart, card.WeekEnd)
	}

	want := []schedulecard.Item{
		{Day: novemberDay(10), Start: "12:00", Status: schedulecard.StatusPlanned, Title: "잡담", Converted: true},
		{Day: novemberDay(16), Start: "11:00", Status: schedulecard.StatusPlanned, Title: "노래", Converted: true},
		{Day: novemberDay(12), Start: "22:30", Status: schedulecard.StatusPlanned, Title: "콜라보"},
	}

	if !slices.EqualFunc(card.Items, want, scheduleItemEqual) {
		t.Fatalf("card items = %+v, want %+v", card.Items, want)
	}
}

func recordedChange(change scheduleposter.Change, recorded time.Time) scheduleposter.Change {
	change.PostedAt, change.RecordedAt = recorded.Add(-time.Minute), recorded

	return change
}

func changedSchedule() scheduleposter.PublishedSchedule {
	at := func(d, h int) time.Time { return time.Date(2026, time.November, d, h, 0, 0, 0, time.UTC) }

	return scheduleposter.PublishedSchedule{
		PosterID:    7,
		WeekStart:   novemberDay(9),
		WeekEnd:     novemberDay(15),
		ExtractedAt: at(9, 3),
		Rows: []scheduleposter.Row{
			{Ordinal: 1, Day: novemberDay(9), Start: "20:30", End: late23, Status: scheduleposter.RowPlanned, TitleKo: "잡담"},
			{Ordinal: 2, Day: novemberDay(10), Start: "18:30", Status: scheduleposter.RowPlanned, TitleKo: "게임"},
			{Ordinal: 3, Day: novemberDay(11), Start: "19:00", Status: scheduleposter.RowPlanned, TitleKo: "노래"},
			{Ordinal: 4, Day: novemberDay(13), Start: "18:00", Status: scheduleposter.RowPlanned, TitleKo: "잡담"},
			{Ordinal: 5, Day: novemberDay(13), Start: late25, Status: scheduleposter.RowPlanned, TitleKo: "심야"},
			{Ordinal: 6, Day: novemberDay(14), Start: "22:00", Status: scheduleposter.RowPlanned, TitleKo: "ASMR"},
		},
		Changes: []scheduleposter.Change{
			recordedChange(scheduleposter.Change{PostID: 104, Item: 1, Kind: scheduleposter.ChangeAdded, Day: novemberDay(15), Start: new("17:00"), Status: scheduleposter.RowPlanned, TitleKo: "콜라보"}, at(12, 7)),
			recordedChange(scheduleposter.Change{PostID: 103, Item: 2, Kind: scheduleposter.ChangeMovedOut, Slot: 6, Day: novemberDay(20)}, at(11, 8)),
			recordedChange(scheduleposter.Change{PostID: 103, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 5, Day: novemberDay(14), Start: new("02:00")}, at(11, 8)),
			recordedChange(scheduleposter.Change{PostID: 102, Item: 2, Kind: scheduleposter.ChangeContentChanged, Slot: 4, TitleKo: "마인크래프트"}, at(10, 9)),
			recordedChange(scheduleposter.Change{PostID: 102, Item: 1, Kind: scheduleposter.ChangeCancelled, Slot: 3, Status: scheduleposter.RowCancelled}, at(10, 9)),
			recordedChange(scheduleposter.Change{PostID: 101, Item: 2, Kind: scheduleposter.ChangeMoved, Slot: 2, Day: novemberDay(12), Start: new("19:30"), Status: scheduleposter.RowPlanned}, at(9, 10)),
			recordedChange(scheduleposter.Change{PostID: 101, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("21:30")}, at(9, 10)),
		},
	}
}

func TestScheduleCardShowsChangedSlots(t *testing.T) {
	drawer := &recordingCardDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)
	schedule := changedSchedule()

	if _, err := renderer.RenderScheduleCard(t.Context(), scheduleCardMember(), schedule); err != nil {
		t.Fatalf("RenderScheduleCard() error = %v", err)
	}

	card := drawer.cards[0]
	if want := time.Date(2026, time.November, 12, 7, 0, 0, 0, time.UTC); !card.ExtractedAt.Equal(want) {
		t.Fatalf("card ExtractedAt = %v, want the latest change record %v", card.ExtractedAt, want)
	}

	want := []schedulecard.Item{
		{Day: novemberDay(9), Start: "21:30", PrevStart: "20:30", Status: schedulecard.StatusPlanned, Title: "잡담"},
		{Day: novemberDay(12), Start: "19:30", MovedFrom: novemberDay(10), Status: schedulecard.StatusPlanned, Title: "게임"},
		{Day: novemberDay(11), Start: "19:00", Status: schedulecard.StatusCancelled, Title: "노래"},
		{Day: novemberDay(13), Start: "18:00", Status: schedulecard.StatusPlanned, Title: "마인크래프트"},
		{Day: novemberDay(14), Start: "02:00", MovedFrom: novemberDay(13), Status: schedulecard.StatusPlanned, Title: "심야"},
		{Day: novemberDay(14), Start: "22:00", Status: schedulecard.StatusCancelled, Title: "ASMR"},
		{Day: novemberDay(15), Start: "17:00", Status: schedulecard.StatusPlanned, Title: "콜라보"},
	}

	if !slices.EqualFunc(card.Items, want, scheduleItemEqual) {
		t.Fatalf("card items = %+v, want %+v", card.Items, want)
	}
}

func TestScheduleCardMarksTimeChangesByCardDay(t *testing.T) {
	recorded := time.Date(2026, time.November, 13, 9, 0, 0, 0, time.UTC)
	schedule := scheduleposter.PublishedSchedule{
		PosterID:    8,
		WeekStart:   novemberDay(9),
		WeekEnd:     novemberDay(15),
		ExtractedAt: novemberDay(9),
		Rows: []scheduleposter.Row{
			{Ordinal: 1, Day: novemberDay(13), Start: late23, Status: scheduleposter.RowPlanned, TitleKo: "늦춘 방송"},
			{Ordinal: 2, Day: novemberDay(13), Start: late25, Status: scheduleposter.RowPlanned, TitleKo: "미정 방송"},
		},
		Changes: []scheduleposter.Change{
			recordedChange(scheduleposter.Change{PostID: 201, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new(late25)}, recorded),
			recordedChange(scheduleposter.Change{PostID: 201, Item: 2, Kind: scheduleposter.ChangeTentative, Slot: 2, Start: new(""), Status: scheduleposter.RowTentative}, recorded),
		},
	}

	card := newScheduleCard(scheduleCardMember(), schedule, nil)
	want := []schedulecard.Item{
		{Day: novemberDay(13), Start: late25, PrevStart: late23, Status: schedulecard.StatusPlanned, Title: "늦춘 방송"},
		{Day: novemberDay(13), PrevStart: late25, Status: schedulecard.StatusTentative, Title: "미정 방송"},
	}

	if !slices.EqualFunc(card.Items, want, scheduleItemEqual) {
		t.Fatalf("card items = %+v, want %+v", card.Items, want)
	}
}

func TestScheduleCardRendererRedrawsAfterRevokedChange(t *testing.T) {
	drawer := &recordingCardDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)
	schedule := changedSchedule()

	if _, err := renderer.RenderScheduleCard(t.Context(), scheduleCardMember(), schedule); err != nil {
		t.Fatalf("RenderScheduleCard() error = %v", err)
	}

	schedule.Changes = slices.DeleteFunc(schedule.Changes, func(c scheduleposter.Change) bool { return c.PostID == 102 })

	if _, err := renderer.RenderScheduleCard(t.Context(), scheduleCardMember(), schedule); err != nil || len(drawer.cards) != 2 {
		t.Fatalf("after revoking an earlier post: %v, draws = %d; want a new draw", err, len(drawer.cards))
	}

	if drawer.cards[1].Items[2].Status != schedulecard.StatusPlanned {
		t.Fatalf("revoked cancellation still drawn: %+v", drawer.cards[1].Items[2])
	}
}

func TestScheduleCardRendererRedrawsAfterPolish(t *testing.T) {
	drawer := &recordingCardDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)
	schedule := changedSchedule()
	polishedAt := schedule.UpdatedAt().Add(time.Hour)

	render := func(wantDraws int, what string) {
		t.Helper()

		if _, err := renderer.RenderScheduleCard(t.Context(), scheduleCardMember(), schedule); err != nil || len(drawer.cards) != wantDraws {
			t.Fatalf("%s: RenderScheduleCard() = %v, draws = %d; want %d", what, err, len(drawer.cards), wantDraws)
		}
	}

	render(1, "before polish")

	schedule.Rows[0].TitleKo, schedule.Rows[0].EditedAt = "윤문한 잡담", polishedAt

	render(2, "polished row")

	schedule.Changes[0].TitleKo, schedule.Changes[0].EditedAt = "윤문한 콜라보", polishedAt.Add(time.Minute)

	render(3, "polished change")
	render(3, "same polish")

	if got := drawer.cards[1].Items[0].Title; got != "윤문한 잡담" {
		t.Fatalf("polished row title = %q", got)
	}
}

func TestScheduleCardRendererDoesNotShareInFlightDrawAcrossChanges(t *testing.T) {
	base, revoked, later, rematched, reextracted, confirmed := changedSchedule(), changedSchedule(), changedSchedule(), changedSchedule(), changedSchedule(), changedSchedule()
	newTitle := scheduleposter.Change{PostID: 105, Item: 1, Kind: scheduleposter.ChangeContentChanged, Slot: 1, TitleKo: "새 제목"}

	confirmed.Changes = append(confirmed.Changes, recordedChange(scheduleposter.Change{PostID: 106, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1, Day: novemberDay(9), Start: new("20:30"), Status: scheduleposter.RowPlanned}, base.ExtractedAt.Add(time.Hour)))

	revoked.Changes = slices.DeleteFunc(revoked.Changes, func(c scheduleposter.Change) bool { return c.PostID == 102 })
	later.Changes = append(later.Changes, recordedChange(newTitle, base.UpdatedAt().Add(time.Hour)))
	rematched.Changes[0] = recordedChange(newTitle, base.UpdatedAt().Add(time.Hour))
	reextracted.ExtractedAt = base.ExtractedAt.Add(time.Minute)
	reextracted.Rows[0].TitleKo = "다시 뽑은 제목"

	for name, next := range map[string]scheduleposter.PublishedSchedule{
		"later change": later, "revoked earlier change": revoked, "same count, later record": rematched, "re-extracted rows": reextracted,
		"earlier same change": confirmed,
	} {
		synctest.Test(t, func(t *testing.T) {
			release := make(chan struct{})
			draws := 0
			renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, func(card schedulecard.Card, _ schedulecard.Format) ([]byte, error) {
				draws++

				<-release

				return slices.Concat(pngSignature, []byte(card.Items[0].Title), []byte(card.Items[2].Status)), nil
			})
			member := scheduleCardMember()
			firstDone, nextDone := make(chan error, 1), make(chan []byte, 1)

			go func() {
				_, err := renderer.RenderScheduleCard(t.Context(), member, base)
				firstDone <- err
			}()

			synctest.Wait()

			go func() {
				data, err := renderer.RenderScheduleCard(t.Context(), member, next)
				if err != nil {
					t.Errorf("%s: next RenderScheduleCard() error = %v", name, err)
				}

				nextDone <- data
			}()

			synctest.Wait()

			startedDraws := draws

			close(release)

			if err := <-firstDone; err != nil {
				t.Fatalf("%s: first RenderScheduleCard() error = %v", name, err)
			}

			got := <-nextDone
			want := newScheduleCard(member, next, nil)

			if startedDraws != 2 || !slices.Equal(got, slices.Concat(pngSignature, []byte(want.Items[0].Title), []byte(want.Items[2].Status))) {
				t.Fatalf("%s: draws in flight = %d, next card = %q; want its own draw of its own changes", name, startedDraws, got)
			}
		})
	}
}

func TestScheduleCardRendererRedrawsAfterAppliedChange(t *testing.T) {
	drawer := &recordingCardDrawer{}
	member := scheduleCardMember()
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)
	schedule := scheduleCardSchedule(1)
	recorded := schedule.ExtractedAt.Add(time.Hour)

	render := func(wantDraws int, what string) {
		t.Helper()

		if _, err := renderer.RenderScheduleCard(t.Context(), member, schedule); err != nil || len(drawer.cards) != wantDraws {
			t.Fatalf("%s: RenderScheduleCard() = %v, draws = %d; want %d", what, err, len(drawer.cards), wantDraws)
		}
	}

	render(1, "no changes")

	schedule.Changes = []scheduleposter.Change{
		recordedChange(scheduleposter.Change{PostID: 101, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("21:30")}, recorded),
	}

	render(2, "first applied change")

	schedule.Changes = append(schedule.Changes,
		recordedChange(scheduleposter.Change{PostID: 102, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1, Day: novemberDay(9), Start: new("21:30"), Status: scheduleposter.RowPlanned}, recorded.Add(time.Hour)),
	)

	render(3, "later same")

	if got := drawer.cards[2].ExtractedAt; !got.Equal(recorded) {
		t.Fatalf("card updated at = %v after a same change, want the applied change record %v", got, recorded)
	}

	schedule.Changes = append(schedule.Changes,
		recordedChange(scheduleposter.Change{PostID: 103, Item: 1, Kind: scheduleposter.ChangeOff, Slot: 2, Start: new(""), Status: scheduleposter.RowOff}, recorded.Add(2*time.Hour)),
	)

	render(4, "later applied change")
	render(4, "cached later change")
}

func TestScheduleCardRendererRedrawsAfterRevokedSameChange(t *testing.T) {
	drawer := &recordingCardDrawer{}
	member := scheduleCardMember()
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)
	schedule := scheduleCardSchedule(1)
	earlierPost := recordedChange(scheduleposter.Change{PostID: 101, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00")}, schedule.ExtractedAt.Add(2*time.Hour))

	earlierPost.PostedAt = schedule.ExtractedAt.Add(time.Minute)

	laterPost := recordedChange(scheduleposter.Change{PostID: 102, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1, Day: novemberDay(9), Start: new("21:00"), Status: scheduleposter.RowPlanned}, schedule.ExtractedAt.Add(time.Hour))

	schedule.Changes = []scheduleposter.Change{earlierPost, laterPost}

	if _, err := renderer.RenderScheduleCard(t.Context(), member, schedule); err != nil || drawer.cards[0].Items[0].Start != "21:00" {
		t.Fatalf("RenderScheduleCard() error = %v, start = %q; want the later post's 21:00", err, drawer.cards[0].Items[0].Start)
	}

	schedule.Changes = schedule.Changes[:1]

	if _, err := renderer.RenderScheduleCard(t.Context(), member, schedule); err != nil || len(drawer.cards) != 2 {
		t.Fatalf("after revoking the same change: %v, draws = %d; want a new draw", err, len(drawer.cards))
	}

	if got := drawer.cards[1].Items[0].Start; got != "22:00" {
		t.Fatalf("start after revoking the same change = %q, want 22:00", got)
	}
}

func TestScheduleCardRendererRedrawsWhileLaterPostIsRequeued(t *testing.T) {
	drawer := &recordingCardDrawer{}
	member := scheduleCardMember()
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)
	schedule := scheduleCardSchedule(1)
	at := func(h int) time.Time { return schedule.ExtractedAt.Add(time.Duration(h) * time.Hour) }

	earlier := recordedChange(scheduleposter.Change{PostID: 101, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new(late23)}, at(2))

	earlier.PostedAt = at(0).Add(time.Minute)

	laterFirst := recordedChange(scheduleposter.Change{PostID: 102, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new(late23)}, at(1))
	laterAgain := recordedChange(scheduleposter.Change{PostID: 102, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1, Day: novemberDay(9), Start: new(late23), Status: scheduleposter.RowPlanned}, at(3))

	steps := []struct {
		name    string
		changes []scheduleposter.Change
		start   string
		updated time.Time
	}{
		{"no changes", nil, "21:00", at(0)},
		{"later post matched first", []scheduleposter.Change{laterFirst}, late23, at(1)},
		{"earlier post requeues the later one", []scheduleposter.Change{earlier}, late23, at(2)},
		{"later post matched again as same", []scheduleposter.Change{earlier, laterAgain}, late23, at(2)},
		{"earlier post revoked", []scheduleposter.Change{laterAgain}, late23, at(0)},
	}

	for i, step := range steps {
		schedule.Changes = step.changes

		if _, err := renderer.RenderScheduleCard(t.Context(), member, schedule); err != nil || len(drawer.cards) != i+1 {
			t.Fatalf("%s: RenderScheduleCard() = %v, draws = %d; want %d", step.name, err, len(drawer.cards), i+1)
		}

		if card := drawer.cards[i]; card.Items[0].Start != step.start || !card.ExtractedAt.Equal(step.updated) {
			t.Fatalf("%s: start = %q, updated at = %v; want %q, %v", step.name, card.Items[0].Start, card.ExtractedAt, step.start, step.updated)
		}
	}
}

func TestScheduleCardRendererDrawsEachKeyOnce(t *testing.T) {
	drawer := &recordingCardDrawer{}
	member := scheduleCardMember()
	schedule := scheduleCardSchedule(1)
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)

	first, err := renderer.RenderScheduleCard(t.Context(), member, schedule)
	if err != nil {
		t.Fatalf("first RenderScheduleCard() error = %v", err)
	}

	second, err := renderer.RenderScheduleCard(t.Context(), member, schedule)
	if err != nil || !slices.Equal(first, second) || len(drawer.cards) != 1 {
		t.Fatalf("second render = %v, draws = %d; want cached bytes and one draw", err, len(drawer.cards))
	}

	schedule.ExtractedAt = schedule.ExtractedAt.Add(time.Minute)

	if _, err := renderer.RenderScheduleCard(t.Context(), member, schedule); err != nil || len(drawer.cards) != 2 {
		t.Fatalf("re-extracted render = %v, draws = %d; want a new draw", err, len(drawer.cards))
	}

	other := newScheduleCardRenderer(schedulecard.Format16x9, 4, drawer.draw)

	other.cache = renderer.cache

	if _, err := other.RenderScheduleCard(t.Context(), member, schedule); err != nil || len(drawer.cards) != 3 || drawer.formats[2] != schedulecard.Format16x9 {
		t.Fatalf("other format render = %v, draws = %d; want a new 16:9 draw", err, len(drawer.cards))
	}
}

func TestScheduleCardRendererEvictsOldestBeyondLimit(t *testing.T) {
	drawer := &recordingCardDrawer{}
	member := scheduleCardMember()
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 2, drawer.draw)

	for _, posterID := range []int64{1, 2, 3, 3, 2, 1} {
		if _, err := renderer.RenderScheduleCard(t.Context(), member, scheduleCardSchedule(posterID)); err != nil {
			t.Fatalf("RenderScheduleCard(%d) error = %v", posterID, err)
		}
	}

	if len(drawer.cards) != 4 {
		t.Fatalf("draws = %d, want 4 (poster 1 evicted by poster 3)", len(drawer.cards))
	}
}

func TestScheduleCardRendererDoesNotCacheFailures(t *testing.T) {
	drawer := &recordingCardDrawer{err: schedulecard.ErrInvalidCard}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)

	for range 2 {
		if _, err := renderer.RenderScheduleCard(t.Context(), scheduleCardMember(), scheduleCardSchedule(1)); !errors.Is(err, schedulecard.ErrInvalidCard) {
			t.Fatalf("RenderScheduleCard() error = %v, want ErrInvalidCard", err)
		}
	}

	if len(drawer.cards) != 2 {
		t.Fatalf("draws = %d, want a new draw after a failure", len(drawer.cards))
	}
}

func TestScheduleCardRendererUsesCalendarPhotoLoader(t *testing.T) {
	pngData := tinyPNG(t)
	member := scheduleCardMember()

	member.Photo = "https://yt3.googleusercontent.com/kyoko=s88-c"

	withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return calendarPhotoTestResponse(req, calendarPhotoContentTypePNG, pngData), nil
	})))

	drawer := &recordingCardDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)

	if _, err := renderer.RenderScheduleCard(t.Context(), member, scheduleCardSchedule(1)); err != nil {
		t.Fatalf("RenderScheduleCard() error = %v", err)
	}

	if drawer.cards[0].Photo == nil {
		t.Fatal("card photo is nil, want the downloaded profile photo")
	}
}

func TestScheduleCardRendererDrawsWithoutRejectedPhoto(t *testing.T) {
	member := scheduleCardMember()

	member.Photo = "https://yt3.googleusercontent.com/kyoko=s88-c"

	withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return calendarPhotoTestResponse(req, "text/html", []byte("<html>")), nil
	})))

	drawer := &recordingCardDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)

	if _, err := renderer.RenderScheduleCard(t.Context(), member, scheduleCardSchedule(1)); err != nil {
		t.Fatalf("RenderScheduleCard() error = %v", err)
	}

	if len(drawer.cards) != 1 || drawer.cards[0].Photo != nil {
		t.Fatalf("draws = %d, want one draw without photo", len(drawer.cards))
	}
}

func TestScheduleCardRendererRedrawsAfterTransientPhotoFailure(t *testing.T) {
	pngData := tinyPNG(t)
	member := scheduleCardMember()

	member.Photo = "https://yt3.googleusercontent.com/kyoko=s88-c"

	var calls atomic.Int32

	withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, syscall.ECONNRESET
		}

		return calendarPhotoTestResponse(req, calendarPhotoContentTypePNG, pngData), nil
	})))

	drawer := &recordingCardDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)

	for range 3 {
		if _, err := renderer.RenderScheduleCard(t.Context(), member, scheduleCardSchedule(1)); err != nil {
			t.Fatalf("RenderScheduleCard() error = %v", err)
		}
	}

	if len(drawer.cards) != 2 || drawer.cards[0].Photo != nil || drawer.cards[1].Photo == nil {
		t.Fatalf("draws = %d, want a photo-less draw, then one cached redraw with the photo", len(drawer.cards))
	}
}

func TestScheduleCardRendererDoesNotCacheAfterPhotoBudget(t *testing.T) {
	member := scheduleCardMember()

	member.Photo = "https://yt3.googleusercontent.com/kyoko=s88-c"

	drawer := &recordingCardDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, drawer.draw)

	synctest.Test(t, func(t *testing.T) {
		client := newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()

			return nil, req.Context().Err()
		}))

		client.Timeout = 0

		withCalendarPhotoClient(t, client)

		for range 2 {
			if _, err := renderer.RenderScheduleCard(t.Context(), member, scheduleCardSchedule(1)); err != nil {
				t.Fatalf("RenderScheduleCard() error = %v", err)
			}
		}
	})

	if len(drawer.cards) != 2 {
		t.Fatalf("draws = %d, want a new draw after the photo budget expired", len(drawer.cards))
	}
}

func TestScheduleCardRendererSharesOneDrawAcrossWaiters(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})

	var draws atomic.Int32

	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, func(card schedulecard.Card, _ schedulecard.Format) ([]byte, error) {
		if draws.Add(1) == 1 {
			close(started)
		}

		<-release

		return slices.Concat(pngSignature, []byte(card.MemberName)), nil
	})
	member := scheduleCardMember()

	firstCtx, cancelFirst := context.WithCancel(t.Context())
	firstErr := make(chan error, 1)

	go func() {
		_, err := renderer.RenderScheduleCard(firstCtx, member, scheduleCardSchedule(1))
		firstErr <- err
	}()

	<-started

	const waiters = 8

	results := make(chan []byte, waiters)

	var wg sync.WaitGroup

	for range waiters {
		wg.Go(func() {
			data, err := renderer.RenderScheduleCard(t.Context(), member, scheduleCardSchedule(1))
			if err != nil {
				t.Errorf("waiter RenderScheduleCard() error = %v", err)
			}

			results <- data
		})
	}

	cancelFirst()

	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("first RenderScheduleCard() error = %v, want context.Canceled", err)
	}

	close(release)
	wg.Wait()
	close(results)

	for data := range results {
		if !isPNGData(data) {
			t.Fatalf("waiter data = %q, want the shared PNG", data)
		}
	}

	if _, err := renderer.RenderScheduleCard(t.Context(), member, scheduleCardSchedule(1)); err != nil || draws.Load() != 1 {
		t.Fatalf("after shared render = %v, draws = %d; want one cached draw", err, draws.Load())
	}
}

func TestScheduleCardRendererReturnsDrawPanicAsError(t *testing.T) {
	drawer := &recordingCardDrawer{}
	panicked := false
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, func(card schedulecard.Card, format schedulecard.Format) ([]byte, error) {
		if !panicked {
			panicked = true

			panic("draw exploded")
		}

		return drawer.draw(card, format)
	})

	if _, err := renderer.RenderScheduleCard(t.Context(), scheduleCardMember(), scheduleCardSchedule(1)); err == nil {
		t.Fatal("RenderScheduleCard() error = nil, want the recovered draw panic")
	}

	if data, err := renderer.RenderScheduleCard(t.Context(), scheduleCardMember(), scheduleCardSchedule(1)); err != nil || !isPNGData(data) || len(drawer.cards) != 1 {
		t.Fatalf("after panic = %q, %v, draws = %d; want a fresh draw", data, err, len(drawer.cards))
	}
}
