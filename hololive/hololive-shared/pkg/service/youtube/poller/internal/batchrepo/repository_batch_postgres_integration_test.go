//go:build integration

package batchrepo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestGormBatchRepositoryInsertNotificationsChunkPostgresDeduplicatesSameBatchIdentity(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	ctx := context.Background()
	schema := fmt.Sprintf("polling_batch_test_%d", time.Now().UnixNano())
	require.NoError(t, db.WithContext(ctx).Exec("CREATE SCHEMA "+quotePostgresIdentifier(schema)).Error)
	t.Cleanup(func() {
		require.NoError(t, db.WithContext(context.Background()).Exec("DROP SCHEMA IF EXISTS "+quotePostgresIdentifier(schema)+" CASCADE").Error)
	})

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO " + quotePostgresIdentifier(schema)).Error; err != nil {
			return err
		}
		if err := createPostgresNotificationOutboxTable(tx); err != nil {
			return err
		}

		repository := &GormBatchRepository{DB: tx}
		if err := repository.insertNotificationsChunk(ctx, tx, []*domain.YouTubeNotificationOutbox{
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
		}); err != nil {
			return err
		}

		var outboxCount int64
		if err := tx.Model(&domain.YouTubeNotificationOutbox{}).
			Where("kind = ? AND content_id = ?", domain.OutboxKindNewVideo, "video-1").
			Count(&outboxCount).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, outboxCount)
		return nil
	})
	require.NoError(t, err)
}

func createPostgresNotificationOutboxTable(tx *gorm.DB) error {
	return tx.Exec(`
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
	`).Error
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
