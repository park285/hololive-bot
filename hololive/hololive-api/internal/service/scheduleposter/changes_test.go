package scheduleposter_test

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
)

var jst = time.FixedZone("JST", 9*60*60)

func jstAt(day, hour, minute int) time.Time {
	return time.Date(2026, time.November, day, hour, minute, 0, 0, jst)
}

func post(id int64, postedAt time.Time) scheduleposter.Post {
	return scheduleposter.Post{ID: id, PostedAt: postedAt, Body: "今日は20時から"}
}

func publishedSophia(t *testing.T, store *scheduleposter.Store) scheduleposter.PublishedSchedule {
	t.Helper()

	schedule, found, err := store.PublishedRows(t.Context(), sophiaChannel, date(t, "2026-11-12"))
	require.NoError(t, err)
	require.True(t, found)

	return schedule
}

func newChangesStore(t *testing.T) (*scheduleposter.Store, *pgxpool.Pool, int64, scheduleposter.PublishedSchedule) {
	t.Helper()

	store, pool := newRowsStore(t)
	revision := confirmPoster101(t, store)

	recorded, err := store.RecordRows(t.Context(), onlyPendingRows(t, store), []scheduleposter.Row{
		scheduleRow(t, "2026-11-10", "잡담"), scheduleRow(t, "2026-11-11", "노래"), scheduleRow(t, "2026-11-12", "게임"),
	}, 0)
	require.NoError(t, err)
	require.True(t, recorded)

	return store, pool, revision, publishedSophia(t, store)
}

func pendingPostIDs(t *testing.T, store *scheduleposter.Store, schedule scheduleposter.PublishedSchedule) []int64 {
	t.Helper()

	posts, err := store.PendingPosts(t.Context(), schedule, 10)
	require.NoError(t, err)

	ids := make([]int64, 0, len(posts))
	for _, item := range posts {
		ids = append(ids, item.ID)
	}

	return ids
}

func storedPost(t *testing.T, pool *pgxpool.Pool, postID int64) scheduleposter.Post {
	t.Helper()

	var (
		item     scheduleposter.Post
		posterID *int64
		rowsAt   *time.Time
	)

	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT post_id, posted_at, body, status, created_at, matched_poster_id, matched_rows_at FROM schedule_poster_posts WHERE post_id = $1`,
		postID).Scan(&item.ID, &item.PostedAt, &item.Body, &item.Status, &item.CreatedAt, &posterID, &rowsAt))

	if posterID != nil && rowsAt != nil {
		item.MatchedPosterID, item.MatchedRowsAt = *posterID, *rowsAt
	}

	return item
}

func decide(t *testing.T, store *scheduleposter.Store, postID int64, decision scheduleposter.PostDecision) {
	t.Helper()

	recorded, err := store.RecordDecision(t.Context(), sophiaChannel, postID, decision)
	require.NoError(t, err)
	require.True(t, recorded)
}

var relevant = scheduleposter.PostDecision{Status: scheduleposter.PostRelevant, ScheduleInfo: 0.9, Kind: "change"}

var matchFailed = scheduleposter.MatchResult{Status: scheduleposter.PostMatchFailed, Reason: "bad_answer"}

func matched(changes ...scheduleposter.Change) scheduleposter.MatchResult {
	return scheduleposter.MatchResult{Status: scheduleposter.PostMatched, Changes: changes}
}

func changeCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM schedule_poster_changes`).Scan(&count))

	return count
}

func postStatus(t *testing.T, pool *pgxpool.Pool, postID int64) string {
	t.Helper()

	var status string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT status FROM schedule_poster_posts WHERE post_id = $1`, postID).Scan(&status))

	return status
}

type storedAnswer struct {
	info   *float64
	kind   *string
	reason string
}

func storedAnswers(t *testing.T, pool *pgxpool.Pool) map[int64]storedAnswer {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT post_id, round(schedule_info::numeric, 2)::float8, kind, reason FROM schedule_poster_posts`)
	require.NoError(t, err)

	answers := map[int64]storedAnswer{}

	for rows.Next() {
		var (
			id     int64
			answer storedAnswer
		)

		require.NoError(t, rows.Scan(&id, &answer.info, &answer.kind, &answer.reason))

		answers[id] = answer
	}

	require.NoError(t, rows.Err())

	return answers
}

func TestStorePostsAreJudgedOnceWithoutTouchingThePoster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)
	require.True(t, schedule.PostedAt.Equal(testPostedAt))
	require.False(t, schedule.RowsPending)
	require.Empty(t, schedule.Changes)

	before, err := store.Get(ctx, 101)
	require.NoError(t, err)

	posts := []scheduleposter.Post{
		post(11, jstAt(9, 11, 0)), post(12, testPostedAt), post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0)), post(15, jstAt(11, 10, 0)),
	}

	inserted, err := store.InsertPosts(ctx, 101, posts)
	require.NoError(t, err)
	require.EqualValues(t, 3, inserted, "포스터 게시 시각과 같거나 이른 글은 넣지 않는다")

	inserted, err = store.InsertPosts(ctx, 101, posts)
	require.NoError(t, err)
	require.Zero(t, inserted)

	inserted, err = store.InsertPosts(ctx, 102, []scheduleposter.Post{post(16, jstAt(10, 18, 0))})
	require.NoError(t, err)
	require.Zero(t, inserted, "확정하지 않은 포스터로는 넣지 않는다")

	pending, err := store.PendingPosts(ctx, schedule, 10)
	require.NoError(t, err)
	require.Len(t, pending, 3)
	require.Equal(t, int64(13), pending[0].ID)
	require.True(t, pending[0].PostedAt.Equal(jstAt(10, 18, 0)))
	require.Equal(t, "今日は20時から", pending[0].Body)
	require.Equal(t, scheduleposter.PostUnjudged, pending[0].Status)
	require.False(t, pending[0].CreatedAt.IsZero())

	decide(t, store, 13, relevant)
	decide(t, store, 14, scheduleposter.PostDecision{Status: scheduleposter.PostNotRelevant, ScheduleInfo: 0.25, Kind: "other"})
	decide(t, store, 15, scheduleposter.PostDecision{Status: scheduleposter.PostJudgeFailed, Reason: "blocked"})

	recorded, err := store.RecordDecision(ctx, sophiaChannel, 13, scheduleposter.PostDecision{Status: scheduleposter.PostNotRelevant, ScheduleInfo: 0.1, Kind: "other"})
	require.NoError(t, err)
	require.False(t, recorded, "판별한 글은 다시 판별하지 않는다")
	require.Equal(t, []int64{13}, pendingPostIDs(t, store, schedule), "종단한 글은 대기열에 없다")
	require.Equal(t, map[int64]storedAnswer{
		13: {info: new(0.9), kind: new("change")},
		14: {info: new(0.25), kind: new("other")},
		15: {reason: "blocked"},
	}, storedAnswers(t, pool), "판별 답이 없는 실패는 점수와 종류를 비운다")

	purged, err := store.PurgePosts(ctx, jstAt(30, 0, 0), 10)
	require.NoError(t, err)
	require.EqualValues(t, 3, purged)

	after, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision, "글 쓰기는 포스터의 revision 조건을 깨지 않는다")
	require.True(t, before.UpdatedAt.Equal(after.UpdatedAt))
}

func TestStoreRecordMatchStoresWhatThePostSaidOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	before, err := store.Get(ctx, 101)
	require.NoError(t, err)

	result := matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("20:00"), Text: "20時から"},
		scheduleposter.Change{PostID: 13, Item: 2, Kind: scheduleposter.ChangeSame, Slot: 2},
		scheduleposter.Change{PostID: 13, Item: 3, Kind: scheduleposter.ChangeRejected, Reason: "duplicate_row", Slot: 2},
		scheduleposter.Change{
			PostID: 13, Item: 4, Kind: scheduleposter.ChangeAdded, Day: date(t, "2026-11-13"), Start: new("25:00"),
			Status: scheduleposter.RowPlanned, Text: "歌枠", TitleKo: "노래", WithKo: []string{"쿄코"},
			Terms: []scheduleposter.Term{{Source: "歌枠", Ko: "노래", Basis: "translated"}}, Warnings: []string{"title_guessed"},
		},
		scheduleposter.Change{PostID: 13, Item: 5, Kind: scheduleposter.ChangeTentative, Slot: 3, Start: new(""), Status: scheduleposter.RowTentative},
	)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), result)
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), result)
	require.NoError(t, err)
	require.False(t, recorded, "같은 묶음에 대응한 글은 다시 기록하지 않는다")
	require.Equal(t, 5, changeCount(t, pool), "같음과 거절도 기록한다")
	require.Equal(t, []int64{14}, pendingPostIDs(t, store, schedule))

	after, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
	require.True(t, before.UpdatedAt.Equal(after.UpdatedAt))

	published := publishedSophia(t, store)
	require.Len(t, published.Changes, 4, "거절만 읽지 않는다")

	published.Changes = slices.Delete(published.Changes, 1, 2)

	for i := range published.Changes {
		require.True(t, published.Changes[i].PostedAt.Equal(jstAt(10, 18, 0)))
		require.False(t, published.Changes[i].RecordedAt.IsZero())

		published.Changes[i].PostedAt, published.Changes[i].RecordedAt = time.Time{}, time.Time{}
	}

	require.Equal(t, []scheduleposter.Change{
		{PostID: 13, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("20:00"), Text: "20時から", WithKo: []string{}, TagsKo: []string{}},
		{
			PostID: 13, Item: 4, Kind: scheduleposter.ChangeAdded, Day: date(t, "2026-11-13"), Start: new("25:00"),
			Status: scheduleposter.RowPlanned, Text: "歌枠", TitleKo: "노래", WithKo: []string{"쿄코"}, TagsKo: []string{},
		},
		{PostID: 13, Item: 5, Kind: scheduleposter.ChangeTentative, Slot: 3, Start: new(""), Status: scheduleposter.RowTentative, WithKo: []string{}, TagsKo: []string{}},
	}, published.Changes, "말하지 않은 날짜와 시각은 비고, 시각을 지운 변경은 빈 문자열이다")
}

func TestStoreRecordMatchFollowsTheCurrentBundle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, revision, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("20:00")},
		scheduleposter.Change{PostID: 13, Item: 2, Kind: scheduleposter.ChangeSame, Slot: 2},
	))
	require.NoError(t, err)
	require.True(t, recorded)

	requeueRows(t, pool)

	_, err = store.InsertPosts(ctx, 101, []scheduleposter.Post{post(15, jstAt(11, 9, 0))})
	require.NoError(t, err)

	waiting := publishedSophia(t, store)
	require.True(t, waiting.RowsPending)
	require.Len(t, waiting.Changes, 2, "재추출을 기다리는 동안 이전 묶음과 그 변경을 게시한다")
	require.Equal(t, []int64{15}, pendingPostIDs(t, store, waiting), "재추출을 기다리는 동안은 판별할 글만 고른다")

	recorded, err = store.RecordRows(ctx, onlyPendingRows(t, store), []scheduleposter.Row{scheduleRow(t, "2026-11-10", "잡담")}, 0)
	require.NoError(t, err)
	require.True(t, recorded)

	current := publishedSophia(t, store)
	require.False(t, current.RowsPending)
	require.Empty(t, current.Changes, "다른 묶음에 대응한 변경은 읽지 않는다")
	require.Equal(t, []int64{13, 14, 15}, pendingPostIDs(t, store, current), "새 묶음에는 대응한 글도 게시 순서로 다시 대응한다")

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 14), matched())
	require.NoError(t, err)
	require.False(t, recorded, "읽은 뒤 바뀐 묶음에는 기록하지 않는다")
	require.Equal(t, "relevant", postStatus(t, pool, 14))

	recorded, err = store.RecordMatch(ctx, current, storedPost(t, pool, 13), matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeContentChanged, Slot: 1, Text: "歌枠", TitleKo: "노래"},
	))
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, 1, changeCount(t, pool), "다시 대응하면 옛 변경을 지운다")

	recorded, err = store.RecordMatch(ctx, current, storedPost(t, pool, 14), matchFailed)
	require.NoError(t, err)
	require.True(t, recorded)

	current = publishedSophia(t, store)
	require.Len(t, current.Changes, 1)
	require.Equal(t, "노래", current.Changes[0].TitleKo)
	require.Equal(t, []int64{15}, pendingPostIDs(t, store, current), "대응에 실패한 글은 같은 묶음에 다시 대응하지 않는다")

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, revision, poster.Revision)
}

func TestStoreRecordMatchWritesNothingAfterReject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, revision, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)

	_, err = store.Reject(ctx, 101, revision)
	require.NoError(t, err)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeOff, Slot: 1, Status: scheduleposter.RowOff},
	))
	require.NoError(t, err)
	require.False(t, recorded)
	require.Equal(t, "relevant", postStatus(t, pool, 13))
	require.Zero(t, changeCount(t, pool))
	require.Empty(t, pendingPostIDs(t, store, schedule))

	inserted, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(14, jstAt(10, 19, 0))})
	require.NoError(t, err)
	require.Zero(t, inserted, "거절한 포스터로는 글을 넣지 않는다")
}

func TestStorePostsBeforeTheWeekStartDoNotMatchThePoster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, revision, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(21, jstAt(9, 20, 0))})
	require.NoError(t, err)
	decide(t, store, 21, relevant)

	_, err = store.UpdateWeek(ctx, 101, revision, date(t, "2026-11-10"), date(t, "2026-11-16"))
	require.NoError(t, err)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 21), matched())
	require.NoError(t, err)
	require.False(t, recorded, "주 범위를 고친 뒤에는 옛 묶음으로 기록하지 않는다")

	recorded, err = store.RecordRows(ctx, onlyPendingRows(t, store), []scheduleposter.Row{scheduleRow(t, "2026-11-10", "잡담")}, 0)
	require.NoError(t, err)
	require.True(t, recorded)

	moved := publishedSophia(t, store)
	require.Empty(t, pendingPostIDs(t, store, moved), "주 첫날 00:00 JST보다 먼저 올린 글은 대응하지 않는다")

	recorded, err = store.RecordMatch(ctx, moved, storedPost(t, pool, 21), matched())
	require.NoError(t, err)
	require.False(t, recorded)

	inserted, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(22, jstAt(9, 23, 59)), post(23, jstAt(10, 0, 0))})
	require.NoError(t, err)
	require.EqualValues(t, 1, inserted)
	require.Equal(t, []int64{23}, pendingPostIDs(t, store, moved))
}

func TestStorePurgePostsRemovesTheirChanges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0)), post(15, jstAt(11, 9, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeCancelled, Slot: 1, Status: scheduleposter.RowCancelled},
	))
	require.NoError(t, err)
	require.True(t, recorded)

	purged, err := store.PurgePosts(ctx, jstAt(11, 0, 0), 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, purged)

	purged, err = store.PurgePosts(ctx, jstAt(11, 0, 0), 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, purged)
	require.Zero(t, changeCount(t, pool))
	require.Equal(t, []int64{15}, pendingPostIDs(t, store, schedule))
}

func TestStoreChangeWritesRejectInvalidInputBeforeWriting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	for name, posts := range map[string][]scheduleposter.Post{
		"빈 본문":      {{ID: 13, PostedAt: jstAt(10, 18, 0)}},
		"본문 4001자":  {{ID: 13, PostedAt: jstAt(10, 18, 0), Body: string(make([]rune, 4001))}},
		"NUL 문자":    {{ID: 13, PostedAt: jstAt(10, 18, 0), Body: "a\x00b"}},
		"잘못된 UTF-8": {{ID: 13, PostedAt: jstAt(10, 18, 0), Body: "\xff"}},
		"글 번호 0":    {{ID: 0, PostedAt: jstAt(10, 18, 0), Body: "a"}},
		"같은 글 두 번":  {post(13, jstAt(10, 18, 0)), post(13, jstAt(10, 18, 0))},
	} {
		_, err := store.InsertPosts(ctx, 101, posts)
		require.Error(t, err, name)
	}

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0))})
	require.NoError(t, err)

	for name, decision := range map[string]scheduleposter.PostDecision{
		"대응 상태":  {Status: scheduleposter.PostMatched, ScheduleInfo: 0.9, Kind: "change"},
		"미판별 상태": {Status: scheduleposter.PostUnjudged},
	} {
		_, err = store.RecordDecision(ctx, sophiaChannel, 13, decision)
		require.Error(t, err, name)
	}

	decide(t, store, 13, relevant)

	for name, result := range map[string]scheduleposter.MatchResult{
		"판별 상태":    {Status: scheduleposter.PostRelevant},
		"다른 글의 변경": matched(scheduleposter.Change{PostID: 14, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1}),
		"검증 실패":    matched(scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeMoved, Slot: 1}),
		"변경 있는 실패": {
			Status: scheduleposter.PostMatchFailed, Reason: matchFailed.Reason,
			Changes: []scheduleposter.Change{{PostID: 13, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1}},
		},
	} {
		_, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), result)
		require.Error(t, err, name)
	}

	require.Equal(t, "relevant", postStatus(t, pool, 13))
	require.Zero(t, changeCount(t, pool))
}
