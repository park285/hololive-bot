package observation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryTryClaimAlarmStateReturnsFalseForAlreadySentRow(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	firstAuthorizedAt := time.Date(2026, 4, 10, 1, 4, 30, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	laterAuthorizedAt := firstAuthorizedAt.Add(30 * time.Second)

	recordAlarmSentAt := alarmSentAt
	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:         domain.OutboxKindCommunityPost,
		PostID:       "post-claim-already-sent",
		ContentID:    "post-claim-already-sent",
		ChannelID:    "UC_TEST",
		DetectedAt:   detectedAt,
		AlarmSentAt:  &recordAlarmSentAt,
		AuthorizedAt: &firstAuthorizedAt,
	}))

	claimed, err := repository.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:         domain.OutboxKindCommunityPost,
		PostID:       "post-claim-already-sent",
		ContentID:    "post-claim-already-sent",
		ChannelID:    "UC_TEST",
		DetectedAt:   detectedAt,
		AuthorizedAt: &laterAuthorizedAt,
	})
	require.NoError(t, err)
	require.False(t, claimed)

	row, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-claim-already-sent")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.NotNil(t, row.AlarmSentAt)
	require.Equal(t, alarmSentAt, row.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, row.DeliveryStatus)
}
