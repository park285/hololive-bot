package notificationdelivery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNotificationHandoffFirstCheckClipsShortHorizon(t *testing.T) {
	repo := testRepository(t)
	require.NoError(t, repo.Enqueue(t.Context(), domain.DeliveryKindMemberNewsWeekly, "short-horizon", testRoomA, testDeliveryMessage("message")))

	dispatcher := mustNewDispatcher(t, repo, &preparedSenderSpy{body: testPinnedBody, route: testPreparedText}, dispatcherLogger(), nil)

	dispatcher.config.HandoffHorizon = 1500 * time.Millisecond
	dispatcher.processReady(t.Context())

	var (
		status  string
		delayMS int64
	)

	require.NoError(t, repo.pool.QueryRow(t.Context(), `SELECT status,
		floor(EXTRACT(EPOCH FROM (handoff_next_check_at - sent_at)) * 1000)::BIGINT
		FROM notification_delivery_outbox WHERE period_key = 'short-horizon'`).Scan(&status, &delayMS))
	require.Equal(t, string(domain.DeliveryStatusSent), status)
	require.Equal(t, dispatcher.config.HandoffHorizon.Milliseconds(), delayMS)
}
