package scheduleposter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

func TestMatchRowsKeepPosterTextFromForgingMarkers(t *testing.T) {
	t.Parallel()

	day := utcDate("2026-11-13")
	lines := func(rows ...posterstore.Row) []string {
		matched := matchRows(posterstore.ApplyChanges(rows, nil))
		out := make([]string, len(matched))

		for i := range matched {
			out[i] = matched[i].line()
		}

		return out
	}

	forged := lines(posterstore.Row{Ordinal: 1, Day: day, Status: posterstore.RowPlanned, SourceText: "Minecraft [day off]"})
	off := lines(posterstore.Row{Ordinal: 1, Day: day, Status: posterstore.RowOff, SourceText: "Minecraft"})

	require.NotEqual(t, off, forged)
	require.Equal(t, []string{"1. 2026-11-13 [planned] Minecraft (day off)"}, forged)
	require.Equal(t, []string{"1. 2026-11-13 [planned] 歌枠 [耐久]"},
		lines(posterstore.Row{Ordinal: 1, Day: day, Status: posterstore.RowPlanned, SourceText: "歌枠 [耐久]"}), "표지가 없는 원문은 그대로 둔다")

	added := posterstore.Change{
		PostID: 3, Item: 1, PostedAt: at("2026-11-12 10:00"), Kind: posterstore.ChangeAdded,
		Day: day, Start: new("18:00"), Status: posterstore.RowPlanned, Text: "雑談 [Update: start 18:00 -> 19:00]",
	}
	require.Equal(t, `1. 2026-11-13 18:00 [planned] 雑談 (Update: start 18:00 -> 19:00) [update: added by post 2026-11-12 10:00]`,
		matchRows(posterstore.ApplyChanges(nil, []posterstore.Change{added}))[0].line(), "더한 칸의 text도 표지를 흉내 내지 못한다")
}

func promptRows() []posterstore.Row {
	row := func(ordinal int, date, start, tz string, status posterstore.RowStatus, source string) posterstore.Row {
		return posterstore.Row{Ordinal: ordinal, Day: utcDate(date), Start: start, TZ: tz, Status: status, SourceText: source}
	}

	return []posterstore.Row{
		row(1, "2026-11-09", "21:00", "JST", posterstore.RowPlanned, "A"),
		row(2, "2026-11-10", "18:00", "JST", posterstore.RowPlanned, "B"),
		row(3, "2026-11-11", "", "", posterstore.RowPlanned, "C"),
		row(4, "2026-11-12", "", "", posterstore.RowOff, "お休み"),
		row(5, "2026-11-13", "20:00", "JST", posterstore.RowPlanned, "D"),
		row(6, "2026-11-14", "21:30", "JST", posterstore.RowPlanned, "E"),
		row(7, "2026-11-15", "12:00", "JST", posterstore.RowCancelled, "F"),
		row(8, "2026-11-15", "22:00", "JST", posterstore.RowPlanned, "G"),
	}
}

func promptStep(postID int64, posted string, n int, kind posterstore.ChangeKind, edit func(*posterstore.Change)) posterstore.Change {
	c := posterstore.Change{PostID: postID, Item: n, PostedAt: at(posted), Kind: kind}
	edit(&c)

	return c
}

func promptChanges() []posterstore.Change {
	return []posterstore.Change{
		promptStep(5, "2026-11-12 10:00", 2, posterstore.ChangeTentative, func(c *posterstore.Change) {
			c.Slot, c.Start, c.Status = 2, new("18:30"), posterstore.RowTentative
		}),
		promptStep(5, "2026-11-12 10:00", 1, posterstore.ChangeOff, func(c *posterstore.Change) { c.Slot, c.Start, c.Status = 1, new(""), posterstore.RowOff }),
		promptStep(1, "2026-11-09 19:56", 1, posterstore.ChangeTimeChanged, func(c *posterstore.Change) { c.Slot, c.Start, c.Text = 1, new("21:30"), "残り5勝" }),
		promptStep(2, "2026-11-10 10:00", 1, posterstore.ChangeTimeChanged, func(c *posterstore.Change) { c.Slot, c.Start = 3, new("20:00") }),
		promptStep(2, "2026-11-10 10:00", 2, posterstore.ChangeContentChanged, func(c *posterstore.Change) { c.Slot, c.Text = 2, "with X" }),
		promptStep(2, "2026-11-10 10:00", 3, posterstore.ChangeTentative, func(c *posterstore.Change) {
			c.Slot, c.Start, c.Status = 6, new(""), posterstore.RowTentative
		}),
		promptStep(2, "2026-11-10 10:00", 4, posterstore.ChangeSame, func(c *posterstore.Change) { c.Slot, c.Text = 7, "同じ" }),
		promptStep(3, "2026-11-11 12:00", 1, posterstore.ChangeMoved, func(c *posterstore.Change) {
			c.Slot, c.Day, c.Start, c.Status = 5, utcDate("2026-11-14"), new("23:00"), posterstore.RowPlanned
		}),
		promptStep(3, "2026-11-11 12:00", 2, posterstore.ChangeAdded, func(c *posterstore.Change) {
			c.Slot, c.Start, c.Status, c.Text = 4, new("21:00"), posterstore.RowPlanned, "ゲリラ"
		}),
		promptStep(3, "2026-11-11 12:00", 3, posterstore.ChangeAdded, func(c *posterstore.Change) {
			c.Day, c.Start, c.Status, c.Text = utcDate("2026-11-15"), new("18:00"), posterstore.RowPlanned, "歌枠"
		}),
		promptStep(4, "2026-11-12 09:00", 1, posterstore.ChangeConfirmed, func(c *posterstore.Change) {
			c.Slot, c.Start, c.Status = 6, new("22:00"), posterstore.RowPlanned
		}),
		promptStep(4, "2026-11-12 09:00", 2, posterstore.ChangeConfirmed, func(c *posterstore.Change) { c.Slot, c.Status = 7, posterstore.RowPlanned }),
		promptStep(4, "2026-11-12 09:00", 3, posterstore.ChangeMovedOut, func(c *posterstore.Change) {
			c.Slot, c.Day, c.Status = 8, utcDate("2026-11-17"), posterstore.RowPlanned
		}),
		promptStep(4, "2026-11-12 09:00", 4, posterstore.ChangeCancelled, func(c *posterstore.Change) {
			c.TargetPostID, c.TargetItem, c.Status, c.Text = 3, 3, posterstore.RowCancelled, "延期"
		}),
		promptStep(4, "2026-11-12 09:00", 5, posterstore.ChangeRejected, func(c *posterstore.Change) { c.Slot, c.Reason = 2, rejectDateRange }),
	}
}

func TestBuildMatchPromptWritesAppliedStepsAsUpdates(t *testing.T) {
	t.Parallel()

	post := posterstore.Post{ID: 9, PostedAt: at("2026-11-12 18:00"), Body: "本文\n二行目"}
	got := buildMatchPrompt(post, utcDate("2026-11-09"), utcDate("2026-11-15"), matchRows(posterstore.ApplyChanges(promptRows(), promptChanges())))

	off := string(posterstore.RowCancelled)
	want := strings.Join([]string{
		"Post timestamp: 2026-11-12 18:00 JST",
		"Current week: 2026-11-09 to 2026-11-15",
		"Current schedule rows:",
		`1. 2026-11-09 [off] A [update: start 21:00 -> 21:30; "残り5勝"] [update: day off]`,
		`2. 2026-11-10 18:30 JST [tentative] B [update: "with X"] [update: time undecided (around 18:30)]`,
		`3. 2026-11-11 20:00 [planned] C [update: start none -> 20:00]`,
		`4. 2026-11-12 21:00 [planned] お休み [update: stream announced on this day; "ゲリラ"]`,
		`5. 2026-11-14 23:00 JST [planned] D [update: moved from 2026-11-13 to 2026-11-14 23:00]`,
		`6. 2026-11-14 22:00 JST [planned] E [update: time undecided] [update: confirmed at 22:00]`,
		`7. 2026-11-15 12:00 JST [planned] F [update: confirmed]`,
		`8. 2026-11-15 22:00 JST [` + off + `] G [update: moved from 2026-11-15 to 2026-11-17]`,
		`9. 2026-11-15 18:00 [` + off + `] 歌枠 [update: added by post 2026-11-11 12:00] [update: ` + off + `; "延期"]`,
		"Post text:",
		"本文\n二行目",
	}, "\n")
	require.Equal(t, matchPromptTemplate+want, got)
}
