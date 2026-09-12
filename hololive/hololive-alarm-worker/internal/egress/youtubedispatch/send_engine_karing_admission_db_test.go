package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestKaringAdmissionTimeoutReleasesClaimAfterRetryCommit(t *testing.T) {
	db := newDeliveryPool(t)
	item, delivery, postID := seedRetryExactOnceFixture(t, db, retryFinalizeOnceTestCase{
		kind: domain.OutboxKindNewShort, channelID: "UC_admission", contentID: "short-admission", roomID: testRoomOne,
		payload: `{"canonical_post_id":"short:short-admission","video_id":"short-admission","title":"admission"}`,
	})
	sender := &youtubeOutboxKaringTestSender{}
	dispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		BatchSize: 10, LockTimeout: time.Minute, PollInterval: time.Second,
		MaxRetries: 3, RetryBackoff: time.Minute, DeliveryParallelism: 1, DeliverySendTimeout: 20 * time.Millisecond,
	})
	dispatcher.send.karingMu.Lock()
	func() {
		defer dispatcher.send.karingMu.Unlock()

		dispatcher.ProcessOnceForTest(t.Context())
	}()
	require.Zero(t, sender.calls)
	assertRetryExactOnceFirstAttemptDeferred(t, db, item, delivery.ID, postID)

	require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationDelivery{}, map[string]any{
		"next_attempt_at": time.Now().UTC().Add(-time.Second),
	}, "id = ?", delivery.ID).Error)
	dispatcher.ProcessOnceForTest(t.Context())
	dispatcher.ProcessOnceForTest(t.Context())

	snapshot := assertCommunityShortsPostSent(t, db, item, delivery.ID, postID)
	require.Equal(t, 1, snapshot.delivery.AttemptCount)
	require.Equal(t, 1, sender.calls)
}
