package botruntime

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v6/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	dbtest "github.com/kapu/hololive-dbtest"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

const testHandoffRequestID = "iris-request"

func TestDecideReplyHandoff(t *testing.T) {
	t.Parallel()

	const (
		horizon     = 105 * time.Second
		maxAttempts = int32(5)
	)

	inFlight := durability.ReplyHandoffClaim{IrisRequestID: testHandoffRequestID, Attempts: 1, Elapsed: time.Second}
	atHorizon := durability.ReplyHandoffClaim{IrisRequestID: testHandoffRequestID, Attempts: 1, Elapsed: horizon}
	exhausted := durability.ReplyHandoffClaim{IrisRequestID: testHandoffRequestID, Attempts: maxAttempts, Elapsed: time.Second}
	granted := durability.ReplyHandoffClaim{IrisRequestID: testHandoffRequestID, Attempts: maxAttempts, OperatorReplayGrants: 1, Elapsed: time.Second}
	replayExpired := durability.ReplyHandoffClaim{IrisRequestID: testHandoffRequestID, Attempts: 1, Elapsed: time.Second, ReplayHorizonExpired: true}

	snapshot := func(state string) *iris.ReplyStatusSnapshot {
		return &iris.ReplyStatusSnapshot{RequestID: testHandoffRequestID, State: state}
	}
	notFound := &iris.HTTPError{StatusCode: http.StatusNotFound}
	unavailable := fmt.Errorf("get reply status: %w", &iris.HTTPError{StatusCode: http.StatusServiceUnavailable})

	cases := []struct {
		name      string
		claim     durability.ReplyHandoffClaim
		snapshot  *iris.ReplyStatusSnapshot
		err       error
		wantState string
		wantCode  string
	}{
		{name: "handoff completed confirms the reply", claim: inFlight, snapshot: snapshot("handoff_completed"), wantState: durability.ReplyOutboxHandoffCompleted},
		{name: "padded upper-case completed needs review", claim: inFlight, snapshot: snapshot("  HANDOFF_COMPLETED "), wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeOutcomeUnknown},
		{name: "capitalized failed needs review instead of reissue", claim: inFlight, snapshot: snapshot("Failed"), wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeOutcomeUnknown},
		{name: "failed with budget is reissued", claim: inFlight, snapshot: snapshot("failed"), wantState: durability.ReplyOutboxRetryablePreDispatch, wantCode: contractsalarm.HandoffCodeFailed},
		{name: "failed with operator grant is reissued", claim: granted, snapshot: snapshot("failed"), wantState: durability.ReplyOutboxRetryablePreDispatch, wantCode: contractsalarm.HandoffCodeFailed},
		{name: "failed with exhausted attempts is dead", claim: exhausted, snapshot: snapshot("failed"), wantState: durability.ReplyOutboxDead, wantCode: contractsalarm.HandoffCodeFailed},
		{name: "failed past the replay horizon is dead", claim: replayExpired, snapshot: snapshot("failed"), wantState: durability.ReplyOutboxDead, wantCode: contractsalarm.HandoffCodeFailed},
		{name: "outcome unknown needs review", claim: inFlight, snapshot: snapshot("outcome_unknown"), wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeOutcomeUnknown},
		{name: "unrecognized state needs review", claim: inFlight, snapshot: snapshot("delivered"), wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeOutcomeUnknown},
		{name: "empty snapshot needs review", claim: inFlight, wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeOutcomeUnknown},
		{name: "another request id needs review", claim: inFlight, snapshot: &iris.ReplyStatusSnapshot{RequestID: "iris-other", State: "handoff_completed"}, wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeOutcomeUnknown},
		{name: "missing status needs review", claim: inFlight, err: fmt.Errorf("get reply status: %w", notFound), wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeStatusMissing},
		{name: "in flight before the horizon is rechecked", claim: inFlight, snapshot: snapshot("sending")},
		{name: "in flight at the horizon is unconfirmed", claim: atHorizon, snapshot: snapshot("queued"), wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeUnconfirmed},
		{name: "lookup error before the horizon is rechecked without a transition", claim: inFlight, err: unavailable},
		{name: "lookup timeout before the horizon is rechecked without a transition", claim: inFlight, err: context.DeadlineExceeded},
		{name: "oversized response before the horizon is rechecked without a transition", claim: inFlight, err: &iris.TransportError{Err: iris.ErrResponseTooLarge}},
		{name: "lookup error at the horizon is unconfirmed", claim: atHorizon, err: unavailable, wantState: durability.ReplyOutboxManualReview, wantCode: contractsalarm.HandoffCodeUnconfirmed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := decideReplyHandoff(tc.claim, tc.snapshot, tc.err, horizon, maxAttempts)
			assert.Equal(t, replyHandoffDecision{status: tc.wantState, lastError: tc.wantCode}, got)
		})
	}
}

func TestDecideReplyHandoffRoomNotJoinedIsDead(t *testing.T) {
	t.Parallel()

	reason := iris.ReplyFailureReasonRoomNotJoined
	snapshot := &iris.ReplyStatusSnapshot{RequestID: testHandoffRequestID, State: "failed", Reason: &reason}

	for _, claim := range []durability.ReplyHandoffClaim{
		{IrisRequestID: testHandoffRequestID, Attempts: 1, Elapsed: time.Second},
		{IrisRequestID: testHandoffRequestID, Attempts: 5, OperatorReplayGrants: 1, Elapsed: time.Second},
	} {
		got := decideReplyHandoff(claim, snapshot, nil, 105*time.Second, 5)
		assert.Equal(t, durability.ReplyOutboxDead, got.status)
		assert.Equal(t, contractsalarm.HandoffCodeRoomNotJoined, got.lastError)
	}
}

func TestReplyHandoffNextStep(t *testing.T) {
	t.Parallel()

	cases := []struct {
		elapsed, want time.Duration
	}{
		{elapsed: 0, want: 250 * time.Millisecond},
		{elapsed: 250 * time.Millisecond, want: 500 * time.Millisecond},
		{elapsed: 749 * time.Millisecond, want: 500 * time.Millisecond},
		{elapsed: 750 * time.Millisecond, want: time.Second},
		{elapsed: 1750 * time.Millisecond, want: 2 * time.Second},
		{elapsed: 3750 * time.Millisecond, want: 5 * time.Second},
		{elapsed: 8750 * time.Millisecond, want: 15 * time.Second},
		{elapsed: 23750 * time.Millisecond, want: time.Minute},
		{elapsed: 83750 * time.Millisecond, want: time.Minute},
		{elapsed: 10 * time.Minute, want: time.Minute},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, replyHandoffNextStep(tc.elapsed), "elapsed %s", tc.elapsed)
	}

	assert.Equal(t, 250*time.Millisecond, replyHandoffFirstCheck())
}

type handoffIrisClient struct {
	stubIrisClient

	mu        sync.Mutex
	failedErr error
	sent      []string
	accepted  map[string]bool
	failed    map[string]bool
	states    map[string]string
	statusErr error
}

func newHandoffIrisClient(t *testing.T) *handoffIrisClient {
	t.Helper()

	return &handoffIrisClient{
		failedErr: sharedtestutil.IrisHTTPError(t.Context(), t, http.StatusConflict, `{"code":"CLIENT_REQUEST_ID_FAILED"}`),
		accepted:  map[string]bool{}, failed: map[string]bool{}, states: map[string]string{},
	}
}

func irisRequestIDFor(clientRequestID string) string { return "iris-" + clientRequestID }

func (c *handoffIrisClient) SendMessageAccepted(_ context.Context, _, _ string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := sentClientRequestID(opts)

	c.sent = append(c.sent, id)

	if c.failed[id] {
		return nil, c.failedErr
	}

	duplicate := c.accepted[id]

	c.accepted[id] = true

	return &iris.ReplyAcceptedResponse{Success: true, Delivery: "queued", RequestID: irisRequestIDFor(id), Duplicate: &duplicate}, nil
}

func (c *handoffIrisClient) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.statusErr != nil {
		return nil, c.statusErr
	}

	return &iris.ReplyStatusSnapshot{RequestID: requestID, State: cmp.Or(c.states[requestID], "queued")}, nil
}

func (c *handoffIrisClient) failRequest(clientRequestID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.failed[clientRequestID] = true
	c.states[irisRequestIDFor(clientRequestID)] = "failed"
}

func (c *handoffIrisClient) setState(requestID, state string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.states[requestID] = state
}

func (c *handoffIrisClient) sentIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.sent...)
}

func sentClientRequestID(opts []iris.SendOption) string {
	box := reflect.New(reflect.TypeFor[iris.SendOption]().In(0).Elem())

	for _, opt := range opts {
		reflect.ValueOf(opt).Call([]reflect.Value{box})
	}

	value, ok := reflect.TypeAssert[*string](box.Elem().FieldByName("ClientRequestID"))
	if !ok || value == nil {
		return ""
	}

	return *value
}

func newHandoffRuntime(t *testing.T, pool *pgxpool.Pool, client durableIrisClient, maxAttempts int32) *durableRuntime {
	t.Helper()

	return &durableRuntime{
		outbox: newTestReplyOutboxRepository(t, pool), irisClient: client, logger: slog.New(slog.DiscardHandler),
		outboxTracker: workercontract.NewExecutorTracker(), outboxTotals: &workercontract.Counters{},
		outboxClaimLease: time.Minute, dispatchBudget: time.Hour, settlementTimeout: 2 * time.Hour,
		outboxMaxAttempts: maxAttempts, outboxRetryAfter: time.Millisecond,
	}
}

func insertHandoffReply(t *testing.T, r *durableRuntime, messageID string) string {
	t.Helper()

	clientRequestID := transport.ReplyClientRequestID(messageID, 0)
	_, err := r.outbox.Insert(t.Context(), &durability.ReplyOutboxEntry{
		MessageID: messageID, Phase: transport.ReplyPhase, RoomID: testRoomID,
		Payload: []byte(`{"kind":"text","message":"answer"}`), ClientRequestID: clientRequestID,
	})
	require.NoError(t, err)

	return clientRequestID
}

func dispatchNext(t *testing.T, r *durableRuntime) {
	t.Helper()

	require.True(t, r.processNextOutbox(t.Context(), func() {}), "no reply became claimable")
}

func reconcileDue(t *testing.T, r *durableRuntime, pool *pgxpool.Pool, clientRequestID string) {
	t.Helper()

	tag, err := pool.Exec(t.Context(), `UPDATE bot_reply_outbox
		SET accepted_at = statement_timestamp() - (handoff_next_check_at - accepted_at), handoff_next_check_at = statement_timestamp()
		WHERE client_request_id = $1 AND handoff_next_check_at IS NOT NULL`, clientRequestID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	require.True(t, r.reconcileReplyHandoffBatch(t.Context(), func() {}), "no accepted reply became due for a handoff check")
}

func freezeHandoffClock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		CREATE TABLE handoff_clock AS SELECT pg_catalog.statement_timestamp() AS instant;
		CREATE FUNCTION public.clock_timestamp() RETURNS timestamptz LANGUAGE sql VOLATILE
			AS 'SELECT instant FROM public.handoff_clock';
		DO $$ BEGIN
			EXECUTE format('ALTER DATABASE %I SET search_path TO public, pg_catalog', current_database());
		END $$`)
	require.NoError(t, err)
	pool.Reset()
}

func advanceToStoredHandoffCheck(t *testing.T, pool *pgxpool.Pool, clientRequestID string) (accepted, due time.Time) {
	t.Helper()

	require.NoError(t, pool.QueryRow(t.Context(), `UPDATE handoff_clock SET instant = handoff_next_check_at
		FROM bot_reply_outbox WHERE client_request_id = $1 AND status = 'accepted'
		RETURNING accepted_at, instant`, clientRequestID).Scan(&accepted, &due))

	return accepted, due
}

type handoffRow struct {
	status, irisRequestID, lastError string
	payloadKept, checkPending        bool
	attempts                         int32
}

func readHandoffRow(t *testing.T, pool *pgxpool.Pool, clientRequestID string) handoffRow {
	t.Helper()

	var row handoffRow

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT status, iris_request_id, last_error, payload IS NOT NULL,
			handoff_next_check_at IS NOT NULL, attempts
		FROM bot_reply_outbox WHERE client_request_id = $1`, clientRequestID).
		Scan(&row.status, &row.irisRequestID, &row.lastError, &row.payloadKept, &row.checkPending, &row.attempts))

	return row
}

func TestReplyHandoffFailedReissuesWithinTheGenerationCap(t *testing.T) {
	pool := dbtest.NewPool(t)
	client := newHandoffIrisClient(t)
	r := newHandoffRuntime(t, pool, client, testDurableProfile(t).BotReplyOutbox.MaxAttempts)
	base := insertHandoffReply(t, r, "message:handoff-reissue")
	generations := []string{base, base + ":r1", base + ":r2"}

	freezeHandoffClock(t, pool)

	for generation, id := range generations {
		dispatchNext(t, r)

		row := readHandoffRow(t, pool, base)
		require.Equal(t, durability.ReplyOutboxAccepted, row.status, "generation %d", generation)
		require.Equal(t, irisRequestIDFor(id), row.irisRequestID, "generation %d", generation)
		require.True(t, row.checkPending)

		client.failRequest(id)

		advanceToStoredHandoffCheck(t, pool, base)
		require.True(t, r.reconcileReplyHandoffBatch(t.Context(), func() {}), "no accepted reply became due for a handoff check")

		var available, updated time.Time

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT available_at, updated_at FROM bot_reply_outbox
			WHERE client_request_id = $1`, base).Scan(&available, &updated))
		require.Equal(t, time.Millisecond<<generation, available.Sub(updated), "generation %d retry delay", generation)

		_, err := pool.Exec(t.Context(), `UPDATE bot_reply_outbox SET available_at = statement_timestamp() WHERE client_request_id = $1`, base)
		require.NoError(t, err)

		row = readHandoffRow(t, pool, base)
		require.Equal(t, durability.ReplyOutboxRetryablePreDispatch, row.status, "generation %d", generation)
		require.Equal(t, contractsalarm.HandoffCodeFailed, row.lastError)
		require.True(t, row.payloadKept)
		require.False(t, row.checkPending)
	}

	dispatchNext(t, r)

	row := readHandoffRow(t, pool, base)
	assert.Equal(t, durability.ReplyOutboxPermanentConflict, row.status)
	assert.False(t, row.payloadKept)
	assert.Equal(t, []string{
		base,
		base, base + ":r1",
		base, base + ":r1", base + ":r2",
		base, base + ":r1", base + ":r2",
	}, client.sentIDs())

	for _, id := range client.sentIDs() {
		assert.NotContains(t, id, ":r3", "reissue generation must stay within the cap of two")
	}
}

func TestReplyHandoffFailedWithoutAttemptBudgetIsDead(t *testing.T) {
	pool := dbtest.NewPool(t)
	client := newHandoffIrisClient(t)
	r := newHandoffRuntime(t, pool, client, 1)
	base := insertHandoffReply(t, r, "message:handoff-dead")

	dispatchNext(t, r)
	client.failRequest(base)
	reconcileDue(t, r, pool, base)

	row := readHandoffRow(t, pool, base)
	assert.Equal(t, durability.ReplyOutboxDead, row.status)
	assert.Equal(t, contractsalarm.HandoffCodeFailed, row.lastError)
	assert.False(t, row.payloadKept)
	assert.Equal(t, []string{base}, client.sentIDs())
}

func TestReplyHandoffObservationOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		state     string
		statusErr error
		want      handoffRow
	}{
		{name: "completed", state: "handoff_completed", want: handoffRow{status: durability.ReplyOutboxHandoffCompleted}},
		{name: "outcome unknown", state: "outcome_unknown", want: handoffRow{status: durability.ReplyOutboxManualReview, lastError: contractsalarm.HandoffCodeOutcomeUnknown, payloadKept: true}},
		{name: "missing status", statusErr: &iris.HTTPError{StatusCode: http.StatusNotFound}, want: handoffRow{status: durability.ReplyOutboxManualReview, lastError: contractsalarm.HandoffCodeStatusMissing, payloadKept: true}},
		{name: "lookup error", statusErr: &iris.HTTPError{StatusCode: http.StatusServiceUnavailable}, want: handoffRow{status: durability.ReplyOutboxAccepted, payloadKept: true, checkPending: true}},
		{name: "still sending", state: "sending", want: handoffRow{status: durability.ReplyOutboxAccepted, payloadKept: true, checkPending: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			client := newHandoffIrisClient(t)
			r := newHandoffRuntime(t, pool, client, testDurableProfile(t).BotReplyOutbox.MaxAttempts)
			base := insertHandoffReply(t, r, "message:handoff-"+strings.ReplaceAll(tc.name, " ", "-"))

			dispatchNext(t, r)
			client.setState(irisRequestIDFor(base), tc.state)

			client.statusErr = tc.statusErr

			reconcileDue(t, r, pool, base)

			row := readHandoffRow(t, pool, base)

			row.irisRequestID, row.attempts = "", 0
			assert.Equal(t, tc.want, row)
			assert.Equal(t, []string{base}, client.sentIDs(), "handoff 확인은 재발송하지 않는다")
		})
	}
}

func TestReplyHandoffRescheduleRevokesClaimAtSameDeadline(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	r := newHandoffRuntime(t, pool, newHandoffIrisClient(t), testDurableProfile(t).BotReplyOutbox.MaxAttempts)
	base := insertHandoffReply(t, r, "message:handoff-same-deadline")
	freezeHandoffClock(t, pool)
	dispatchNext(t, r)

	accepted, due := advanceToStoredHandoffCheck(t, pool, base)
	horizon := due.Sub(accepted) + replyHandoffSoftClaim
	claims, err := r.outbox.ClaimHandoffDue(ctx, 1, replyHandoffSoftClaim)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.Equal(t, accepted.Add(horizon), claims[0].CheckToken)

	delay, applied, err := r.outbox.RescheduleHandoff(ctx, claims[0], time.Hour, horizon)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, replyHandoffSoftClaim, delay)

	for _, status := range []string{durability.ReplyOutboxHandoffCompleted, durability.ReplyOutboxManualReview, durability.ReplyOutboxRetryablePreDispatch} {
		applied, err = r.outbox.SettleHandoff(ctx, durability.ReplyHandoffSettlement{Claim: claims[0], Status: status, RetryAfter: time.Millisecond})
		require.NoError(t, err)
		require.False(t, applied, "재예약 뒤 이전 확인으로 %s 전이를 적용하지 않는다", status)
	}

	_, applied, err = r.outbox.RescheduleHandoff(ctx, claims[0], time.Millisecond, horizon)
	require.NoError(t, err)
	require.False(t, applied, "이전 확인으로 다시 재예약하지 않는다")

	_, next := advanceToStoredHandoffCheck(t, pool, base)
	require.Equal(t, accepted.Add(horizon), next)

	claims, err = r.outbox.ClaimHandoffDue(ctx, 1, replyHandoffSoftClaim)
	require.NoError(t, err)
	require.Len(t, claims, 1)

	applied, err = r.outbox.SettleHandoff(ctx, durability.ReplyHandoffSettlement{Claim: claims[0], Status: durability.ReplyOutboxHandoffCompleted})
	require.NoError(t, err)
	require.True(t, applied)
}

func TestReplyHandoffUnconfirmedAtTheHorizonNeedsReview(t *testing.T) {
	pool := dbtest.NewPool(t)
	client := newHandoffIrisClient(t)
	r := newHandoffRuntime(t, pool, client, testDurableProfile(t).BotReplyOutbox.MaxAttempts)
	base := insertHandoffReply(t, r, "message:handoff-horizon")
	freezeHandoffClock(t, pool)

	dispatchNext(t, r)

	accepted, firstCheck := advanceToStoredHandoffCheck(t, pool, base)
	require.Equal(t, 250*time.Millisecond, firstCheck.Sub(accepted), "dispatch first handoff check delay")
	require.True(t, r.reconcileReplyHandoffBatch(t.Context(), func() {}))

	_, due := advanceToStoredHandoffCheck(t, pool, base)
	require.Equal(t, 500*time.Millisecond, due.Sub(firstCheck), "unclipped handoff recheck delay")

	claims, err := r.outbox.ClaimHandoffDue(t.Context(), replyHandoffBatchSize, replyHandoffSoftClaim)
	require.NoError(t, err)
	require.Len(t, claims, 1)

	var softClaimVersion string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT xmin::text FROM bot_reply_outbox WHERE client_request_id = $1`, base).Scan(&softClaimVersion))

	r.dispatchBudget = claims[0].Elapsed + time.Millisecond
	r.reconcileReplyHandoff(t.Context(), claims[0])

	pending := readHandoffRow(t, pool, base)
	require.Equal(t, durability.ReplyOutboxAccepted, pending.status)
	require.True(t, pending.checkPending)

	var (
		nextCheck          time.Time
		rescheduledVersion string
	)

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT handoff_next_check_at, xmin::text FROM bot_reply_outbox
		WHERE client_request_id = $1`, base).Scan(&nextCheck, &rescheduledVersion))
	require.NotEqual(t, softClaimVersion, rescheduledVersion, "reschedule must update the soft-claimed row")
	require.True(t, nextCheck.After(due), "reschedule must advance past the previous check: got %s, previous %s", nextCheck, due)
	require.Equal(t, accepted.Add(r.dispatchBudget), nextCheck, "last recheck must land on the dispatch horizon")

	advanceToStoredHandoffCheck(t, pool, base)
	require.True(t, r.reconcileReplyHandoffBatch(t.Context(), func() {}))

	row := readHandoffRow(t, pool, base)
	assert.Equal(t, durability.ReplyOutboxManualReview, row.status)
	assert.Equal(t, contractsalarm.HandoffCodeUnconfirmed, row.lastError)
	assert.True(t, row.payloadKept)
	assert.Equal(t, []string{base}, client.sentIDs())
}

func TestAcceptedSettlementLossResendsTheSameRequest(t *testing.T) {
	pool := dbtest.NewPool(t)
	client := newHandoffIrisClient(t)
	r := newHandoffRuntime(t, pool, client, testDurableProfile(t).BotReplyOutbox.MaxAttempts)
	base := insertHandoffReply(t, r, "message:handoff-lost-settlement")

	claim, err := r.outbox.Claim(t.Context(), "crashed-owner", time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, claim)

	requestID, err := transport.DispatchStoredReply(t.Context(), client, claim.RoomID, claim.Payload, claim.ClientRequestID)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `UPDATE bot_reply_outbox SET lease_until = lease_until - interval '1 millisecond' WHERE id = $1`, claim.ID)
	require.NoError(t, err)

	reclaim, err := r.outbox.ReclaimExpired(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, durability.ReplyOutboxReclaim{Requeued: 1}, reclaim)

	dispatchNext(t, r)

	row := readHandoffRow(t, pool, base)
	assert.Equal(t, durability.ReplyOutboxAccepted, row.status)
	assert.Equal(t, requestID, row.irisRequestID)
	assert.Equal(t, []string{base, base}, client.sentIDs())
}
