package preparation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFirstRoomSuccessConsumesClaimToken(t *testing.T) {
	t.Parallel()

	token := testClaimToken(t)
	requirement, err := lifecycle.NewRequireClaimOrAlreadySent(token)
	require.NoError(t, err)

	action, err := EvaluateTrackingFinalization(requirement, TrackingFinalState{ActiveClaim: &token})
	require.NoError(t, err)
	require.Equal(t, TrackingConsumeClaim, action)
}

func TestLaterRoomSuccessAcceptsAlreadySentTracking(t *testing.T) {
	t.Parallel()

	token := testClaimToken(t)
	requirement, err := lifecycle.NewRequireClaimOrAlreadySent(token)
	require.NoError(t, err)

	action, err := EvaluateTrackingFinalization(requirement, TrackingFinalState{AlreadySent: true})
	require.NoError(t, err)
	require.Equal(t, TrackingNoMutation, action)
}

func TestAlreadySentPreparationFinalizesWithoutClaimMutation(t *testing.T) {
	t.Parallel()

	requirement, err := ResolveTrackingRequirement(TrackingEvidence{
		Kind: domain.OutboxKindCommunityPost, PostID: "community:post-1", AlreadySent: true,
	})
	require.NoError(t, err)

	action, err := EvaluateTrackingFinalization(requirement, TrackingFinalState{AlreadySent: true})
	require.NoError(t, err)
	require.Equal(t, TrackingNoMutation, action)
}

func TestTrackingNeitherClaimNorSentRollsBackDeliverySuccess(t *testing.T) {
	t.Parallel()

	token := testClaimToken(t)
	requirement, err := lifecycle.NewRequireClaimOrAlreadySent(token)
	require.NoError(t, err)

	action, err := EvaluateTrackingFinalization(requirement, TrackingFinalState{})
	require.Error(t, err)
	require.Zero(t, action)
}

func TestTrackingRequirementDeduplicatesSamePostInGroupedBatch(t *testing.T) {
	t.Parallel()

	token := testClaimToken(t)
	claim, err := lifecycle.NewRequireClaimOrAlreadySent(token)
	require.NoError(t, err)

	already, err := lifecycle.NewRequireAlreadySent(token.Kind(), token.PostID())
	require.NoError(t, err)

	got, err := DeduplicateTrackingRequirements([]lifecycle.TrackingRequirement{already, claim, already})
	require.NoError(t, err)
	require.Len(t, got, 1)

	_, ok := got[0].(lifecycle.RequireClaimOrAlreadySent)
	require.True(t, ok)
}

func testClaimToken(t *testing.T) lifecycle.AlarmClaimToken {
	t.Helper()

	token, err := lifecycle.NewAlarmClaimToken(
		domain.OutboxKindCommunityPost,
		"community:post-1",
		time.Date(2026, time.August, 31, 3, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	return token
}
