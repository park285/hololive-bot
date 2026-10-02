package consume

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/content"
)

// 여러 영상의 필드 변경을 한 transaction의 묶음 전송으로 반영해도 행마다 순차 실행과 같은 결과여야 한다.
// 변경에 published_at이 없으면 저장된 값을 유지한다.
func TestPersistContentFieldUpdatesAppliesEveryRowInBatch(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	storedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, published_at, first_seen_at, last_seen_at)
		VALUES ('vid-a', $1, 'old a', $2, $2, $2), ('vid-b', $1, 'old b', $2, $2, $2), ('vid-c', $1, 'same c', $2, $2, $2)
	`, testChannelID, storedAt)
	require.NoError(t, err)

	newPublished := storedAt.Add(time.Hour)
	seenAt := storedAt.Add(2 * time.Hour)
	updates := []content.Entity{
		{VideoID: "vid-a", Title: "new a", PublishedAt: &newPublished},
		{VideoID: "vid-b", Title: "new b"},
	}

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, persistContentFieldUpdates(ctx, tx, updates, seenAt))
	require.NoError(t, tx.Commit(ctx))

	rows, err := pool.Query(ctx, `SELECT video_id, title, published_at, last_seen_at FROM youtube_videos ORDER BY video_id`)
	require.NoError(t, err)

	defer rows.Close()

	type videoRow struct {
		title     string
		published time.Time
		lastSeen  time.Time
	}

	got := map[string]videoRow{}

	for rows.Next() {
		var (
			id  string
			row videoRow
		)

		require.NoError(t, rows.Scan(&id, &row.title, &row.published, &row.lastSeen))

		row.published, row.lastSeen = row.published.UTC(), row.lastSeen.UTC()
		got[id] = row
	}

	require.NoError(t, rows.Err())
	require.Equal(t, videoRow{title: "new a", published: newPublished, lastSeen: seenAt}, got["vid-a"])
	require.Equal(t, videoRow{title: "new b", published: storedAt, lastSeen: seenAt}, got["vid-b"])
	require.Equal(t, videoRow{title: "same c", published: storedAt, lastSeen: storedAt}, got["vid-c"])
}
