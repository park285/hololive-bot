package youtubedispatch

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

func newDispatcherForTest(
	t testing.TB,
	db deliverysql.DeliveryDB,
	cacheClient cache.Client,
	sender delivery.MessageSender,
	renderer *template.Renderer,
	logger *slog.Logger,
	config *dispatchstate.Config,
) *Dispatcher {
	t.Helper()

	dispatcher, err := NewDispatcher(Dependencies{
		DB: db, Cache: cacheClient, Sender: sender, Renderer: renderer,
	}, logger, config)
	require.NoError(t, err)

	return dispatcher
}
