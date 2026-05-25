package delivery

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestStatusUpdaterMarkSentBatchUpdatesPendingRowsOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}))

	lockedAt := time.Now().UTC().Add(-time.Minute)
	pending := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_status",
		ContentID:     "pending",
		Payload:       `{"video_id":"pending","title":"pending"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: time.Now().UTC(),
		LockedAt:      &lockedAt,
		Error:         "old error",
	}
	failed := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_status",
		ContentID:     "failed",
		Payload:       `{"video_id":"failed","title":"failed"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  1,
		NextAttemptAt: time.Now().UTC(),
		LockedAt:      &lockedAt,
		Error:         "keep error",
	}
	require.NoError(t, db.Create(&pending).Error)
	require.NoError(t, db.Create(&failed).Error)

	updater := newStatusUpdater(db, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxRetries: 3, RetryBackoff: time.Minute})
	updater.markSentBatch(ctx, []int64{pending.ID, pending.ID, failed.ID})

	var gotPending domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&gotPending, pending.ID).Error)
	require.Equal(t, domain.OutboxStatusSent, gotPending.Status)
	require.NotNil(t, gotPending.SentAt)
	require.Nil(t, gotPending.LockedAt)
	require.Empty(t, gotPending.Error)

	var gotFailed domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&gotFailed, failed.ID).Error)
	require.Equal(t, domain.OutboxStatusFailed, gotFailed.Status)
	require.Nil(t, gotFailed.SentAt)
	require.NotNil(t, gotFailed.LockedAt)
	require.Equal(t, "keep error", gotFailed.Error)
}

func TestStatusUpdaterMarkFailedSchedulesRetryBeforeMaxRetries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}))

	lockedAt := time.Now().UTC().Add(-time.Minute)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_status",
		ContentID:     "retry",
		Payload:       `{"video_id":"retry","title":"retry"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now().UTC().Add(-time.Hour),
		LockedAt:      &lockedAt,
	}
	require.NoError(t, db.Create(&item).Error)

	updater := newStatusUpdater(db, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxRetries: 3, RetryBackoff: time.Minute})
	before := time.Now().UTC()
	updater.markFailed(ctx, item.ID, "temporary failure")

	var got domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&got, item.ID).Error)
	require.Equal(t, domain.OutboxStatusPending, got.Status)
	require.Equal(t, 1, got.AttemptCount)
	require.Nil(t, got.LockedAt)
	require.Equal(t, "temporary failure", got.Error)
	require.True(t, got.NextAttemptAt.After(before))
	require.True(t, got.NextAttemptAt.Before(before.Add(2*time.Minute)))
}

func TestStatusUpdaterMarkFailedMarksPermanentAtMaxRetriesAndTruncatesReason(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}))

	lockedAt := time.Now().UTC().Add(-time.Minute)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_status",
		ContentID:     "permanent",
		Payload:       `{"video_id":"permanent","title":"permanent"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  2,
		NextAttemptAt: time.Now().UTC().Add(-time.Hour),
		LockedAt:      &lockedAt,
	}
	require.NoError(t, db.Create(&item).Error)

	updater := newStatusUpdater(db, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxRetries: 3, RetryBackoff: time.Minute})
	updater.markFailed(ctx, item.ID, strings.Repeat("가", 600))

	var got domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&got, item.ID).Error)
	require.Equal(t, domain.OutboxStatusFailed, got.Status)
	require.Equal(t, 3, got.AttemptCount)
	require.Nil(t, got.LockedAt)
	require.Len(t, []rune(got.Error), 500)
	require.True(t, strings.HasSuffix(got.Error, "..."))
}

func TestStatusUpdaterMarkSentIfLockedSkipsRowsRelockedByAnotherWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}))

	staleLockedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	currentLockedAt := staleLockedAt.Add(time.Minute)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_status",
		ContentID:     "relocked",
		Payload:       `{"video_id":"relocked","title":"relocked"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now().UTC(),
		LockedAt:      &currentLockedAt,
	}
	require.NoError(t, db.Create(&item).Error)

	updater := newStatusUpdater(db, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxRetries: 3, RetryBackoff: time.Minute})
	updater.markSentIfLocked(ctx, item.ID, &staleLockedAt)

	var got domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&got, item.ID).Error)
	require.Equal(t, domain.OutboxStatusPending, got.Status)
	require.Nil(t, got.SentAt)
	require.NotNil(t, got.LockedAt)
	require.True(t, got.LockedAt.Equal(currentLockedAt), "locked_at = %s, want %s", got.LockedAt, currentLockedAt)
}

func TestStatusUpdaterMarkFailedIfLockedSkipsRowsCompletedByAnotherWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}))

	staleLockedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	sentAt := time.Now().UTC()
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_status",
		ContentID:     "sent",
		Payload:       `{"video_id":"sent","title":"sent"}`,
		Status:        domain.OutboxStatusSent,
		AttemptCount:  0,
		NextAttemptAt: time.Now().UTC(),
		SentAt:        &sentAt,
	}
	require.NoError(t, db.Create(&item).Error)

	updater := newStatusUpdater(db, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxRetries: 3, RetryBackoff: time.Minute})
	updater.markFailedIfLocked(ctx, item.ID, &staleLockedAt, "stale failure")

	var got domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&got, item.ID).Error)
	require.Equal(t, domain.OutboxStatusSent, got.Status)
	require.Equal(t, 0, got.AttemptCount)
	require.Nil(t, got.LockedAt)
	require.NotNil(t, got.SentAt)
	require.Empty(t, got.Error)
}
