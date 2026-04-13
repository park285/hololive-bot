package tracking

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryUpsertAndFindByIdentity(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-1",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AlarmSentAt:       &alarmSentAt,
	}))

	record, err := repo.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, domain.OutboxKindCommunityPost, record.Kind)
	require.Equal(t, "post-1", record.ContentID)
	require.Equal(t, "UC_TEST", record.ChannelID)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, record.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, record.DetectedAt.UTC())
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, alarmSentAt, record.AlarmSentAt.UTC())
	require.NotNil(t, record.AlarmLatencyMillis)
	require.Equal(t, int64(147000), *record.AlarmLatencyMillis)
	require.NotNil(t, record.AlarmLatencyExceeded)
	require.True(t, *record.AlarmLatencyExceeded)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, record.DeliveryStatus)
}

func TestRepositoryUpsertPreservesEarliestDetectionAndSentAt(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	firstDetectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	laterDetectedAt := time.Date(2026, 4, 10, 1, 7, 0, 0, time.UTC)
	firstAlarmSentAt := time.Date(2026, 4, 10, 1, 8, 0, 0, time.UTC)
	laterAlarmSentAt := time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)
	actualPublishedAt := time.Date(2026, 4, 10, 1, 1, 0, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewShort,
		ContentID:  "short-1",
		ChannelID:  "UC_SHORT",
		DetectedAt: firstDetectedAt,
	}))
	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short-1",
		ChannelID:         "UC_SHORT",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        laterDetectedAt,
		AlarmSentAt:       &laterAlarmSentAt,
	}))
	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:        domain.OutboxKindNewShort,
		ContentID:   "short-1",
		ChannelID:   "UC_SHORT",
		DetectedAt:  laterDetectedAt,
		AlarmSentAt: &firstAlarmSentAt,
	}))

	record, err := repo.FindByIdentity(ctx, domain.OutboxKindNewShort, "short-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, record.ActualPublishedAt.UTC())
	require.Equal(t, firstDetectedAt, record.DetectedAt.UTC())
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, firstAlarmSentAt, record.AlarmSentAt.UTC())
	require.NotNil(t, record.AlarmLatencyMillis)
	require.Equal(t, int64(7*time.Minute/time.Millisecond), *record.AlarmLatencyMillis)
	require.NotNil(t, record.AlarmLatencyExceeded)
	require.True(t, *record.AlarmLatencyExceeded)
}

func TestRepositoryFindByIdentitySupportsShortCanonicalAlias(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewShort,
		ContentID:  "short-1",
		ChannelID:  "UC_SHORT",
		DetectedAt: detectedAt,
	}))

	record, err := repo.FindByIdentity(ctx, domain.OutboxKindNewShort, "short:short-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "short-1", record.ContentID)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, record.DeliveryStatus)
}

func TestRepositoryUpsertDedupesByCanonicalContentIdentity(t *testing.T) {
	db := newTrackingTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	laterDetectedAt := detectedAt.Add(2 * time.Minute)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewShort,
		ContentID:  "short-1",
		ChannelID:  "UC_SHORT",
		DetectedAt: detectedAt,
	}))
	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewShort,
		ContentID:  "short:short-1",
		ChannelID:  "UC_SHORT",
		DetectedAt: laterDetectedAt,
	}))

	var rows []domain.YouTubeContentAlarmTracking
	require.NoError(t, db.Order("content_id ASC").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "short-1", rows[0].ContentID)
	require.Equal(t, "short:short-1", rows[0].CanonicalContentID)
	require.Equal(t, detectedAt, rows[0].DetectedAt.UTC())
}

func TestRepositoryUpsertRecomputesLatencyWhenPublishedAtBackfillsAfterSentAt(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	laterDetectedAt := time.Date(2026, 4, 10, 1, 6, 0, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 7, 30, 0, time.UTC)
	actualPublishedAt := time.Date(2026, 4, 10, 1, 6, 0, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:        domain.OutboxKindCommunityPost,
		ContentID:   "post-backfill",
		ChannelID:   "UC_BACKFILL",
		DetectedAt:  detectedAt,
		AlarmSentAt: &alarmSentAt,
	}))

	record, err := repo.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-backfill")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Nil(t, record.AlarmLatencyMillis)
	require.Nil(t, record.AlarmLatencyExceeded)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-backfill",
		ChannelID:         "UC_BACKFILL",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        laterDetectedAt,
	}))

	record, err = repo.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-backfill")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, record.ActualPublishedAt.UTC())
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, alarmSentAt, record.AlarmSentAt.UTC())
	require.NotNil(t, record.AlarmLatencyMillis)
	require.Equal(t, int64(90*time.Second/time.Millisecond), *record.AlarmLatencyMillis)
	require.NotNil(t, record.AlarmLatencyExceeded)
	require.False(t, *record.AlarmLatencyExceeded)
}

func TestPendingPublishedAtResolver_KeysetPaginationStableWithSameDetectedAt(t *testing.T) {
	db := newTrackingTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	rows := []*domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:short-a",
			ContentID:      "short:short-a",
			ChannelID:      "UC_SHORT",
			DetectedAt:     detectedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:short-b",
			ContentID:      "short:short-b",
			ChannelID:      "UC_SHORT",
			DetectedAt:     detectedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:short-c",
			ContentID:      "short:short-c",
			ChannelID:      "UC_SHORT",
			DetectedAt:     detectedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
	}
	require.NoError(t, db.Create(&rows).Error)

	firstPage, cursor, err := repo.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(time.Minute), nil, 2)
	require.NoError(t, err)
	require.Len(t, firstPage, 2)
	require.NotNil(t, cursor)
	require.Equal(t, "short:short-a", firstPage[0].PostID)
	require.Equal(t, "short:short-b", firstPage[1].PostID)
	require.Equal(t, detectedAt, cursor.DetectedAt.UTC())
	require.Equal(t, "short:short-b", cursor.PostID)

	secondPage, nextCursor, err := repo.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(time.Minute), cursor, 2)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.Nil(t, nextCursor)
	require.Equal(t, "short:short-c", secondPage[0].PostID)
}

func TestListPendingPublishedAtResolutionsPage_ExcludesFutureRetryAfter(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	referenceNow := detectedAt.Add(time.Minute)
	futureRetryAfter := referenceNow.Add(time.Minute)

	require.NoError(t, repo.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:eligible",
			ContentID:      "short:eligible",
			ChannelID:      "UC_SHORT",
			DetectedAt:     detectedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:                  domain.OutboxKindNewShort,
			PostID:                "short:backoff",
			ContentID:             "short:backoff",
			ChannelID:             "UC_SHORT",
			DetectedAt:            detectedAt,
			PublishedAtRetryAfter: &futureRetryAfter,
			DeliveryStatus:        domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
	}))
	require.NoError(t, repo.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:backoff", futureRetryAfter))

	candidates, _, err := repo.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, detectedAt.Add(2*time.Minute), nil, 10)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "short:eligible", candidates[0].PostID)
}

func TestListPendingPublishedAtResolutionsPage_IncludesRetryAfterExpired(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	referenceNow := detectedAt.Add(2 * time.Minute)
	expiredRetryAfter := detectedAt.Add(time.Minute)

	require.NoError(t, repo.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:                  domain.OutboxKindNewShort,
			PostID:                "short:expired",
			ContentID:             "short:expired",
			ChannelID:             "UC_SHORT",
			DetectedAt:            detectedAt,
			PublishedAtRetryAfter: &expiredRetryAfter,
			DeliveryStatus:        domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
	}))
	require.NoError(t, repo.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:expired", expiredRetryAfter))

	candidates, _, err := repo.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, detectedAt.Add(3*time.Minute), nil, 10)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "short:expired", candidates[0].PostID)
}

func TestListPendingPublishedAtResolutionsPage_IncludesAuthorizedAndSentRowsWithoutPublishedAt(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := detectedAt.Add(15 * time.Second)
	alarmSentAt := detectedAt.Add(30 * time.Second)

	require.NoError(t, repo.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:authorized",
			ContentID:      "short:authorized",
			ChannelID:      "UC_SHORT",
			DetectedAt:     detectedAt,
			AuthorizedAt:   &authorizedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		},
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:sent",
			ContentID:      "short:sent",
			ChannelID:      "UC_SHORT",
			DetectedAt:     detectedAt.Add(time.Second),
			AlarmSentAt:    &alarmSentAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusSent,
		},
	}))

	candidates, _, err := repo.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(2*time.Minute), nil, 10)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "short:authorized", candidates[0].PostID)
	require.Equal(t, "short:sent", candidates[1].PostID)
}

func TestListPendingPublishedAtResolutionsPage_PrioritizesPendingRowsBeforeMetadataOnlyBackfill(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := detectedAt.Add(10 * time.Second)

	require.NoError(t, repo.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:metadata-only",
			ContentID:      "short:metadata-only",
			ChannelID:      "UC_SHORT",
			DetectedAt:     detectedAt,
			AuthorizedAt:   &authorizedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		},
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:pending",
			ContentID:      "short:pending",
			ChannelID:      "UC_SHORT",
			DetectedAt:     detectedAt.Add(time.Second),
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
	}))

	firstPage, cursor, err := repo.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(2*time.Minute), nil, 1)
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	require.NotNil(t, cursor)
	require.Equal(t, "short:pending", firstPage[0].PostID)
	require.Equal(t, 0, cursor.PriorityBucket)

	secondPage, nextCursor, err := repo.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(2*time.Minute), cursor, 1)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.Equal(t, "short:metadata-only", secondPage[0].PostID)

	thirdPage, finalCursor, err := repo.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(2*time.Minute), nextCursor, 1)
	require.NoError(t, err)
	require.Nil(t, thirdPage)
	require.Nil(t, finalCursor)
}

func TestMarkAndClearPublishedAtRetryAfter(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	retryAfter := detectedAt.Add(2 * time.Minute)

	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindNewShort,
		PostID:         "short:retry",
		ContentID:      "short:retry",
		ChannelID:      "UC_SHORT",
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}))

	require.NoError(t, repo.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:retry", retryAfter))
	row, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:retry")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.NotNil(t, row.PublishedAtRetryAfter)
	require.Equal(t, retryAfter, row.PublishedAtRetryAfter.UTC())

	require.NoError(t, repo.ClearPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:retry"))
	row, err = repo.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:retry")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Nil(t, row.PublishedAtRetryAfter)
}

func TestListPendingPublishedAtResolutionsPage_LegacySchemaWithoutRetryAfterColumnReturnsError(t *testing.T) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s_legacy?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE youtube_community_shorts_alarm_states (
			kind TEXT NOT NULL,
			post_id TEXT NOT NULL,
			content_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			actual_published_at DATETIME,
			detected_at DATETIME NOT NULL,
			authorized_at DATETIME,
			alarm_sent_at DATETIME,
			delivery_status TEXT NOT NULL DEFAULT 'DETECTED',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY (kind, post_id)
		)
	`).Error)

	repo := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	now := detectedAt.Add(time.Minute)
	require.NoError(t, db.Exec(`
		INSERT INTO youtube_community_shorts_alarm_states
			(kind, post_id, content_id, channel_id, detected_at, delivery_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, domain.OutboxKindNewShort, "short:legacy", "short:legacy", "UC_SHORT", detectedAt, domain.YouTubeCommunityShortsAlarmStateStatusDetected, now, now).Error)

	candidates, _, err := repo.ListPendingPublishedAtResolutionsPage(ctx, now, detectedAt.Add(2*time.Minute), nil, 10)
	require.ErrorContains(t, err, "published_at_retry_after")
	require.Nil(t, candidates)
}

func TestMarkAndClearPublishedAtRetryAfter_WithoutRetryAfterColumnReturnsError(t *testing.T) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s_legacy_mark?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE youtube_community_shorts_alarm_states (
			kind TEXT NOT NULL,
			post_id TEXT NOT NULL,
			content_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			actual_published_at DATETIME,
			detected_at DATETIME NOT NULL,
			authorized_at DATETIME,
			alarm_sent_at DATETIME,
			delivery_status TEXT NOT NULL DEFAULT 'DETECTED',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY (kind, post_id)
		)
	`).Error)

	repo := NewRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	require.NoError(t, db.Exec(`
		INSERT INTO youtube_community_shorts_alarm_states
			(kind, post_id, content_id, channel_id, detected_at, delivery_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, domain.OutboxKindNewShort, "short:legacy", "short:legacy", "UC_SHORT", now, domain.YouTubeCommunityShortsAlarmStateStatusDetected, now, now).Error)

	require.ErrorContains(t, repo.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:legacy", now.Add(time.Minute)), "published_at_retry_after")
	require.ErrorContains(t, repo.ClearPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:legacy"), "published_at_retry_after")
}

func TestRepositoryRejectsUnsupportedKind(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	err := repo.Upsert(context.Background(), &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewVideo,
		ContentID:  "video-1",
		ChannelID:  "UC_VIDEO",
		DetectedAt: time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC),
	})
	require.ErrorContains(t, err, "unsupported tracking kind")
}

func TestRepositoryMarkAlarmSentBatchPreservesEarliestTimestamp(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 6, 0, 0, time.UTC)
	firstAlarmSentAt := time.Date(2026, 4, 10, 1, 8, 0, 0, time.UTC)
	laterAlarmSentAt := time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-1",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	require.NoError(t, repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "post-1", AlarmSentAt: laterAlarmSentAt},
		{Kind: domain.OutboxKindCommunityPost, ContentID: "post-1", AlarmSentAt: firstAlarmSentAt},
	}))
	require.NoError(t, repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "post-1", AlarmSentAt: laterAlarmSentAt},
	}))

	record, err := repo.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, firstAlarmSentAt, record.AlarmSentAt.UTC())
	require.NotNil(t, record.AlarmLatencyMillis)
	require.Equal(t, int64(3*time.Minute/time.Millisecond), *record.AlarmLatencyMillis)
	require.NotNil(t, record.AlarmLatencyExceeded)
	require.True(t, *record.AlarmLatencyExceeded)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, record.DeliveryStatus)
}

func TestRepositoryMarkAlarmSentBatchUpdatesLegacyRawShortRowFromCanonicalMark(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 6, 0, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 8, 0, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short-1",
		ChannelID:         "UC_SHORT",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	require.NoError(t, repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:        domain.OutboxKindNewShort,
		ContentID:   "short:short-1",
		AlarmSentAt: alarmSentAt,
	}}))

	record, err := repo.FindByIdentity(ctx, domain.OutboxKindNewShort, "short-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, alarmSentAt, record.AlarmSentAt.UTC())
}

func TestRepositoryUpsertAndFindAlarmStateByPostID(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)

	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-1",
		ContentID:         "post-1",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))

	record, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, domain.OutboxKindCommunityPost, record.Kind)
	require.Equal(t, "community:post-1", record.PostID)
	require.Equal(t, "post-1", record.ContentID)
	require.Equal(t, "UC_TEST", record.ChannelID)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, record.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, record.DetectedAt.UTC())
	require.NotNil(t, record.AuthorizedAt)
	require.Equal(t, authorizedAt, record.AuthorizedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)
}

func TestRepositoryMarkAlarmSentBatchUpdatesAlarmState(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short-1",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindNewShort,
		PostID:            "short:short-1",
		ContentID:         "short-1",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))

	require.NoError(t, repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:        domain.OutboxKindNewShort,
		ContentID:   "short:short-1",
		AlarmSentAt: alarmSentAt,
	}}))

	record, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "short:short-1", record.PostID)
	require.Equal(t, "short-1", record.ContentID)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, alarmSentAt, record.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, record.DeliveryStatus)
}

func TestRepositoryMarkAlarmSentBatchFinalizesMatchingClaimedAlarmState(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-claim-finalize",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "community:post-claim-finalize",
		ContentID:         "post-claim-finalize",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))

	require.NoError(t, repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:         domain.OutboxKindCommunityPost,
		ContentID:    "post-claim-finalize",
		AuthorizedAt: &authorizedAt,
		AlarmSentAt:  alarmSentAt,
	}}))

	record, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "post-claim-finalize")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Nil(t, record.AuthorizedAt)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, alarmSentAt, record.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, record.DeliveryStatus)
}

func TestRepositoryMarkAlarmSentBatchRollsBackOnClaimAuthorizationMismatch(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)
	otherAuthorizedAt := authorizedAt.Add(30 * time.Second)
	alarmSentAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)

	require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short-claim-mismatch",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindNewShort,
		PostID:            "short:short-claim-mismatch",
		ContentID:         "short-claim-mismatch",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))

	err := repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:         domain.OutboxKindNewShort,
		ContentID:    "short-claim-mismatch",
		AuthorizedAt: &otherAuthorizedAt,
		AlarmSentAt:  alarmSentAt,
	}})
	require.ErrorContains(t, err, "claim authorization mismatch")

	trackingRow, trackingErr := repo.FindByIdentity(ctx, domain.OutboxKindNewShort, "short-claim-mismatch")
	require.NoError(t, trackingErr)
	require.NotNil(t, trackingRow)
	require.Nil(t, trackingRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, trackingRow.DeliveryStatus)

	stateRow, stateErr := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short-claim-mismatch")
	require.NoError(t, stateErr)
	require.NotNil(t, stateRow)
	require.NotNil(t, stateRow.AuthorizedAt)
	require.Equal(t, authorizedAt, stateRow.AuthorizedAt.UTC())
	require.Nil(t, stateRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, stateRow.DeliveryStatus)
}

func TestRepositoryTryClaimAlarmStateCreatesMissingRow(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)

	claimed, err := repo.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-claim-create",
		ContentID:         "post-claim-create",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	record, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-claim-create")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "community:post-claim-create", record.PostID)
	require.Equal(t, "post-claim-create", record.ContentID)
	require.Equal(t, "UC_TEST", record.ChannelID)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, record.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, record.DetectedAt.UTC())
	require.NotNil(t, record.AuthorizedAt)
	require.Equal(t, authorizedAt, record.AuthorizedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)
}

func TestRepositoryTryClaimAlarmStateRejectsMismatchedPostAndContentIdentity(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)

	claimed, err := repo.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:         domain.OutboxKindCommunityPost,
		PostID:       "community:post-claim-good",
		ContentID:    "post-claim-other",
		ChannelID:    "UC_TEST",
		DetectedAt:   detectedAt,
		AuthorizedAt: &authorizedAt,
	})
	require.ErrorContains(t, err, "post id/content id mismatch")
	require.False(t, claimed)
}

func TestRepositoryTryClaimAlarmStateReturnsFalseForAlreadyClaimedRow(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	firstAuthorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)
	laterAuthorizedAt := firstAuthorizedAt.Add(30 * time.Second)

	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-claim-existing",
		ContentID:         "post-claim-existing",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &firstAuthorizedAt,
	}))

	claimed, err := repo.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-claim-existing",
		ContentID:         "post-claim-existing",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &laterAuthorizedAt,
	})
	require.NoError(t, err)
	require.False(t, claimed)

	record, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-claim-existing")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AuthorizedAt)
	require.Equal(t, firstAuthorizedAt, record.AuthorizedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)
}

func TestRepositoryTryClaimAlarmStateConcurrentCASClaimsDetectedRowOnce(t *testing.T) {
	repo := NewRepository(newTrackingTestDBWithMaxOpenConns(t, 8))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindNewShort,
		PostID:            "short-claim-race",
		ContentID:         "short-claim-race",
		ChannelID:         "UC_RACE",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	const contenders = 8
	attemptedAuthorizedAt := make([]time.Time, contenders)
	claimedResults := make([]bool, contenders)
	errResults := make([]error, contenders)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < contenders; i++ {
		attemptedAuthorizedAt[i] = detectedAt.Add(time.Duration(i+1) * time.Second)
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			claimedResults[idx], errResults[idx] = repo.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
				Kind:              domain.OutboxKindNewShort,
				PostID:            "short-claim-race",
				ContentID:         "short-claim-race",
				ChannelID:         "UC_RACE",
				ActualPublishedAt: &actualPublishedAt,
				DetectedAt:        detectedAt,
				AuthorizedAt:      &attemptedAuthorizedAt[idx],
			})
		}(i)
	}

	close(start)
	wg.Wait()

	successCount := 0
	for i := 0; i < contenders; i++ {
		require.NoError(t, errResults[i])
		if claimedResults[i] {
			successCount++
		}
	}
	require.Equal(t, 1, successCount)

	record, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:short-claim-race")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AuthorizedAt)
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)

	matchedAttempt := false
	for i := 0; i < contenders; i++ {
		if record.AuthorizedAt.UTC().Equal(attemptedAuthorizedAt[i]) {
			matchedAttempt = true
			break
		}
	}
	require.True(t, matchedAttempt)
}

func TestRepositoryReleaseAlarmStateClaimClearsMatchingUnsentAuthorization(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)

	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindCommunityPost,
		PostID:         "post-release-claim",
		ContentID:      "post-release-claim",
		ChannelID:      "UC_TEST",
		DetectedAt:     detectedAt,
		AuthorizedAt:   &authorizedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}))

	released, err := repo.ReleaseAlarmStateClaim(ctx, domain.OutboxKindCommunityPost, "community:post-release-claim", authorizedAt)
	require.NoError(t, err)
	require.True(t, released)

	record, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-release-claim")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Nil(t, record.AuthorizedAt)
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, record.DeliveryStatus)
}

func TestRepositoryReleaseAlarmStateClaimReturnsFalseForMismatchedAuthorization(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)
	otherAuthorizedAt := authorizedAt.Add(30 * time.Second)

	require.NoError(t, repo.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindNewShort,
		PostID:         "short-release-mismatch",
		ContentID:      "short-release-mismatch",
		ChannelID:      "UC_TEST",
		DetectedAt:     detectedAt,
		AuthorizedAt:   &authorizedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}))

	released, err := repo.ReleaseAlarmStateClaim(ctx, domain.OutboxKindNewShort, "short:short-release-mismatch", otherAuthorizedAt)
	require.NoError(t, err)
	require.False(t, released)

	record, err := repo.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:short-release-mismatch")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AuthorizedAt)
	require.Equal(t, authorizedAt, record.AuthorizedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)
}

func TestRepositoryUpsertAndListSourcePostsWithinDetectedWindow(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	windowStart := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	shortDetectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	shortDetectedLaterAt := time.Date(2026, 4, 10, 1, 7, 0, 0, time.UTC)
	shortPublishedAt := time.Date(2026, 4, 10, 1, 2, 30, 0, time.UTC)
	communityDetectedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 4, 10, 1, 10, 0, 0, time.UTC)

	require.NoError(t, repo.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:       domain.OutboxKindNewShort,
			PostID:     "short-1",
			ChannelID:  "UC_SHORT",
			DetectedAt: shortDetectedLaterAt,
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			PostID:     "community:post-1",
			ChannelID:  "UC_COMMUNITY",
			DetectedAt: communityDetectedAt,
		},
	}))
	require.NoError(t, repo.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:short-1",
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
		},
	}))

	rows, err := repo.ListSourcePostsDetectedWithinWindow(ctx, windowStart, windowEnd)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	rowsByKey := make(map[string]domain.YouTubeCommunityShortsSourcePost, len(rows))
	for i := range rows {
		rowsByKey[string(rows[i].Kind)+":"+rows[i].PostID] = rows[i]
	}

	shortRow, ok := rowsByKey[string(domain.OutboxKindNewShort)+":short:short-1"]
	require.True(t, ok)
	require.Equal(t, "UC_SHORT", shortRow.ChannelID)
	require.NotNil(t, shortRow.ActualPublishedAt)
	require.Equal(t, shortPublishedAt, shortRow.ActualPublishedAt.UTC())
	require.Equal(t, shortDetectedAt, shortRow.DetectedAt.UTC())

	communityRow, ok := rowsByKey[string(domain.OutboxKindCommunityPost)+":community:post-1"]
	require.True(t, ok)
	require.Equal(t, "UC_COMMUNITY", communityRow.ChannelID)
	require.Nil(t, communityRow.ActualPublishedAt)
	require.Equal(t, communityDetectedAt, communityRow.DetectedAt.UTC())
}

func TestRepositoryListSourcePostsWithinObservationWindowUsesPublishedAtAndDetectionCutoff(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	windowStart := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 4, 10, 1, 10, 0, 0, time.UTC)
	beforeWindowPublishedAt := windowStart.Add(-30 * time.Second)
	beforeWindowDetectedAt := windowStart.Add(time.Minute)
	fallbackDetectedAt := windowStart.Add(2 * time.Minute)
	includedPublishedAt := windowStart.Add(3 * time.Minute)
	includedDetectedAt := includedPublishedAt.Add(20 * time.Second)
	lateDetectedPublishedAt := windowStart.Add(4 * time.Minute)
	lateDetectedAt := windowEnd.Add(time.Minute)

	require.NoError(t, repo.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            "community:published-before-window",
			ChannelID:         "UC_BEFORE",
			ActualPublishedAt: &beforeWindowPublishedAt,
			DetectedAt:        beforeWindowDetectedAt,
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			PostID:     "community:detected-fallback",
			ChannelID:  "UC_FALLBACK",
			DetectedAt: fallbackDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:included-published",
			ChannelID:         "UC_INCLUDED",
			ActualPublishedAt: &includedPublishedAt,
			DetectedAt:        includedDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:late-detected",
			ChannelID:         "UC_LATE",
			ActualPublishedAt: &lateDetectedPublishedAt,
			DetectedAt:        lateDetectedAt,
		},
	}))

	rows, err := repo.ListSourcePostsWithinObservationWindow(ctx, windowStart, windowEnd, windowEnd)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	rowsByPostID := make(map[string]domain.YouTubeCommunityShortsSourcePost, len(rows))
	for i := range rows {
		rowsByPostID[rows[i].PostID] = rows[i]
	}

	fallbackRow, ok := rowsByPostID["community:detected-fallback"]
	require.True(t, ok)
	require.Equal(t, domain.OutboxKindCommunityPost, fallbackRow.Kind)
	require.Equal(t, "UC_FALLBACK", fallbackRow.ChannelID)
	require.Nil(t, fallbackRow.ActualPublishedAt)
	require.Equal(t, fallbackDetectedAt, fallbackRow.DetectedAt.UTC())

	includedRow, ok := rowsByPostID["short:included-published"]
	require.True(t, ok)
	require.Equal(t, domain.OutboxKindNewShort, includedRow.Kind)
	require.Equal(t, "UC_INCLUDED", includedRow.ChannelID)
	require.NotNil(t, includedRow.ActualPublishedAt)
	require.Equal(t, includedPublishedAt, includedRow.ActualPublishedAt.UTC())
	require.Equal(t, includedDetectedAt, includedRow.DetectedAt.UTC())
}

func TestRepositoryListCommunityAlarmSentHistoriesByFinalizedObservationWindow(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	finalizedAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	firstCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindCommunityPost, "community-post-1")
	secondCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindCommunityPost, "community-post-2")
	pendingCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindCommunityPost, "community-pending")
	shortCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindNewShort, "short-post-1")

	firstPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	firstDetectedAt := firstPublishedAt.Add(20 * time.Second)
	firstAlarmSentAt := firstPublishedAt.Add(65 * time.Second)
	secondPublishedAt := time.Date(2026, 4, 10, 2, 10, 0, 0, time.UTC)
	secondDetectedAt := secondPublishedAt.Add(15 * time.Second)
	secondAlarmSentAt := secondPublishedAt.Add(80 * time.Second)
	pendingDetectedAt := time.Date(2026, 4, 10, 3, 0, 0, 0, time.UTC)
	shortPublishedAt := time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC)
	shortDetectedAt := shortPublishedAt.Add(10 * time.Second)
	shortAlarmSentAt := shortPublishedAt.Add(40 * time.Second)
	latePublishedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	lateDetectedAt := latePublishedAt.Add(10 * time.Second)
	lateAlarmSentAt := latePublishedAt.Add(time.Minute)

	require.NoError(t, repo.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-post-1",
			ChannelID:         "UC_COMMUNITY_1",
			ActualPublishedAt: &firstPublishedAt,
			DetectedAt:        firstDetectedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-post-2",
			ChannelID:         "UC_COMMUNITY_2",
			ActualPublishedAt: &secondPublishedAt,
			DetectedAt:        secondDetectedAt,
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			ContentID:  "community-pending",
			ChannelID:  "UC_COMMUNITY_PENDING",
			DetectedAt: pendingDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-post-1",
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-late",
			ChannelID:         "UC_COMMUNITY_LATE",
			ActualPublishedAt: &latePublishedAt,
			DetectedAt:        lateDetectedAt,
		},
	}))
	require.NoError(t, repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   "community-post-1",
			AlarmSentAt: firstAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   "community-post-2",
			AlarmSentAt: secondAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindNewShort,
			ContentID:   "short-post-1",
			AlarmSentAt: shortAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   "community-late",
			AlarmSentAt: lateAlarmSentAt,
		},
	}))

	require.NoError(t, repo.db.Create([]domain.YouTubeCommunityShortsObservationPostBaseline{
		{
			RuntimeName:       "youtube-scraper",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            firstCanonicalPostID,
			ChannelID:         "UC_COMMUNITY_1",
			ActualPublishedAt: &firstPublishedAt,
			DetectedAt:        firstDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:       "youtube-scraper",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            secondCanonicalPostID,
			ChannelID:         "UC_COMMUNITY_2",
			ActualPublishedAt: &secondPublishedAt,
			DetectedAt:        secondDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:      "youtube-scraper",
			BigBangCutoverAt: cutoverAt,
			Kind:             domain.OutboxKindCommunityPost,
			PostID:           pendingCanonicalPostID,
			ChannelID:        "UC_COMMUNITY_PENDING",
			DetectedAt:       pendingDetectedAt,
			FinalizedAt:      finalizedAt,
		},
		{
			RuntimeName:       "youtube-scraper",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindNewShort,
			PostID:            shortCanonicalPostID,
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
			FinalizedAt:       finalizedAt,
		},
	}).Error)

	rows, err := repo.ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(ctx, "youtube-scraper", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.Equal(t, firstCanonicalPostID, rows[0].PostID)
	require.Equal(t, "community-post-1", rows[0].ContentID)
	require.Equal(t, "UC_COMMUNITY_1", rows[0].ChannelID)
	require.NotNil(t, rows[0].ActualPublishedAt)
	require.Equal(t, firstPublishedAt, rows[0].ActualPublishedAt.UTC())
	require.Equal(t, firstDetectedAt, rows[0].DetectedAt.UTC())
	require.Equal(t, firstAlarmSentAt, rows[0].AlarmSentAt.UTC())

	require.Equal(t, secondCanonicalPostID, rows[1].PostID)
	require.Equal(t, "community-post-2", rows[1].ContentID)
	require.Equal(t, "UC_COMMUNITY_2", rows[1].ChannelID)
	require.NotNil(t, rows[1].ActualPublishedAt)
	require.Equal(t, secondPublishedAt, rows[1].ActualPublishedAt.UTC())
	require.Equal(t, secondDetectedAt, rows[1].DetectedAt.UTC())
	require.Equal(t, secondAlarmSentAt, rows[1].AlarmSentAt.UTC())
}

func TestRepositoryListShortsAlarmSentHistoriesByFinalizedObservationWindow(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	finalizedAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	communityCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindCommunityPost, "community-post-1")
	firstShortCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindNewShort, "short-post-1")
	secondShortCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindNewShort, "short-post-2")
	pendingShortCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindNewShort, "short-pending")

	communityPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	communityDetectedAt := communityPublishedAt.Add(20 * time.Second)
	communityAlarmSentAt := communityPublishedAt.Add(65 * time.Second)
	firstShortPublishedAt := time.Date(2026, 4, 10, 2, 10, 0, 0, time.UTC)
	firstShortDetectedAt := firstShortPublishedAt.Add(10 * time.Second)
	firstShortAlarmSentAt := firstShortPublishedAt.Add(55 * time.Second)
	secondShortPublishedAt := time.Date(2026, 4, 10, 3, 10, 0, 0, time.UTC)
	secondShortDetectedAt := secondShortPublishedAt.Add(12 * time.Second)
	secondShortAlarmSentAt := secondShortPublishedAt.Add(58 * time.Second)
	pendingShortDetectedAt := time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC)
	lateShortPublishedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	lateShortDetectedAt := lateShortPublishedAt.Add(10 * time.Second)
	lateShortAlarmSentAt := lateShortPublishedAt.Add(time.Minute)

	require.NoError(t, repo.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-post-1",
			ChannelID:         "UC_COMMUNITY_1",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-post-1",
			ChannelID:         "UC_SHORT_1",
			ActualPublishedAt: &firstShortPublishedAt,
			DetectedAt:        firstShortDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-post-2",
			ChannelID:         "UC_SHORT_2",
			ActualPublishedAt: &secondShortPublishedAt,
			DetectedAt:        secondShortDetectedAt,
		},
		{
			Kind:       domain.OutboxKindNewShort,
			ContentID:  "short-pending",
			ChannelID:  "UC_SHORT_PENDING",
			DetectedAt: pendingShortDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-late",
			ChannelID:         "UC_SHORT_LATE",
			ActualPublishedAt: &lateShortPublishedAt,
			DetectedAt:        lateShortDetectedAt,
		},
	}))
	require.NoError(t, repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   "community-post-1",
			AlarmSentAt: communityAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindNewShort,
			ContentID:   "short-post-1",
			AlarmSentAt: firstShortAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindNewShort,
			ContentID:   "short-post-2",
			AlarmSentAt: secondShortAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindNewShort,
			ContentID:   "short-late",
			AlarmSentAt: lateShortAlarmSentAt,
		},
	}))

	require.NoError(t, repo.db.Create([]domain.YouTubeCommunityShortsObservationPostBaseline{
		{
			RuntimeName:       "youtube-scraper",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            communityCanonicalPostID,
			ChannelID:         "UC_COMMUNITY_1",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:       "youtube-scraper",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindNewShort,
			PostID:            firstShortCanonicalPostID,
			ChannelID:         "UC_SHORT_1",
			ActualPublishedAt: &firstShortPublishedAt,
			DetectedAt:        firstShortDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:       "youtube-scraper",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindNewShort,
			PostID:            secondShortCanonicalPostID,
			ChannelID:         "UC_SHORT_2",
			ActualPublishedAt: &secondShortPublishedAt,
			DetectedAt:        secondShortDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:      "youtube-scraper",
			BigBangCutoverAt: cutoverAt,
			Kind:             domain.OutboxKindNewShort,
			PostID:           pendingShortCanonicalPostID,
			ChannelID:        "UC_SHORT_PENDING",
			DetectedAt:       pendingShortDetectedAt,
			FinalizedAt:      finalizedAt,
		},
	}).Error)

	rows, err := repo.ListShortsAlarmSentHistoriesByFinalizedObservationWindow(ctx, "youtube-scraper", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.Equal(t, firstShortCanonicalPostID, rows[0].PostID)
	require.Equal(t, "short-post-1", rows[0].ContentID)
	require.Equal(t, "UC_SHORT_1", rows[0].ChannelID)
	require.NotNil(t, rows[0].ActualPublishedAt)
	require.Equal(t, firstShortPublishedAt, rows[0].ActualPublishedAt.UTC())
	require.Equal(t, firstShortDetectedAt, rows[0].DetectedAt.UTC())
	require.Equal(t, firstShortAlarmSentAt, rows[0].AlarmSentAt.UTC())

	require.Equal(t, secondShortCanonicalPostID, rows[1].PostID)
	require.Equal(t, "short-post-2", rows[1].ContentID)
	require.Equal(t, "UC_SHORT_2", rows[1].ChannelID)
	require.NotNil(t, rows[1].ActualPublishedAt)
	require.Equal(t, secondShortPublishedAt, rows[1].ActualPublishedAt.UTC())
	require.Equal(t, secondShortDetectedAt, rows[1].DetectedAt.UTC())
	require.Equal(t, secondShortAlarmSentAt, rows[1].AlarmSentAt.UTC())
}

var trackingTestDBSequence uint64

func newTrackingTestDB(t *testing.T) *gorm.DB {
	return newTrackingTestDBWithMaxOpenConns(t, 1)
}

func newTrackingTestDBWithMaxOpenConns(t *testing.T, maxOpenConns int) *gorm.DB {
	t.Helper()
	if maxOpenConns < 1 {
		maxOpenConns = 1
	}

	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"), atomic.AddUint64(&trackingTestDBSequence, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(maxOpenConns)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeContentAlarmTracking{}, &domain.YouTubeCommunityShortsSourcePost{}, &domain.YouTubeCommunityShortsAlarmState{}, &domain.YouTubeCommunityShortsObservationWindow{}, &domain.YouTubeCommunityShortsObservationPostBaseline{}))
	return db
}

func TestRepositoryUpsertKeepsSingleTrackingRowForRepeatedSaves(t *testing.T) {
	testCases := []struct {
		name        string
		kind        domain.OutboxKind
		rawID       string
		canonicalID string
	}{
		{
			name:        "community post",
			kind:        domain.OutboxKindCommunityPost,
			rawID:       "post-repeat-1",
			canonicalID: "community:post-repeat-1",
		},
		{
			name:        "short",
			kind:        domain.OutboxKindNewShort,
			rawID:       "short-repeat-1",
			canonicalID: "short:short-repeat-1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTrackingTestDB(t)
			repo := NewRepository(db)
			ctx := context.Background()
			actualPublishedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
			earliestDetectedAt := actualPublishedAt.Add(30 * time.Second)
			laterDetectedAt := actualPublishedAt.Add(2 * time.Minute)
			earliestAlarmSentAt := actualPublishedAt.Add(75 * time.Second)
			laterAlarmSentAt := actualPublishedAt.Add(95 * time.Second)

			require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
				Kind:       tc.kind,
				ContentID:  tc.rawID,
				ChannelID:  "UC_REPEAT",
				DetectedAt: laterDetectedAt,
			}))
			require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
				Kind:              tc.kind,
				ContentID:         tc.canonicalID,
				ChannelID:         "UC_REPEAT",
				ActualPublishedAt: &actualPublishedAt,
				DetectedAt:        earliestDetectedAt,
			}))
			require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
				Kind:        tc.kind,
				ContentID:   tc.rawID,
				ChannelID:   "UC_REPEAT",
				DetectedAt:  laterDetectedAt.Add(time.Minute),
				AlarmSentAt: &laterAlarmSentAt,
			}))
			require.NoError(t, repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
				Kind:        tc.kind,
				ContentID:   tc.canonicalID,
				ChannelID:   "UC_REPEAT",
				DetectedAt:  laterDetectedAt.Add(2 * time.Minute),
				AlarmSentAt: &earliestAlarmSentAt,
			}))

			var rows []domain.YouTubeContentAlarmTracking
			require.NoError(t, db.Order("content_id ASC").Find(&rows).Error)
			require.Len(t, rows, 1)
			require.Equal(t, tc.kind, rows[0].Kind)
			require.Equal(t, tc.canonicalID, rows[0].CanonicalContentID)
			require.NotNil(t, rows[0].ActualPublishedAt)
			require.Equal(t, actualPublishedAt, rows[0].ActualPublishedAt.UTC())
			require.Equal(t, earliestDetectedAt, rows[0].DetectedAt.UTC())
			require.NotNil(t, rows[0].AlarmSentAt)
			require.Equal(t, earliestAlarmSentAt, rows[0].AlarmSentAt.UTC())
			require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, rows[0].DeliveryStatus)

			recordByRawID, err := repo.FindByIdentity(ctx, tc.kind, tc.rawID)
			require.NoError(t, err)
			require.NotNil(t, recordByRawID)
			require.Equal(t, tc.canonicalID, recordByRawID.CanonicalContentID)

			recordByCanonicalID, err := repo.FindByIdentity(ctx, tc.kind, tc.canonicalID)
			require.NoError(t, err)
			require.NotNil(t, recordByCanonicalID)
			require.Equal(t, tc.canonicalID, recordByCanonicalID.CanonicalContentID)
		})
	}
}

func TestRepositoryUpsertKeepsSingleTrackingRowForConcurrentSaves(t *testing.T) {
	testCases := []struct {
		name        string
		kind        domain.OutboxKind
		rawID       string
		canonicalID string
	}{
		{
			name:        "community post",
			kind:        domain.OutboxKindCommunityPost,
			rawID:       "post-concurrent-1",
			canonicalID: "community:post-concurrent-1",
		},
		{
			name:        "short",
			kind:        domain.OutboxKindNewShort,
			rawID:       "short-concurrent-1",
			canonicalID: "short:short-concurrent-1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTrackingTestDB(t)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			sqlDB.SetMaxIdleConns(1)

			repo := NewRepository(db)
			ctx := context.Background()
			actualPublishedAt := time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC)
			earliestDetectedAt := actualPublishedAt.Add(15 * time.Second)
			earliestAlarmSentAt := actualPublishedAt.Add(80 * time.Second)
			laterAlarmSentAt := actualPublishedAt.Add(105 * time.Second)

			variants := []struct {
				contentID         string
				actualPublishedAt *time.Time
				detectedAt        time.Time
				alarmSentAt       *time.Time
			}{
				{
					contentID:  tc.rawID,
					detectedAt: actualPublishedAt.Add(90 * time.Second),
				},
				{
					contentID:         tc.canonicalID,
					actualPublishedAt: &actualPublishedAt,
					detectedAt:        actualPublishedAt.Add(45 * time.Second),
				},
				{
					contentID:         tc.rawID,
					actualPublishedAt: &actualPublishedAt,
					detectedAt:        earliestDetectedAt,
				},
				{
					contentID:   tc.canonicalID,
					detectedAt:  actualPublishedAt.Add(75 * time.Second),
					alarmSentAt: &laterAlarmSentAt,
				},
				{
					contentID:   tc.rawID,
					detectedAt:  actualPublishedAt.Add(30 * time.Second),
					alarmSentAt: &earliestAlarmSentAt,
				},
				{
					contentID:  tc.canonicalID,
					detectedAt: actualPublishedAt.Add(2 * time.Minute),
				},
			}

			errCh := make(chan error, len(variants))
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(len(variants))
			for _, variant := range variants {
				variant := variant
				go func() {
					defer wg.Done()
					<-start
					errCh <- repo.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
						Kind:              tc.kind,
						ContentID:         variant.contentID,
						ChannelID:         "UC_CONCURRENT",
						ActualPublishedAt: variant.actualPublishedAt,
						DetectedAt:        variant.detectedAt,
						AlarmSentAt:       variant.alarmSentAt,
					})
				}()
			}

			close(start)
			wg.Wait()
			close(errCh)
			for err := range errCh {
				require.NoError(t, err)
			}

			var rows []domain.YouTubeContentAlarmTracking
			require.NoError(t, db.Order("content_id ASC").Find(&rows).Error)
			require.Len(t, rows, 1)
			require.Equal(t, tc.kind, rows[0].Kind)
			require.Equal(t, tc.canonicalID, rows[0].CanonicalContentID)
			require.NotNil(t, rows[0].ActualPublishedAt)
			require.Equal(t, actualPublishedAt, rows[0].ActualPublishedAt.UTC())
			require.Equal(t, earliestDetectedAt, rows[0].DetectedAt.UTC())
			require.NotNil(t, rows[0].AlarmSentAt)
			require.Equal(t, earliestAlarmSentAt, rows[0].AlarmSentAt.UTC())
			require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, rows[0].DeliveryStatus)

			recordByRawID, err := repo.FindByIdentity(ctx, tc.kind, tc.rawID)
			require.NoError(t, err)
			require.NotNil(t, recordByRawID)
			require.Equal(t, tc.canonicalID, recordByRawID.CanonicalContentID)

			recordByCanonicalID, err := repo.FindByIdentity(ctx, tc.kind, tc.canonicalID)
			require.NoError(t, err)
			require.NotNil(t, recordByCanonicalID)
			require.Equal(t, tc.canonicalID, recordByCanonicalID.CanonicalContentID)
		})
	}
}
