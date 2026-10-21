package botruntime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
)

// admission 정산은 shutdown 취소 뒤에도 끝나고, accepted는 lease를 놓되 handoff 확인 전까지 같은 방
// 후행 응답을 보류한다. 결과 불명도 같은 방을 보류한 채 같은 ID 재확인을 기다린다.
func TestReplySettlementAfterCancellationHoldsRoomOrder(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		dispatchErr error
		status      string
	}{
		{name: "durable admission", status: durability.ReplyOutboxAccepted},
		{name: "lost admission response", dispatchErr: transport.ErrReplyOutcomeUnknown, status: durability.ReplyOutboxOutcomeUnknown},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			repository := newTestReplyOutboxRepository(t, pool)

			for ordinal := range uint64(2) {
				_, err := repository.Insert(t.Context(), &durability.ReplyOutboxEntry{
					MessageID: "message:settlement", Phase: transport.ReplyPhase, Ordinal: ordinal, RoomID: testRoomID,
					Payload: []byte(`{"kind":"text","message":"answer"}`), ClientRequestID: transport.ReplyClientRequestID("message:settlement", ordinal),
				})
				require.NoError(t, err)
			}

			claim, err := repository.Claim(t.Context(), "settlement-token", time.Minute)
			require.NoError(t, err)
			require.NotNil(t, claim)

			runtime := &durableRuntime{outbox: repository, settlementTimeout: 3 * time.Second, outboxMaxAttempts: testDurableProfile(t).BotReplyOutbox.MaxAttempts, outboxTotals: &workercontract.Counters{}}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			status := replyOutboxSettlementStatusWithMaxAttempts(claim.Attempts, runtime.outboxMaxAttempts, testCase.dispatchErr)
			applied, err := runtime.settleOutboxDispatch(ctx, claim, "settlement-token", status, "iris-request", testCase.dispatchErr)
			require.NoError(t, err)
			require.True(t, applied)
			runtime.finishOutboxSettlement(claim, status, testCase.dispatchErr, applied, err)

			var (
				rowStatus string
				unleased  bool
			)

			require.NoError(t, pool.QueryRow(t.Context(), "SELECT status, claim_token IS NULL AND lease_until IS NULL FROM bot_reply_outbox WHERE id = $1", claim.ID).
				Scan(&rowStatus, &unleased))
			require.Equal(t, testCase.status, rowStatus)
			require.True(t, unleased)

			next, err := repository.Claim(t.Context(), "next-token", time.Minute)
			require.NoError(t, err)

			if testCase.dispatchErr == nil {
				require.Nil(t, next, "handoff 확인 전의 accepted 행은 다시 보내지 않고 같은 방 후행 응답도 보내지 않는다")
			} else if next != nil {
				require.Equal(t, claim.ID, next.ID, "결과 불명 행은 같은 ID로만 다시 보내고 같은 방 후행 응답은 보류한다")
			}

			totals := runtime.outboxTotals.Snapshot().Attempts

			if testCase.dispatchErr == nil {
				require.Equal(t, uint64(1), totals.Success)
			} else {
				require.Equal(t, uint64(1), totals.OutcomeUnknown)
				require.Zero(t, totals.Failed)
			}
		})
	}
}

func TestAcceptedReplyLostSettlementClaimRecordsUnknownAttempt(t *testing.T) {
	runtime := &durableRuntime{logger: slog.New(slog.DiscardHandler), outboxTotals: &workercontract.Counters{}}
	runtime.finishOutboxSettlement(&durability.ReplyOutboxClaim{Attempts: 1}, durability.ReplyOutboxAccepted, nil, false, nil)

	totals := runtime.outboxTotals.Snapshot().Attempts
	require.Equal(t, uint64(1), totals.OutcomeUnknown)
	require.Zero(t, totals.Failed)
}

func TestReplySettlementKeepsBoundedBudgetAfterCallerCancellation(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestReplyOutboxRepository(t, pool)
	_, err := repository.Insert(t.Context(), &durability.ReplyOutboxEntry{
		MessageID: "message:blocked-settlement", Phase: transport.ReplyPhase, RoomID: testRoomID,
		Payload: []byte(`{"kind":"text","message":"answer"}`), ClientRequestID: transport.ReplyClientRequestID("message:blocked-settlement", 0),
	})
	require.NoError(t, err)

	claim, err := repository.Claim(t.Context(), "blocked-token", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claim)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

	_, err = tx.Exec(t.Context(), "SELECT id FROM bot_reply_outbox WHERE id = $1 FOR UPDATE", claim.ID)
	require.NoError(t, err)

	runtime := &durableRuntime{outbox: repository, settlementTimeout: 25 * time.Millisecond, outboxMaxAttempts: testDurableProfile(t).BotReplyOutbox.MaxAttempts}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	applied, err := runtime.settleOutboxDispatch(ctx, claim, "blocked-token", durability.ReplyOutboxAccepted, "iris-request", nil)
	require.False(t, applied)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded, "blocked settlement must expire its own budget: %v", err)
}

func TestReplySettlementUsesRuntimeReserveWhileOrdinaryTransactionHeld(t *testing.T) {
	base := dbtest.NewPool(t)
	db := base.Config().ConnConfig
	resources, closePool, err := databaseproviders.ProvideDatabaseResources(t.Context(), &settings.PostgresConfig{
		Host: db.Host, Port: int(db.Port), User: db.User, Password: db.Password,
		Database: db.Database, SSLMode: "disable", PoolMinConns: 0, PoolMaxConns: 2,
	}, databaseproviders.RuntimePool{Name: "bot", ReservedConns: 1}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(closePool)

	pool := resources.Service.GetPool()
	repository := newTestReplyOutboxRepository(t, pool)

	_, err = repository.Insert(t.Context(), &durability.ReplyOutboxEntry{
		MessageID: "message:reserved-settlement", Phase: transport.ReplyPhase, RoomID: testRoomID,
		Payload: []byte(`{"kind":"text","message":"answer"}`), ClientRequestID: transport.ReplyClientRequestID("message:reserved-settlement", 0),
	})
	require.NoError(t, err)

	claim, err := repository.Claim(t.Context(), "reserved-token", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claim)

	ordinary, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), ordinary)) }()

	queuedCtx, cancelQueued := context.WithTimeout(t.Context(), time.Second)
	defer cancelQueued()

	queued := make(chan error, 1)

	go func() {
		var value int

		queued <- pool.QueryRow(queuedCtx, "SELECT 1").Scan(&value)
	}()

	runtime := &durableRuntime{outbox: repository, settlementTimeout: 3 * time.Second, outboxMaxAttempts: testDurableProfile(t).BotReplyOutbox.MaxAttempts}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	applied, err := runtime.settleOutboxDispatch(ctx, claim, "reserved-token", durability.ReplyOutboxAccepted, "iris-request", nil)
	require.NoError(t, err)
	require.True(t, applied)
	require.ErrorIs(t, <-queued, context.DeadlineExceeded, "ordinary work must not consume the reserved connection")
	require.NoError(t, ordinary.Commit(t.Context()))

	var status string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM bot_reply_outbox WHERE id = $1", claim.ID).Scan(&status))
	require.Equal(t, durability.ReplyOutboxAccepted, status)
}
