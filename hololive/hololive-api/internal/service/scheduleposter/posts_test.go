package scheduleposter_test

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
)

func recordRowsFor(t *testing.T, store *scheduleposter.Store, id int64, today, title string) {
	t.Helper()

	pending, err := store.PendingRows(t.Context(), date(t, today), 10)
	require.NoError(t, err)

	i := slices.IndexFunc(pending, func(item scheduleposter.PendingRows) bool { return item.ID == id })
	require.GreaterOrEqual(t, i, 0)

	recorded, err := store.RecordRows(t.Context(), pending[i], []scheduleposter.Row{scheduleRow(t, today, title)}, 0)
	require.NoError(t, err)
	require.True(t, recorded)
}

func confirmedPosterWithRows(
	t *testing.T, store *scheduleposter.Store, id int64, channelID string, postedAt time.Time, start, end string,
) scheduleposter.PublishedSchedule {
	t.Helper()

	ctx := t.Context()
	item := candidate(id, postedAt)

	item.ChannelID = channelID

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{item}, false)
	require.NoError(t, err)

	poster, err := store.Get(ctx, id)
	require.NoError(t, err)

	_, err = store.Confirm(ctx, id, poster.Revision, date(t, start), date(t, end))
	require.NoError(t, err)

	recordRowsFor(t, store, id, start, "잡담")

	schedule, found, err := store.PublishedRows(ctx, channelID, date(t, start))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, id, schedule.PosterID)

	return schedule
}

func replacementPoster(t *testing.T, store *scheduleposter.Store, pool *pgxpool.Pool, id int64, postedAt time.Time) scheduleposter.PublishedSchedule {
	t.Helper()

	_, err := pool.Exec(t.Context(), `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt,
		review_status, week_start, week_end, reviewed_at, reviewed_by)
		VALUES ($1, $2, '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/b.jpg?name=orig', $3, '',
		        'confirmed', '2026-11-09', '2026-11-15', now(), 'admin')`, id, sophiaChannel, postedAt)
	require.NoError(t, err)

	recordRowsFor(t, store, id, "2026-11-09", "잡담")

	schedule := publishedSophia(t, store)
	require.Equal(t, id, schedule.PosterID, "주중에 확정한 교체본이 게시된다")

	return schedule
}

func pendingPost(t *testing.T, store *scheduleposter.Store, schedule scheduleposter.PublishedSchedule, id int64) scheduleposter.Post {
	t.Helper()

	posts, err := store.PendingPosts(t.Context(), schedule, 10)
	require.NoError(t, err)

	i := slices.IndexFunc(posts, func(item scheduleposter.Post) bool { return item.ID == id })
	require.GreaterOrEqual(t, i, 0)

	return posts[i]
}

func insertRelevantPost(t *testing.T, pool *pgxpool.Pool, id int64, channelID string, postedAt time.Time) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind)
		VALUES ($1, $2, $3, '告知', 'relevant', 0.9, 'change')`, id, channelID, postedAt)
	require.NoError(t, err)
}

func contentChange(postID int64, titleKo string) scheduleposter.Change {
	return scheduleposter.Change{PostID: postID, Item: 1, Kind: scheduleposter.ChangeContentChanged, Slot: 1, Text: "歌枠", TitleKo: titleKo}
}

func TestStoreSameFromALaterPostSurvivesAnEarlierPostRecordedLate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	same := matched(scheduleposter.Change{
		PostID: 14, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1, Day: date(t, "2026-11-10"), Start: new("21:00"),
		Status: scheduleposter.RowPlanned,
	})

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 14), same)
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(scheduleposter.Change{
		PostID: 13, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00"),
	}))
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 14), same)
	require.NoError(t, err)
	require.True(t, recorded)

	published := publishedSophia(t, store)
	slot := published.Slots()[0]
	require.Equal(t, "21:00", slot.Start, "뒤 글이 같다고 말한 시각이 늦게 기록된 앞선 글의 변경을 덮는다")
	require.False(t, slot.TimeChanged)
}

func TestStoreAnEarlierPostAppliedLateSendsLaterPostsBackToMatching(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{
		post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0)), post(15, jstAt(10, 20, 0)), post(16, jstAt(10, 21, 0)),
	})
	require.NoError(t, err)

	for _, id := range []int64{13, 14, 15, 16} {
		decide(t, store, id, relevant)
	}

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 14), matched(contentChange(14, "뒤 글")))
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 15), matchFailed)
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 16), matched(contentChange(16, "철회")))
	require.NoError(t, err)
	require.True(t, recorded)
	require.NoError(t, store.RevokePost(ctx, 101, 16))

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(scheduleposter.Change{
		PostID: 13, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00"),
	}))
	require.NoError(t, err)
	require.True(t, recorded)

	for _, id := range []int64{14, 15} {
		requeued := storedPost(t, pool, id)
		require.Equal(t, scheduleposter.PostRelevant, requeued.Status, "앞선 글이 반영되면 뒤 글은 다시 대응한다")
		require.Zero(t, requeued.MatchedPosterID)
	}

	require.Equal(t, map[int64]storedAnswer{
		13: {info: new(0.9), kind: new("change")},
		14: {info: new(0.9), kind: new("change")},
		15: {info: new(0.9), kind: new("change")},
		16: {info: new(0.9), kind: new("change")},
	}, storedAnswers(t, pool), "다시 대응할 글은 판별 답을 지키고 실패 사유를 비운다")
	require.Equal(t, "revoked", postStatus(t, pool, 16))
	require.Equal(t, []int64{14, 15}, pendingPostIDs(t, store, schedule))

	published := publishedSophia(t, store)
	require.Len(t, published.Changes, 1, "다시 대응하는 동안 뒤 글의 변경은 게시하지 않는다")
	require.EqualValues(t, 13, published.Changes[0].PostID)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 14), matched(contentChange(14, "다시 대응")))
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, "matched", postStatus(t, pool, 13), "뒤 글의 기록은 앞선 글을 되돌리지 않는다")
	require.Equal(t, "relevant", postStatus(t, pool, 15))
	require.Equal(t, []int64{15}, pendingPostIDs(t, store, schedule))
}

func TestStoreAnEarlierPostAppliedLateKeepsLaterPostsOfOtherBundles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)
	tsuzuri := confirmedPosterWithRows(t, store, 201, tsuzuriChannel, testPostedAt, "2026-11-09", "2026-11-15")

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	insertRelevantPost(t, pool, 41, tsuzuriChannel, jstAt(10, 22, 0))
	insertRelevantPost(t, pool, 17, sophiaChannel, jstAt(10, 22, 30))

	recorded, err := store.RecordMatch(ctx, tsuzuri, storedPost(t, pool, 41), matched(contentChange(41, "다른 멤버")))
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 17), matched())
	require.NoError(t, err)
	require.True(t, recorded)

	_, err = pool.Exec(ctx, `UPDATE schedule_poster_posts SET matched_rows_at = matched_rows_at - interval '1 hour' WHERE post_id = 17`)
	require.NoError(t, err)

	otherMember, otherBundle := storedPost(t, pool, 41), storedPost(t, pool, 17)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(scheduleposter.Change{
		PostID: 13, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00"),
	}))
	require.NoError(t, err)
	require.True(t, recorded)

	require.Equal(t, otherMember, storedPost(t, pool, 41), "다른 멤버 확정본에 대응한 뒤 글은 그대로다")
	require.Equal(t, otherBundle, storedPost(t, pool, 17), "같은 포스터의 다른 묶음에 대응한 뒤 글은 그대로다")
}

func TestStoreAnEarlierPostWithoutAppliedChangesKeepsLaterMatches(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 14), matched(contentChange(14, "뒤 글")))
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1, Start: new("21:00")},
		scheduleposter.Change{PostID: 13, Item: 2, Kind: scheduleposter.ChangeRejected, Reason: "past_time", Slot: 2},
	))
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, "matched", postStatus(t, pool, 14))
	require.Empty(t, pendingPostIDs(t, store, schedule))
}

func TestStoreRecordMatchKeepsAMatchWrittenAfterThePostWasRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, first := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(31, jstAt(11, 12, 0))})
	require.NoError(t, err)
	decide(t, store, 31, relevant)

	readForFirst := pendingPost(t, store, first, 31)
	replacement := replacementPoster(t, store, pool, 103, jstAt(11, 10, 0))
	readForReplacement := pendingPost(t, store, replacement, 31)

	recorded, err := store.RecordMatch(ctx, replacement, readForReplacement, matched(contentChange(31, "교체본")))
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, first, readForFirst, matched(contentChange(31, "옛 포스터")))
	require.NoError(t, err)
	require.False(t, recorded, "읽은 뒤 다른 묶음에 대응한 글은 덮어쓰지 않는다")

	stored := storedPost(t, pool, 31)
	require.EqualValues(t, 103, stored.MatchedPosterID)

	current := publishedSophia(t, store)
	require.Len(t, current.Changes, 1)
	require.Equal(t, "교체본", current.Changes[0].TitleKo)
}

func TestStorePostsBeforeAReplacementPosterStayWithTheEarlierPoster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, first := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(31, jstAt(10, 18, 0))})
	require.NoError(t, err)
	decide(t, store, 31, relevant)

	replacement := replacementPoster(t, store, pool, 103, jstAt(11, 10, 0))
	require.Empty(t, pendingPostIDs(t, store, replacement), "교체본 게시 전 글은 교체본에 대응하지 않는다")

	recorded, err := store.RecordMatch(ctx, replacement, storedPost(t, pool, 31), matched())
	require.NoError(t, err)
	require.False(t, recorded)

	recorded, err = store.RecordMatch(ctx, first, storedPost(t, pool, 31), matched())
	require.NoError(t, err)
	require.True(t, recorded, "그 글은 게시 당시의 포스터에 대응한다")
}

func TestStorePostsOfAnotherMemberDoNotMatch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, sophia := newChangesStore(t)
	tsuzuri := confirmedPosterWithRows(t, store, 201, tsuzuriChannel, testPostedAt, "2026-11-09", "2026-11-15")

	_, err := store.InsertPosts(ctx, 201, []scheduleposter.Post{post(41, jstAt(10, 18, 0))})
	require.NoError(t, err)
	decide(t, store, 41, relevant)

	require.Empty(t, pendingPostIDs(t, store, sophia))
	require.Equal(t, []int64{41}, pendingPostIDs(t, store, tsuzuri))

	recorded, err := store.RecordMatch(ctx, sophia, storedPost(t, pool, 41), matched())
	require.NoError(t, err)
	require.False(t, recorded)
	require.Equal(t, "relevant", postStatus(t, pool, 41))
}

func TestStoreRecordMatchCountsTheWeekFromMidnightJST(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	schedule := confirmedPosterWithRows(t, store, 301, sophiaChannel, jstAt(8, 20, 0), "2026-11-09", "2026-11-15")

	insertRelevantPost(t, pool, 50, sophiaChannel, jstAt(9, 0, 0))
	insertRelevantPost(t, pool, 51, sophiaChannel, jstAt(8, 23, 59))

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 50), matched())
	require.NoError(t, err)
	require.True(t, recorded, "주 첫날 00:00 JST 글은 그 주 포스터에 대응한다")

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 51), matched())
	require.NoError(t, err)
	require.False(t, recorded)
}

func TestStorePostsAfterTheWeekEndBelongOnlyToTheNextWeek(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, lastWeek := newChangesStore(t)

	inserted, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(61, jstAt(15, 23, 59)), post(62, jstAt(16, 0, 0))})
	require.NoError(t, err)
	require.EqualValues(t, 1, inserted, "주 마지막 날 24:00 JST부터는 그 주 포스터의 글이 아니다")

	decide(t, store, 61, relevant)
	insertRelevantPost(t, pool, 63, sophiaChannel, jstAt(16, 1, 0))

	thisWeek := confirmedPosterWithRows(t, store, 104, sophiaChannel, jstAt(15, 20, 0), "2026-11-16", "2026-11-22")

	require.Equal(t, []int64{61}, pendingPostIDs(t, store, lastWeek))
	require.Equal(t, []int64{63}, pendingPostIDs(t, store, thisWeek))

	recorded, err := store.RecordMatch(ctx, lastWeek, storedPost(t, pool, 63), matched())
	require.NoError(t, err)
	require.False(t, recorded, "다음 주 글은 지난주 포스터에 대응하지 않는다")

	listed := func(posterID int64) []int64 {
		posts, listErr := store.PosterPosts(ctx, posterID)
		require.NoError(t, listErr)

		ids := make([]int64, 0, len(posts))
		for _, item := range posts {
			ids = append(ids, item.ID)
		}

		return ids
	}

	require.Equal(t, []int64{61}, listed(101))
	require.Equal(t, []int64{63}, listed(104))
	require.ErrorIs(t, store.RevokePost(ctx, 101, 63), scheduleposter.ErrNotFound, "지난주 포스터 화면에서 이번 주 글을 철회하지 않는다")
	require.Equal(t, "relevant", postStatus(t, pool, 63))

	require.NoError(t, store.RevokePost(ctx, 104, 63))
	require.ErrorIs(t, store.RestorePost(ctx, 101, 63), scheduleposter.ErrNotFound)
	require.NoError(t, store.RestorePost(ctx, 104, 63))
}

func TestStoreRevokePostRemovesItsChangesForGood(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0)), post(15, jstAt(11, 9, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeCancelled, Slot: 1, Status: scheduleposter.RowCancelled},
	))
	require.NoError(t, err)
	require.True(t, recorded)

	readBeforeRevoke := storedPost(t, pool, 14)

	require.NoError(t, store.RevokePost(ctx, 101, 13))
	require.NoError(t, store.RevokePost(ctx, 101, 14))

	revoked := storedPost(t, pool, 13)
	require.Equal(t, scheduleposter.PostRevoked, revoked.Status)
	require.Zero(t, revoked.MatchedPosterID)
	require.True(t, revoked.MatchedRowsAt.IsZero())
	require.Equal(t, map[int64]storedAnswer{
		13: {info: new(0.9), kind: new("change")},
		14: {info: new(0.9), kind: new("change")},
		15: {},
	}, storedAnswers(t, pool), "철회해도 판별 답은 남는다")
	require.Zero(t, changeCount(t, pool))
	require.Empty(t, publishedSophia(t, store).Changes)
	require.Equal(t, []int64{15}, pendingPostIDs(t, store, schedule), "철회한 글은 다시 대응하지 않는다")

	recorded, err = store.RecordMatch(ctx, schedule, readBeforeRevoke, matched())
	require.NoError(t, err)
	require.False(t, recorded, "철회 전에 읽은 작업의 기록은 버린다")

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 14), matched())
	require.NoError(t, err)
	require.False(t, recorded)

	recorded, err = store.RecordDecision(ctx, 14, relevant)
	require.NoError(t, err)
	require.False(t, recorded)
	require.Equal(t, "revoked", postStatus(t, pool, 14))

	require.ErrorIs(t, store.RevokePost(ctx, 101, 13), scheduleposter.ErrInvalidState, "이미 철회했다")
	require.ErrorIs(t, store.RevokePost(ctx, 101, 15), scheduleposter.ErrInvalidState, "판별하지 않은 글")

	insertRelevantPost(t, pool, 16, sophiaChannel, jstAt(9, 11, 0))
	insertRelevantPost(t, pool, 17, tsuzuriChannel, jstAt(10, 18, 0))

	for name, ids := range map[string][2]int64{
		"없는 포스터":     {999, 13},
		"없는 글":       {101, 999},
		"포스터 게시 전 글": {101, 16},
		"다른 멤버의 글":   {101, 17},
	} {
		require.ErrorIs(t, store.RevokePost(ctx, ids[0], ids[1]), scheduleposter.ErrNotFound, name)
		require.ErrorIs(t, store.RestorePost(ctx, ids[0], ids[1]), scheduleposter.ErrNotFound, name)
	}

	require.ErrorIs(t, store.RevokePost(ctx, 102, 13), scheduleposter.ErrInvalidState, "확정하지 않은 포스터")
	require.ErrorIs(t, store.RestorePost(ctx, 102, 13), scheduleposter.ErrInvalidState, "확정하지 않은 포스터")
	require.Equal(t, "relevant", postStatus(t, pool, 16))
	require.Equal(t, "relevant", postStatus(t, pool, 17))
}

func TestStoreRestorePostSendsARevokedPostBackToMatching(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(contentChange(13, "노래")))
	require.NoError(t, err)
	require.True(t, recorded)
	require.NoError(t, store.RevokePost(ctx, 101, 13))

	require.ErrorIs(t, store.RestorePost(ctx, 101, 14), scheduleposter.ErrInvalidState, "철회하지 않은 글")
	require.NoError(t, store.RestorePost(ctx, 101, 13))
	require.ErrorIs(t, store.RestorePost(ctx, 101, 13), scheduleposter.ErrInvalidState, "이미 되살렸다")

	restored := storedPost(t, pool, 13)
	require.Equal(t, scheduleposter.PostRelevant, restored.Status)
	require.Zero(t, restored.MatchedPosterID)
	require.Equal(t, storedAnswer{info: new(0.9), kind: new("change")}, storedAnswers(t, pool)[13], "되살린 글은 판별을 다시 하지 않는다")
	require.Equal(t, []int64{13, 14}, pendingPostIDs(t, store, schedule))

	recorded, err = store.RecordMatch(ctx, schedule, restored, matched(contentChange(13, "되살림")))
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, "되살림", publishedSophia(t, store).Changes[0].TitleKo)
}

func TestStoreRevokeAndRestoreNeedAConfirmedPoster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(contentChange(13, "노래")))
	require.NoError(t, err)
	require.True(t, recorded)
	require.NoError(t, store.RevokePost(ctx, 101, 14))

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	_, err = store.Reject(ctx, 101, poster.Revision)
	require.NoError(t, err)

	require.ErrorIs(t, store.RevokePost(ctx, 101, 13), scheduleposter.ErrInvalidState, "거절한 포스터의 글은 철회하지 않는다")
	require.ErrorIs(t, store.RestorePost(ctx, 101, 14), scheduleposter.ErrInvalidState, "거절한 포스터의 글은 되살리지 않는다")
	require.Equal(t, "matched", postStatus(t, pool, 13))
	require.Equal(t, "revoked", postStatus(t, pool, 14))

	posts, err := store.PosterPosts(ctx, 101)
	require.NoError(t, err, "확정본이 아니어도 글 목록은 읽는다")
	require.Len(t, posts, 2)

	for _, item := range posts {
		require.False(t, item.Revocable, item.ID)
		require.False(t, item.Restorable, item.ID)
	}
}

func TestStoreRequestRowsReschedulesOnlyRowExtraction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmedPosterWithRows(t, store, 401, sophiaChannel, time.Date(2099, time.November, 9, 3, 0, 0, 0, time.UTC), "2099-11-09", "2099-11-15")

	before, err := store.Get(ctx, 401)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.RowsSucceeded, before.RowsStatus)
	require.NotNil(t, before.RowsExtractedAt)
	require.Equal(t, 1, before.RowsCount)

	rowsRevision := func() int64 {
		var revision int64

		require.NoError(t, pool.QueryRow(ctx, `SELECT rows_revision FROM schedule_posters WHERE id = 401`).Scan(&revision))

		return revision
	}
	beforeRowsRevision := rowsRevision()

	revision, err := store.RequestRows(ctx, 401, before.Revision)
	require.NoError(t, err)
	require.Equal(t, before.Revision, revision, "Console의 revision 조건을 깨지 않는다")

	after, err := store.Get(ctx, 401)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.RowsPending, after.RowsStatus)
	require.Equal(t, beforeRowsRevision+1, rowsRevision())
	require.True(t, before.RowsExtractedAt.Equal(*after.RowsExtractedAt))
	require.Equal(t, 1, after.RowsCount, "새 묶음이 저장될 때까지 이전 행을 게시한다")
	require.True(t, before.UpdatedAt.Equal(after.UpdatedAt))

	_, err = store.RequestRows(ctx, 401, before.Revision)
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState, "이미 기다리는 중이다")

	pending, err := store.PendingRows(ctx, date(t, "2099-11-09"), 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)

	recorded, err := store.RecordRowsFailure(ctx, pending[0], "extraction_failed")
	require.NoError(t, err)
	require.True(t, recorded)

	failed, err := store.Get(ctx, 401)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.RowsFailed, failed.RowsStatus)
	require.Equal(t, "extraction_failed", failed.RowsError)

	_, err = store.RequestRows(ctx, 401, before.Revision)
	require.NoError(t, err, "실패한 추출도 다시 뽑는다")

	retried, err := store.Get(ctx, 401)
	require.NoError(t, err)
	require.Empty(t, retried.RowsError)
}

func TestStoreRequestRowsRefusesPostersThatCannotBeExtracted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _ := newRowsStore(t)
	confirmedPosterWithRows(t, store, 401, sophiaChannel, time.Date(2099, time.November, 9, 3, 0, 0, 0, time.UTC), "2099-11-09", "2099-11-15")

	poster, err := store.Get(ctx, 401)
	require.NoError(t, err)

	_, err = store.RequestRows(ctx, 401, poster.Revision+1)
	require.ErrorIs(t, err, scheduleposter.ErrRevisionConflict)

	_, err = store.RequestRows(ctx, 999, 1)
	require.ErrorIs(t, err, scheduleposter.ErrNotFound)

	_, err = store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(402, time.Date(2099, time.November, 20, 3, 0, 0, 0, time.UTC))}, false)
	require.NoError(t, err)

	_, err = store.RequestRows(ctx, 402, 1)
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState, "확정하지 않은 포스터")

	confirmedPosterWithRows(t, store, 403, tsuzuriChannel, time.Date(2020, time.November, 9, 3, 0, 0, 0, time.UTC), "2020-11-09", "2020-11-15")

	ended, err := store.Get(ctx, 403)
	require.NoError(t, err)

	_, err = store.RequestRows(ctx, 403, ended.Revision)
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState, "주가 끝난 포스터는 추출 대상이 아니다")
}

func TestStorePosterRowsStateIsReadWithThePoster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _, _, schedule := newChangesStore(t)

	confirmed, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.RowsSucceeded, confirmed.RowsStatus)
	require.Empty(t, confirmed.RowsError)
	require.NotNil(t, confirmed.RowsExtractedAt)
	require.True(t, schedule.ExtractedAt.Equal(*confirmed.RowsExtractedAt))
	require.Equal(t, 3, confirmed.RowsCount)

	listed, err := store.List(ctx, scheduleposter.ReviewConfirmed, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, confirmed, listed[0])

	waiting, err := store.Get(ctx, 102)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.RowsPending, waiting.RowsStatus)
	require.Nil(t, waiting.RowsExtractedAt)
	require.Zero(t, waiting.RowsCount)
}

func TestStorePosterPostsShowsCollectedPostsWithCurrentChanges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	long := post(13, jstAt(10, 18, 0))

	long.Body = strings.Repeat("가", 300)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{
		long, post(14, jstAt(10, 19, 0)), post(15, jstAt(11, 9, 0)), post(17, jstAt(11, 11, 0)),
	})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	decide(t, store, 15, scheduleposter.PostDecision{Status: scheduleposter.PostNotRelevant, ScheduleInfo: 0.1, Kind: "other"})

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		contentChange(13, "노래"),
		scheduleposter.Change{PostID: 13, Item: 2, Kind: scheduleposter.ChangeSame, Slot: 2, Start: new("21:00")},
		scheduleposter.Change{PostID: 13, Item: 3, Kind: scheduleposter.ChangeRejected, Reason: "duplicate_row", Slot: 2},
	))
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, schedule, storedPost(t, pool, 14), matchFailed)
	require.NoError(t, err)
	require.True(t, recorded)

	insertRelevantPost(t, pool, 18, sophiaChannel, jstAt(9, 11, 0))
	insertRelevantPost(t, pool, 19, tsuzuriChannel, jstAt(10, 18, 0))

	posts, err := store.PosterPosts(ctx, 101)
	require.NoError(t, err)

	byID := map[int64]scheduleposter.PosterPost{}
	ids := make([]int64, 0, len(posts))

	for _, item := range posts {
		byID[item.ID] = item
		ids = append(ids, item.ID)
	}

	require.Equal(t, []int64{17, 15, 14, 13}, ids, "수집 조건에 드는 글만 최근 글부터 돌려준다")

	first := byID[13]
	require.True(t, first.PostedAt.Equal(jstAt(10, 18, 0)))
	require.Equal(t, 280, utf8.RuneCountInString(first.TextExcerpt))
	require.Equal(t, scheduleposter.PostMatched, first.Status)
	require.True(t, first.Current)
	require.True(t, first.Revocable)
	require.Equal(t, []scheduleposter.Change{
		{PostID: 13, Item: 1, Kind: scheduleposter.ChangeContentChanged, Slot: 1, Text: "歌枠", TitleKo: "노래"},
		{PostID: 13, Item: 2, Kind: scheduleposter.ChangeSame, Slot: 2, Start: new("21:00")},
		{PostID: 13, Item: 3, Kind: scheduleposter.ChangeRejected, Reason: "duplicate_row", Slot: 2},
	}, first.Changes)

	require.Equal(t, scheduleposter.PosterPost{
		ID: 14, PostedAt: byID[14].PostedAt, TextExcerpt: "今日は20時から", Status: scheduleposter.PostMatchFailed, Reason: matchFailed.Reason,
		Current: true, Revocable: true,
	}, byID[14])
	require.False(t, byID[15].Revocable)
	require.False(t, byID[15].Current)
	require.Equal(t, scheduleposter.PostUnjudged, byID[17].Status)
	require.False(t, byID[17].Revocable)
}

func TestStorePosterPostsLeavesOutChangesOfAnEarlierBundle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(16, jstAt(11, 10, 0))})
	require.NoError(t, err)
	decide(t, store, 16, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 16), matched(contentChange(16, "옛 묶음")))
	require.NoError(t, err)
	require.True(t, recorded)

	_, err = pool.Exec(ctx, `UPDATE schedule_poster_posts SET matched_rows_at = matched_rows_at - interval '1 hour' WHERE post_id = 16`)
	require.NoError(t, err)

	posts, err := store.PosterPosts(ctx, 101)
	require.NoError(t, err)
	require.Len(t, posts, 1)
	require.True(t, posts[0].Revocable)
	require.False(t, posts[0].Current, "다른 묶음에 대응한 글은 지금 묶음의 글이 아니다")
	require.Empty(t, posts[0].Changes, "지금 묶음에 대응하지 않은 글의 변경은 게시되지 않아 싣지 않는다")
}

func TestStorePosterPostsKeepsActionablePostsBeyondTheLatestFifty(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0)), post(15, jstAt(10, 20, 0))})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)
	decide(t, store, 15, scheduleposter.PostDecision{Status: scheduleposter.PostNotRelevant, ScheduleInfo: 0.1, Kind: "other"})

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(contentChange(13, "노래")))
	require.NoError(t, err)
	require.True(t, recorded)
	require.NoError(t, store.RevokePost(ctx, 101, 14))

	posts, err := store.PosterPosts(ctx, 101)
	require.NoError(t, err)
	require.Len(t, posts, 3)
	require.Equal(t, scheduleposter.PosterPost{
		ID: 14, PostedAt: posts[1].PostedAt, TextExcerpt: "今日は20時から", Status: scheduleposter.PostRevoked, Restorable: true,
	}, posts[1])

	many := make([]scheduleposter.Post, 0, 51)

	for n := range 51 {
		many = append(many, post(int64(1000+n), jstAt(12, 0, n)))
	}

	_, err = store.InsertPosts(ctx, 101, many)
	require.NoError(t, err)

	posts, err = store.PosterPosts(ctx, 101)
	require.NoError(t, err)
	require.Len(t, posts, 52, "판별하지 않은 글은 최근 50건만 싣고 철회·되살릴 글은 밀려나지 않는다")
	require.EqualValues(t, 1050, posts[0].ID)
	require.EqualValues(t, 1001, posts[49].ID)
	require.Equal(t, []int64{14, 13}, []int64{posts[50].ID, posts[51].ID})
	require.True(t, posts[50].Restorable)
	require.True(t, posts[51].Revocable)

	_, err = store.PosterPosts(ctx, 999)
	require.ErrorIs(t, err, scheduleposter.ErrNotFound)
}

func TestStorePosterPostsCapsActionablePostsAtTwoHundred(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, _ := newChangesStore(t)

	_, err := pool.Exec(ctx, `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind)
		SELECT 2000 + n, $1, $2::timestamptz + n * interval '1 minute', '告知', 'relevant', 0.9, 'change'
		FROM generate_series(0, 200) AS n`, sophiaChannel, jstAt(12, 0, 0))
	require.NoError(t, err)

	posts, err := store.PosterPosts(ctx, 101)
	require.NoError(t, err)
	require.Len(t, posts, 200)
	require.EqualValues(t, 2200, posts[0].ID)
	require.EqualValues(t, 2001, posts[199].ID)
}

func TestStoreRowExtractionFollowsTheDayShiftedWeekEnd(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	now := time.Now().In(tokyo)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start, end := today.AddDate(0, 0, -7).Format(time.DateOnly), today.AddDate(0, 0, -1).Format(time.DateOnly)

	confirmedPosterWithRows(t, store, 401, sophiaChannel, today.AddDate(0, 0, -7), start, end)

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET rows_day_shift = 1 WHERE id = 401`)
	require.NoError(t, err)

	poster, err := store.Get(ctx, 401)
	require.NoError(t, err)

	_, err = store.RequestRows(ctx, 401, poster.Revision)
	require.NoError(t, err, "하루 민 범위의 마지막 날에는 아직 !스케쥴에 보이므로 다시 뽑는다")

	pending, err := store.PendingRows(ctx, today, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.EqualValues(t, 401, pending[0].ID)

	pending, err = store.PendingRows(ctx, today.AddDate(0, 0, 1), 10)
	require.NoError(t, err)
	require.Empty(t, pending, "민 범위도 끝나면 뽑지 않는다")
}
