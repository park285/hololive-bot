package scheduleposter

import (
	jsonv2 "encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const (
	judgePostID = 7
	judgeBody   = "EV 本文"
	judgePosted = "2026-11-13 16:00"
)

func at(text string) time.Time {
	parsed, err := time.ParseInLocation("2006-01-02 15:04", text, jst)
	if err != nil {
		panic(err)
	}

	return parsed
}

func judgeRows() []posterstore.Row {
	return []posterstore.Row{
		{Ordinal: 1, Day: utcDate("2026-11-13"), Start: "19:00", Status: posterstore.RowPlanned, SourceText: "A", TitleKo: "에이"},
		{Ordinal: 2, Day: utcDate("2026-11-13"), Start: "20:00", Status: posterstore.RowPlanned, SourceText: "B", TitleKo: "비"},
		{Ordinal: 3, Day: utcDate("2026-11-14"), Start: "21:00", Status: posterstore.RowTentative, SourceText: "C", TitleKo: "씨"},
		{Ordinal: 4, Day: utcDate("2026-11-14"), Status: posterstore.RowOff, SourceText: "お休み", TitleKo: "휴방"},
		{Ordinal: 5, Day: utcDate("2026-11-15"), Start: "12:00", Status: posterstore.RowCancelled, SourceText: "D", TitleKo: "디"},
		{Ordinal: 6, Day: utcDate("2026-11-13"), Start: "25:00", Status: posterstore.RowPlanned, SourceText: "E", TitleKo: "이"},
	}
}

func judge(posted string, rows []posterstore.Row, prior []posterstore.Change, items ...matchItem) []posterstore.Change {
	post := posterstore.Post{ID: judgePostID, PostedAt: at(posted), Body: judgeBody}

	return judgeItems(post, utcDate("2026-11-09"), utcDate("2026-11-15"), rows, prior, items)
}

func item(row *int, date string, start *string, status posterstore.RowStatus, body string) matchItem {
	return matchItem{Row: row, Date: date, Start: start, Status: status, Text: body, Evidence: "EV"}
}

func inZone(it matchItem, zone string) matchItem {
	it.TZ = &zone

	return it
}

func citing(it matchItem, evidence string) matchItem {
	it.Evidence = evidence

	return it
}

func change(n int, kind posterstore.ChangeKind, edit func(*posterstore.Change)) posterstore.Change {
	c := posterstore.Change{PostID: judgePostID, Item: n, PostedAt: at(judgePosted), Kind: kind}

	if edit != nil {
		edit(&c)
	}

	return c
}

func rejected(n, slot int, reason string) posterstore.Change {
	return change(n, posterstore.ChangeRejected, func(c *posterstore.Change) { c.Reason, c.Slot = reason, slot })
}

func sameAs(slot int, day string, start *string, status posterstore.RowStatus) posterstore.Change {
	return change(1, posterstore.ChangeSame, func(c *posterstore.Change) { c.Slot, c.Day, c.Start, c.Status = slot, utcDate(day), start, status })
}

func timeChanged(n, slot int, start string) posterstore.Change {
	return change(n, posterstore.ChangeTimeChanged, func(c *posterstore.Change) { c.Slot, c.Start = slot, new(start) })
}

type singleItemCase struct {
	item matchItem
	want posterstore.Change
}

var itemValidationCases = map[string]singleItemCase{
	"date is not YYYY-MM-DD":       {item(new(1), "2026-11-3", nil, posterstore.RowPlanned, ""), rejected(1, 0, rejectFormat)},
	"hour above 29":                {item(new(1), "2026-11-13", new("30:00"), posterstore.RowPlanned, ""), rejected(1, 0, rejectFormat)},
	"empty start":                  {item(new(1), "2026-11-13", new(""), posterstore.RowPlanned, ""), rejected(1, 0, rejectFormat)},
	"unknown status":               {item(new(1), "2026-11-13", nil, "live", ""), rejected(1, 0, rejectFormat)},
	"text over 500 characters":     {item(new(1), "2026-11-13", nil, posterstore.RowPlanned, strings.Repeat("あ", 501)), rejected(1, 0, rejectFormat)},
	"folded text over 500":         {item(new(1), "2026-11-13", nil, posterstore.RowPlanned, strings.Repeat("あ\n", 200)), rejected(1, 0, rejectFormat)},
	"tz over 40 characters":        {inZone(item(new(1), "2026-11-13", nil, posterstore.RowPlanned, ""), strings.Repeat("J", 41)), rejected(1, 0, rejectFormat)},
	"NUL":                          {citing(item(new(1), "2026-11-13", nil, posterstore.RowPlanned, ""), "EV\x00"), rejected(1, 0, rejectFormat)},
	"invalid UTF-8":                {item(new(1), "2026-11-13", nil, posterstore.RowPlanned, "\xff"), rejected(1, 0, rejectFormat)},
	"time zone other than JST":     {inZone(item(new(1), "2026-11-13", new("19:30"), posterstore.RowPlanned, ""), "PDT"), rejected(1, 0, rejectForeignZone)},
	"empty evidence":               {citing(item(new(1), "2026-11-13", nil, posterstore.RowPlanned, ""), " "), rejected(1, 0, rejectEvidence)},
	"evidence not in the post":     {citing(item(new(1), "2026-11-13", nil, posterstore.RowPlanned, ""), "EV 別文"), rejected(1, 0, rejectEvidence)},
	"update marker in text":        {item(new(1), "2026-11-13", nil, posterstore.RowPlanned, "見て [Update: start 19:00 -> 20:00]"), rejected(1, 0, rejectTextMarker)},
	"day off marker in text":       {item(new(1), "2026-11-13", nil, posterstore.RowPlanned, "x [DAY OFF]"), rejected(1, 0, rejectTextMarker)},
	"marker borrowing the bracket": {item(new(1), "2026-11-13", nil, posterstore.RowPlanned, "22時から] [day off"), rejected(1, 0, rejectTextMarker)},
	"row zero":                     {item(new(0), "2026-11-13", nil, posterstore.RowPlanned, ""), rejected(1, 0, rejectNoSuchRow)},
	"row past the schedule":        {item(new(7), "2026-11-13", nil, posterstore.RowPlanned, ""), rejected(1, 0, rejectNoSuchRow)},
	"call-off without row":         {item(nil, "2026-11-13", nil, posterstore.RowCancelled, "A"), rejected(1, 0, rejectNoSuchRow)},
	"day off on a busy day":        {item(nil, "2026-11-13", nil, posterstore.RowOff, "休み"), rejected(1, 0, rejectNoSuchRow)},
	"date before the post":         {item(new(1), "2026-11-12", nil, posterstore.RowPlanned, ""), rejected(1, 1, rejectDateRange)},
	"call-off after the week":      {item(new(1), "2026-11-16", nil, posterstore.RowCancelled, ""), rejected(1, 1, rejectDateRange)},
	"new stream after the week":    {item(nil, "2026-11-16", new("20:00"), posterstore.RowPlanned, "X"), rejected(1, 0, rejectDateRange)},
	"time already passed":          {item(new(1), "2026-11-13", new("15:30"), posterstore.RowPlanned, ""), rejected(1, 1, rejectPastTime)},
	"new stream already passed":    {item(nil, "2026-11-13", new("12:00"), posterstore.RowPlanned, "X"), rejected(1, 0, rejectPastTime)},
	"new stream without text":      {item(nil, "2026-11-14", new("18:00"), posterstore.RowPlanned, " \n "), rejected(1, 0, rejectNoContent)},
	"day off on an empty day":      {item(nil, "2026-11-15", nil, posterstore.RowOff, ""), rejected(1, 0, rejectNoContent)},
	"lower case JST":               {inZone(item(new(1), "2026-11-13", new("19:30"), posterstore.RowPlanned, ""), "jst"), timeChanged(1, 1, "19:30")},
	"evidence after whitespace":    {citing(item(new(1), "2026-11-13", new("19:30"), posterstore.RowPlanned, ""), " EV\n\t本文 "), timeChanged(1, 1, "19:30")},
	"new stream at a row's time":   {item(nil, "2026-11-13", new("20:00"), posterstore.RowPlanned, ""), sameAs(2, "2026-11-13", new("20:00"), posterstore.RowPlanned)},
	"evidence without the post's spaces": {
		citing(item(new(1), "2026-11-13", new("19:30"), posterstore.RowPlanned, ""), "EV本文"), timeChanged(1, 1, "19:30"),
	},
	"time equal to the post time": {item(new(1), "2026-11-13", new("16:00"), posterstore.RowPlanned, ""), timeChanged(1, 1, "16:00")},
	"day off for another day":     {item(new(1), "2026-11-14", nil, posterstore.RowOff, ""), rejected(1, 1, rejectRowDate)},
	"call-off for another day":    {item(new(2), "2026-11-14", new("20:00"), posterstore.RowCancelled, ""), rejected(1, 2, rejectRowDate)},
	"call-off of a 25:00 row by its next-day date": {
		item(new(6), "2026-11-14", nil, posterstore.RowCancelled, ""),
		change(1, posterstore.ChangeCancelled, func(c *posterstore.Change) { c.Slot, c.Status = 6, posterstore.RowCancelled }),
	},
	"next-day date of a 25:00 row without a time": {
		item(new(6), "2026-11-14", nil, posterstore.RowPlanned, "内容変更"),
		change(1, posterstore.ChangeContentChanged, func(c *posterstore.Change) { c.Slot, c.Text = 6, "内容変更" }),
	},
	"next-day clock of a 25:00 row": {
		item(nil, "2026-11-14", new("01:00"), posterstore.RowPlanned, "E2"),
		change(1, posterstore.ChangeContentChanged, func(c *posterstore.Change) { c.Slot, c.Text = 6, "E2" }),
	},
	"multi-line text is folded": {
		item(new(1), "2026-11-13", nil, posterstore.RowPlanned, "一\n二\r\n三\tx\u2028四"),
		change(1, posterstore.ChangeContentChanged, func(c *posterstore.Change) { c.Slot, c.Text = 1, "一 / 二 / 三 x / 四" }),
	},
}

func TestJudgeItemsValidatesEachItem(t *testing.T) {
	t.Parallel()

	for name, tc := range itemValidationCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := judge(judgePosted, judgeRows(), nil, tc.item)
			require.Equal(t, []posterstore.Change{tc.want}, got)
			require.NoError(t, posterstore.ValidateChanges(got))
		})
	}
}

var slotComparisonCases = map[string]singleItemCase{
	"J1 new stream": {item(nil, "2026-11-14", new("18:00"), posterstore.RowPlanned, "ゲリラ"), change(1, posterstore.ChangeAdded, func(c *posterstore.Change) {
		c.Day, c.Start, c.Status, c.Text = utcDate("2026-11-14"), new("18:00"), posterstore.RowPlanned, "ゲリラ"
	})},
	"J1 new stream without a time": {item(nil, "2026-11-14", nil, posterstore.RowTentative, "雑談"), change(1, posterstore.ChangeAdded, func(c *posterstore.Change) {
		c.Day, c.Start, c.Status, c.Text = utcDate("2026-11-14"), new(""), posterstore.RowTentative, "雑談"
	})},
	"J1 25:00 after midnight": {item(nil, "2026-11-13", new("25:30"), posterstore.RowPlanned, "ASMR"), change(1, posterstore.ChangeAdded, func(c *posterstore.Change) {
		c.Day, c.Start, c.Status, c.Text = utcDate("2026-11-13"), new("25:30"), posterstore.RowPlanned, "ASMR"
	})},
	"J2 day off": {item(new(1), "2026-11-13", nil, posterstore.RowOff, ""), change(1, posterstore.ChangeOff, func(c *posterstore.Change) {
		c.Slot, c.Start, c.Status = 1, new(""), posterstore.RowOff
	})},
	"J2 already off": {item(new(4), "2026-11-14", nil, posterstore.RowOff, ""), change(1, posterstore.ChangeSame, func(c *posterstore.Change) {
		c.Slot, c.Start, c.Status = 4, new(""), posterstore.RowOff
	})},
	"J3 called off": {item(new(1), "2026-11-13", nil, posterstore.RowCancelled, "体調不良"), change(1, posterstore.ChangeCancelled, func(c *posterstore.Change) {
		c.Slot, c.Status, c.Text = 1, posterstore.RowCancelled, "体調不良"
	})},
	"J3 already called off": {item(new(5), "2026-11-15", nil, posterstore.RowCancelled, ""), change(1, posterstore.ChangeSame, func(c *posterstore.Change) {
		c.Slot, c.Status = 5, posterstore.RowCancelled
	})},
	"J3 call-off of a stream that already started": {
		item(new(1), "2026-11-13", new("15:00"), posterstore.RowCancelled, ""),
		change(1, posterstore.ChangeCancelled, func(c *posterstore.Change) { c.Slot, c.Status = 1, posterstore.RowCancelled }),
	},
	"J4 moved out of the week": {item(new(1), "2026-11-17", new("20:00"), posterstore.RowPlanned, ""), change(1, posterstore.ChangeMovedOut, func(c *posterstore.Change) {
		c.Slot, c.Day, c.Start, c.Status = 1, utcDate("2026-11-17"), new("20:00"), posterstore.RowPlanned
	})},
	"J4 moved out without a time": {item(new(1), "2026-11-17", nil, posterstore.RowTentative, ""), change(1, posterstore.ChangeMovedOut, func(c *posterstore.Change) {
		c.Slot, c.Day, c.Status = 1, utcDate("2026-11-17"), posterstore.RowTentative
	})},
	"J5 moved": {item(new(1), "2026-11-14", new("22:00"), posterstore.RowPlanned, ""), change(1, posterstore.ChangeMoved, func(c *posterstore.Change) {
		c.Slot, c.Day, c.Start, c.Status = 1, utcDate("2026-11-14"), new("22:00"), posterstore.RowPlanned
	})},
	"J5 moved without a time": {item(new(1), "2026-11-14", nil, posterstore.RowTentative, ""), change(1, posterstore.ChangeMoved, func(c *posterstore.Change) {
		c.Slot, c.Day, c.Start, c.Status = 1, utcDate("2026-11-14"), new(""), posterstore.RowTentative
	})},
	"J6 stream on a day off": {item(new(4), "2026-11-14", new("18:30"), posterstore.RowPlanned, "ゲリラ雑談"), change(1, posterstore.ChangeAdded, func(c *posterstore.Change) {
		c.Slot, c.Start, c.Status, c.Text = 4, new("18:30"), posterstore.RowPlanned, "ゲリラ雑談"
	})},
	"J6 stream on a day off without a time": {item(new(4), "2026-11-14", nil, posterstore.RowPlanned, ""), change(1, posterstore.ChangeAdded, func(c *posterstore.Change) {
		c.Slot, c.Status = 4, posterstore.RowPlanned
	})},
	"J6 called-off stream is back": {item(new(5), "2026-11-15", nil, posterstore.RowPlanned, ""), change(1, posterstore.ChangeConfirmed, func(c *posterstore.Change) {
		c.Slot, c.Status = 5, posterstore.RowPlanned
	})},
	"J7 undecided clears the time": {item(new(1), "2026-11-13", nil, posterstore.RowTentative, ""), change(1, posterstore.ChangeTentative, func(c *posterstore.Change) {
		c.Slot, c.Start, c.Status = 1, new(""), posterstore.RowTentative
	})},
	"J7 approximate time": {item(new(1), "2026-11-13", new("19:30"), posterstore.RowTentative, ""), change(1, posterstore.ChangeTentative, func(c *posterstore.Change) {
		c.Slot, c.Start, c.Status = 1, new("19:30"), posterstore.RowTentative
	})},
	"J8 confirmed": {item(new(3), "2026-11-14", new("21:00"), posterstore.RowPlanned, ""), change(1, posterstore.ChangeConfirmed, func(c *posterstore.Change) {
		c.Slot, c.Start, c.Status = 3, new("21:00"), posterstore.RowPlanned
	})},
	"J8 confirmed without a time": {item(new(3), "2026-11-14", nil, posterstore.RowPlanned, ""), change(1, posterstore.ChangeConfirmed, func(c *posterstore.Change) {
		c.Slot, c.Status = 3, posterstore.RowPlanned
	})},
	"J9 time changed": {item(new(1), "2026-11-13", new("19:30"), posterstore.RowPlanned, ""), timeChanged(1, 1, "19:30")},
	"J9 next-day clock of a 25:00 row": {item(new(6), "2026-11-14", new("02:00"), posterstore.RowPlanned, ""), change(1, posterstore.ChangeTimeChanged, func(c *posterstore.Change) {
		c.Slot, c.Day, c.Start = 6, utcDate("2026-11-14"), new("02:00")
	})},
	"J9 same moment as a 25:00 row": {item(new(6), "2026-11-14", new("01:00"), posterstore.RowPlanned, ""), sameAs(6, "2026-11-13", new("25:00"), posterstore.RowPlanned)},
	"J10 content changed": {item(new(1), "2026-11-13", new("19:00"), posterstore.RowPlanned, "with X"), change(1, posterstore.ChangeContentChanged, func(c *posterstore.Change) {
		c.Slot, c.Text = 1, "with X"
	})},
	"J11 same":                  {item(new(1), "2026-11-13", new("19:00"), posterstore.RowPlanned, ""), sameAs(1, "2026-11-13", new("19:00"), posterstore.RowPlanned)},
	"J11 same without a time":   {item(new(3), "2026-11-14", nil, posterstore.RowTentative, ""), sameAs(3, "2026-11-14", nil, posterstore.RowTentative)},
	"time collision":            {item(new(1), "2026-11-13", new("20:00"), posterstore.RowPlanned, ""), rejected(1, 1, rejectTimeCollision)},
	"collision with 25:00 slot": {item(new(1), "2026-11-14", new("01:00"), posterstore.RowPlanned, ""), rejected(1, 1, rejectTimeCollision)},
}

func TestJudgeItemsComparesWithTheTargetSlot(t *testing.T) {
	t.Parallel()

	for name, tc := range slotComparisonCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := judge(judgePosted, judgeRows(), nil, tc.item)
			require.Equal(t, []posterstore.Change{tc.want}, got)
			require.NoError(t, posterstore.ValidateChanges(got))
		})
	}
}

func TestJudgeItemsJudgesThePostAsAWhole(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		items []matchItem
		want  []posterstore.Change
	}{
		"chained delays are both applied": {
			[]matchItem{
				item(new(1), "2026-11-13", new("20:00"), posterstore.RowPlanned, ""),
				item(new(2), "2026-11-13", new("21:00"), posterstore.RowPlanned, ""),
			},
			[]posterstore.Change{timeChanged(1, 1, "20:00"), timeChanged(2, 2, "21:00")},
		},
		"swapped streams are both applied": {
			[]matchItem{
				item(new(1), "2026-11-13", new("20:00"), posterstore.RowPlanned, ""),
				item(new(2), "2026-11-13", new("19:00"), posterstore.RowPlanned, ""),
			},
			[]posterstore.Change{timeChanged(1, 1, "20:00"), timeChanged(2, 2, "19:00")},
		},
		"a rejected collision is taken out before judging again": {
			[]matchItem{
				item(new(1), "2026-11-13", new("20:00"), posterstore.RowPlanned, ""),
				item(new(2), "2026-11-13", new("21:00"), posterstore.RowPlanned, ""),
				item(new(6), "2026-11-13", new("21:00"), posterstore.RowPlanned, ""),
			},
			[]posterstore.Change{rejected(1, 1, rejectTimeCollision), rejected(2, 2, rejectTimeCollision), rejected(3, 6, rejectTimeCollision)},
		},
		"a called-off slot does not collide": {
			[]matchItem{
				item(new(1), "2026-11-13", new("20:00"), posterstore.RowPlanned, ""),
				item(new(2), "2026-11-13", nil, posterstore.RowCancelled, ""),
			},
			[]posterstore.Change{
				timeChanged(1, 1, "20:00"),
				change(2, posterstore.ChangeCancelled, func(c *posterstore.Change) { c.Slot, c.Status = 2, posterstore.RowCancelled }),
			},
		},
		"duplicate row keeps the other items": {
			[]matchItem{
				item(new(1), "2026-11-13", new("19:30"), posterstore.RowPlanned, ""),
				item(new(2), "2026-11-13", new("21:30"), posterstore.RowPlanned, ""),
				item(nil, "2026-11-13", new("19:00"), posterstore.RowCancelled, ""),
				item(new(1), "2026-11-13", nil, posterstore.RowCancelled, ""),
			},
			[]posterstore.Change{
				rejected(1, 1, rejectDuplicateRow), timeChanged(2, 2, "21:30"), rejected(3, 0, rejectNoSuchRow), rejected(4, 1, rejectDuplicateRow),
			},
		},
		"a new stream at a row's time counts toward duplicates": {
			[]matchItem{
				item(nil, "2026-11-13", new("20:00"), posterstore.RowPlanned, "B2"),
				item(new(2), "2026-11-13", new("20:30"), posterstore.RowPlanned, ""),
			},
			[]posterstore.Change{rejected(1, 2, rejectDuplicateRow), rejected(2, 2, rejectDuplicateRow)},
		},
		"no items": {nil, []posterstore.Change{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := judge(judgePosted, judgeRows(), nil, tc.items...)
			require.Equal(t, tc.want, got)
			require.NoError(t, posterstore.ValidateChanges(got))
		})
	}
}

func TestJudgeItemsUsesThePostTimeForTheLowerBound(t *testing.T) {
	t.Parallel()

	const posted = "2026-11-14 00:30"

	got := judge(posted, judgeRows(), nil,
		item(new(6), "2026-11-13", new("25:30"), posterstore.RowPlanned, ""),
		item(nil, "2026-11-13", new("26:00"), posterstore.RowPlanned, "延長"),
		item(new(2), "2026-11-13", nil, posterstore.RowCancelled, ""),
		item(nil, "2026-11-13", new("24:00"), posterstore.RowPlanned, "X"),
	)

	want := []posterstore.Change{
		timeChanged(1, 6, "25:30"),
		change(2, posterstore.ChangeAdded, func(c *posterstore.Change) {
			c.Day, c.Start, c.Status, c.Text = utcDate("2026-11-13"), new("26:00"), posterstore.RowPlanned, "延長"
		}),
		rejected(3, 2, rejectDateRange),
		rejected(4, 0, rejectPastTime),
	}
	for i := range want {
		want[i].PostedAt = at(posted)
	}

	require.Equal(t, want, got, "날짜 하한과 지난 시각은 24시 이상 표기를 다음 날 시각으로 바꿔 견준다")
}

func TestJudgeItemsBorrowsTheRowTimeForTheLowerBound(t *testing.T) {
	t.Parallel()

	const posted = "2026-11-14 00:30"

	got := judge(posted, judgeRows(), nil, item(new(6), "2026-11-13", nil, posterstore.RowCancelled, ""))

	want := change(1, posterstore.ChangeCancelled, func(c *posterstore.Change) { c.Slot, c.Status, c.PostedAt = 6, posterstore.RowCancelled, at(posted) })
	require.Equal(t, []posterstore.Change{want}, got, "시각 없는 취소는 칸의 25:00을 빌려 날짜 하한을 견준다")
}

func TestJudgeItemsRedirectsToTheRowBeforeCheckingPastTime(t *testing.T) {
	t.Parallel()

	const posted = "2026-11-13 20:30"

	got := judge(posted, judgeRows(), nil, item(nil, "2026-11-13", new("20:00"), posterstore.RowPlanned, "ゲスト追加"))

	want := change(1, posterstore.ChangeContentChanged, func(c *posterstore.Change) { c.Slot, c.Text, c.PostedAt = 2, "ゲスト追加", at(posted) })
	require.Equal(t, []posterstore.Change{want}, got)
}

func TestJudgeItemsChecksCollisionsWhenRestoringACalledOffSlot(t *testing.T) {
	t.Parallel()

	cancelA := posterstore.Change{
		PostID: 3, Item: 1, PostedAt: at("2026-11-12 10:00"), Kind: posterstore.ChangeCancelled, Slot: 1, Status: posterstore.RowCancelled,
	}
	moveB := posterstore.Change{PostID: 4, Item: 1, PostedAt: at("2026-11-12 11:00"), Kind: posterstore.ChangeTimeChanged, Slot: 2, Start: new("19:00")}
	restore := item(new(1), "2026-11-13", nil, posterstore.RowPlanned, "")

	require.Equal(t, []posterstore.Change{rejected(1, 1, rejectTimeCollision)},
		judge(judgePosted, judgeRows(), []posterstore.Change{cancelA, moveB}, restore), "되살린 칸이 옮겨 온 방송과 같은 시각이면 거절한다")

	require.Equal(t, []posterstore.Change{change(1, posterstore.ChangeConfirmed, func(c *posterstore.Change) { c.Slot, c.Status = 1, posterstore.RowPlanned })},
		judge(judgePosted, judgeRows(), []posterstore.Change{cancelA}, restore))
}

func TestJudgeItemsTargetsSlotsAddedByEarlierPosts(t *testing.T) {
	t.Parallel()

	prior := []posterstore.Change{{
		PostID: 3, Item: 2, PostedAt: at("2026-11-12 10:00"), Kind: posterstore.ChangeAdded,
		Day: utcDate("2026-11-14"), Start: new("18:00"), Status: posterstore.RowPlanned, Text: "ゲリラ",
	}}

	got := judge(judgePosted, judgeRows(), prior,
		item(new(7), "2026-11-14", new("18:30"), posterstore.RowPlanned, ""),
		item(new(7), "2026-11-14", nil, posterstore.RowCancelled, ""),
	)
	require.Equal(t, []posterstore.Change{rejected(1, 0, rejectDuplicateRow), rejected(2, 0, rejectDuplicateRow)}, got,
		"더한 칸은 거절 행에 slot으로 남기지 않는다")

	got = judge(judgePosted, judgeRows(), prior, item(new(7), "2026-11-14", new("18:30"), posterstore.RowPlanned, ""))
	require.Equal(t, []posterstore.Change{change(1, posterstore.ChangeTimeChanged, func(c *posterstore.Change) {
		c.TargetPostID, c.TargetItem, c.Start = 3, 2, new("18:30")
	})}, got)
}

func TestJudgeItemsStopsAddingAtTheCardLimit(t *testing.T) {
	t.Parallel()

	rows := make([]posterstore.Row, posterstore.MaxRows-1)
	for i := range rows {
		rows[i] = posterstore.Row{Ordinal: i + 1, Day: utcDate("2026-11-15"), Status: posterstore.RowPlanned, SourceText: "x"}
	}

	got := judge(judgePosted, rows, nil,
		item(nil, "2026-11-14", new("18:00"), posterstore.RowPlanned, "一"),
		item(nil, "2026-11-14", new("20:00"), posterstore.RowPlanned, "二"),
	)

	require.Equal(t, posterstore.ChangeAdded, got[0].Kind)
	require.Equal(t, rejected(2, 0, rejectSlotLimit), got[1])
}

var exampleRowPattern = regexp.MustCompile(`^(\d+)\. (\d{4}-\d\d-\d\d)(?: (\d\d:\d\d)(?: (\S+))?)? \[(\w+)\] (.*)$`)

type promptExample struct {
	post      posterstore.Post
	weekStart time.Time
	weekEnd   time.Time
	rows      []posterstore.Row
	items     []matchItem
}

func parsePromptExample(t *testing.T, block string) promptExample {
	t.Helper()

	_, rest, _ := strings.Cut(block, "\nPost timestamp: ")
	stamp, rest, _ := strings.Cut(rest, "\nCurrent week: ")
	week, rest, _ := strings.Cut(rest, "\nCurrent schedule rows:\n")
	listed, rest, _ := strings.Cut(rest, "\nPost text:\n")
	body, output, ok := strings.Cut(rest, "\nOutput:\n")
	require.True(t, ok, block)

	output, _, _ = strings.Cut(output, "\n\n")

	first, last, _ := strings.Cut(week, " to ")
	example := promptExample{post: posterstore.Post{ID: 1, PostedAt: at(stamp[:16]), Body: body}, weekStart: utcDate(first), weekEnd: utcDate(last)}

	for line := range strings.Lines(listed) {
		match := exampleRowPattern.FindStringSubmatch(strings.TrimSuffix(line, "\n"))
		require.NotNil(t, match, line)

		number, err := strconv.Atoi(match[1])
		require.NoError(t, err)

		for len(example.rows) < number-1 {
			example.rows = append(example.rows, posterstore.Row{
				Ordinal: len(example.rows) + 1, Day: example.weekStart, Status: posterstore.RowPlanned, SourceText: "x",
			})
		}

		example.rows = append(example.rows, posterstore.Row{
			Ordinal: number, Day: utcDate(match[2]), Start: match[3], TZ: match[4], Status: posterstore.RowStatus(match[5]), SourceText: match[6],
		})
	}

	var decoded struct {
		Items []matchItem `json:"items"`
	}

	require.NoError(t, jsonv2.Unmarshal([]byte(output), &decoded), output)

	example.items = decoded.Items

	return example
}

func TestJudgeItemsOnMatchPromptExamples(t *testing.T) {
	t.Parallel()

	want := map[string][]string{
		"1": {"time_changed"},
		"2": {"time_changed", "time_changed", "tentative"},
		"3": {"off", "same", "rejected:" + rejectForeignZone},
		"4": {},
		"5": {"content_changed", "added", string(posterstore.ChangeCancelled), "same"},
		"6": {"added", "moved"},
		"7": {"content_changed"},
		"8": {},
	}
	blocks := strings.Split(matchPromptTemplate, "\n\nExample ")[1:]
	require.Len(t, blocks, len(want))

	for _, block := range blocks {
		name, _, _ := strings.Cut(block, "\n")
		example := parsePromptExample(t, block)

		got := judgeItems(example.post, example.weekStart, example.weekEnd, example.rows, nil, example.items)
		require.Equal(t, want[name], outcomes(got), "Example %s", name)
		require.NoError(t, posterstore.ValidateChanges(got), "Example %s", name)
	}
}

func outcomes(changes []posterstore.Change) []string {
	out := make([]string, len(changes))
	for i := range changes {
		out[i] = string(changes[i].Kind)
		if changes[i].Reason != "" {
			out[i] += ":" + changes[i].Reason
		}
	}

	return out
}

type recordedExpect struct {
	Kind   posterstore.ChangeKind `json:"kind"`
	Reason string                 `json:"reason,omitempty"`
	Why    string                 `json:"why,omitempty"`
}

type recordedPost struct {
	Name     string           `json:"name,omitempty"`
	ID       int64            `json:"id,omitempty"`
	Member   string           `json:"member"`
	PostedAt string           `json:"posted_at"`
	Body     string           `json:"body"`
	Items    []matchItem      `json:"items"`
	Expect   []recordedExpect `json:"expect"`
	Tail     string           `json:"tail,omitempty"`
	TailFix  []struct {
		Harness string `json:"harness"`
		Go      string `json:"go"`
		Why     string `json:"why"`
	} `json:"tail_fix,omitempty"`
}

type recordedSample struct {
	Week    [2]string `json:"week"`
	Posters map[string][]struct {
		Date   string                `json:"date"`
		Start  string                `json:"start"`
		TZ     string                `json:"tz"`
		Status posterstore.RowStatus `json:"status"`
		Text   string                `json:"text"`
	} `json:"posters"`
	Runs []struct {
		Name  string         `json:"name"`
		Posts []recordedPost `json:"posts"`
	} `json:"runs"`
	Probes []recordedPost `json:"probes"`
}

func (s *recordedSample) rows(member string) []posterstore.Row {
	rows := make([]posterstore.Row, len(s.Posters[member]))
	for i, row := range s.Posters[member] {
		rows[i] = posterstore.Row{Ordinal: i + 1, Day: utcDate(row.Date), Start: row.Start, TZ: row.TZ, Status: row.Status, SourceText: row.Text}
	}

	return rows
}

func (s *recordedSample) check(t *testing.T, name string, p *recordedPost, id int64, prior []posterstore.Change, tails *int) []posterstore.Change {
	t.Helper()

	rows := s.rows(p.Member)
	weekStart, weekEnd := utcDate(s.Week[0]), utcDate(s.Week[1])
	post := posterstore.Post{ID: id, PostedAt: at(p.PostedAt), Body: p.Body}

	if p.Tail != "" {
		*tails++

		want := p.Tail

		for _, fix := range p.TailFix {
			require.Contains(t, want, fix.Harness, name)

			want = strings.Replace(want, fix.Harness, fix.Go, 1)
		}

		prompt := buildMatchPrompt(post, weekStart, weekEnd, matchRows(posterstore.ApplyChanges(rows, prior)))
		tail, ok := strings.CutPrefix(prompt, matchPromptTemplate)
		require.True(t, ok, name)
		require.Equal(t, want, tail, "%s 지시문 꼬리는 하니스 입력과 바이트 단위로 같아야 한다", name)
	}

	got := judgeItems(post, weekStart, weekEnd, rows, prior, p.Items)
	want := make([]string, len(p.Expect))

	for i, e := range p.Expect {
		want[i] = string(e.Kind)
		if e.Reason != "" {
			want[i] += ":" + e.Reason
		}
	}

	require.Equal(t, want, outcomes(got), name)
	require.NoError(t, posterstore.ValidateChanges(got), name)

	return append(prior, got...)
}

func TestJudgeItemsMatchesTheRecordedHarness(t *testing.T) {
	t.Parallel()

	raw, err := fs.ReadFile(os.DirFS(filepath.Join("testdata", "changes")), "recorded.json")
	require.NoError(t, err)

	var sample recordedSample

	require.NoError(t, jsonv2.Unmarshal(raw, &sample))

	require.Len(t, sample.Runs, 3)
	require.Len(t, sample.Probes, 34)

	byRun := map[string]map[string][]posterstore.Change{}
	tails := 0

	for _, run := range sample.Runs {
		require.Len(t, run.Posts, 30, run.Name)

		prior := map[string][]posterstore.Change{}

		for i := range run.Posts {
			p := &run.Posts[i]

			prior[p.Member] = sample.check(t, run.Name+"/"+p.Member+"/"+p.PostedAt, p, p.ID, prior[p.Member], &tails)
		}

		byRun[run.Name] = prior
	}

	for i := range sample.Probes {
		p := &sample.Probes[i]

		var prior []posterstore.Change

		for _, c := range byRun["run1"][p.Member] {
			if c.PostedAt.Before(at(p.PostedAt)) {
				prior = append(prior, c)
			}
		}

		sample.check(t, p.Name, p, int64(1000+i), prior, &tails)
	}

	require.Equal(t, 38, tails, "지시문 꼬리 바이트 대조 건수")
}
