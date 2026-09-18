package youtubedispatch

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

func TestDeliveryClaimUsesWallClockAndFencesStaleOwner(t *testing.T) {
	for _, offset := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			d, db := newClaimGateTestDispatcher(t, &claimGateTestSender{}, &dispatchstate.Config{LockTimeout: 2 * time.Minute})
			now := time.Now().UTC()
			row, outbox, postID := newCommunityClaimGateFixture(now.Add(offset), "wall-clock")
			oldToken := now.Add(-10 * time.Minute).Truncate(time.Microsecond)
			require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
				Kind: outbox.Kind, PostID: postID, ContentID: outbox.ContentID, ChannelID: outbox.ChannelID,
				DetectedAt: now.Add(-11 * time.Minute), AuthorizedAt: &oldToken,
				DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
			}).Error)

			type attempt struct {
				result claimResult
				err    error
			}

			results := make(chan attempt, 2)

			var workers sync.WaitGroup

			before := time.Now().UTC().Truncate(time.Microsecond)

			for range 2 {
				workers.Go(func() {
					result, err := d.claim.tryClaimDelivery(t.Context(), &row, &outbox)
					results <- attempt{result: result, err: err}
				})
			}

			workers.Wait()
			close(results)

			after := time.Now().UTC()
			winners := 0

			for result := range results {
				require.NoError(t, result.err)

				if result.result.decision == deliveryClaimDecisionProceed {
					winners++

					require.NotNil(t, result.result.claimToken)
					require.False(t, result.result.claimToken.AuthorizedAt.Before(before))
					require.False(t, result.result.claimToken.AuthorizedAt.After(after))
				}
			}

			require.Equal(t, 1, winners)

			repository := observation.NewRepositoryContext(t.Context(), db)
			released, err := repository.ReleaseAlarmStateClaim(t.Context(), outbox.Kind, postID, oldToken)
			require.NoError(t, err)
			require.False(t, released, "stale owner cannot release the replacement claim")

			state, err := repository.FindAlarmStateByPostID(t.Context(), outbox.Kind, postID)
			require.NoError(t, err)
			require.NotNil(t, state.AuthorizedAt)
			require.False(t, state.AuthorizedAt.Before(before))
		})
	}
}
