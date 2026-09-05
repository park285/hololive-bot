package youtubedispatch_test

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func newIntegrationDispatcher(
	t testing.TB,
	db *pgxpool.Pool,
	cacheClient cache.Client,
	sender delivery.MessageSender,
	logger *slog.Logger,
	config *dispatchstate.Config,
) *youtubedispatch.Dispatcher {
	t.Helper()

	require.NotNil(t, db)

	dispatcher, err := youtubedispatch.NewDispatcher(youtubedispatch.Dependencies{
		DB: db, Cache: cacheClient, Sender: sender,
		Renderer: template.NewRenderer(db, logger), MessageStrings: messagestrings.NewStore(db, logger),
	}, logger, config)
	require.NoError(t, err)

	return dispatcher
}
