package notificationdelivery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 첫 확인 간격보다 짧은 지평도 허용되므로 admission 정산은 첫 확인부터 지평을 넘기면 안 된다.
func TestNotificationHandoffFirstCheckClipsShortHorizon(t *testing.T) {
	repo := testRepository(t)
	require.NoError(t, repo.Enqueue(t.Context(), domain.DeliveryKindMemberNewsWeekly, "short-horizon", testRoomA, "message"))

	dispatcher := mustNewDispatcher(t, repo, &preparedSenderSpy{body: testPinnedBody, route: testPreparedText}, dispatcherLogger(), nil)

	dispatcher.config.AttemptTimeout = 1500 * time.Millisecond
	dispatcher.processReady(t.Context())

	var (
		status  string
		delayMS int64
	)

	require.NoError(t, repo.pool.QueryRow(t.Context(), `SELECT status,
		floor(EXTRACT(EPOCH FROM (handoff_next_check_at - sent_at)) * 1000)::BIGINT
		FROM notification_delivery_outbox WHERE period_key = 'short-horizon'`).Scan(&status, &delayMS))
	require.Equal(t, string(domain.DeliveryStatusSent), status)
	require.Equal(t, dispatcher.config.AttemptTimeout.Milliseconds(), delayMS)
}
