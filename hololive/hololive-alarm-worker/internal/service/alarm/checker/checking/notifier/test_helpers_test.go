package notifier

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

type SendResult = delivery.SendResult

func newCheckerTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newCheckerTestCacheClient(t *testing.T) cache.Client {
	t.Helper()

	return testutil.NewTestCacheService(t.Context(), t)
}

func claimNotificationPair(t *testing.T, n *Notifier, payload *sendInput) (notifyKey string, notifyClaimed bool) {
	t.Helper()

	notifyKey, logicalKey := n.notificationDedupKeys(payload)

	notifyClaimed, _, err := n.dedupService.TryClaimPair(t.Context(), notifyKey, logicalKey, constants.CacheTTL.NotificationSent)
	require.NoError(t, err)

	return notifyKey, notifyClaimed
}

func newCheckerTestPublisher(t *testing.T, c queue.WakeupCache, outbox dispatchoutbox.Writer, config queue.PublishConfig) *queue.Publisher {
	t.Helper()

	publisher, err := queue.NewPublisher(c, outbox, config, newCheckerTestLogger())
	require.NoError(t, err)

	return publisher
}
