package botruntime

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
)

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

func TestEpochBootstrapFailurePreservesStoredReplyPayload(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestReplyOutboxRepository(t, pool)
	_, err := repository.Insert(t.Context(), &durability.ReplyOutboxEntry{
		MessageID: "message:epoch-bootstrap", Phase: transport.ReplyPhase, RoomID: testRoomID,
		Payload:         []byte(`{"kind":"text","message":"answer"}`),
		ClientRequestID: transport.ReplyClientRequestID("message:epoch-bootstrap", 0),
	})
	require.NoError(t, err)

	claim, err := repository.Claim(t.Context(), "epoch-bootstrap-token", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claim)

	var discoveries, protected atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/auth/epoch" {
			discoveries.Add(1)
		} else {
			protected.Add(1)
		}

		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	defer server.Close()

	client, err := iris.NewClient(
		iris.WithBaseURL(server.URL), iris.WithBotToken("fixture-token"),
		iris.WithHTTPClient(server.Client()), iris.WithReplyRetry(3),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	requestID, dispatchErr := transport.DispatchStoredReply(t.Context(), client, claim.RoomID, claim.Payload, claim.ClientRequestID)
	require.Error(t, dispatchErr)
	require.Equal(t, int32(1), discoveries.Load())
	require.Zero(t, protected.Load())

	runtime := &durableRuntime{outbox: repository, settlementTimeout: 3 * time.Second, outboxMaxAttempts: testDurableProfile(t).BotReplyOutbox.MaxAttempts}
	status := replyOutboxSettlementStatusWithMaxAttempts(claim.Attempts, runtime.outboxMaxAttempts, dispatchErr)
	applied, err := runtime.settleOutboxDispatch(t.Context(), claim, "epoch-bootstrap-token", status, requestID, dispatchErr)
	require.NoError(t, err)
	require.True(t, applied)

	var (
		rowStatus   string
		payloadKept bool
	)

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status, payload IS NOT NULL FROM bot_reply_outbox WHERE id = $1", claim.ID).Scan(&rowStatus, &payloadKept))

	if rowStatus != durability.ReplyOutboxRetryablePreDispatch || !payloadKept {
		t.Fatalf("temporary bootstrap failure settled as %q with payload_kept=%t", rowStatus, payloadKept)
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

	// 같은 정산 deadline이 pgx의 context 취소나 socket write timeout으로 관측될 수 있다.
	timeoutErr, ok := errors.AsType[net.Error](err)
	require.True(t, ok && timeoutErr.Timeout(), "정산 자체의 시간 예산으로 종료되어야 함: %v", err)
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
