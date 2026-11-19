package handoffloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

const (
	testLedgerName  = "test ledger"
	testRequestID   = "iris-request-1"
	testHorizon     = 10 * time.Minute
	testSubjectKey  = "subject"
	opComplete      = "complete"
	opReschedule    = "reschedule"
	opReissue       = "reissue"
	opExhaust       = "exhaust"
	opQuarantine    = "quarantine"
	stateCompleted  = "handoff_completed"
	stateFailed     = "failed"
	stateQueued     = "queued"
	gatedClaimCount = 8
)

var (
	errTestCheckClosed  = errors.New("test check closed")
	errTestNotRechecked = errors.New("test follow-up not retried")
)

type testClaim struct {
	id         int64
	requestID  string
	elapsed    time.Duration
	generation int
	budgetLeft bool
}

type ledgerCall struct {
	op      string
	id      int64
	step    time.Duration
	horizon time.Duration
	code    string
}

type testLedger struct {
	mu       sync.Mutex
	claims   []testClaim
	claimErr error
	calls    []ledgerCall
	err      error
	release  chan struct{}
	inflight atomic.Int32
}

func (l *testLedger) ClaimHandoffDue(context.Context, int, time.Duration) ([]testClaim, error) {
	claims := l.claims

	l.claims = nil

	return claims, l.claimErr
}

func (*testLedger) Check(claim *testClaim) Check {
	return Check{
		IrisRequestID:   claim.requestID,
		Elapsed:         claim.elapsed,
		Generation:      claim.generation,
		RetryBudgetLeft: claim.budgetLeft,
		Subject:         slog.Int64(testSubjectKey, claim.id),
	}
}

func (l *testLedger) record(call ledgerCall) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = append(l.calls, call)

	return l.err
}

func (l *testLedger) recorded() []ledgerCall {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.calls)
}

func (l *testLedger) Complete(_ context.Context, claim *testClaim) error {
	if l.release != nil {
		l.inflight.Add(1)
		defer l.inflight.Add(-1)

		<-l.release
	}

	return l.record(ledgerCall{op: opComplete, id: claim.id})
}

func (l *testLedger) Reschedule(_ context.Context, claim *testClaim, step, horizon time.Duration) error {
	return l.record(ledgerCall{op: opReschedule, id: claim.id, step: step, horizon: horizon})
}

func (l *testLedger) Reissue(_ context.Context, claim *testClaim, code, _ string) error {
	return l.record(ledgerCall{op: opReissue, id: claim.id, code: code})
}

func (l *testLedger) Exhaust(_ context.Context, claim *testClaim, code, _ string) error {
	return l.record(ledgerCall{op: opExhaust, id: claim.id, code: code})
}

func (l *testLedger) Quarantine(_ context.Context, claim *testClaim, code, _ string) error {
	return l.record(ledgerCall{op: opQuarantine, id: claim.id, code: code})
}

type testStatusClient struct {
	mu       sync.Mutex
	queried  []string
	snapshot func(requestID string) (*iris.ReplyStatusSnapshot, error)
}

func (c *testStatusClient) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	c.mu.Lock()

	c.queried = append(c.queried, requestID)
	c.mu.Unlock()

	return c.snapshot(requestID)
}

func (c *testStatusClient) queriedIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.queried)
}

func status(state string) func(string) (*iris.ReplyStatusSnapshot, error) {
	return func(requestID string) (*iris.ReplyStatusSnapshot, error) {
		return &iris.ReplyStatusSnapshot{RequestID: requestID, State: state}, nil
	}
}

func statusError(code int) func(string) (*iris.ReplyStatusSnapshot, error) {
	return func(string) (*iris.ReplyStatusSnapshot, error) {
		return nil, &iris.HTTPError{StatusCode: code, URL: "/reply-status/" + testRequestID}
	}
}

type logRecord struct {
	level slog.Level
	msg   string
	attrs map[string]string
}

type logRecorder struct {
	mu      sync.Mutex
	records []logRecord
}

func (*logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (h *logRecorder) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]string)

	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.String()

		return true
	})

	h.mu.Lock()
	defer h.mu.Unlock()

	h.records = append(h.records, logRecord{level: record.Level, msg: record.Message, attrs: attrs})

	return nil
}

func (h *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *logRecorder) WithGroup(string) slog.Handler { return h }

func (h *logRecorder) snapshot() []logRecord {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.records)
}

func testConfig() Config {
	return Config{
		Name:               testLedgerName,
		Horizon:            testHorizon,
		PollInterval:       time.Second,
		FenceMissMessage:   "test ledger handoff check lost its claim",
		CheckClosed:        errTestCheckClosed,
		CheckClosedMessage: "test ledger handoff check closed",
		NotRechecked:       errTestNotRechecked,
		QueryErrorAttrs: func(err error) []any {
			return []any{slog.Bool("ledger_attr", err != nil)}
		},
	}
}

func requireSingleLog(t *testing.T, logs *logRecorder, level slog.Level, msg string) logRecord {
	t.Helper()

	records := logs.snapshot()
	require.Len(t, records, 1)
	require.Equal(t, level, records[0].level)
	require.Equal(t, msg, records[0].msg)

	return records[0]
}

func newTestReconciler(t *testing.T, ledger *testLedger, client *testStatusClient, config Config, logs *logRecorder) *Reconciler[testClaim] {
	t.Helper()

	reconciler, err := New[testClaim](ledger, client, config, slog.New(logs))
	require.NoError(t, err)

	return reconciler
}

func TestReconcilerRoutesDecisionToLedger(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		status    func(string) (*iris.ReplyStatusSnapshot, error)
		claim     testClaim
		want      ledgerCall
		wantLevel slog.Level
		wantLog   string
	}{
		{name: "completed", status: status(stateCompleted), want: ledgerCall{op: opComplete}},
		{
			name: "failed with budget", status: status(stateFailed), claim: testClaim{budgetLeft: true},
			want:      ledgerCall{op: opReissue, code: contractsalarm.HandoffCodeFailed},
			wantLevel: slog.LevelWarn, wantLog: "test ledger handoff failed; requeued with the next request generation",
		},
		{
			name: "failed without budget", status: status(stateFailed),
			want:      ledgerCall{op: opExhaust, code: contractsalarm.HandoffCodeFailed},
			wantLevel: slog.LevelError, wantLog: "test ledger handoff failed; reissue budget exhausted",
		},
		{
			name: "queued before horizon", status: status(stateQueued), claim: testClaim{elapsed: 30 * time.Second},
			want: ledgerCall{op: opReschedule, step: time.Minute, horizon: testHorizon},
		},
		{
			name: "queued at horizon", status: status(stateQueued), claim: testClaim{elapsed: testHorizon},
			want:      ledgerCall{op: opQuarantine, code: contractsalarm.HandoffCodeUnconfirmed},
			wantLevel: slog.LevelError, wantLog: "test ledger handoff outcome unknown; quarantined",
		},
		{
			name: "status missing", status: statusError(http.StatusNotFound),
			want:      ledgerCall{op: opQuarantine, code: contractsalarm.HandoffCodeStatusMissing},
			wantLevel: slog.LevelError, wantLog: "test ledger handoff outcome unknown; quarantined",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.claim.id = 7
			tc.claim.requestID = testRequestID

			ledger := &testLedger{claims: []testClaim{tc.claim}}
			client := &testStatusClient{snapshot: tc.status}
			logs := &logRecorder{}

			claimed, err := newTestReconciler(t, ledger, client, testConfig(), logs).RunOnce(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, claimed)
			require.Equal(t, []string{testRequestID}, client.queriedIDs())

			tc.want.id = 7
			require.Equal(t, []ledgerCall{tc.want}, ledger.recorded())

			if tc.wantLog == "" {
				require.Empty(t, logs.snapshot())

				return
			}

			record := requireSingleLog(t, logs, tc.wantLevel, tc.wantLog)
			require.Equal(t, "7", record.attrs[testSubjectKey])
		})
	}
}

func TestReconcilerLogsDeferredQueryWithoutRequestID(t *testing.T) {
	t.Parallel()

	ledger := &testLedger{claims: []testClaim{{id: 7, requestID: testRequestID, elapsed: 5 * time.Second}}}
	logs := &logRecorder{}

	_, err := newTestReconciler(t, ledger, &testStatusClient{snapshot: statusError(http.StatusServiceUnavailable)}, testConfig(), logs).RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, []ledgerCall{{op: opReschedule, id: 7, step: 15 * time.Second, horizon: testHorizon}}, ledger.recorded())

	record := requireSingleLog(t, logs, slog.LevelWarn, "test ledger handoff status query deferred")
	require.Equal(t, map[string]string{
		testSubjectKey: "7",
		"ledger_attr":  "true",
		"error":        "iris reply status query failed with http 503",
	}, record.attrs)

	for _, value := range record.attrs {
		require.NotContains(t, value, testRequestID)
	}
}

func TestReconcilerSkipsLedgerWhenStatusQueryIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ledger := &testLedger{claims: []testClaim{{id: 7, requestID: testRequestID}}}
	client := &testStatusClient{snapshot: func(string) (*iris.ReplyStatusSnapshot, error) {
		cancel()

		return nil, context.Canceled
	}}

	claimed, err := newTestReconciler(t, ledger, client, testConfig(), &logRecorder{}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, claimed)
	require.Empty(t, ledger.recorded())
}

func TestReconcilerReturnsClaimFailure(t *testing.T) {
	t.Parallel()

	ledger := &testLedger{claimErr: errors.New("connection refused")}
	client := &testStatusClient{snapshot: status(stateCompleted)}

	claimed, err := newTestReconciler(t, ledger, client, testConfig(), &logRecorder{}).RunOnce(t.Context())
	require.ErrorIs(t, err, ledger.claimErr)
	require.ErrorContains(t, err, "claim test ledger handoff checks")
	require.Zero(t, claimed)
	require.Empty(t, client.queriedIDs())
}

type gatedRun struct {
	ledger *testLedger
	logs   *logRecorder
	done   chan int
}

func startGatedRun(t *testing.T) gatedRun {
	t.Helper()

	claims := make([]testClaim, gatedClaimCount)
	for i := range claims {
		claims[i] = testClaim{id: int64(i + 1), requestID: fmt.Sprintf("iris-%d", i+1)}
	}

	run := gatedRun{
		ledger: &testLedger{claims: claims, release: make(chan struct{})},
		logs:   &logRecorder{},
		done:   make(chan int),
	}
	client := &testStatusClient{snapshot: status(stateCompleted)}
	reconciler := newTestReconciler(t, run.ledger, client, testConfig(), run.logs)

	go func() {
		claimed, runErr := reconciler.RunOnce(t.Context())
		assert.NoError(t, runErr)

		run.done <- claimed
	}()

	synctest.Wait()

	require.Len(t, client.queriedIDs(), gatedClaimCount, "every status query runs before settlement slots free up")
	require.EqualValues(t, settleConcurrency, run.ledger.inflight.Load())

	return run
}

func TestReconcilerBoundsConcurrentSettlements(t *testing.T) {
	t.Run("queued settlements apply once slots free within budget", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			run := startGatedRun(t)

			close(run.ledger.release)

			require.Equal(t, gatedClaimCount, <-run.done)
			require.Len(t, run.ledger.recorded(), gatedClaimCount)
			require.Empty(t, run.logs.snapshot())
		})
	})

	t.Run("settlements waiting past the budget are not applied", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			run := startGatedRun(t)

			time.Sleep(stateTimeout + time.Millisecond)
			close(run.ledger.release)

			require.Equal(t, gatedClaimCount, <-run.done)

			calls := run.ledger.recorded()
			require.Len(t, calls, settleConcurrency, "only settlements already holding a slot apply")

			applied := make([]string, 0, len(calls))
			for _, call := range calls {
				applied = append(applied, fmt.Sprint(call.id))
			}

			records := run.logs.snapshot()
			require.Len(t, records, gatedClaimCount-settleConcurrency)

			for _, record := range records {
				require.NotContains(t, applied, record.attrs[testSubjectKey])
				require.Equal(t, slog.LevelWarn, record.level, "an over-budget settlement is left to the next check")
				require.Equal(t, "test ledger handoff settlement exceeded its budget; the next check decides again", record.msg)
			}
		})
	})
}

func settleWithError(t *testing.T, config Config, err error, level slog.Level, msg string) (logRecord, int32) {
	t.Helper()

	var observed atomic.Int32

	config.ObserveFenceMiss = func(*sendfence.MissError) { observed.Add(1) }

	ledger := &testLedger{claims: []testClaim{{id: 7, requestID: testRequestID}}, err: err}
	logs := &logRecorder{}

	claimed, runErr := newTestReconciler(t, ledger, &testStatusClient{snapshot: status(stateCompleted)}, config, logs).RunOnce(t.Context())
	require.NoError(t, runErr)
	require.Equal(t, 1, claimed)

	return requireSingleLog(t, logs, level, msg), observed.Load()
}

func TestReconcilerClassifiesSettlementFailures(t *testing.T) {
	t.Parallel()

	miss := &sendfence.MissError{Action: "handoff_complete", Expected: 1, Rows: []sendfence.RowMiss{{ID: 1, Reason: sendfence.ReasonVersionChanged}}}
	closed := fmt.Errorf("complete handoff check: %w", fmt.Errorf("%w: %w", errTestCheckClosed, miss))

	withoutClosed := testConfig()

	withoutClosed.CheckClosed, withoutClosed.CheckClosedMessage = nil, ""

	const (
		missLog   = "test ledger handoff check lost its claim"
		failedLog = "test ledger handoff check failed"
	)

	cases := []struct {
		name         string
		config       Config
		err          error
		wantLevel    slog.Level
		wantLog      string
		wantObserved int32
	}{
		{name: "check closed", config: testConfig(), err: closed, wantLevel: slog.LevelError, wantLog: "test ledger handoff check closed"},
		{name: "claim lost", config: testConfig(), err: fmt.Errorf("complete handoff check: %w", miss), wantLevel: slog.LevelWarn, wantLog: missLog, wantObserved: 1},
		{name: "ledger without closed checks", config: withoutClosed, err: closed, wantLevel: slog.LevelWarn, wantLog: missLog, wantObserved: 1},
		{
			name: "settlement budget exceeded", config: testConfig(), err: fmt.Errorf("complete handoff check: %w", context.DeadlineExceeded),
			wantLevel: slog.LevelWarn, wantLog: "test ledger handoff settlement exceeded its budget; the next check decides again",
		},
		{
			name: "follow-up after the check closed hits a deadline", config: testConfig(),
			err:       fmt.Errorf("release keys: %w: %w", errTestNotRechecked, context.DeadlineExceeded),
			wantLevel: slog.LevelError, wantLog: failedLog,
		},
		{name: "store failure", config: testConfig(), err: errors.New("connection refused"), wantLevel: slog.LevelError, wantLog: failedLog},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record, observed := settleWithError(t, tc.config, tc.err, tc.wantLevel, tc.wantLog)
			require.Equal(t, opComplete, record.attrs["observed_action"])
			require.True(t, strings.HasPrefix(record.attrs["error"], "apply test ledger handoff: "), record.attrs["error"])
			require.Equal(t, tc.wantObserved, observed)
		})
	}
}
