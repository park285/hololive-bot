package youtubedispatch_test

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/backfill"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
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

	// 전용 DB도 운영과 같은 replay epoch/backfill 절차를 마친 뒤 dispatch를 시작한다.
	activation, err := sourceobservation.NewRepository(db).ActivateReplayEpoch(tb.Context(), sourceobservation.ReplayEpochInput{
		ActivatedBy: "dispatcher-integration-test", Reason: "prepare owned disposable database",
	})
	require.NoError(tb, err)

	coverageStart := activation.Epoch.CutoffReceivedAt
	runner, err := backfill.New(db, backfill.Options{LegacyCoverageStartAt: &coverageStart, HistoricalCoverageChecked: true})
	require.NoError(tb, err)

	result, err := runner.Run(tb.Context())
	require.NoError(tb, err)
	require.True(tb, result.Completed)

	dispatcher, err := youtubedispatch.NewDispatcher(youtubedispatch.Dependencies{
		DB: db, Cache: cacheClient, Sender: sender,
		Renderer: template.NewRenderer(db, logger), MessageStrings: messagestrings.NewStore(db, logger),
	}, logger, config)
	require.NoError(tb, err)

	return dispatcher
}
