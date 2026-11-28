package scheduleposter_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const noticePending scheduleposter.NoticeStatus = "pending"

type storedNotice struct {
	kind   scheduleposter.NoticeKind
	poster int64
	post   *int64
	status scheduleposter.NoticeStatus
	rooms  *int
}

func storedNotices(t *testing.T, pool *pgxpool.Pool) []storedNotice {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT kind, poster_id, post_id, status, rooms FROM schedule_poster_notices ORDER BY id`)
	require.NoError(t, err)

	notices := []storedNotice{}

	for rows.Next() {
		var notice storedNotice

		require.NoError(t, rows.Scan(&notice.kind, &notice.poster, &notice.post, &notice.status, &notice.rooms))

		notices = append(notices, notice)
	}

	require.NoError(t, rows.Err())

	return notices
}

func claimNotice(t *testing.T, store *scheduleposter.Store) (scheduleposter.Notice, bool) {
	t.Helper()

	notice, ok, err := store.ClaimNotice(t.Context(), time.Minute)
	require.NoError(t, err)

	return notice, ok
}

func TestStoreRecordRowsQueuesOnePublishedNoticePerPoster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	revision := confirmPoster101(t, store)

	require.Empty(t, storedNotices(t, pool))
	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))
	require.Equal(t, []storedNotice{{kind: scheduleposter.NoticePublished, poster: 101, status: noticePending}}, storedNotices(t, pool))

	requeueRows(t, pool)
	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))

	revision, err := store.UpdateWeek(ctx, 101, revision, date(t, "2026-11-16"), date(t, "2026-11-22"))
	require.NoError(t, err)

	recorded, err := store.RecordRows(ctx, onlyPendingRows(t, store), []scheduleposter.Row{scheduleRow(t, "2026-11-17", "잡담")}, 0)
	require.NoError(t, err)
	require.True(t, recorded)
	require.Len(t, storedNotices(t, pool), 1, "다시 뽑거나 주를 고쳐 다시 뽑아도 새 일정 알림은 한 번이다")

	notice, ok := claimNotice(t, store)
	require.True(t, ok)
	require.NoError(t, store.SettleNotice(ctx, notice, scheduleposter.NoticeSkipped, 0))

	_, err = store.UpdateWeek(ctx, 101, revision, date(t, "2026-11-09"), date(t, "2026-11-15"))
	require.NoError(t, err)

	recorded, err = store.RecordRows(ctx, onlyPendingRows(t, store), []scheduleposter.Row{scheduleRow(t, "2026-11-10", "잡담")}, 0)
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, []storedNotice{
		{kind: scheduleposter.NoticePublished, poster: 101, status: scheduleposter.NoticeSkipped},
		{kind: scheduleposter.NoticePublished, poster: 101, status: noticePending},
	}, storedNotices(t, pool), "보내지 못하고 닫힌 알림은 다음 추출의 알림을 막지 않는다")
}

func TestStoreClaimsLeasesAndSettlesNotices(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmPoster101(t, store)
	require.True(t, recordChat(t, store, onlyPendingRows(t, store)))

	notice, ok := claimNotice(t, store)
	require.True(t, ok)
	require.Equal(t, scheduleposter.NoticePublished, notice.Kind)
	require.EqualValues(t, 101, notice.PosterID)
	require.Equal(t, sophiaChannel, notice.ChannelID)
	require.Zero(t, notice.PostID)
	require.False(t, notice.ClaimedAt.Before(notice.CreatedAt))

	_, ok = claimNotice(t, store)
	require.False(t, ok, "빌려 간 알림은 다시 빌려 주지 않는다")

	_, err := pool.Exec(ctx, `UPDATE schedule_poster_notices SET lease_until = now() - interval '1 second'`)
	require.NoError(t, err)

	notice, ok = claimNotice(t, store)
	require.True(t, ok, "기한이 지난 임차는 다시 빌린다")

	var memberID int

	require.NoError(t, pool.QueryRow(ctx, `SELECT min(id) FROM members`).Scan(&memberID))

	schedule, _, found, err := store.MemberPosterRows(ctx, 101, memberID, sophiaChannel, date(t, "2026-11-15"))
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 101, schedule.PosterID)
	require.Len(t, schedule.Rows, 1)

	_, _, found, err = store.MemberPosterRows(ctx, 101, memberID, sophiaChannel, date(t, "2026-11-16"))
	require.NoError(t, err)
	require.False(t, found, "끝난 주의 포스터는 알리지 않는다")

	_, _, found, err = store.MemberPosterRows(ctx, 101, memberID, "UC_other", date(t, "2026-11-12"))
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, store.SettleNotice(ctx, notice, scheduleposter.NoticeSent, 3))
	require.Equal(t, []storedNotice{{kind: scheduleposter.NoticePublished, poster: 101, status: scheduleposter.NoticeSent, rooms: new(3)}}, storedNotices(t, pool))

	_, ok = claimNotice(t, store)
	require.False(t, ok)
}

func TestStoreMemberPosterRowsReadsANextWeekPoster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	confirmPoster101(t, store)
	insertConfirmedWithRows(t, store, pool, 502, "2026-11-16", "2026-11-22", testPostedAt.AddDate(0, 0, 6))

	var memberID int

	require.NoError(t, pool.QueryRow(ctx, `SELECT min(id) FROM members`).Scan(&memberID))

	schedule, _, found, err := store.MemberPosterRows(ctx, 502, memberID, sophiaChannel, date(t, "2026-11-13"))
	require.NoError(t, err)
	require.True(t, found, "다음 주 포스터는 게시되기 전에도 알린다")
	require.EqualValues(t, 502, schedule.PosterID)
}

func TestStoreRecordMatchQueuesChangedNoticesOnlyForAppliedChanges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := pool.Exec(ctx, `DELETE FROM schedule_poster_notices`)
	require.NoError(t, err)

	_, err = store.InsertPosts(ctx, 101, []scheduleposter.Post{
		post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0)), post(15, jstAt(10, 20, 0)), post(16, jstAt(10, 21, 0)),
	})
	require.NoError(t, err)

	for _, id := range []int64{13, 14, 15, 16} {
		decide(t, store, id, relevant)
	}

	for _, tt := range []struct {
		id     int64
		result scheduleposter.MatchResult
		notice bool
	}{
		{13, matched(scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeSame, Slot: 1}), false},
		{14, matched(scheduleposter.Change{PostID: 14, Item: 1, Kind: scheduleposter.ChangeRejected, Reason: "duplicate_row", Slot: 1}), false},
		{15, matchFailed, false},
		{16, matched(scheduleposter.Change{PostID: 16, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("22:00"), Text: "22時から"}), true},
	} {
		recorded, matchErr := store.RecordMatch(ctx, schedule, storedPost(t, pool, tt.id), tt.result)
		require.NoError(t, matchErr)
		require.True(t, recorded)
		require.Equal(t, tt.notice, len(storedNotices(t, pool)) == 1, "바뀐 것이 없거나 대응에 실패한 글은 알리지 않는다")
	}

	require.Equal(t, []storedNotice{{kind: scheduleposter.NoticeChanged, poster: 101, post: new(int64(16)), status: noticePending}}, storedNotices(t, pool))

	notice, ok := claimNotice(t, store)
	require.True(t, ok)
	require.Equal(t, scheduleposter.NoticeChanged, notice.Kind)
	require.EqualValues(t, 16, notice.PostID)

	posts, err := store.NoticeChangePosts(ctx, notice)
	require.NoError(t, err)
	require.Equal(t, []int64{16}, posts)

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET rows_extracted_at = rows_extracted_at + interval '1 second' WHERE id = 101`)
	require.NoError(t, err)

	posts, err = store.NoticeChangePosts(ctx, notice)
	require.NoError(t, err)
	require.Empty(t, posts, "다시 뽑은 묶음에 아직 대응하지 않은 변경은 알리지 않는다")
}

func recordTimeChange(t *testing.T, store *scheduleposter.Store, pool *pgxpool.Pool, schedule scheduleposter.PublishedSchedule, id int64, slot int) {
	t.Helper()

	recorded, err := store.RecordMatch(t.Context(), schedule, storedPost(t, pool, id),
		matched(scheduleposter.Change{PostID: id, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: slot, Start: new("22:00"), Text: "22時から"}))
	require.NoError(t, err)
	require.True(t, recorded)
}

func TestStoreRequeuedChangeIsAnnouncedAfterItsRematch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := pool.Exec(ctx, `DELETE FROM schedule_poster_notices`)
	require.NoError(t, err)

	_, err = store.InsertPosts(ctx, 101, []scheduleposter.Post{post(17, jstAt(10, 18, 0)), post(18, jstAt(10, 19, 0))})
	require.NoError(t, err)

	decide(t, store, 17, relevant)
	decide(t, store, 18, relevant)

	recordTimeChange(t, store, pool, schedule, 18, 1)
	recordTimeChange(t, store, pool, schedule, 17, 2)
	require.Equal(t, scheduleposter.PostRelevant, storedPost(t, pool, 18).Status, "앞선 글의 대응이 뒤 글을 다시 대응하게 돌려놓는다")

	requeued, ok := claimNotice(t, store)
	require.True(t, ok)
	require.EqualValues(t, 18, requeued.PostID)

	posts, err := store.NoticeChangePosts(ctx, requeued)
	require.NoError(t, err)
	require.Equal(t, []int64{17}, posts, "다시 대응하게 돌려진 글은 빠지고 함께 닫힐 현재 변경만 남는다")
	require.NoError(t, store.SettleNotice(ctx, requeued, scheduleposter.NoticeSkipped, 0))

	recordTimeChange(t, store, pool, schedule, 18, 1)

	_, err = pool.Exec(ctx, `INSERT INTO schedule_poster_notices (kind, poster_id, channel_id, post_id) VALUES ('changed', 101, $1, 99)`, sophiaChannel)
	require.NoError(t, err)

	notice, ok := claimNotice(t, store)
	require.True(t, ok)
	require.EqualValues(t, 17, notice.PostID)

	posts, err = store.NoticeChangePosts(ctx, notice)
	require.NoError(t, err)
	require.Equal(t, []int64{17, 18}, posts, "카드에 표시할 변경은 보낸 뒤 함께 닫히는 알림과 같다")
	require.NoError(t, store.SettleNotice(ctx, notice, scheduleposter.NoticeSent, 2))
	require.Equal(t, []storedNotice{
		{kind: scheduleposter.NoticeChanged, poster: 101, post: new(int64(18)), status: scheduleposter.NoticeSkipped},
		{kind: scheduleposter.NoticeChanged, poster: 101, post: new(int64(17)), status: scheduleposter.NoticeSent, rooms: new(2)},
		{kind: scheduleposter.NoticeChanged, poster: 101, post: new(int64(18)), status: scheduleposter.NoticeSent, rooms: new(2)},
		{kind: scheduleposter.NoticeChanged, poster: 101, post: new(int64(99)), status: noticePending},
	}, storedNotices(t, pool), "다시 대응한 글은 새로 알리고, 보낸 카드에 담긴 대기 변경만 함께 닫는다")
}

func TestStoreNoticeChangePostsListsOnlyTheChangesTheSettleCloses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := pool.Exec(ctx, `DELETE FROM schedule_poster_notices`)
	require.NoError(t, err)

	_, err = store.InsertPosts(ctx, 101, []scheduleposter.Post{post(17, jstAt(10, 18, 0)), post(18, jstAt(10, 19, 0))})
	require.NoError(t, err)

	decide(t, store, 17, relevant)
	decide(t, store, 18, relevant)

	recordTimeChange(t, store, pool, schedule, 17, 1)
	recordTimeChange(t, store, pool, schedule, 18, 2)

	notice, ok := claimNotice(t, store)
	require.True(t, ok)
	require.EqualValues(t, 17, notice.PostID)

	for _, tt := range []struct {
		name, sibling string
		want          []int64
	}{
		{"leased by another processor", `lease_until = now() + interval '1 minute'`, []int64{17}},
		{"created after the claim", `lease_until = NULL, created_at = $1::timestamptz + interval '1 second'`, []int64{17}},
		{"already settled", `created_at = $1::timestamptz, status = 'skipped', settled_at = now()`, []int64{17}},
		{"pending and unleased", `status = 'pending', settled_at = NULL`, []int64{17, 18}},
	} {
		_, err = pool.Exec(ctx, `UPDATE schedule_poster_notices SET `+tt.sibling+` WHERE post_id = 18 AND $1::timestamptz IS NOT NULL`, notice.ClaimedAt)
		require.NoError(t, err)

		posts, listErr := store.NoticeChangePosts(ctx, notice)
		require.NoError(t, listErr)
		require.Equal(t, tt.want, posts, tt.name)
	}

	require.NoError(t, store.SettleNotice(ctx, notice, scheduleposter.NoticeSent, 1))
	require.Equal(t, []storedNotice{
		{kind: scheduleposter.NoticeChanged, poster: 101, post: new(int64(17)), status: scheduleposter.NoticeSent, rooms: new(1)},
		{kind: scheduleposter.NoticeChanged, poster: 101, post: new(int64(18)), status: scheduleposter.NoticeSent, rooms: new(1)},
	}, storedNotices(t, pool), "목록에 든 변경이 곧 함께 닫히는 변경이다")
}

func TestStoreSettleSentPublishedNoticeAbsorbsPendingChangesOnItsCard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{post(17, jstAt(10, 18, 0))})
	require.NoError(t, err)

	decide(t, store, 17, relevant)

	recordTimeChange(t, store, pool, schedule, 17, 1)

	_, err = pool.Exec(ctx, `INSERT INTO schedule_poster_notices (kind, poster_id, channel_id, post_id) VALUES ('changed', 102, $1, 23)`, sophiaChannel)
	require.NoError(t, err)

	notice, ok := claimNotice(t, store)
	require.True(t, ok)
	require.Equal(t, scheduleposter.NoticePublished, notice.Kind)
	require.NoError(t, store.SettleNotice(ctx, notice, scheduleposter.NoticeSent, 2))
	require.Equal(t, []storedNotice{
		{kind: scheduleposter.NoticePublished, poster: 101, status: scheduleposter.NoticeSent, rooms: new(2)},
		{kind: scheduleposter.NoticeChanged, poster: 101, post: new(int64(17)), status: scheduleposter.NoticeSent, rooms: new(2)},
		{kind: scheduleposter.NoticeChanged, poster: 102, post: new(int64(23)), status: noticePending},
	}, storedNotices(t, pool), "새 일정 카드에 이미 담긴 변경은 따로 알리지 않는다")

	notice, ok = claimNotice(t, store)
	require.True(t, ok)
	require.NoError(t, store.SettleNotice(ctx, notice, scheduleposter.NoticeSkipped, 0))
	require.Equal(t, scheduleposter.NoticeSkipped, storedNotices(t, pool)[2].status)
	require.Nil(t, storedNotices(t, pool)[2].rooms)
}
