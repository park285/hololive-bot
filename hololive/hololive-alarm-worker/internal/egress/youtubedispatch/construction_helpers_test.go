package youtubedispatch

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func newDispatcherForTest(
	t testing.TB,
	db *pgxpool.Pool,
	cacheClient cache.Client,
	sender delivery.MessageSender,
	renderer *template.Renderer,
	logger *slog.Logger,
	config *dispatchstate.Config,
) *Dispatcher {
	t.Helper()

	deps := Dependencies{Cache: cacheClient, Sender: sender, Renderer: renderer}
	if db != nil {
		deps.DB = db
		deps.MessageStrings = messagestrings.NewStore(db, logger)

		if deps.Renderer == nil {
			deps.Renderer = template.NewRenderer(db, logger)
		}
	}

	dispatcher, err := NewDispatcher(deps, logger, config)
	require.NoError(t, err)

	return dispatcher
}
