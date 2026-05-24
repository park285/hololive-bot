package notifier

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

type SendResult = checking.SendResult

func newCheckerTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newCheckerTestCacheClient(t *testing.T) cache.Client {
	t.Helper()
	return testutil.NewTestCacheService(t, t.Context())
}
