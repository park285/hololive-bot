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
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	dbtest "github.com/kapu/hololive-dbtest"
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
		{name: "state tolerates case and padding", claim: inFlight, snapshot: snapshot("  HANDOFF_COMPLETED "), wantState: durability.ReplyOutboxHandoffCompleted},
		{name: "failed with budget is reissued", claim: inFlight, snapshot: snapshot("failed"), wantState: durability.ReplyOutboxRetryablePreDispatch, wantCode: replyHandoffErrorFailed},
		{name: "failed with operator grant is reissued", claim: granted, snapshot: snapshot("failed"), wantState: durability.ReplyOutboxRetryablePreDispatch, wantCode: replyHandoffErrorFailed},
		{name: "failed with exhausted attempts is dead", claim: exhausted, snapshot: snapshot("failed"), wantState: durability.ReplyOutboxDead, wantCode: replyHandoffErrorFailed},
		{name: "failed past the replay horizon is dead", claim: replayExpired, snapshot: snapshot("failed"), wantState: durability.ReplyOutboxDead, wantCode: replyHandoffErrorFailed},
		{name: "outcome unknown needs review", claim: inFlight, snapshot: snapshot("outcome_unknown"), wantState: durability.ReplyOutboxManualReview, wantCode: replyHandoffErrorOutcomeUnknown},
		{name: "unrecognized state needs review", claim: inFlight, snapshot: snapshot("delivered"), wantState: durability.ReplyOutboxManualReview, wantCode: replyHandoffErrorOutcomeUnknown},
		{name: "empty snapshot needs review", claim: inFlight, wantState: durability.ReplyOutboxManualReview, wantCode: replyHandoffErrorOutcomeUnknown},
		{name: "another request id needs review", claim: inFlight, snapshot: &iris.ReplyStatusSnapshot{RequestID: "iris-other", State: "handoff_completed"}, wantState: durability.ReplyOutboxManualReview, wantCode: replyHandoffErrorOutcomeUnknown},
		{name: "missing status needs review", claim: inFlight, err: fmt.Errorf("get reply status: %w", notFound), wantState: durability.ReplyOutboxManualReview, wantCode: replyHandoffErrorStatusMissing},
		{name: "in flight before the horizon is rechecked", claim: inFlight, snapshot: snapshot("sending")},
		{name: "in flight at the horizon is unconfirmed", claim: atHorizon, snapshot: snapshot("queued"), wantState: durability.ReplyOutboxManualReview, wantCode: replyHandoffErrorUnconfirmed},
		{name: "lookup error before the horizon is rechecked without a transition", claim: inFlight, err: unavailable},
		{name: "lookup timeout before the horizon is rechecked without a transition", claim: inFlight, err: context.DeadlineExceeded},
		{name: "lookup error at the horizon is unconfirmed", claim: atHorizon, err: unavailable, wantState: durability.ReplyOutboxManualReview, wantCode: replyHandoffErrorUnconfirmed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := decideReplyHandoff(tc.claim, tc.snapshot, tc.err, horizon, maxAttempts)
			assert.Equal(t, replyHandoffDecision{status: tc.wantState, lastError: tc.wantCode}, got)
		})
	}
}

// 확인 간격은 250ms에서 시작해 60s까지 늘고, 늦게 돈 확인은 이미 지난 간격을 다시 쓰지 않는다.
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

// handoffIrisClient는 clientRequestId별 admission 상태와 requestId별 handoff 상태를 가진 가짜 Iris다.
// Failed로 표시한 요청의 같은 ID 재전송은 409 CLIENT_REQUEST_ID_FAILED, 수리된 ID의 재전송은 같은
// requestId의 duplicate 202다.
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

// failRequest는 Iris가 pre-dispatch에서 포기한 요청을 흉내 낸다. 상태 조회는 failed이고 같은 ID 재전송은 409다.
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

// sentClientRequestID는 SDK의 비공개 send option 구조체에 option을 적용해 실제로 보낸 clientRequestId를 읽는다.
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

func newHandoffRuntime(t *testing.T, pool *pgxpool.Pool, client durableIrisClient, horizon time.Duration, maxAttempts int32) *durableRuntime {
	t.Helper()

	return &durableRuntime{
		outbox: newTestReplyOutboxRepository(t, pool), irisClient: client, logger: slog.New(slog.DiscardHandler),
		outboxTracker: workercontract.NewExecutorTracker(), outboxTotals: &workercontract.Counters{},
		outboxClaimLease: time.Minute, dispatchBudget: horizon, settlementTimeout: 3 * time.Second,
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

// dispatchNext는 발송 큐에서 한 행을 claim해 admission과 정산까지 진행한다. 재발송 backoff가 남아 있으면 기다린다.
func dispatchNext(t *testing.T, r *durableRuntime) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for !r.processNextOutbox(t.Context(), func() {}) {
		require.True(t, time.Now().Before(deadline), "no reply became claimable")
		time.Sleep(5 * time.Millisecond)
	}
}

// reconcileDue는 확인 시각이 된 accepted 행이 생길 때까지 기다려 handoff 확인 한 batch를 돌린다.
func reconcileDue(t *testing.T, r *durableRuntime) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for !r.reconcileReplyHandoffBatch(t.Context(), func() {}) {
		require.True(t, time.Now().Before(deadline), "no accepted reply became due for a handoff check")
		time.Sleep(10 * time.Millisecond)
	}
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

// Iris failed는 같은 행을 재발송 큐로 되돌리고, 다음 발송의 reissue ladder가 409 FAILED를 지나 다음 세대로
// 보낸다. :r2까지 실패하면 다음 발송은 세 세대 모두 409를 받고 :r3 없이 permanent_conflict로 끝난다.
func TestReplyHandoffFailedReissuesWithinTheGenerationCap(t *testing.T) {
	pool := dbtest.NewPool(t)
	client := newHandoffIrisClient(t)
	r := newHandoffRuntime(t, pool, client, 10*time.Second, testDurableProfile(t).BotReplyOutbox.MaxAttempts)
	base := insertHandoffReply(t, r, "message:handoff-reissue")
	generations := []string{base, base + ":r1", base + ":r2"}

	for generation, id := range generations {
		dispatchNext(t, r)

		row := readHandoffRow(t, pool, base)
		require.Equal(t, durability.ReplyOutboxAccepted, row.status, "generation %d", generation)
		require.Equal(t, irisRequestIDFor(id), row.irisRequestID, "generation %d", generation)
		require.True(t, row.checkPending)

		client.failRequest(id)
		reconcileDue(t, r)

		row = readHandoffRow(t, pool, base)
		require.Equal(t, durability.ReplyOutboxRetryablePreDispatch, row.status, "generation %d", generation)
		require.Equal(t, replyHandoffErrorFailed, row.lastError)
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
	r := newHandoffRuntime(t, pool, client, 10*time.Second, 1)
	base := insertHandoffReply(t, r, "message:handoff-dead")

	dispatchNext(t, r)
	client.failRequest(base)
	reconcileDue(t, r)

	row := readHandoffRow(t, pool, base)
	assert.Equal(t, durability.ReplyOutboxDead, row.status)
	assert.Equal(t, replyHandoffErrorFailed, row.lastError)
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
		{name: "outcome unknown", state: "outcome_unknown", want: handoffRow{status: durability.ReplyOutboxManualReview, lastError: replyHandoffErrorOutcomeUnknown, payloadKept: true}},
		{name: "missing status", statusErr: &iris.HTTPError{StatusCode: http.StatusNotFound}, want: handoffRow{status: durability.ReplyOutboxManualReview, lastError: replyHandoffErrorStatusMissing, payloadKept: true}},
		{name: "lookup error", statusErr: &iris.HTTPError{StatusCode: http.StatusServiceUnavailable}, want: handoffRow{status: durability.ReplyOutboxAccepted, payloadKept: true, checkPending: true}},
		{name: "still sending", state: "sending", want: handoffRow{status: durability.ReplyOutboxAccepted, payloadKept: true, checkPending: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			client := newHandoffIrisClient(t)
			r := newHandoffRuntime(t, pool, client, 10*time.Second, testDurableProfile(t).BotReplyOutbox.MaxAttempts)
			base := insertHandoffReply(t, r, "message:handoff-"+strings.ReplaceAll(tc.name, " ", "-"))

			dispatchNext(t, r)
			client.setState(irisRequestIDFor(base), tc.state)

			client.statusErr = tc.statusErr

			reconcileDue(t, r)

			row := readHandoffRow(t, pool, base)

			row.irisRequestID, row.attempts = "", 0
			assert.Equal(t, tc.want, row)
			assert.Equal(t, []string{base}, client.sentIDs(), "handoff 확인은 재발송하지 않는다")
		})
	}
}

// 지평(bot_reply_outbox attempt timeout) 안에서는 진행 중 상태를 다시 확인하고, 지평에 닿으면 handoff 미확인으로
// payload를 보존한 manual_review가 된다.
func TestReplyHandoffUnconfirmedAtTheHorizonNeedsReview(t *testing.T) {
	pool := dbtest.NewPool(t)
	client := newHandoffIrisClient(t)
	r := newHandoffRuntime(t, pool, client, 600*time.Millisecond, testDurableProfile(t).BotReplyOutbox.MaxAttempts)
	base := insertHandoffReply(t, r, "message:handoff-horizon")

	dispatchNext(t, r)

	checks := 0

	for readHandoffRow(t, pool, base).status == durability.ReplyOutboxAccepted {
		require.Less(t, checks, 10, "the horizon must end rechecks")
		reconcileDue(t, r)

		checks++
	}

	row := readHandoffRow(t, pool, base)
	assert.Equal(t, durability.ReplyOutboxManualReview, row.status)
	assert.Equal(t, replyHandoffErrorUnconfirmed, row.lastError)
	assert.True(t, row.payloadKept)
	assert.GreaterOrEqual(t, checks, 2, "지평 전 확인은 전이 없이 다시 예약된다")
	assert.Equal(t, []string{base}, client.sentIDs())
}

// admission 뒤 accepted 정산이 기록되지 않으면 행은 submitting lease로 남는다. Lease reclaim이 같은 ID 재발송으로
// 되돌리고 Iris의 duplicate 202가 같은 requestId로 accepted를 만든다.
func TestAcceptedSettlementLossResendsTheSameRequest(t *testing.T) {
	pool := dbtest.NewPool(t)
	client := newHandoffIrisClient(t)
	r := newHandoffRuntime(t, pool, client, 10*time.Second, testDurableProfile(t).BotReplyOutbox.MaxAttempts)
	base := insertHandoffReply(t, r, "message:handoff-lost-settlement")

	claim, err := r.outbox.Claim(t.Context(), "crashed-owner", time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, claim)

	requestID, err := transport.DispatchStoredReply(t.Context(), client, claim.RoomID, claim.Payload, claim.ClientRequestID)
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)

	reclaim, err := r.outbox.ReclaimExpired(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, durability.ReplyOutboxReclaim{Requeued: 1}, reclaim)

	dispatchNext(t, r)

	row := readHandoffRow(t, pool, base)
	assert.Equal(t, durability.ReplyOutboxAccepted, row.status)
	assert.Equal(t, requestID, row.irisRequestID)
	assert.Equal(t, []string{base, base}, client.sentIDs())
}
