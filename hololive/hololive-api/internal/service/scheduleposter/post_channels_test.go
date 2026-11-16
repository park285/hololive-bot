package scheduleposter_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const sharedPost = 61

func memberPost(t *testing.T, pool *pgxpool.Pool, channelID string) scheduleposter.Post {
	t.Helper()

	var (
		item     scheduleposter.Post
		posterID *int64
		rowsAt   *time.Time
	)

	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT post_id, posted_at, body, status, created_at, matched_poster_id, matched_rows_at
		 FROM schedule_poster_posts WHERE post_id = $1 AND channel_id = $2`,
		sharedPost, channelID).Scan(&item.ID, &item.PostedAt, &item.Body, &item.Status, &item.CreatedAt, &posterID, &rowsAt))

	if posterID != nil && rowsAt != nil {
		item.MatchedPosterID, item.MatchedRowsAt = *posterID, *rowsAt
	}

	return item
}

func changeKinds(changes []scheduleposter.Change) []scheduleposter.ChangeKind {
	kinds := make([]scheduleposter.ChangeKind, len(changes))
	for i := range changes {
		kinds[i] = changes[i].Kind
	}

	return kinds
}

func TestStoreKeepsOnePostSeparatelyForEachMember(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, sophia := newChangesStore(t)
	tsuzuri := confirmedPosterWithRows(t, store, 201, tsuzuriChannel, testPostedAt, "2026-11-09", "2026-11-15")

	for _, posterID := range []int64{101, 201} {
		stored, err := store.InsertPosts(ctx, posterID, []scheduleposter.Post{post(sharedPost, jstAt(10, 18, 0))})
		require.NoError(t, err)
		require.EqualValues(t, 1, stored)
	}

	recorded, err := store.RecordDecision(ctx, sophiaChannel, sharedPost, relevant)
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, sophia, memberPost(t, pool, sophiaChannel), matched(
		scheduleposter.Change{PostID: sharedPost, Item: 1, Kind: scheduleposter.ChangeCancelled, Slot: 1, Status: scheduleposter.RowCancelled},
	))
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, scheduleposter.PostUnjudged, memberPost(t, pool, tsuzuriChannel).Status, "다른 멤버 앞으로 기록한 같은 글은 따로 판별한다")

	recorded, err = store.RecordDecision(ctx, tsuzuriChannel, sharedPost, relevant)
	require.NoError(t, err)
	require.True(t, recorded)

	recorded, err = store.RecordMatch(ctx, tsuzuri, memberPost(t, pool, tsuzuriChannel), matched(
		scheduleposter.Change{PostID: sharedPost, Item: 1, Kind: scheduleposter.ChangeTimeChanged, Slot: 1, Start: new("21:00")},
	))
	require.NoError(t, err)
	require.True(t, recorded)

	for posterID, kind := range map[int64]scheduleposter.ChangeKind{101: scheduleposter.ChangeCancelled, 201: scheduleposter.ChangeTimeChanged} {
		posts, listErr := store.PosterPosts(ctx, posterID)
		require.NoError(t, listErr)
		require.Len(t, posts, 1)
		require.Equal(t, []scheduleposter.ChangeKind{kind}, changeKinds(posts[0].Changes))
	}

	require.Equal(t, []scheduleposter.ChangeKind{scheduleposter.ChangeCancelled}, changeKinds(publishedSophia(t, store).Changes))

	require.NoError(t, store.RevokePost(ctx, 101, sharedPost))
	require.Equal(t, scheduleposter.PostRevoked, memberPost(t, pool, sophiaChannel).Status)
	require.Equal(t, scheduleposter.PostMatched, memberPost(t, pool, tsuzuriChannel).Status, "한 멤버의 철회는 다른 멤버의 행을 건드리지 않는다")
	require.Empty(t, publishedSophia(t, store).Changes)

	published, found, err := store.PublishedRows(ctx, tsuzuriChannel, date(t, "2026-11-12"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []scheduleposter.ChangeKind{scheduleposter.ChangeTimeChanged}, changeKinds(published.Changes))
}

func TestStoreInsertPostsRejudgesAPostWhoseBodyGrew(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool, _, schedule := newChangesStore(t)
	first, second := post(13, jstAt(10, 18, 0)), post(14, jstAt(10, 19, 0))

	_, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{first, second})
	require.NoError(t, err)
	decide(t, store, 13, relevant)
	decide(t, store, 14, relevant)

	recorded, err := store.RecordMatch(ctx, schedule, storedPost(t, pool, 13), matched(
		scheduleposter.Change{PostID: 13, Item: 1, Kind: scheduleposter.ChangeCancelled, Slot: 1, Status: scheduleposter.RowCancelled},
	))
	require.NoError(t, err)
	require.True(t, recorded)
	require.NoError(t, store.RevokePost(ctx, 101, 14))

	rewritten := first

	rewritten.Body = "明日は21時から、長めにやります"

	stored, err := store.InsertPosts(ctx, 101, []scheduleposter.Post{first, rewritten})
	require.Error(t, err, "같은 글 번호 둘은 받지 않는다")
	require.Zero(t, stored)

	for _, again := range []scheduleposter.Post{first, rewritten} {
		stored, err = store.InsertPosts(ctx, 101, []scheduleposter.Post{again})
		require.NoError(t, err)
		require.Zero(t, stored, "같은 본문과 이어 붙인 것이 아닌 본문은 저장한 글을 바꾸지 않는다")
	}

	require.Equal(t, scheduleposter.PostMatched, storedPost(t, pool, 13).Status)
	require.Equal(t, 1, changeCount(t, pool))

	const quote = "\n\nQuoted post by 霧島なぎ at 2026-11-10 17:00 JST:\n20時からコラボ"

	first.Body += quote
	second.Body += quote

	stored, err = store.InsertPosts(ctx, 101, []scheduleposter.Post{first, second})
	require.NoError(t, err)
	require.EqualValues(t, 1, stored)

	grown := storedPost(t, pool, 13)
	require.Equal(t, scheduleposter.PostUnjudged, grown.Status)
	require.Equal(t, first.Body, grown.Body)
	require.Zero(t, grown.MatchedPosterID)
	require.Equal(t, storedAnswer{}, storedAnswers(t, pool)[13], "판별 답을 비워 다시 판별한다")
	require.Zero(t, changeCount(t, pool), "다시 판별하는 글의 옛 변경은 지운다")
	require.Empty(t, publishedSophia(t, store).Changes)
	require.Equal(t, []int64{13}, pendingPostIDs(t, store, schedule))

	revoked := storedPost(t, pool, 14)
	require.Equal(t, scheduleposter.PostRevoked, revoked.Status)
	require.Equal(t, second.Body[:len(second.Body)-len(quote)], revoked.Body, "철회한 글은 본문이 늘어도 그대로 둔다")
}
