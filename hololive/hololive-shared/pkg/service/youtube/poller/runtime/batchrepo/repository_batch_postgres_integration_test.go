//go:build integration

package batchrepo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPgxBatchRepositoryInsertNotificationsChunkPostgresDeduplicatesSameBatchIdentity(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	schema := fmt.Sprintf("polling_batch_test_%d", time.Now().UnixNano())
	_, err = pool.Exec(ctx, "CREATE SCHEMA "+quotePostgresIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotePostgresIdentifier(schema)+" CASCADE")
		require.NoError(t, err)
	})

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, "SET LOCAL search_path TO "+quotePostgresIdentifier(schema))
	require.NoError(t, err)
	require.NoError(t, createPostgresNotificationOutboxTable(ctx, tx))

	repository := &PgxBatchRepository{DB: pool}
	err = repository.insertNotificationsChunk(ctx, tx, []*domain.YouTubeNotificationOutbox{
		{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: "channel-1",
			ContentID: "video-1",
			Payload:   `{"video_id":"video-1","kind":"first"}`,
			Status:    domain.OutboxStatusPending,
		},
		{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: "channel-1",
			ContentID: "video-1",
			Payload:   `{"video_id":"video-1","kind":"duplicate"}`,
			Status:    domain.OutboxStatusPending,
		},
	})
	require.NoError(t, err)

	var outboxCount int64
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM youtube_notification_outbox
		WHERE kind = $1 AND content_id = $2`,
		domain.OutboxKindNewVideo,
		"video-1",
	).Scan(&outboxCount)
	require.NoError(t, err)
	require.EqualValues(t, 1, outboxCount)
	require.NoError(t, tx.Commit(ctx))
}

func createPostgresNotificationOutboxTable(ctx context.Context, tx batchDB) error {
	_, err := tx.Exec(ctx, `
		CREATE TABLE youtube_notification_outbox (
			id BIGSERIAL PRIMARY KEY,
			kind TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			content_id TEXT NOT NULL,
			payload JSONB NOT NULL,
			status TEXT NOT NULL,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			locked_at TIMESTAMPTZ,
			sent_at TIMESTAMPTZ,
			error TEXT,
			UNIQUE(kind, content_id)
		)
	`)
	return err
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
