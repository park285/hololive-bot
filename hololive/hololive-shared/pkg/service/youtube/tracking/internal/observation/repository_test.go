package observation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	require.Equal(t, "community:post-1", record.CanonicalContentID)
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
	require.Equal(t, "short:short-1", record.CanonicalContentID)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, record.DeliveryStatus)

	aliasRecord, err := repository.FindByIdentity(ctx, domain.OutboxKindNewShort, "short-1")
	require.NoError(t, err)
	require.NotNil(t, aliasRecord)
	require.Equal(t, "short-1", aliasRecord.ContentID)
	require.Equal(t, "short:short-1", aliasRecord.CanonicalContentID)
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

	rows := selectTrackingRowsForTest(t, db)
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
