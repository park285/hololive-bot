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
	tb testing.TB,
	db *pgxpool.Pool,
	cacheClient cache.Client,
	sender delivery.MessageSender,
	logger *slog.Logger,
	config *dispatchstate.Config,
) *youtubedispatch.Dispatcher {
	tb.Helper()

	require.NotNil(tb, db)

	messageStrings := messagestrings.NewStore(db, logger)
	require.NoError(tb, messageStrings.Load(tb.Context()))

	dispatcher, err := youtubedispatch.NewDispatcher(youtubedispatch.Dependencies{
		DB: db, Cache: cacheClient, Sender: sender,
		Renderer: template.NewRenderer(db, logger), MessageStrings: messageStrings,
	}, logger, config)
	require.NoError(tb, err)

	return dispatcher
}
