package botruntime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestAcceptedReplySettlementAfterCancellationReleasesRoomQueue(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		dispatchErr error
		status      string
	}{
		{name: "known handoff", status: durability.ReplyOutboxHandoffCompleted},
		{name: "unknown handoff", dispatchErr: transport.ErrReplyOutcomeUnknown, status: durability.ReplyOutboxManualReview},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			repository := durability.NewReplyOutboxRepository(pool)

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

			applied, err := repository.MarkAccepted(t.Context(), claim.ID, "settlement-token", "iris-request")
			require.NoError(t, err)
			require.True(t, applied)

			runtime := &durableRuntime{outbox: repository, settlementTimeout: 3 * time.Second, outboxMaxAttempts: durableMaxAttempts, outboxTotals: &workercontract.Counters{}}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			applied, err = runtime.settleOutboxDispatch(ctx, claim, "settlement-token", true, testCase.dispatchErr)
			require.NoError(t, err)
			require.True(t, applied)
			runtime.finishOutboxSettlement(claim, true, testCase.dispatchErr, applied, err)

			var status string

			require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM bot_reply_outbox WHERE id = $1", claim.ID).Scan(&status))
			require.Equal(t, testCase.status, status)

			next, err := repository.Claim(t.Context(), "next-token", time.Minute)
			require.NoError(t, err)
			require.NotNil(t, next, "settled accepted row must release the following room reply")
			require.Equal(t, uint64(1), next.Ordinal, "accepted reply must not enter automatic replay")

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
	runtime.finishOutboxSettlement(&durability.ReplyOutboxClaim{Attempts: 1}, true, nil, false, nil)

	totals := runtime.outboxTotals.Snapshot().Attempts
	require.Equal(t, uint64(1), totals.OutcomeUnknown)
	require.Zero(t, totals.Failed)
}

func TestReplySettlementKeepsBoundedBudgetAfterCallerCancellation(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := durability.NewReplyOutboxRepository(pool)
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

	defer func() { require.NoError(t, tx.Rollback(context.WithoutCancel(t.Context()))) }()

	_, err = tx.Exec(t.Context(), "SELECT id FROM bot_reply_outbox WHERE id = $1 FOR UPDATE", claim.ID)
	require.NoError(t, err)

	runtime := &durableRuntime{outbox: repository, settlementTimeout: 25 * time.Millisecond, outboxMaxAttempts: durableMaxAttempts}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	applied, err := runtime.settleOutboxDispatch(ctx, claim, "blocked-token", false, nil)
	require.False(t, applied)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded, "blocked settlement must expire its own budget: %v", err)
}
