package scheduleposter_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
)

func poster101Slots(t *testing.T, store *scheduleposter.Store) (time.Time, []scheduleposter.Slot) {
	t.Helper()

	schedule, err := store.PosterRows(t.Context(), 101, date(t, "2026-11-12"))
	require.NoError(t, err)

	return schedule.UpdatedAt().Truncate(time.Second), schedule.Slots()
}

func TestStorePolishSlotRewritesThePosterRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmPoster101(t, store)

	row := scheduleRow(t, "2026-11-10", "아침 방송 잡담")

	row.Terms = []scheduleposter.Term{{Source: "朝活", Ko: "아침 방송"}, {Source: "雑談", Ko: "잡담"}}
	row.DoubtKo, row.Warnings = "朝活: 뜻이 분명하지 않음", []string{"T3 원문 숫자 누락"}

	recorded, err := store.RecordRows(ctx, onlyPendingRows(t, store), []scheduleposter.Row{row, scheduleRow(t, "2026-11-11", "노래")}, 0)
	require.NoError(t, err)
	require.True(t, recorded)

	seen, _ := poster101Slots(t, store)
	polished := scheduleposter.Korean{TitleKo: "모닝 잡담", WithKo: []string{"쿄코"}, NoteKo: "짧게 진행"}

	require.NoError(t, store.PolishSlot(ctx, 101, seen, scheduleposter.SlotKey{Ordinal: 1}, polished))

	updated, slots := poster101Slots(t, store)
	require.Equal(t, slots[0].EditedAt.Truncate(time.Second).UTC(), updated.UTC(), "일정의 UpdatedAt은 윤문한 시각을 따른다")
	require.Equal(t, []string{"모닝 잡담", "노래"}, []string{slots[0].TitleKo, slots[1].TitleKo})
	require.Equal(t, []string{"쿄코"}, slots[0].WithKo)
	require.Equal(t, "짧게 진행", slots[0].NoteKo)
	require.Empty(t, slots[0].DoubtKo)
	require.Empty(t, slots[0].Warnings)
	require.False(t, slots[0].EditedAt.IsZero())
	require.True(t, slots[1].EditedAt.IsZero())

	var terms string

	require.NoError(t, pool.QueryRow(ctx, `SELECT terms::text FROM schedule_poster_rows WHERE poster_id = 101 AND ordinal = 1`).Scan(&terms))
	require.JSONEq(t, `[{"source": "雑談", "ko": "잡담", "basis": ""}]`, terms, "고친 글에 남지 않은 표기는 다른 포스터로 퍼지지 않게 뺀다")

	_, titles := publishedTitles(t, store, sophiaChannel, "2026-11-12")
	require.Equal(t, []string{"모닝 잡담", "노래"}, titles)

	err = store.PolishSlot(ctx, 101, updated.Add(-time.Hour), scheduleposter.SlotKey{Ordinal: 2}, scheduleposter.Korean{TitleKo: "노래 방송"})
	require.ErrorIs(t, err, scheduleposter.ErrRevisionConflict, "읽은 뒤에 일정이 바뀌었으면 쓰지 않는다")
}

func TestStorePolishSlotRewritesTheChangeThatSetTheKorean(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		scheduleposter.Change{
			PostID: 13, Item: 1, Kind: scheduleposter.ChangeContentChanged, Slot: 3, Text: "マイクラ", TitleKo: "마인크래프트",
			Terms: []scheduleposter.Term{{Source: "マイクラ", Ko: "마인크래프트"}}, DoubtKo: "줄임말", Warnings: []string{"w"},
		},
		scheduleposter.Change{
			PostID: 13, Item: 2, Kind: scheduleposter.ChangeAdded, Day: date(t, "2026-11-13"), Start: new("25:00"),
			Status: scheduleposter.RowPlanned, Text: "歌枠",
		},
	))
	require.NoError(t, err)
	require.True(t, recorded)

	seen, _ := poster101Slots(t, store)
	require.NoError(t, store.PolishSlot(ctx, 101, seen, scheduleposter.SlotKey{Ordinal: 3}, scheduleposter.Korean{TitleKo: "마크"}))

	seen, slots := poster101Slots(t, store)
	require.Equal(t, "마크", slots[2].TitleKo)
	require.False(t, slots[2].EditedAt.IsZero())
	require.Equal(t, "방송", slots[3].TitleKo, "제목 없이 더한 칸은 기본 제목을 보인다")

	var (
		rowTitle, changeTerms, changeDoubt string
		changeWarnings                     []string
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT title_ko FROM schedule_poster_rows WHERE poster_id = 101 AND ordinal = 3`).Scan(&rowTitle))
	require.Equal(t, "게임", rowTitle, "글이 바꾼 내용의 윤문은 포스터 행을 건드리지 않는다")
	require.NoError(t, pool.QueryRow(ctx, `SELECT terms::text, doubt_ko, warnings FROM schedule_poster_changes WHERE post_id = 13 AND item = 1`).
		Scan(&changeTerms, &changeDoubt, &changeWarnings))
	require.Equal(t, "[]", changeTerms)
	require.Empty(t, changeDoubt)
	require.Empty(t, changeWarnings)

	added := scheduleposter.SlotKey{AddedPostID: 13, AddedItem: 2}
	require.NoError(t, store.PolishSlot(ctx, 101, seen, added, scheduleposter.Korean{TitleKo: "노래 방송", TagsKo: []string{"아카이브 없음"}}))

	seen, slots = poster101Slots(t, store)
	require.Equal(t, "노래 방송", slots[3].TitleKo)
	require.Equal(t, []string{"아카이브 없음"}, slots[3].TagsKo)

	gone := scheduleposter.SlotKey{AddedPostID: 13, AddedItem: 9}
	require.ErrorIs(t, store.PolishSlot(ctx, 101, seen, gone, scheduleposter.Korean{TitleKo: "없음"}), scheduleposter.ErrRevisionConflict)
}

func TestStorePolishSlotRejectsWhatItCannotStore(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _ := newRowsStore(t)
	confirmPoster101(t, store)

	first := scheduleposter.SlotKey{Ordinal: 1}
	chat := scheduleposter.Korean{TitleKo: "잡담"}

	require.ErrorIs(t, store.PolishSlot(ctx, 999, time.Time{}, first, chat), scheduleposter.ErrNotFound)
	require.ErrorIs(t, store.PolishSlot(ctx, 101, time.Time{}, first, chat), scheduleposter.ErrInvalidState, "행 묶음을 저장한 적이 없다")

	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))

	seen, _ := poster101Slots(t, store)

	for name, korean := range map[string]scheduleposter.Korean{
		"빈 제목":      {},
		"제목 1001자":  {TitleKo: strings.Repeat("가", 1001)},
		"빈 참가자":     {TitleKo: "잡담", WithKo: []string{""}},
		"딱지 41개":    {TitleKo: "잡담", TagsKo: strings.Split(strings.Repeat("딱지 ", 41), " ")[:41]},
		"줄바꿈 든 메모":  {TitleKo: "잡담", NoteKo: "첫 줄\n둘째 줄"},
		"잘못된 UTF-8": {TitleKo: "잡담", PlaceKo: "\xff"},
	} {
		require.ErrorIs(t, store.PolishSlot(ctx, 101, seen, first, korean), scheduleposter.ErrInvalidKorean, name)
	}

	_, slots := poster101Slots(t, store)
	require.True(t, slots[0].EditedAt.IsZero())
}

func TestStoreTranslationMemoSavesOnlyOverTheRevisionItRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _ := newRowsStore(t)

	memo, err := store.TranslationMemo(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.Memo{Revision: 1}, memo)

	next, err := store.SaveTranslationMemo(ctx, 1, "朝活은 아침 방송으로 옮긴다\n문장형 제목은 명사구로")
	require.NoError(t, err)
	require.Equal(t, int64(2), next)

	_, err = store.SaveTranslationMemo(ctx, 1, "늦은 저장")
	require.ErrorIs(t, err, scheduleposter.ErrRevisionConflict)

	for name, body := range map[string]string{"4001자": strings.Repeat("가", 4001), "탭": "a\tb", "잘못된 UTF-8": "\xff"} {
		_, err = store.SaveTranslationMemo(ctx, 2, body)
		require.ErrorIs(t, err, scheduleposter.ErrInvalidMemo, name)
	}

	memo, err = store.TranslationMemo(ctx)
	require.NoError(t, err)
	require.Equal(t, "朝活은 아침 방송으로 옮긴다\n문장형 제목은 명사구로", memo.Body)
	require.Equal(t, int64(2), memo.Revision)
	require.False(t, memo.UpdatedAt.IsZero())
}
