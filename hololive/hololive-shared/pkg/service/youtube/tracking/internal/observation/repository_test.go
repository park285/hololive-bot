package observation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryUpsertAndFindByIdentity(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	actualPublishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-1",
		ChannelID:         "UC_TEST",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AlarmSentAt:       &alarmSentAt,
	}))

	record, err := repository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-1")
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
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	firstDetectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	laterDetectedAt := time.Date(2026, 4, 10, 1, 7, 0, 0, time.UTC)
	firstAlarmSentAt := time.Date(2026, 4, 10, 1, 8, 0, 0, time.UTC)
	laterAlarmSentAt := time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)
	actualPublishedAt := time.Date(2026, 4, 10, 1, 1, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewShort,
		ContentID:  "short-1",
		ChannelID:  "UC_SHORT",
		DetectedAt: firstDetectedAt,
	}))
	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short-1",
		ChannelID:         "UC_SHORT",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        laterDetectedAt,
		AlarmSentAt:       &laterAlarmSentAt,
	}))
	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:        domain.OutboxKindNewShort,
		ContentID:   "short-1",
		ChannelID:   "UC_SHORT",
		DetectedAt:  laterDetectedAt,
		AlarmSentAt: &firstAlarmSentAt,
	}))

	record, err := repository.FindByIdentity(ctx, domain.OutboxKindNewShort, "short-1")
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

func TestRepositoryUpsertPreservesExistingActualPublishedAt(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	firstActualPublishedAt := time.Date(2026, 4, 10, 1, 1, 0, 0, time.UTC)
	laterActualPublishedAt := firstActualPublishedAt.Add(5 * time.Minute)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short-stable-published-at",
		ChannelID:         "UC_SHORT",
		ActualPublishedAt: &firstActualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short-stable-published-at",
		ChannelID:         "UC_SHORT",
		ActualPublishedAt: &laterActualPublishedAt,
		DetectedAt:        detectedAt.Add(time.Minute),
	}))

	record, err := repository.FindByIdentity(ctx, domain.OutboxKindNewShort, "short-stable-published-at")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, firstActualPublishedAt, record.ActualPublishedAt.UTC())
}

func TestRepositoryFindByIdentitySupportsShortCanonicalAlias(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewShort,
		ContentID:  "short-1",
		ChannelID:  "UC_SHORT",
		DetectedAt: detectedAt,
	}))

	record, err := repository.FindByIdentity(ctx, domain.OutboxKindNewShort, "short:short-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "short-1", record.ContentID)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, record.DeliveryStatus)
}

func TestRepositoryUpsertDedupesByCanonicalContentIdentity(t *testing.T) {
	db := newTrackingTestDB(t)
	repository := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	laterDetectedAt := detectedAt.Add(2 * time.Minute)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewShort,
		ContentID:  "short-1",
		ChannelID:  "UC_SHORT",
		DetectedAt: detectedAt,
	}))
	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
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
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	laterDetectedAt := time.Date(2026, 4, 10, 1, 6, 0, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 7, 30, 0, time.UTC)
	actualPublishedAt := time.Date(2026, 4, 10, 1, 6, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:        domain.OutboxKindCommunityPost,
		ContentID:   "post-backfill",
		ChannelID:   "UC_BACKFILL",
		DetectedAt:  detectedAt,
		AlarmSentAt: &alarmSentAt,
	}))

	record, err := repository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-backfill")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Nil(t, record.AlarmLatencyMillis)
	require.Nil(t, record.AlarmLatencyExceeded)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-backfill",
		ChannelID:         "UC_BACKFILL",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        laterDetectedAt,
	}))

	record, err = repository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-backfill")
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
	repository := NewRepository(db)
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

	firstPage, cursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(time.Minute), nil, 2)
	require.NoError(t, err)
	require.Len(t, firstPage, 2)
	require.NotNil(t, cursor)
	require.Equal(t, "short:short-a", firstPage[0].PostID)
	require.Equal(t, "short:short-b", firstPage[1].PostID)
	require.Equal(t, detectedAt, cursor.DetectedAt.UTC())
	require.Equal(t, "short:short-b", cursor.PostID)

	secondPage, nextCursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(time.Minute), cursor, 2)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.Nil(t, nextCursor)
	require.Equal(t, "short:short-c", secondPage[0].PostID)
}

func TestListPendingPublishedAtResolutionsPage_ExcludesFutureRetryAfter(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	referenceNow := detectedAt.Add(time.Minute)
	futureRetryAfter := referenceNow.Add(time.Minute)

	require.NoError(t, repository.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
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
	require.NoError(t, repository.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:backoff", futureRetryAfter))

	candidates, _, err := repository.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, detectedAt.Add(2*time.Minute), nil, 10)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "short:eligible", candidates[0].PostID)
}

func TestListPendingPublishedAtResolutionsPage_IncludesRetryAfterExpired(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	referenceNow := detectedAt.Add(2 * time.Minute)
	expiredRetryAfter := detectedAt.Add(time.Minute)

	require.NoError(t, repository.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
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
	require.NoError(t, repository.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:expired", expiredRetryAfter))

	candidates, _, err := repository.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, detectedAt.Add(3*time.Minute), nil, 10)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "short:expired", candidates[0].PostID)
}

func TestListPendingPublishedAtResolutionsPage_IncludesAuthorizedAndSentRowsWithoutPublishedAt(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := detectedAt.Add(15 * time.Second)
	alarmSentAt := detectedAt.Add(30 * time.Second)

	require.NoError(t, repository.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
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

	candidates, _, err := repository.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(2*time.Minute), nil, 10)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "short:authorized", candidates[0].PostID)
	require.Equal(t, "short:sent", candidates[1].PostID)
}

func TestListPendingPublishedAtResolutionsPage_PrioritizesPendingRowsBeforeMetadataOnlyBackfill(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := detectedAt.Add(10 * time.Second)

	require.NoError(t, repository.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
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

	firstPage, cursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(2*time.Minute), nil, 1)
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	require.NotNil(t, cursor)
	require.Equal(t, "short:pending", firstPage[0].PostID)
	require.Equal(t, 0, cursor.PriorityBucket)

	secondPage, nextCursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(2*time.Minute), cursor, 1)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.Equal(t, "short:metadata-only", secondPage[0].PostID)

	thirdPage, finalCursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, detectedAt.Add(time.Minute), detectedAt.Add(2*time.Minute), nextCursor, 1)
	require.NoError(t, err)
	require.Nil(t, thirdPage)
	require.Nil(t, finalCursor)
}

func TestMarkAndClearPublishedAtRetryAfter(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	retryAfter := detectedAt.Add(2 * time.Minute)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindNewShort,
		PostID:         "short:retry",
		ContentID:      "short:retry",
		ChannelID:      "UC_SHORT",
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}))

	require.NoError(t, repository.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:retry", retryAfter))
	row, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:retry")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.NotNil(t, row.PublishedAtRetryAfter)
	require.Equal(t, retryAfter, row.PublishedAtRetryAfter.UTC())

	require.NoError(t, repository.ClearPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:retry"))
	row, err = repository.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:retry")
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

	repository := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	now := detectedAt.Add(time.Minute)
	require.NoError(t, db.Exec(`
		INSERT INTO youtube_community_shorts_alarm_states
			(kind, post_id, content_id, channel_id, detected_at, delivery_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, domain.OutboxKindNewShort, "short:legacy", "short:legacy", "UC_SHORT", detectedAt, domain.YouTubeCommunityShortsAlarmStateStatusDetected, now, now).Error)

	candidates, _, err := repository.ListPendingPublishedAtResolutionsPage(ctx, now, detectedAt.Add(2*time.Minute), nil, 10)
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

	repository := NewRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	require.NoError(t, db.Exec(`
		INSERT INTO youtube_community_shorts_alarm_states
			(kind, post_id, content_id, channel_id, detected_at, delivery_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, domain.OutboxKindNewShort, "short:legacy", "short:legacy", "UC_SHORT", now, domain.YouTubeCommunityShortsAlarmStateStatusDetected, now, now).Error)

	require.ErrorContains(t, repository.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:legacy", now.Add(time.Minute)), "published_at_retry_after")
	require.ErrorContains(t, repository.ClearPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:legacy"), "published_at_retry_after")
}
