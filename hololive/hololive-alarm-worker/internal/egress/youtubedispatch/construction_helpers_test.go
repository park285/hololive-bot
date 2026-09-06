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

func TestNewDispatcherRejectsTypedNilDatabase(t *testing.T) {
	var pool *pgxpool.Pool

	dispatcher, err := NewDispatcher(Dependencies{DB: pool}, nil, nil)
	require.ErrorContains(t, err, "db contains a nil value")
	require.Nil(t, dispatcher)
}

func newDispatcherForTest(
	tb testing.TB,
	db *pgxpool.Pool,
	cacheClient cache.Client,
	sender delivery.MessageSender,
	renderer *template.Renderer,
	logger *slog.Logger,
	config *dispatchstate.Config,
) *Dispatcher {
	tb.Helper()

	deps := Dependencies{Cache: cacheClient, Sender: sender, Renderer: renderer}

	if db != nil {
		deps.DB = db
		deps.MessageStrings = messagestrings.NewStore(db, logger)

		if deps.Renderer == nil {
			deps.Renderer = template.NewRenderer(db, logger)
		}
	}

	dispatcher, err := NewDispatcher(deps, logger, config)
	require.NoError(tb, err)

	return dispatcher
}
