package scheduleposter_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	dbtest "github.com/kapu/hololive-dbtest"
)

const tsuzuriChannel = "UCy9mgxB8pn2C4aNK_MPthDQ"

func scheduleRow(t *testing.T, day, title string) scheduleposter.Row {
	t.Helper()

	return scheduleposter.Row{
		Day: date(t, day), Start: "21:00", TZ: "JST", Status: scheduleposter.RowPlanned,
		SourceText: "21:00 " + title, TitleKo: title,
	}
}

func newRowsStore(t *testing.T) (*scheduleposter.Store, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	return store, pool
}

func pendingRows(t *testing.T, store *scheduleposter.Store) []scheduleposter.PendingRows {
	t.Helper()

	pending, err := store.PendingRows(t.Context(), date(t, "2026-11-09"), 10)
	require.NoError(t, err)

	return pending
}

func onlyPendingRows(t *testing.T, store *scheduleposter.Store) scheduleposter.PendingRows {
	t.Helper()

	pending := pendingRows(t, store)
	require.Len(t, pending, 1)

	return pending[0]
}

func recordChat(t *testing.T, store *scheduleposter.Store, item scheduleposter.PendingRows) bool {
	t.Helper()

	recorded, err := store.RecordRows(t.Context(), item, []scheduleposter.Row{scheduleRow(t, "2026-11-10", "잡담")}, 0)
	require.NoError(t, err)

	return recorded
}

func requeueRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `UPDATE schedule_posters SET rows_status = 'pending', rows_error = '', rows_revision = rows_revision + 1
		WHERE id = 101 AND review_status = 'confirmed'`)
	require.NoError(t, err)
}

func poster101RowCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM schedule_poster_rows WHERE poster_id = 101`).Scan(&count))

	return count
}

func poster101RowsState(t *testing.T, pool *pgxpool.Pool) (status, reason string, extractedAt *time.Time) {
	t.Helper()

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT rows_status, rows_error, rows_extracted_at FROM schedule_posters WHERE id = 101`).
		Scan(&status, &reason, &extractedAt))

	return status, reason, extractedAt
}

func publishedTitles(t *testing.T, store *scheduleposter.Store, channelID, day string) (int64, []string) {
	t.Helper()

	schedule, found, err := store.PublishedRows(t.Context(), channelID, date(t, day))
	require.NoError(t, err)

	if !found {
		return 0, nil
	}

	titles := make([]string, 0, len(schedule.Rows))
	for i := range schedule.Rows {
		titles = append(titles, schedule.Rows[i].TitleKo)
	}

	return schedule.PosterID, titles
}

func TestStoreRecordRowsStoresEveryColumn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmPoster101(t, store)

	item := onlyPendingRows(t, store)
	require.Equal(t, scheduleposter.PendingRows{
		ID: 101, ChannelID: sophiaChannel, ImageURL: candidate(101, testPostedAt).ImageURL,
		WeekStart: date(t, "2026-11-09"), WeekEnd: date(t, "2026-11-15"), RowsRevision: 1,
	}, item)

	first := scheduleposter.Row{
		Day: date(t, "2026-11-10"), Start: "25:30", End: "26:00", TZ: "JST", Status: scheduleposter.RowTentative,
		SourceText: "25:30 雑談", Badges: []string{"コラボ"}, TitleKo: "잡담", WithKo: []string{"츠즈리", "쿄코"},
		PlaceKo: "스튜디오", TagsKo: []string{"멤버십 한정"}, NoteKo: "시간 미정",
		Terms: []scheduleposter.Term{{Source: "ホロドリ", Ko: "홀로도리", Basis: "official_ko"}}, DoubtKo: "제목 판독 불확실",
		Warnings: []string{"end_before_start"},
	}
	off := scheduleposter.Row{
		Day: date(t, "2026-11-11"), Status: scheduleposter.RowOff, SourceText: "おやすみ", Badges: []string{"休み", "告知"}, TitleKo: "휴방",
		DoubtKo: "날짜 칸 겹침", Warnings: []string{"badge_shared", "title_guessed"},
	}

	recorded, err := store.RecordRows(ctx, item, []scheduleposter.Row{first, off}, 0)
	require.NoError(t, err)
	require.True(t, recorded)

	schedule, found, err := store.PublishedRows(ctx, sophiaChannel, date(t, "2026-11-12"))
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, schedule.ExtractedAt.IsZero())
	require.True(t, schedule.PostedAt.Equal(testPostedAt))
	require.Equal(t, scheduleposter.PublishedSchedule{
		PosterID: 101, WeekStart: date(t, "2026-11-09"), WeekEnd: date(t, "2026-11-15"), ExtractedAt: schedule.ExtractedAt, PostedAt: schedule.PostedAt,
		Rows: []scheduleposter.Row{
			{
				Ordinal: 1, Day: first.Day, Start: "25:30", End: "26:00", TZ: "JST", Status: scheduleposter.RowTentative,
				SourceText: "25:30 雑談", Badges: []string{"コラボ"},
				TitleKo: "잡담", WithKo: []string{"츠즈리", "쿄코"}, PlaceKo: "스튜디오", TagsKo: []string{"멤버십 한정"}, NoteKo: "시간 미정",
			},
			{
				Ordinal: 2, Day: off.Day, Status: scheduleposter.RowOff, SourceText: "おやすみ", Badges: []string{"休み", "告知"},
				TitleKo: "휴방", WithKo: []string{}, TagsKo: []string{},
			},
		},
		Changes: []scheduleposter.Change{},
	}, schedule, "날짜는 DateOnly로, 목록은 text[]로, nil 목록은 빈 배열로 저장되고 판독 메모·경고·용어는 읽지 않는다")

	var (
		nullTimes          int
		firstTerms, noTerm string
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE start_time IS NULL AND end_time IS NULL),
		max(terms::text) FILTER (WHERE ordinal = 1), max(terms::text) FILTER (WHERE ordinal = 2)
		FROM schedule_poster_rows WHERE poster_id = 101`).Scan(&nullTimes, &firstTerms, &noTerm))
	require.Equal(t, 1, nullTimes, "빈 시각은 NULL로 저장된다")
	require.JSONEq(t, `[{"source": "ホロドリ", "ko": "홀로도리", "basis": "official_ko"}]`, firstTerms)
	require.JSONEq(t, `[]`, noTerm)
	require.Equal(t, []storedRow{
		{sourceText: "25:30 雑談", badges: []string{"コラボ"}, doubtKo: "제목 판독 불확실", warnings: []string{"end_before_start"}},
		{sourceText: "おやすみ", badges: []string{"休み", "告知"}, doubtKo: "날짜 칸 겹침", warnings: []string{"badge_shared", "title_guessed"}},
	}, poster101UnpublishedColumns(t, pool), "카드에 쓰지 않는 칸도 행마다 제자리에 저장된다")
}

type storedRow struct {
	sourceText, doubtKo string
	badges, warnings    []string
}

func poster101UnpublishedColumns(t *testing.T, pool *pgxpool.Pool) []storedRow {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT source_text, badges, doubt_ko, warnings FROM schedule_poster_rows WHERE poster_id = 101 ORDER BY ordinal`)
	require.NoError(t, err)

	stored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedRow, error) {
		var item storedRow

		return item, row.Scan(&item.sourceText, &item.badges, &item.doubtKo, &item.warnings)
	})
	require.NoError(t, err)

	return stored
}

func TestStoreRecordRowsReplacesTheBundleWithoutTouchingRevision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmPoster101(t, store)

	before, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))

	after, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision, "행 쓰기는 Console의 revision 조건을 깨지 않는다")
	require.True(t, before.UpdatedAt.Equal(after.UpdatedAt))
	require.Empty(t, pendingRows(t, store))

	requeueRows(t, pool)

	_, titles := publishedTitles(t, store, sophiaChannel, "2026-11-12")
	require.Equal(t, []string{"잡담"}, titles, "재추출을 기다리는 동안 이전 묶음을 게시한다")

	again := onlyPendingRows(t, store)
	require.EqualValues(t, 2, again.RowsRevision)

	recorded, err := store.RecordRows(ctx, again, []scheduleposter.Row{scheduleRow(t, "2026-11-13", "노래"), scheduleRow(t, "2026-11-14", "게임")}, 0)
	require.NoError(t, err)
	require.True(t, recorded)

	_, titles = publishedTitles(t, store, sophiaChannel, "2026-11-12")
	require.Equal(t, []string{"노래", "게임"}, titles, "새 묶음이 이전 묶음을 통째로 바꾼다")
	require.Equal(t, 2, poster101RowCount(t, pool))
}

func TestStoreRecordRowsDiscardsResultsAfterWeekUpdate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	revision := confirmPoster101(t, store)
	item := onlyPendingRows(t, store)

	recorded, err := store.RecordRows(ctx, item, []scheduleposter.Row{scheduleRow(t, "2026-11-10", "잡담")}, 1)
	require.NoError(t, err)
	require.True(t, recorded)

	_, err = store.UpdateWeek(ctx, 101, revision, date(t, "2026-11-16"), date(t, "2026-11-22"))
	require.NoError(t, err)
	require.Zero(t, poster101RowCount(t, pool), "틀린 주로 뽑은 날짜를 게시하지 않는다")

	var shift int

	require.NoError(t, pool.QueryRow(ctx, `SELECT rows_day_shift FROM schedule_posters WHERE id = 101`).Scan(&shift))
	require.Zero(t, shift, "묶음을 지우면 하루 밀기 값도 되돌린다")

	status, _, extractedAt := poster101RowsState(t, pool)
	require.Equal(t, "pending", status)
	require.Nil(t, extractedAt)
	require.False(t, recordChat(t, store, item))

	recorded, err = store.RecordRowsFailure(ctx, item, "translation_failed")
	require.NoError(t, err)
	require.False(t, recorded)

	moved := onlyPendingRows(t, store)
	require.Equal(t, date(t, "2026-11-16"), moved.WeekStart)
	require.Greater(t, moved.RowsRevision, item.RowsRevision)
}

func TestStoreRecordRowsDiscardsResultsAfterReject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	revision := confirmPoster101(t, store)
	item := onlyPendingRows(t, store)

	require.True(t, recordChat(t, store, item))

	_, err := store.Reject(ctx, 101, revision)
	require.NoError(t, err)
	require.Zero(t, poster101RowCount(t, pool))

	status, _, extractedAt := poster101RowsState(t, pool)
	require.Equal(t, "pending", status)
	require.Nil(t, extractedAt)
	require.Empty(t, pendingRows(t, store), "거절한 포스터는 추출하지 않는다")
	require.False(t, recordChat(t, store, item))

	recorded, err := store.RecordRowsFailure(ctx, item, "translation_failed")
	require.NoError(t, err)
	require.False(t, recorded)
}

func TestStoreRecordRowsDiscardsResultsAfterRowsRequest(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmedPosterWithRows(t, store, 401, sophiaChannel, time.Date(2099, time.November, 9, 3, 0, 0, 0, time.UTC), "2099-11-09", "2099-11-15")

	request := func() {
		t.Helper()

		poster, err := store.Get(ctx, 401)
		require.NoError(t, err)

		_, err = store.RequestRows(ctx, 401, poster.Revision)
		require.NoError(t, err)
	}
	pending := func() scheduleposter.PendingRows {
		t.Helper()

		items, err := store.PendingRows(ctx, date(t, "2099-11-09"), 10)
		require.NoError(t, err)
		require.Len(t, items, 1)

		return items[0]
	}
	record := func(item scheduleposter.PendingRows) bool {
		t.Helper()

		recorded, err := store.RecordRows(ctx, item, []scheduleposter.Row{scheduleRow(t, "2099-11-10", "노래")}, 0)
		require.NoError(t, err)

		return recorded
	}

	request()

	item := pending()

	recorded, err := store.RecordRowsFailure(ctx, item, "extraction_failed")
	require.NoError(t, err)
	require.True(t, recorded)

	request()
	require.False(t, record(item), "재예약 전에 읽은 입력(용어집·이전 표기)으로 뽑은 결과는 버린다")

	recorded, err = store.RecordRowsFailure(ctx, item, "translation_failed")
	require.NoError(t, err)
	require.False(t, recorded)

	var titles []string

	require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(title_ko ORDER BY ordinal) FROM schedule_poster_rows WHERE poster_id = 401`).Scan(&titles))
	require.Equal(t, []string{"잡담"}, titles, "버린 결과는 이전 묶음을 바꾸지 않는다")
	require.True(t, record(pending()))
}

func TestStoreRecordRowsFailureKeepsThePreviousBundle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	revision := confirmPoster101(t, store)

	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))

	_, _, extractedAt := poster101RowsState(t, pool)
	require.NotNil(t, extractedAt)

	requeueRows(t, pool)

	item := onlyPendingRows(t, store)

	recorded, err := store.RecordRowsFailure(ctx, item, "extraction_failed")
	require.NoError(t, err)
	require.True(t, recorded)

	status, reason, failedExtractedAt := poster101RowsState(t, pool)
	require.Equal(t, "failed", status)
	require.Equal(t, "extraction_failed", reason)
	require.Equal(t, extractedAt, failedExtractedAt, "게시 중인 묶음의 시각을 그대로 둔다")
	require.Empty(t, pendingRows(t, store), "실패한 포스터는 재예약 전까지 다시 뽑지 않는다")

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, revision, poster.Revision)

	_, titles := publishedTitles(t, store, sophiaChannel, "2026-11-10")
	require.Equal(t, []string{"잡담"}, titles)

	recorded, err = store.RecordRowsFailure(ctx, item, "extraction_failed")
	require.NoError(t, err)
	require.False(t, recorded, "대기 상태가 아니면 기록하지 않는다")
}

func insertConfirmedWithRows(t *testing.T, store *scheduleposter.Store, pool *pgxpool.Pool, id int64, start, end string, postedAt time.Time) {
	t.Helper()

	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt,
		review_status, week_start, week_end, reviewed_at, reviewed_by)
		VALUES ($1, $2, '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/b.jpg?name=orig', $3, '',
		        'confirmed', $4, $5, now(), 'admin')`, id, sophiaChannel, postedAt, start, end)
	require.NoError(t, err)

	recordRowsFor(t, store, id, start, strconv.FormatInt(id, 10))
}

func TestStorePublishedRowsFallsBackToTheUpcomingWeekWhenNothingCoversTheDay(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmPoster101(t, store)
	insertConfirmedWithRows(t, store, pool, 502, "2026-11-16", "2026-11-22", testPostedAt.AddDate(0, 0, 6))

	posterID, titles := publishedTitles(t, store, sophiaChannel, "2026-11-15")
	require.EqualValues(t, 502, posterID, "오늘을 덮는 확정본에 행이 없으면 내일 시작하는 확정본을 고른다")
	require.Equal(t, []string{"502"}, titles)

	for _, id := range []int64{101, 502} {
		got, err := store.PosterRows(ctx, id, date(t, "2026-11-15"))
		require.NoError(t, err)
		require.EqualValues(t, 502, got.TodayPosterID, id)
	}
}

func TestStorePublishedRowsPrefersThePosterCoveringTheDayOverTheUpcomingWeek(t *testing.T) {
	t.Parallel()

	store, pool := newRowsStore(t)
	confirmPoster101(t, store)
	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))
	insertConfirmedWithRows(t, store, pool, 502, "2026-11-16", "2026-11-22", testPostedAt.AddDate(0, 0, 6))

	for day, want := range map[string]int64{"2026-11-14": 101, "2026-11-16": 502} {
		posterID, _ := publishedTitles(t, store, sophiaChannel, day)
		require.Equal(t, want, posterID, day)
	}
}

func TestStorePublishedRowsPicksTheNearestUpcomingWeekWithinSevenDays(t *testing.T) {
	t.Parallel()

	store, pool := newRowsStore(t)
	insertConfirmedWithRows(t, store, pool, 504, "2026-11-16", "2026-11-22", testPostedAt.AddDate(0, 0, 5))
	insertConfirmedWithRows(t, store, pool, 502, "2026-11-16", "2026-11-22", testPostedAt.AddDate(0, 0, 6))
	insertConfirmedWithRows(t, store, pool, 503, "2026-11-20", "2026-11-26", testPostedAt.AddDate(0, 0, 7))

	for day, want := range map[string]int64{"2026-11-08": 0, "2026-11-09": 502, "2026-11-14": 502, "2026-11-19": 502} {
		posterID, _ := publishedTitles(t, store, sophiaChannel, day)
		require.Equal(t, want, posterID, "7일 안에 시작하는 가장 가까운 주의 늦게 올린 확정본을 고른다: %s", day)
	}
}

func TestStorePublishedRowsJudgesTheUpcomingWeekByTheShiftedStart(t *testing.T) {
	t.Parallel()

	store, pool := newRowsStore(t)
	confirmPoster101(t, store)
	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))

	for shift, days := range map[int]map[string]int64{
		1:  {"2026-11-02": 0, "2026-11-03": 101, "2026-11-09": 101},
		-1: {"2026-10-31": 0, "2026-11-01": 101, "2026-11-08": 101},
	} {
		_, err := pool.Exec(t.Context(), `UPDATE schedule_posters SET rows_day_shift = $1 WHERE id = 101`, shift)
		require.NoError(t, err)

		for day, want := range days {
			posterID, _ := publishedTitles(t, store, sophiaChannel, day)
			require.Equal(t, want, posterID, "shift %d, %s", shift, day)
		}
	}
}

func TestStorePublishedRowsPicksTheLatestWeekCoveringTheDay(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _ := newRowsStore(t)
	confirmPoster101(t, store)

	poster, err := store.Get(ctx, 102)
	require.NoError(t, err)

	_, err = store.Confirm(ctx, 102, poster.Revision, date(t, "2026-11-05"), date(t, "2026-11-11"))
	require.NoError(t, err)

	pending := pendingRows(t, store)
	require.Len(t, pending, 2)

	for _, item := range pending {
		require.True(t, recordChat(t, store, item))
	}

	for day, want := range map[string]int64{"2026-10-28": 0, "2026-11-06": 102, "2026-11-10": 101, "2026-11-15": 101, "2026-11-16": 0} {
		posterID, _ := publishedTitles(t, store, sophiaChannel, day)
		require.Equal(t, want, posterID, "주 범위가 그날을 덮는 확정본 가운데 주 시작일이 늦은 것을 고른다: %s", day)
	}

	posterID, _ := publishedTitles(t, store, tsuzuriChannel, "2026-11-10")
	require.Zero(t, posterID, "다른 멤버의 포스터는 고르지 않는다")
}

func TestStoreWeekTermsSkipsOwnPosterInSavedOrder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _ := newRowsStore(t)
	jst := time.FixedZone("JST", 9*60*60)
	at := func(day int) time.Time { return time.Date(2026, time.November, day, 12, 0, 0, 0, jst) }

	other := candidate(301, at(9))

	other.ChannelID = tsuzuriChannel

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, at(9)), candidate(102, at(5)), candidate(103, at(1)), other}, false)
	require.NoError(t, err)

	for _, week := range []struct {
		id         int64
		start, end string
	}{
		{101, "2026-11-09", "2026-11-15"},
		{102, "2026-11-05", "2026-11-11"},
		{103, "2026-10-26", "2026-11-01"},
		{301, "2026-11-09", "2026-11-15"},
	} {
		_, err = store.Confirm(ctx, week.id, 1, date(t, week.start), date(t, week.end))
		require.NoError(t, err)
	}

	pending, err := store.PendingRows(ctx, date(t, "2026-10-26"), 10)
	require.NoError(t, err)

	byID := map[int64]scheduleposter.PendingRows{}

	for _, item := range pending {
		byID[item.ID] = item
	}

	term := func(source, ko string) scheduleposter.Term {
		return scheduleposter.Term{Source: source, Ko: ko, Basis: "translated"}
	}

	for _, saved := range []struct {
		id    int64
		terms [][]scheduleposter.Term
	}{
		{301, [][]scheduleposter.Term{{term("B", "비"), term("A", "에이")}, {term("C", "씨")}}},
		{103, [][]scheduleposter.Term{{term("X", "엑스")}}},
		{101, [][]scheduleposter.Term{{term("SELF", "자기")}}},
		{102, [][]scheduleposter.Term{{term("A", "아")}}},
	} {
		rows := make([]scheduleposter.Row, 0, len(saved.terms))
		for _, terms := range saved.terms {
			row := scheduleRow(t, "2026-11-10", "잡담")

			row.Terms = terms
			rows = append(rows, row)
		}

		recorded, recordErr := store.RecordRows(ctx, byID[saved.id], rows, 0)
		require.NoError(t, recordErr)
		require.True(t, recorded)
	}

	terms, err := store.WeekTerms(ctx, byID[101])
	require.NoError(t, err)
	require.Equal(t, []scheduleposter.Term{{Source: "B", Ko: "비"}, {Source: "A", Ko: "에이"}, {Source: "C", Ko: "씨"}, {Source: "A", Ko: "아"}}, terms,
		"자기 포스터와 주가 겹치지 않는 포스터는 빼고, 먼저 저장한 포스터·행·항목 순서로 돌려준다")
}

func validRow() scheduleposter.Row {
	return scheduleposter.Row{
		Day: time.Date(2026, time.November, 10, 0, 0, 0, 0, time.UTC), Start: "9:00", End: "29:59",
		Status: scheduleposter.RowPlanned, SourceText: "9:00 歌枠", TitleKo: "노래",
	}
}

func TestValidateRowsRejectsWhatTheDatabaseRejects(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*scheduleposter.Row){
		"모르는 상태":     func(r *scheduleposter.Row) { r.Status = "done" },
		"시각 형식":      func(r *scheduleposter.Row) { r.Start = "21:5" },
		"분이 60 이상":   func(r *scheduleposter.Row) { r.End = "21:60" },
		"세 자리 시":     func(r *scheduleposter.Row) { r.Start = "100:00" },
		"시가 30 이상":   func(r *scheduleposter.Row) { r.Start = "30:00" },
		"빈 원문":       func(r *scheduleposter.Row) { r.SourceText = "" },
		"빈 제목":       func(r *scheduleposter.Row) { r.TitleKo = "" },
		"제목 1001자":   func(r *scheduleposter.Row) { r.TitleKo = strings.Repeat("가", 1001) },
		"메모 1001자":   func(r *scheduleposter.Row) { r.NoteKo = strings.Repeat("a", 1001) },
		"의심 1001자":   func(r *scheduleposter.Row) { r.DoubtKo = strings.Repeat("a", 1001) },
		"장소 1001자":   func(r *scheduleposter.Row) { r.PlaceKo = strings.Repeat("a", 1001) },
		"원문 1001자":   func(r *scheduleposter.Row) { r.SourceText = strings.Repeat("あ", 1001) },
		"시간대 41자":    func(r *scheduleposter.Row) { r.TZ = strings.Repeat("a", 41) },
		"원래 시간대 41자": func(r *scheduleposter.Row) { r.SourceZone = strings.Repeat("a", 41) },
		"원래 시간대 NUL": func(r *scheduleposter.Row) { r.SourceZone = "P\x00T" },
		"배지 41개":     func(r *scheduleposter.Row) { r.Badges = make([]string, 41) },
		"참가자 41개":    func(r *scheduleposter.Row) { r.WithKo = make([]string, 41) },
		"태그 41개":     func(r *scheduleposter.Row) { r.TagsKo = make([]string, 41) },
		"경고 41개":     func(r *scheduleposter.Row) { r.Warnings = make([]string, 41) },
		"NUL 문자":     func(r *scheduleposter.Row) { r.NoteKo = "a\x00b" },
		"잘못된 UTF-8":  func(r *scheduleposter.Row) { r.WithKo = []string{"\xff"} },
		"용어의 NUL 문자": func(r *scheduleposter.Row) { r.Terms = []scheduleposter.Term{{Source: "\x00", Ko: "a"}} },
		"날짜 없음":      func(r *scheduleposter.Row) { r.Day = time.Time{} },
		"0년":         func(r *scheduleposter.Row) { r.Day = time.Date(0, time.December, 31, 0, 0, 0, 0, time.UTC) },
		"10000년":     func(r *scheduleposter.Row) { r.Day = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC) },
	} {
		row := validRow()
		mutate(&row)
		require.Error(t, scheduleposter.ValidateRows([]scheduleposter.Row{row}), name)
	}

	require.Error(t, scheduleposter.ValidateRows(nil))

	rows := make([]scheduleposter.Row, scheduleposter.MaxRows+1)
	for i := range rows {
		rows[i] = validRow()
	}

	require.Error(t, scheduleposter.ValidateRows(rows))
}

func TestStoreRecordRowsAcceptsTheDatabaseLimitsAndRejectsBeyondBeforeWriting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)

	confirmPoster101(t, store)

	item := onlyPendingRows(t, store)
	edge := validRow()

	edge.TitleKo = strings.Repeat("가", 1000)
	edge.SourceText = strings.Repeat("あ", 1000)
	edge.TZ = strings.Repeat("a", 40)
	edge.SourceZone = strings.Repeat("a", 40)
	edge.Badges = make([]string, 40)
	edge.WithKo = make([]string, 40)
	edge.TagsKo = make([]string, 40)
	edge.Warnings = make([]string, 40)

	rows := make([]scheduleposter.Row, scheduleposter.MaxRows)
	for i := range rows {
		rows[i] = edge
	}

	require.NoError(t, scheduleposter.ValidateRows(rows))

	over := edge

	over.TitleKo += "가"

	recorded, err := store.RecordRows(ctx, item, append(rows[:scheduleposter.MaxRows-1:scheduleposter.MaxRows-1], over), 0)
	require.Error(t, err)
	require.False(t, recorded)
	require.Zero(t, poster101RowCount(t, pool))

	status, _, _ := poster101RowsState(t, pool)
	require.Equal(t, "pending", status, "검증에 걸린 묶음은 상태도 바꾸지 않는다")

	recorded, err = store.RecordRows(ctx, item, rows, 0)
	require.NoError(t, err)
	require.True(t, recorded, "DB는 Go 검증을 통과한 상한 값을 받는다")
	require.Equal(t, scheduleposter.MaxRows, poster101RowCount(t, pool))
}

func TestStoreRecordRowsDateRangeMatchesTheDatabase(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)

	confirmPoster101(t, store)

	_, err := time.Parse(time.DateOnly, "0000-01-01")
	require.NoError(t, err, "모델이 0년을 쓰면 Go 날짜 해석은 받는다")

	_, err = pool.Exec(ctx, `SELECT '0000-01-01'::date`)
	require.Error(t, err, "DB date는 0년을 받지 않는다")

	first, last := validRow(), validRow()

	first.Day = time.Date(1, time.January, 2, 0, 0, 0, 0, time.UTC)
	last.Day = time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)

	recorded, err := store.RecordRows(ctx, onlyPendingRows(t, store), []scheduleposter.Row{first, last}, 0)
	require.NoError(t, err)
	require.True(t, recorded, "Go 검증이 받는 날짜 경계는 DB도 받는다")

	var days []string

	require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(day::text ORDER BY ordinal) FROM schedule_poster_rows WHERE poster_id = 101`).Scan(&days))
	require.Equal(t, []string{"0001-01-02", "9999-12-31"}, days)
}

func TestStoreRejectedPosterKeepsRowsHiddenAfterAnEarlierImageRejects(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)

	confirmPoster101(t, store)

	_, err := store.Confirm(ctx, 102, 1, date(t, "2026-11-05"), date(t, "2026-11-11"))
	require.NoError(t, err)

	byID := map[int64]scheduleposter.PendingRows{}

	for _, item := range pendingRows(t, store) {
		byID[item.ID] = item
	}

	row := scheduleRow(t, "2026-11-10", "잡담")

	row.Terms = []scheduleposter.Term{{Source: "ホロドリ", Ko: "홀로도리", Basis: "official_ko"}}

	recorded, err := store.RecordRows(ctx, byID[101], []scheduleposter.Row{row}, 0)
	require.NoError(t, err)
	require.True(t, recorded)

	_, err = pool.Exec(ctx, `UPDATE schedule_posters
		SET review_status = 'rejected', reviewed_at = now(), reviewed_by = 'admin', revision = revision + 1, updated_at = now()
		WHERE id = 101`)
	require.NoError(t, err, "행 열을 모르는 이전 image의 거절도 저장한 행이 있는 확정본을 거절한다")
	require.Equal(t, 1, poster101RowCount(t, pool))

	posterID, _ := publishedTitles(t, store, sophiaChannel, "2026-11-10")
	require.Zero(t, posterID, "거절한 포스터의 남은 묶음은 게시하지 않는다")

	pending := pendingRows(t, store)
	require.Len(t, pending, 1)
	require.EqualValues(t, 102, pending[0].ID, "거절한 포스터는 다시 뽑지 않는다")

	terms, err := store.WeekTerms(ctx, byID[102])
	require.NoError(t, err)
	require.Empty(t, terms, "거절한 포스터의 표기를 같은 주 표기로 넘기지 않는다")
}

func TestStorePublishedRowsFallsBackToAnOverlappingPosterWhileTheLatestHasNoRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _ := newRowsStore(t)

	confirmPoster101(t, store)

	_, err := store.Confirm(ctx, 102, 1, date(t, "2026-11-05"), date(t, "2026-11-11"))
	require.NoError(t, err)

	byID := map[int64]scheduleposter.PendingRows{}

	for _, item := range pendingRows(t, store) {
		byID[item.ID] = item
	}

	require.True(t, recordChat(t, store, byID[102]))

	posterID, titles := publishedTitles(t, store, sophiaChannel, "2026-11-10")
	require.EqualValues(t, 102, posterID, "가장 최근 확정본이 추출을 기다리는 동안 겹치는 이전 확정본의 행을 보여 준다")
	require.Equal(t, []string{"잡담"}, titles)

	recorded, err := store.RecordRowsFailure(ctx, byID[101], "extraction_failed")
	require.NoError(t, err)
	require.True(t, recorded)

	posterID, _ = publishedTitles(t, store, sophiaChannel, "2026-11-10")
	require.EqualValues(t, 102, posterID, "가장 최근 확정본의 추출이 실패해도 같다")

	posterID, _ = publishedTitles(t, store, sophiaChannel, "2026-11-12")
	require.Zero(t, posterID, "이전 확정본이 덮지 않는 날은 없음이다")
}

func TestStoreRecordRowsKeepsTheSourceZoneAndTheDayShift(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmPoster101(t, store)

	item := onlyPendingRows(t, store)
	converted := scheduleRow(t, "2026-11-16", "잡담")

	converted.SourceZone = "PT"

	for _, shift := range []int{-2, 2} {
		recorded, err := store.RecordRows(ctx, item, []scheduleposter.Row{converted}, shift)
		require.Error(t, err, shift)
		require.False(t, recorded)
	}

	recorded, err := store.RecordRows(ctx, item, []scheduleposter.Row{converted}, 1)
	require.NoError(t, err)
	require.True(t, recorded)

	for day, want := range map[string]bool{"2026-11-02": false, "2026-11-10": true, "2026-11-16": true, "2026-11-17": false} {
		schedule, found, publishedErr := store.PublishedRows(ctx, sophiaChannel, date(t, day))
		require.NoError(t, publishedErr)
		require.Equal(t, want, found, "!스케쥴은 하루 민 주 범위로 포스터를 고른다: %s", day)

		if found {
			require.Equal(t, 1, schedule.RowsDayShift)
			require.Equal(t, "PT", schedule.Rows[0].SourceZone)
			require.Equal(t, "JST", schedule.Rows[0].TZ)
		}
	}

	requeueRows(t, pool)

	failed, err := store.RecordRowsFailure(ctx, onlyPendingRows(t, store), "extraction_failed")
	require.NoError(t, err)
	require.True(t, failed)

	schedule, found, err := store.PublishedRows(ctx, sophiaChannel, date(t, "2026-11-16"))
	require.NoError(t, err)
	require.True(t, found, "실패 기록은 이전 묶음과 하루 밀기 값을 그대로 둔다")
	require.Equal(t, 1, schedule.RowsDayShift)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	_, err = store.Reject(ctx, 101, poster.Revision)
	require.NoError(t, err)

	var shift int

	require.NoError(t, pool.QueryRow(ctx, `SELECT rows_day_shift FROM schedule_posters WHERE id = 101`).Scan(&shift))
	require.Zero(t, shift, "거절로 묶음을 지우면 하루 밀기 값도 되돌린다")
}

func TestStorePublishedRowsPrefersTheLaterPosterOfTheSameWeekWhateverTheDayShift(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)

	confirmPoster101(t, store)

	converted := scheduleRow(t, "2026-11-16", "원래 일정")

	converted.SourceZone = "PT"

	recorded, err := store.RecordRows(ctx, onlyPendingRows(t, store), []scheduleposter.Row{converted}, 1)
	require.NoError(t, err)
	require.True(t, recorded)

	insertConfirmedWithRows(t, store, pool, 501, "2026-11-09", "2026-11-15", testPostedAt.AddDate(0, 0, 1))
	insertConfirmedWithRows(t, store, pool, 502, "2026-11-16", "2026-11-22", testPostedAt.AddDate(0, 0, 7))

	for day, want := range map[string]int64{"2026-11-10": 501, "2026-11-15": 501, "2026-11-16": 502} {
		posterID, _ := publishedTitles(t, store, sophiaChannel, day)
		require.Equal(t, want, posterID, "같은 주의 늦게 올린 확정본과 다음 주 확정본이 하루 민 이전 확정본보다 앞선다: %s", day)
	}

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET review_status = 'rejected' WHERE id = 502`)
	require.NoError(t, err)

	posterID, titles := publishedTitles(t, store, sophiaChannel, "2026-11-16")
	require.EqualValues(t, 101, posterID, "다음 주 확정본이 없으면 민 범위의 마지막 날은 하루 민 확정본이 덮는다")
	require.Equal(t, []string{"원래 일정"}, titles)
}

func TestStorePosterRowsMatchesWhatTheBotPublishes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("20:00")},
		scheduleposter.Change{PostID: 13, Item: 2, Kind: scheduleposter.ChangeCancelled, Slot: 2, Status: scheduleposter.RowCancelled},
	))
	require.NoError(t, err)
	require.True(t, recorded)

	published := publishedSophia(t, store)

	got, err := store.PosterRows(ctx, 101, date(t, "2026-11-12"))
	require.NoError(t, err)
	require.True(t, got.Confirmed)
	require.EqualValues(t, 101, got.TodayPosterID)
	require.True(t, published.ExtractedAt.Equal(got.ExtractedAt))
	require.True(t, published.UpdatedAt().Equal(got.UpdatedAt()))

	slots := got.Slots()
	for i := range slots {
		require.Empty(t, slots[i].DoubtKo)
		require.Empty(t, slots[i].Warnings)

		slots[i].Warnings = nil
	}

	require.Equal(t, published.Slots(), slots, "!스케쥴 카드와 같은 칸을 돌려준다(PublishedRows는 경고를 읽지 않는다)")
	require.True(t, slots[0].TimeChanged)
	require.Equal(t, scheduleposter.RowCancelled, slots[1].Status)
}

func TestStorePosterRowsKeepsWarningsAndTheDayShift(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _ := newRowsStore(t)
	confirmPoster101(t, store)

	row := scheduleRow(t, "2026-11-16", "잡담")

	row.SourceZone = "PT"
	row.DoubtKo = "제목 판독 불확실"
	row.Warnings = []string{"E1 날짜: 주 범위 밖"}
	row.Terms = []scheduleposter.Term{{Source: "ホロドリ", Ko: "홀로도리", Basis: "official_ko"}}

	recorded, err := store.RecordRows(ctx, onlyPendingRows(t, store), []scheduleposter.Row{row}, 1)
	require.NoError(t, err)
	require.True(t, recorded)

	got, err := store.PosterRows(ctx, 101, date(t, "2026-11-16"))
	require.NoError(t, err)
	require.EqualValues(t, 101, got.TodayPosterID, "하루 민 게시 범위의 마지막 날이다")
	require.Equal(t, 1, got.RowsDayShift)
	require.Equal(t, date(t, "2026-11-09"), got.WeekStart)
	require.Equal(t, date(t, "2026-11-15"), got.WeekEnd)
	require.Len(t, got.Rows, 1)
	require.Equal(t, "제목 판독 불확실", got.Rows[0].DoubtKo)
	require.Equal(t, []string{"E1 날짜: 주 범위 밖"}, got.Rows[0].Warnings)
	require.Equal(t, "PT", got.Rows[0].SourceZone)
	require.Nil(t, got.Rows[0].Terms)

	got, err = store.PosterRows(ctx, 101, date(t, "2026-11-02"))
	require.NoError(t, err)
	require.Zero(t, got.TodayPosterID, "하루 민 게시 범위가 7일 넘게 남은 날은 게시하지 않는다")
	require.Len(t, got.Rows, 1, "게시하지 않는 날에도 행은 보여 준다")

	pending, err := store.PosterRows(ctx, 102, date(t, "2026-11-12"))
	require.NoError(t, err)
	require.False(t, pending.Confirmed)
	require.Zero(t, pending.TodayPosterID)
	require.Empty(t, pending.Rows)

	_, err = store.PosterRows(ctx, 999, date(t, "2026-11-12"))
	require.ErrorIs(t, err, scheduleposter.ErrNotFound)
}

func TestStorePosterRowsPointsAtThePosterPublishedToday(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmPoster101(t, store)
	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))

	todayPosterID := func(id int64, day string) int64 {
		t.Helper()

		got, err := store.PosterRows(ctx, id, date(t, day))
		require.NoError(t, err)

		return got.TodayPosterID
	}

	require.EqualValues(t, 101, todayPosterID(101, "2026-11-12"))

	replacementPoster(t, store, pool, 501, testPostedAt.AddDate(0, 0, 1))

	_, err := pool.Exec(ctx, `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt,
		review_status, week_start, week_end, reviewed_at, reviewed_by)
		VALUES (502, $1, '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/c.jpg?name=orig', $2, '',
		        'confirmed', '2026-11-16', '2026-11-22', now(), 'admin')`, sophiaChannel, testPostedAt.AddDate(0, 0, 6))
	require.NoError(t, err)
	recordRowsFor(t, store, 502, "2026-11-16", "잡담")

	require.EqualValues(t, 501, todayPosterID(101, "2026-11-12"), "같은 주에 늦게 올린 수정본이 게시된다")
	require.EqualValues(t, 501, todayPosterID(502, "2026-11-12"), "다음 주 포스터는 이번 주 게시 대상이 아니다")
	require.EqualValues(t, 502, todayPosterID(502, "2026-11-16"))

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET review_status = 'rejected', revision = revision + 1 WHERE id = 501`)
	require.NoError(t, err)

	rejected, err := store.PosterRows(ctx, 501, date(t, "2026-11-12"))
	require.NoError(t, err)
	require.False(t, rejected.Confirmed)
	require.Zero(t, rejected.TodayPosterID)
	require.Empty(t, rejected.Rows, "행을 지우지 않은 이전 image의 거절이 남긴 묶음도 보여 주지 않는다")
	require.EqualValues(t, 101, todayPosterID(101, "2026-11-12"))
}
