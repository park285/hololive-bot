package scheduleposter_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const (
	tuesday   = "2026-11-10"
	wednesday = "2026-11-11"
	thursday  = "2026-11-12"
	friday    = "2026-11-13"
	saturday  = "2026-11-14"
)

func day(value string) time.Time {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		panic(err)
	}

	return parsed
}

func postedAt(value string) time.Time {
	parsed, err := time.Parse("2006-01-02 15:04", value)
	if err != nil {
		panic(err)
	}

	return parsed
}

func posterRows() []scheduleposter.Row {
	return []scheduleposter.Row{
		{
			Ordinal: 1, Day: day(tuesday), Start: "21:00", End: "23:00", Status: scheduleposter.RowPlanned,
			SourceText: "21:00 歌枠", TitleKo: "노래", WithKo: []string{"게스트"}, PlaceKo: "본인 채널", TagsKo: []string{"노래"}, NoteKo: "메모",
		},
		{Ordinal: 2, Day: day(wednesday), Status: scheduleposter.RowOff, SourceText: "おやすみ", TitleKo: "휴방"},
		{Ordinal: 3, Day: day(thursday), Start: "20:00", Status: scheduleposter.RowCancelled, SourceText: "20:00 ゲーム", TitleKo: "게임"},
		{Ordinal: 4, Day: day(friday), Start: "25:00", Status: scheduleposter.RowTentative, SourceText: "25:00 雑談", TitleKo: "잡담"},
	}
}

func TestApplyChangesLetsTheLaterPostWinWhateverTheInputOrder(t *testing.T) {
	t.Parallel()

	early := scheduleposter.Change{
		PostID: 10, Item: 1, PostedAt: postedAt("2026-11-10 09:00"),
		Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00"),
	}
	late := scheduleposter.Change{
		PostID: 11, Item: 1, PostedAt: postedAt("2026-11-10 12:00"),
		Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:30"),
	}
	sameTimeLowerID := scheduleposter.Change{
		PostID: 9, Item: 1, PostedAt: postedAt("2026-11-10 12:00"),
		Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:45"),
	}
	confirm := scheduleposter.Change{
		PostID: 12, Item: 1, PostedAt: postedAt("2026-11-10 12:00"),
		Kind: scheduleposter.ChangeConfirmed, Slot: 4, Status: scheduleposter.RowPlanned,
	}

	want := scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{early, sameTimeLowerID, late, confirm})

	require.Equal(t, "22:30", want[0].Start, "게시 시각이 늦은 글, 같으면 글 번호가 큰 글의 값이 남는다")
	require.Empty(t, want[0].End, "시작 시각이 바뀌면 끝 시각을 비운다")
	require.Len(t, want[0].Steps, 3)
	require.Equal(t, scheduleposter.RowPlanned, want[3].Status)

	for _, shuffled := range [][]scheduleposter.Change{
		{late, early, confirm, sameTimeLowerID},
		{confirm, late, sameTimeLowerID, early},
		{late, sameTimeLowerID, confirm, early},
	} {
		require.Equal(t, want, scheduleposter.ApplyChanges(posterRows(), shuffled))
	}
}

func TestApplyChangesKeepsLaterValuesWhenAnEarlierPostIsRecordedLate(t *testing.T) {
	t.Parallel()

	later := scheduleposter.Change{
		PostID: 20, Item: 1, PostedAt: postedAt("2026-11-13 18:00"), RecordedAt: postedAt("2026-11-13 18:10"),
		Kind: scheduleposter.ChangeConfirmed, Slot: 4, Status: scheduleposter.RowPlanned,
	}
	earlier := scheduleposter.Change{
		PostID: 19, Item: 1, PostedAt: postedAt("2026-11-13 12:00"), RecordedAt: postedAt("2026-11-13 19:00"),
		Kind: scheduleposter.ChangeTimeChanged, Slot: 4, Start: new("26:00"),
	}

	slots := scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{later, earlier})

	require.Equal(t, "26:00", slots[3].Start)
	require.Equal(t, scheduleposter.RowPlanned, slots[3].Status, "앞선 글이 말하지 않은 상태는 뒤 글의 값 그대로다")
	require.Len(t, slots[3].Steps, 2)
	require.Equal(t, int64(19), slots[3].Steps[0].PostID)
}

func TestApplyChangesKeepsWhatALaterSamePostSaidWhenAnEarlierPostIsRecordedLate(t *testing.T) {
	t.Parallel()

	laterSame := scheduleposter.Change{
		PostID: 21, Item: 1, PostedAt: postedAt("2026-11-10 12:00"), RecordedAt: postedAt("2026-11-10 12:10"),
		Kind: scheduleposter.ChangeSame, Slot: 1, Day: day(tuesday), Start: new("21:00"), Status: scheduleposter.RowPlanned,
	}
	earlierRetime := scheduleposter.Change{
		PostID: 20, Item: 1, PostedAt: postedAt("2026-11-10 09:00"), RecordedAt: postedAt("2026-11-10 13:00"),
		Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00"),
	}

	got := scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{laterSame, earlierRetime})[0]

	require.Equal(t, "21:00", got.Start, "뒤 글이 같다고 말한 시각이 앞선 글의 변경을 덮는다")
	require.False(t, got.TimeChanged)
	require.Equal(t, scheduleposter.RowPlanned, got.Status)
	require.Len(t, got.Steps, 1, "같음은 대응 입력의 변경 메모에 남지 않는다")
	require.Equal(t, int64(20), got.Steps[0].PostID)
	require.Equal(t, "노래", got.TitleKo)
	require.Equal(t, []string{"게스트"}, got.WithKo)

	bare := scheduleposter.Change{PostID: 22, Item: 1, PostedAt: postedAt("2026-11-10 12:00"), Kind: scheduleposter.ChangeSame, Slot: 3}
	slots := scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{bare})
	require.Equal(t, posterRows()[2], slots[2].Row, "값이 없는 같음은 아무것도 바꾸지 않는다")
	require.Empty(t, slots[2].Steps)
}

func TestApplyChangesAddsSlotsThatLaterPostsCanChange(t *testing.T) {
	t.Parallel()

	added := scheduleposter.Change{
		PostID: 30, Item: 2, PostedAt: postedAt("2026-11-12 10:00"), Kind: scheduleposter.ChangeAdded,
		Day: day(saturday), Start: new("20:00"), Status: scheduleposter.RowTentative, Text: "コラボ",
	}
	retimed := scheduleposter.Change{
		PostID: 31, Item: 1, PostedAt: postedAt("2026-11-12 11:00"), Kind: scheduleposter.ChangeTimeChanged,
		TargetPostID: 30, TargetItem: 2, Start: new("21:00"),
	}

	slots := scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{retimed, added})

	require.Len(t, slots, 5)

	got := slots[4]
	require.Equal(t, 5, got.Number)
	require.Zero(t, got.Ordinal)
	require.Equal(t, int64(30), got.AddedPostID)
	require.Equal(t, 2, got.AddedItem)
	require.Equal(t, day(saturday), got.Day)
	require.Equal(t, "21:00", got.Start)
	require.Equal(t, scheduleposter.RowTentative, got.Status)
	require.Equal(t, "コラボ", got.SourceText)
	require.Equal(t, "방송", got.TitleKo, "번역 전의 새 칸은 제목이 \"방송\"이다")
	require.True(t, got.OrigDay.IsZero())
	require.False(t, got.TimeChanged, "더한 칸은 원래 시각이 없어 시각 변경으로 보지 않는다")

	for i, slot := range slots[:4] {
		require.Equal(t, i+1, slot.Number)
	}
}

func TestApplyChangesSkipsChangesWithoutATarget(t *testing.T) {
	t.Parallel()

	changes := []scheduleposter.Change{
		{
			PostID: 40, Item: 1, PostedAt: postedAt("2026-11-10 09:00"), Kind: scheduleposter.ChangeCancelled,
			Slot: 9, Status: scheduleposter.RowCancelled,
		},
		{
			PostID: 41, Item: 1, PostedAt: postedAt("2026-11-10 09:00"), Kind: scheduleposter.ChangeTimeChanged,
			TargetPostID: 999, TargetItem: 1, Start: new("10:00"),
		},
		{
			PostID: 42, Item: 1, PostedAt: postedAt("2026-11-10 10:00"), Kind: scheduleposter.ChangeCancelled,
			TargetPostID: 43, TargetItem: 1, Status: scheduleposter.RowCancelled,
		},
		{
			PostID: 43, Item: 1, PostedAt: postedAt("2026-11-10 11:00"), Kind: scheduleposter.ChangeAdded,
			Day: day(saturday), Start: new("20:00"), Status: scheduleposter.RowPlanned,
		},
		{PostID: 44, Item: 1, PostedAt: postedAt("2026-11-10 09:00"), Kind: scheduleposter.ChangeSame, Slot: 1},
		{PostID: 45, Item: 1, PostedAt: postedAt("2026-11-10 09:00"), Kind: scheduleposter.ChangeRejected, Slot: 1, Reason: "time_collision"},
	}

	slots := scheduleposter.ApplyChanges(posterRows(), changes)

	require.Len(t, slots, 5)
	require.Equal(t, scheduleposter.RowPlanned, slots[4].Status, "더한 칸보다 먼저 게시된 글은 그 칸을 바꾸지 못한다")
	require.Len(t, slots[4].Steps, 1)

	for i, row := range posterRows() {
		require.Equal(t, row, slots[i].Row)
		require.Empty(t, slots[i].Steps)
	}
}

func TestApplyChangesLeavesSlotsMovedOutOfTheWeekCancelledOnTheirDay(t *testing.T) {
	t.Parallel()

	out := scheduleposter.Change{
		PostID: 50, Item: 1, PostedAt: postedAt("2026-11-10 09:00"), Kind: scheduleposter.ChangeMovedOut,
		Slot: 1, Day: day("2026-11-17"), Start: new("20:00"), Status: scheduleposter.RowPlanned,
	}

	got := scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{out})[0]

	require.Equal(t, scheduleposter.RowCancelled, got.Status)
	require.Equal(t, day(tuesday), got.Day)
	require.Equal(t, "21:00", got.Start)
	require.Equal(t, day("2026-11-17"), got.MovedTo)
	require.Equal(t, "노래", got.TitleKo)
	require.False(t, got.TimeChanged)
}

func TestApplyChangesComparesTimeWithThePoster(t *testing.T) {
	t.Parallel()

	there := scheduleposter.Change{
		PostID: 60, Item: 1, PostedAt: postedAt("2026-11-10 09:00"),
		Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00"),
	}
	back := scheduleposter.Change{
		PostID: 61, Item: 1, PostedAt: postedAt("2026-11-10 10:00"),
		Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("21:00"),
	}

	slots := scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{there, back})
	require.False(t, slots[0].TimeChanged, "A→B→A는 포스터와 같은 시각이다")
	require.Equal(t, day(tuesday), slots[0].OrigDay)
	require.Equal(t, "21:00", slots[0].OrigStart)
	require.Equal(t, "22:00", slots[0].Steps[1].PrevStart)

	nextDay := scheduleposter.Change{
		PostID: 62, Item: 1, PostedAt: postedAt("2026-11-12 09:00"), Kind: scheduleposter.ChangeMoved,
		Slot: 4, Day: day(saturday), Start: new("01:00"), Status: scheduleposter.RowTentative,
	}

	slots = scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{nextDay})
	require.False(t, slots[3].TimeChanged, "11-13 25:00과 11-14 01:00은 같은 시각이다")

	moved := scheduleposter.Change{
		PostID: 63, Item: 1, PostedAt: postedAt("2026-11-09 09:00"), Kind: scheduleposter.ChangeMoved,
		Slot: 1, Day: day(saturday), Status: scheduleposter.RowPlanned,
	}

	slots = scheduleposter.ApplyChanges(posterRows(), []scheduleposter.Change{moved})
	require.True(t, slots[0].TimeChanged)
	require.Equal(t, day(saturday), slots[0].Day)
	require.Equal(t, day(tuesday), slots[0].OrigDay)
	require.Equal(t, day(tuesday), slots[0].Steps[0].PrevDay)
}

func TestNormalizeDayTimeFoldsLateNightHours(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		day, start, wantDay, wantStart string
	}{
		{friday, "25:00", saturday, "01:00"},
		{friday, "24:00", saturday, "00:00"},
		{friday, "9:05", friday, "09:05"},
		{friday, "23:59", friday, "23:59"},
		{friday, "", friday, ""},
	} {
		gotDay, gotStart := scheduleposter.NormalizeDayTime(day(tc.day), tc.start)
		require.Equal(t, day(tc.wantDay), gotDay, tc.start)
		require.Equal(t, tc.wantStart, gotStart, tc.start)
	}
}

func TestApplyChangesTitlesOffAndRevivedSlots(t *testing.T) {
	t.Parallel()

	at := postedAt("2026-11-09 09:00")
	changes := []scheduleposter.Change{
		{PostID: 70, Item: 1, PostedAt: at, Kind: scheduleposter.ChangeOff, Slot: 1, Start: new(""), Status: scheduleposter.RowOff},
		{PostID: 71, Item: 1, PostedAt: at, Kind: scheduleposter.ChangeAdded, Slot: 2, Start: new("20:00"), Status: scheduleposter.RowPlanned},
		{PostID: 72, Item: 1, PostedAt: at, Kind: scheduleposter.ChangeConfirmed, Slot: 3, Status: scheduleposter.RowPlanned},
		{
			PostID: 73, Item: 1, PostedAt: at, Kind: scheduleposter.ChangeContentChanged, Slot: 4,
			Text: "ASMR", TitleKo: "ASMR 방송", TagsKo: []string{"ASMR"},
		},
	}

	slots := scheduleposter.ApplyChanges(posterRows(), changes)

	require.Equal(t, scheduleposter.RowOff, slots[0].Status)
	require.Empty(t, slots[0].Start)
	require.Empty(t, slots[0].End)
	require.Equal(t, "휴방", slots[0].TitleKo)
	require.Empty(t, slots[0].WithKo)
	require.Empty(t, slots[0].PlaceKo)
	require.Empty(t, slots[0].TagsKo)
	require.Empty(t, slots[0].NoteKo)

	require.Equal(t, scheduleposter.RowPlanned, slots[1].Status)
	require.Equal(t, "20:00", slots[1].Start)
	require.Equal(t, "방송", slots[1].TitleKo, "휴방 칸을 되살리면 제목이 \"방송\"이다")
	require.Zero(t, slots[1].AddedPostID, "되살린 칸은 원래 행이다")

	require.Equal(t, scheduleposter.RowPlanned, slots[2].Status)
	require.Equal(t, "게임", slots[2].TitleKo, "취소 칸을 되살리면 한국어 칸을 지킨다")
	require.Equal(t, "20:00", slots[2].Start)

	require.Equal(t, "ASMR 방송", slots[3].TitleKo)
	require.Equal(t, []string{"ASMR"}, slots[3].TagsKo)
	require.Equal(t, "25:00 雑談", slots[3].SourceText, "원래 행의 원문은 바꾸지 않는다")
	require.False(t, slots[3].TimeChanged)
}

func TestApplyChangesDoesNotModifyItsInput(t *testing.T) {
	t.Parallel()

	rows := posterRows()
	changes := []scheduleposter.Change{
		{
			PostID: 81, Item: 1, PostedAt: postedAt("2026-11-10 12:00"), Kind: scheduleposter.ChangeOff,
			Slot: 1, Start: new(""), Status: scheduleposter.RowOff,
		},
		{
			PostID: 80, Item: 1, PostedAt: postedAt("2026-11-10 09:00"), Kind: scheduleposter.ChangeTimeChanged,
			Slot: 1, Start: new("22:00"),
		},
	}

	scheduleposter.ApplyChanges(rows, changes)

	require.Equal(t, posterRows(), rows)
	require.Equal(t, int64(81), changes[0].PostID)
}

func TestPublishedScheduleSlotsAndUpdatedAt(t *testing.T) {
	t.Parallel()

	schedule := scheduleposter.PublishedSchedule{
		ExtractedAt: postedAt("2026-11-09 12:00"),
		Rows:        posterRows(),
		Changes: []scheduleposter.Change{
			{
				PostID: 90, Item: 1, PostedAt: postedAt("2026-11-10 09:00"), RecordedAt: postedAt("2026-11-10 09:30"),
				Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00"),
			},
			{
				PostID: 91, Item: 1, PostedAt: postedAt("2026-11-10 08:00"), RecordedAt: postedAt("2026-11-10 10:00"),
				Kind: scheduleposter.ChangeCancelled, Slot: 4, Status: scheduleposter.RowCancelled,
			},
			{
				PostID: 92, Item: 1, PostedAt: postedAt("2026-11-10 11:00"), RecordedAt: postedAt("2026-11-10 11:30"),
				Kind: scheduleposter.ChangeSame, Slot: 1, Start: new("22:00"),
			},
		},
	}

	require.Equal(t, scheduleposter.ApplyChanges(schedule.Rows, schedule.Changes), schedule.Slots())
	require.Equal(t, postedAt("2026-11-10 10:00"), schedule.UpdatedAt(), "같음은 카드를 바꾸지 않아 갱신 시각에 넣지 않는다")

	schedule.Changes = nil
	require.Equal(t, postedAt("2026-11-09 12:00"), schedule.UpdatedAt())
}

func TestPostURL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://x.com/i/status/1989012345678901234", scheduleposter.PostURL(1989012345678901234))
}

func validChanges() []scheduleposter.Change {
	at := postedAt("2026-11-10 09:00")

	return []scheduleposter.Change{
		{
			PostID: 100, Item: 1, PostedAt: at, Kind: scheduleposter.ChangeAdded, Day: day(saturday), Start: new("29:59"),
			Status: scheduleposter.RowPlanned, Text: strings.Repeat("あ", scheduleposter.MaxChangeTextChars),
		},
		{
			PostID: 100, Item: 2, PostedAt: at, Kind: scheduleposter.ChangeTimeChanged, TargetPostID: 99, TargetItem: 20,
			Start: new("9:00"), TitleKo: strings.Repeat("가", 1000), WithKo: make([]string, 40),
			Terms: []scheduleposter.Term{{Source: "歌", Ko: "노래", Basis: "translated"}},
		},
		{PostID: 100, Item: 3, PostedAt: at, Kind: scheduleposter.ChangeOff, Slot: 100, Start: new(""), Status: scheduleposter.RowOff},
		{PostID: 100, Item: 20, PostedAt: at, Kind: scheduleposter.ChangeRejected, Slot: 1, Reason: strings.Repeat("r", 40)},
		{PostID: 101, Item: 1, PostedAt: at, Kind: scheduleposter.ChangeSame},
		{PostID: 101, Item: 2, PostedAt: at, Kind: scheduleposter.ChangeSame, Slot: 1, Day: day(tuesday), Start: new("21:00"), Status: scheduleposter.RowPlanned},
	}
}

func TestValidateChangesRejectsWhatTheDatabaseRejects(t *testing.T) {
	t.Parallel()

	require.NoError(t, scheduleposter.ValidateChanges(validChanges()))
	require.NoError(t, scheduleposter.ValidateChanges(nil), "항목 0개인 글도 기록한다")

	for name, mutate := range map[string]func([]scheduleposter.Change){
		"글 번호 0":         func(c []scheduleposter.Change) { c[0].PostID = 0 },
		"항목 0":           func(c []scheduleposter.Change) { c[0].Item = 0 },
		"항목 21":          func(c []scheduleposter.Change) { c[0].Item = 21 },
		"같은 글의 같은 항목":    func(c []scheduleposter.Change) { c[1].Item = 1 },
		"모르는 kind":       func(c []scheduleposter.Change) { c[1].Kind = "edited" },
		"사유 없는 거절":       func(c []scheduleposter.Change) { c[3].Reason = "" },
		"사유 있는 반영":       func(c []scheduleposter.Change) { c[1].Reason = "format" },
		"사유 41자":         func(c []scheduleposter.Change) { c[3].Reason = strings.Repeat("r", 41) },
		"칸 101":          func(c []scheduleposter.Change) { c[2].Slot = 101 },
		"음수 칸":           func(c []scheduleposter.Change) { c[2].Slot = -1 },
		"칸과 더한 칸을 함께":    func(c []scheduleposter.Change) { c[1].Slot = 1 },
		"더한 칸의 항목 없음":    func(c []scheduleposter.Change) { c[1].TargetItem = 0 },
		"더한 칸의 글 없음":     func(c []scheduleposter.Change) { c[1].TargetPostID = 0 },
		"더한 칸의 항목 21":    func(c []scheduleposter.Change) { c[1].TargetItem = 21 },
		"대상 없는 시각변경":     func(c []scheduleposter.Change) { c[1].TargetPostID, c[1].TargetItem = 0, 0 },
		"시각이 있는 거절":      func(c []scheduleposter.Change) { c[3].Start = new("") },
		"날짜가 있는 거절":      func(c []scheduleposter.Change) { c[3].Day = day(tuesday) },
		"원문이 있는 거절":      func(c []scheduleposter.Change) { c[3].Text = "歌枠" },
		"더한 칸을 가리키는 거절":  func(c []scheduleposter.Change) { c[3].Slot, c[3].TargetPostID, c[3].TargetItem = 0, 99, 1 },
		"번역이 있는 거절":      func(c []scheduleposter.Change) { c[3].NoteKo = "메모" },
		"날짜 없는 옮김":       func(c []scheduleposter.Change) { c[2].Kind = scheduleposter.ChangeMoved },
		"날짜 없는 주 밖 이동":   func(c []scheduleposter.Change) { c[2].Kind = scheduleposter.ChangeMovedOut },
		"시각 없는 새 칸":      func(c []scheduleposter.Change) { c[0].Start = nil },
		"상태 없는 새 칸":      func(c []scheduleposter.Change) { c[0].Status = "" },
		"날짜 없는 새 칸":      func(c []scheduleposter.Change) { c[0].Day = time.Time{} },
		"시가 30":          func(c []scheduleposter.Change) { c[1].Start = new("30:00") },
		"분이 한 자리":        func(c []scheduleposter.Change) { c[1].Start = new("21:5") },
		"모르는 상태":         func(c []scheduleposter.Change) { c[2].Status = "done" },
		"원문 501자":        func(c []scheduleposter.Change) { c[0].Text += "あ" },
		"제목 1001자":       func(c []scheduleposter.Change) { c[1].TitleKo += "가" },
		"장소 1001자":       func(c []scheduleposter.Change) { c[1].PlaceKo = strings.Repeat("a", 1001) },
		"메모 1001자":       func(c []scheduleposter.Change) { c[1].NoteKo = strings.Repeat("a", 1001) },
		"의심 1001자":       func(c []scheduleposter.Change) { c[1].DoubtKo = strings.Repeat("a", 1001) },
		"참가자 41개":        func(c []scheduleposter.Change) { c[1].WithKo = make([]string, 41) },
		"태그 41개":         func(c []scheduleposter.Change) { c[1].TagsKo = make([]string, 41) },
		"경고 41개":         func(c []scheduleposter.Change) { c[1].Warnings = make([]string, 41) },
		"NUL 문자":         func(c []scheduleposter.Change) { c[0].Text = "a\x00b" },
		"용어의 잘못된 UTF-8":  func(c []scheduleposter.Change) { c[1].Terms = []scheduleposter.Term{{Source: "\xff", Ko: "a"}} },
		"사유의 잘못된 UTF-8":  func(c []scheduleposter.Change) { c[3].Reason = "\xff" },
		"0년 날짜":          func(c []scheduleposter.Change) { c[0].Day = time.Date(0, time.December, 31, 0, 0, 0, 0, time.UTC) },
		"10000년 날짜":      func(c []scheduleposter.Change) { c[0].Day = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC) },
		"시각 문자열의 NUL 문자": func(c []scheduleposter.Change) { c[1].Start = new("\x00") },
	} {
		changes := validChanges()
		mutate(changes)
		require.Error(t, scheduleposter.ValidateChanges(changes), name)
	}
}

func TestValidateChangesLetsSameCarryOnlyWhatThePostSaid(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*scheduleposter.Change){
		"제목":  func(c *scheduleposter.Change) { c.TitleKo = "노래" },
		"참가자": func(c *scheduleposter.Change) { c.WithKo = []string{"쿄코"} },
		"장소":  func(c *scheduleposter.Change) { c.PlaceKo = "본인 채널" },
		"태그":  func(c *scheduleposter.Change) { c.TagsKo = []string{"노래"} },
		"메모":  func(c *scheduleposter.Change) { c.NoteKo = "메모" },
	} {
		changes := validChanges()
		mutate(&changes[5])
		require.Error(t, scheduleposter.ValidateChanges(changes), name)
	}

	rejected := validChanges()

	rejected[3].Status = scheduleposter.RowPlanned
	require.Error(t, scheduleposter.ValidateChanges(rejected), "거절은 상태를 갖지 않는다")
}
