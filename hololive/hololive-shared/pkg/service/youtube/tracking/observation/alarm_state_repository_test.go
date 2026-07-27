package observation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFindAlarmStateByPostIDReturnsNilOnMissing(t *testing.T) {
	repository := NewRepository(newAlarmStateRepositoryTestDB(t))

	row, err := repository.FindAlarmStateByPostID(context.Background(), domain.OutboxKindCommunityPost, "community:missing")

	require.NoError(t, err)
	require.Nil(t, row)
}

func TestFindAlarmStateByPostIDReturnsExisting(t *testing.T) {
	repository := NewRepository(newAlarmStateRepositoryTestDB(t))
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 2, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(30 * time.Second)
	authorizedAt := publishedAt.Add(45 * time.Second)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-existing",
		ContentID:         "post-existing",
		ChannelID:         "UC_EXISTING",
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))

	row, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-existing")

	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, domain.OutboxKindCommunityPost, row.Kind)
	require.Equal(t, "community:post-existing", row.PostID)
	require.Equal(t, "post-existing", row.ContentID)
	require.Equal(t, "UC_EXISTING", row.ChannelID)
	require.NotNil(t, row.ActualPublishedAt)
	require.Equal(t, publishedAt, row.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, row.DetectedAt.UTC())
	require.NotNil(t, row.AuthorizedAt)
	require.Equal(t, authorizedAt, row.AuthorizedAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, row.DeliveryStatus)
}

func TestUpsertAlarmStateBatchMergesDuplicateRecords(t *testing.T) {
	repository := NewRepository(newAlarmStateRepositoryTestDB(t))
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 1, 0, 0, time.UTC)
	laterDetectedAt := publishedAt.Add(2 * time.Minute)
	earlierDetectedAt := publishedAt.Add(time.Minute)
	laterAuthorizedAt := publishedAt.Add(3 * time.Minute)
	earlierAuthorizedAt := publishedAt.Add(2 * time.Minute)
	alarmSentAt := publishedAt.Add(4 * time.Minute)

	require.NoError(t, repository.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:         domain.OutboxKindCommunityPost,
			PostID:       "merge-duplicate",
			ContentID:    "merge-duplicate",
			ChannelID:    "UC_FIRST",
			DetectedAt:   laterDetectedAt,
			AuthorizedAt: &laterAuthorizedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            "community:merge-duplicate",
			ContentID:         "merge-duplicate-updated",
			ChannelID:         "UC_SECOND",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        earlierDetectedAt,
			AuthorizedAt:      &earlierAuthorizedAt,
			AlarmSentAt:       &alarmSentAt,
		},
	}))

	row, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "merge-duplicate")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "community:merge-duplicate", row.PostID)
	require.Equal(t, "merge-duplicate-updated", row.ContentID)
	require.Equal(t, "UC_SECOND", row.ChannelID)
	require.NotNil(t, row.ActualPublishedAt)
	require.Equal(t, publishedAt, row.ActualPublishedAt.UTC())
	require.Equal(t, earlierDetectedAt, row.DetectedAt.UTC())
	require.NotNil(t, row.AuthorizedAt)
	require.Equal(t, earlierAuthorizedAt, row.AuthorizedAt.UTC())
	require.NotNil(t, row.AlarmSentAt)
	require.Equal(t, alarmSentAt, row.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, row.DeliveryStatus)
}

func TestMergeNormalizedAlarmStateHandlesNilAndTimestampPriority(t *testing.T) {
	existingDetectedAt := time.Date(2026, 4, 10, 1, 2, 0, 0, time.UTC)
	nextDetectedAt := existingDetectedAt.Add(-time.Minute)
	existingAuthorizedAt := existingDetectedAt.Add(2 * time.Minute)
	nextAuthorizedAt := existingDetectedAt.Add(time.Minute)
	existingAlarmSentAt := existingDetectedAt.Add(4 * time.Minute)

	existing := &domain.YouTubeCommunityShortsAlarmState{
		Kind:         domain.OutboxKindNewShort,
		PostID:       "short:merge-helper",
		ContentID:    "merge-helper",
		ChannelID:    "UC_EXISTING",
		DetectedAt:   existingDetectedAt,
		AuthorizedAt: &existingAuthorizedAt,
		AlarmSentAt:  &existingAlarmSentAt,
	}
	next := &domain.YouTubeCommunityShortsAlarmState{
		Kind:         domain.OutboxKindNewShort,
		PostID:       "short:merge-helper",
		ContentID:    "merge-helper-updated",
		ChannelID:    "UC_NEXT",
		DetectedAt:   nextDetectedAt,
		AuthorizedAt: &nextAuthorizedAt,
	}

	require.Same(t, next, mergeNormalizedAlarmState(nil, next))
	require.Same(t, existing, mergeNormalizedAlarmState(existing, nil))

	merged := mergeNormalizedAlarmState(existing, next)
	require.NotNil(t, merged)

	require.Equal(t, "merge-helper-updated", merged.ContentID)
	require.Equal(t, "UC_NEXT", merged.ChannelID)
	require.Equal(t, nextDetectedAt, merged.DetectedAt)
	require.NotNil(t, merged.AuthorizedAt)
	require.Equal(t, nextAuthorizedAt, *merged.AuthorizedAt)
	require.NotNil(t, merged.AlarmSentAt)
	require.Equal(t, existingAlarmSentAt, *merged.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, merged.DeliveryStatus)
}

func TestAlarmStateRepositoryNilDBReturnsError(t *testing.T) {
	repository := NewRepository(nil)
	ctx := context.Background()

	row, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:nil")
	require.Nil(t, row)
	require.ErrorContains(t, err, "db is nil")
}

func newAlarmStateRepositoryTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newTrackingTestDB(t)
}
