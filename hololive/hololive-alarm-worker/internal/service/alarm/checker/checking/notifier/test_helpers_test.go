package notifier

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

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

// claimNotificationPair는 production claimDedup과 같은 notify/logical key 쌍을 TryClaimPair로 선점하고
// notify key와 그 선점 여부를 돌려준다.
func claimNotificationPair(t *testing.T, n *Notifier, payload *sendInput) (notifyKey string, notifyClaimed bool) {
	t.Helper()

	notifyKey, logicalKey := n.notificationDedupKeys(payload)

	notifyClaimed, _, err := n.dedupService.TryClaimPair(t.Context(), notifyKey, logicalKey, constants.CacheTTL.NotificationSent)
	require.NoError(t, err)

	return notifyKey, notifyClaimed
}
