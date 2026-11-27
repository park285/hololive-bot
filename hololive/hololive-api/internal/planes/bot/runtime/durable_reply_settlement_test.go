package botruntime

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v6/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability/durabilitytest"
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
			durable := durabilitytest.Open(t)
			repository := newTestReplyOutboxRepository(t, durable.Store)

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

			require.NoError(t, durable.DB.QueryRowContext(t.Context(), "SELECT status, claim_token IS NULL AND lease_until IS NULL FROM bot_reply_outbox WHERE id = ?1", claim.ID).
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
	durable := durabilitytest.Open(t)
	repository := newTestReplyOutboxRepository(t, durable.Store)
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

	require.NoError(t, durable.DB.QueryRowContext(t.Context(), "SELECT status, payload IS NOT NULL FROM bot_reply_outbox WHERE id = ?1", claim.ID).Scan(&rowStatus, &payloadKept))

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
	durable := durabilitytest.Open(t)
	repository := newTestReplyOutboxRepository(t, durable.Store)
	_, err := repository.Insert(t.Context(), &durability.ReplyOutboxEntry{
		MessageID: "message:blocked-settlement", Phase: transport.ReplyPhase, RoomID: testRoomID,
		Payload: []byte(`{"kind":"text","message":"answer"}`), ClientRequestID: transport.ReplyClientRequestID("message:blocked-settlement", 0),
	})
	require.NoError(t, err)

	claim, err := repository.Claim(t.Context(), "blocked-token", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claim)

	holder, err := durable.DB.Conn(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, holder.Close()) }()

	_, err = holder.ExecContext(t.Context(), "BEGIN IMMEDIATE")
	require.NoError(t, err)

	defer func() {
		_, rollbackErr := holder.ExecContext(context.WithoutCancel(t.Context()), "ROLLBACK")
		require.NoError(t, rollbackErr)
	}()

	runtime := &durableRuntime{outbox: repository, settlementTimeout: 25 * time.Millisecond, outboxMaxAttempts: testDurableProfile(t).BotReplyOutbox.MaxAttempts}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	applied, err := runtime.settleOutboxDispatch(ctx, claim, "blocked-token", durability.ReplyOutboxAccepted, "iris-request", nil)
	require.False(t, applied)
	require.Error(t, err)

	require.ErrorIs(t, err, context.DeadlineExceeded, "정산 자체의 시간 예산으로 종료되어야 함")
}
