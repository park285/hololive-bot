package observation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryListAlarmSentHistoriesWithinObservationWindowFiltersRows(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()

	windowStart := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)

	communityPublishedAt := windowStart.Add(20 * time.Minute)
	communityDetectedAt := communityPublishedAt.Add(10 * time.Second)
	communityAlarmSentAt := communityPublishedAt.Add(55 * time.Second)
	communityLateDetectedAt := windowEnd.Add(30 * time.Second)
	communityOutOfWindowPublishedAt := windowEnd.Add(2 * time.Minute)
	shortPublishedAt := windowStart.Add(40 * time.Minute)
	shortDetectedAt := shortPublishedAt.Add(8 * time.Second)
	shortAlarmSentAt := shortPublishedAt.Add(45 * time.Second)
	pendingDetectedAt := windowStart.Add(50 * time.Minute)

	require.NoError(t, repo.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-window",
			ChannelID:         "UC_COMMUNITY",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-late-detected",
			ChannelID:         "UC_COMMUNITY_LATE",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityLateDetectedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-out-of-window",
			ChannelID:         "UC_COMMUNITY_OUT",
			ActualPublishedAt: &communityOutOfWindowPublishedAt,
			DetectedAt:        communityOutOfWindowPublishedAt.Add(10 * time.Second),
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-window",
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			ContentID:  "community-pending",
			ChannelID:  "UC_PENDING",
			DetectedAt: pendingDetectedAt,
		},
	}))
	require.NoError(t, repo.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "community-window", AlarmSentAt: communityAlarmSentAt},
		{Kind: domain.OutboxKindCommunityPost, ContentID: "community-late-detected", AlarmSentAt: communityAlarmSentAt},
		{Kind: domain.OutboxKindCommunityPost, ContentID: "community-out-of-window", AlarmSentAt: communityOutOfWindowPublishedAt.Add(time.Minute)},
		{Kind: domain.OutboxKindNewShort, ContentID: "short-window", AlarmSentAt: shortAlarmSentAt},
	}))

	communityRows, err := repo.ListCommunityAlarmSentHistoriesWithinObservationWindow(ctx, windowStart, windowEnd, windowEnd)
	require.NoError(t, err)
	require.Len(t, communityRows, 1)
	require.Equal(t, "community:community-window", communityRows[0].PostID)
	require.Equal(t, communityAlarmSentAt, communityRows[0].AlarmSentAt.UTC())

	shortRows, err := repo.ListShortsAlarmSentHistoriesWithinObservationWindow(ctx, windowStart, windowEnd, windowEnd)
	require.NoError(t, err)
	require.Len(t, shortRows, 1)
	require.Equal(t, "short:short-window", shortRows[0].PostID)
	require.Equal(t, shortAlarmSentAt, shortRows[0].AlarmSentAt.UTC())
}
