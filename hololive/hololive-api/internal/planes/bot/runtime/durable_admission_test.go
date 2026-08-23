package botruntime

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-dbtest"
	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/iris-client-go/v2/webhook"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type durableMessageProcessorFunc func(context.Context, *webhook.Message) error

func (f durableMessageProcessorFunc) ProcessMessage(ctx context.Context, msg *webhook.Message) error {
	return f(ctx, msg)
}

type safeDurableTestError struct {
	raw string
}

func (e safeDurableTestError) Error() string          { return e.raw }
func (safeDurableTestError) SafeMessageToken() string { return "anon:test-token" }
func (safeDurableTestError) SafeReason() string       { return "database_operation_failed" }

func TestReplyOutboxSettlementPreservesDispatchCertainty(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		accepted bool
		err      error
		attempts int32
		want     string
	}{
		{name: "handoff", want: durability.ReplyOutboxHandoffCompleted},
		{name: "pre-dispatch", attempts: 1, err: iris.ErrRetryable, want: durability.ReplyOutboxRetryablePreDispatch},
		{name: "unknown local error", err: errors.New("bug"), want: durability.ReplyOutboxDead},
		{name: "invalid stored payload", err: transport.ErrStoredReplyInvalid, want: durability.ReplyOutboxManualReview},
		{name: "reissue ladder exhausted conflict", err: &iris.HTTPError{StatusCode: http.StatusConflict, URL: "https://iris/reply", Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`}, want: durability.ReplyOutboxPermanentConflict},
		{name: "retry exhausted", attempts: durableMaxAttempts, err: iris.ErrRetryable, want: durability.ReplyOutboxDead},
		{name: "unknown", err: transport.ErrReplyOutcomeUnknown, want: durability.ReplyOutboxOutcomeUnknown},
		{name: "unknown exhausted", attempts: durableMaxAttempts, err: transport.ErrReplyOutcomeUnknown, want: durability.ReplyOutboxManualReview},
		{name: "accepted observation failure", accepted: true, err: errors.New("status unavailable"), want: durability.ReplyOutboxOutcomeUnknown},
		{name: "accepted observation exhausted", accepted: true, attempts: durableMaxAttempts, err: errors.New("status unavailable"), want: durability.ReplyOutboxManualReview},
		{name: "explicit failure after acceptance", accepted: true, err: transport.ErrReplyStatusFailed, want: durability.ReplyOutboxDead},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := replyOutboxSettlementStatus(tc.accepted, tc.attempts, tc.err); got != tc.want {
				t.Fatalf("replyOutboxSettlementStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReplyOutboxOutcomeUnknownRetryDelayIncreasesByAttempt(t *testing.T) {
	t.Parallel()
	previous := time.Duration(0)
	for attempt := int32(1); attempt < durableMaxAttempts; attempt++ {
		delay := replyOutboxRetryAfter(durability.ReplyOutboxOutcomeUnknown, attempt)
		if delay <= previous {
			t.Fatalf("attempt %d retry delay = %s, previous = %s", attempt, delay, previous)
		}
		previous = delay
	}
	if got := replyOutboxRetryAfter(durability.ReplyOutboxManualReview, durableMaxAttempts); got != 0 {
		t.Fatalf("manual review retry delay = %s, want 0", got)
	}
}

func TestAcceptedLeaseReclaimEmitsActionableCounterAndLog(t *testing.T) {
	var logs bytes.Buffer
	r := &durableRuntime{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	before := testutil.ToFloat64(replyOutboxAcceptedReclaimedTotal)
	r.observeReplyOutboxReclaim(durability.ReplyOutboxReclaim{AcceptedManualReview: 2})
	after := testutil.ToFloat64(replyOutboxAcceptedReclaimedTotal)
	if after-before != 2 {
		t.Fatalf("accepted reclaim counter delta = %v", after-before)
	}
	if !strings.Contains(logs.String(), "inspect bot_reply_outbox status=manual_review before replay decision") {
		t.Fatalf("actionable reclaim log missing: %s", logs.String())
	}
}

func TestManualReviewBacklogMetricsExposeCountAndOldestAge(t *testing.T) {
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), "TRUNCATE bot_reply_outbox_resolution_audit, bot_reply_outbox_replay_audit, bot_reply_outbox RESTART IDENTITY")
	if err != nil {
		t.Fatal(err)
	}
	repo := durability.NewReplyOutboxRepository(pool)
	_, err = repo.Insert(t.Context(), &durability.ReplyOutboxEntry{
		MessageID: "message:metric", Phase: transport.ReplyPhase, RoomID: "room-1",
		Payload: []byte(`{"kind":"text","message":"answer"}`), ClientRequestID: "hololive:v1:message:metric:reply:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), "UPDATE bot_reply_outbox SET status = 'manual_review', updated_at = clock_timestamp() - interval '10 seconds'")
	if err != nil {
		t.Fatal(err)
	}
	r := &durableRuntime{outbox: repo}
	r.observeReplyOutboxManualReview(t.Context())
	if got := testutil.ToFloat64(replyOutboxManualReviewBacklog); got != 1 {
		t.Fatalf("manual review backlog = %v", got)
	}
	if got := testutil.ToFloat64(replyOutboxManualReviewOldestAgeSeconds); got < 9 {
		t.Fatalf("manual review oldest age = %v", got)
	}
}

func TestHeartbeatClaimRetriesTransientFailures(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	r := &durableRuntime{heartbeatRetryDelay: time.Millisecond}
	wantLeaseUntil := time.Now().Add(time.Minute)
	r.inboxHeartbeat = func(context.Context, string, string, time.Duration) (time.Time, bool, error) {
		if calls.Add(1) < 3 {
			return time.Time{}, false, errors.New("temporary postgres error")
		}
		return wantLeaseUntil, true, nil
	}
	r.commandHeartbeat = func(context.Context, string, string) (bool, error) { return true, nil }
	leaseUntil, owned, confirmed := r.heartbeatClaim(t.Context(), "message:1", "token")
	if leaseUntil != wantLeaseUntil || !owned || !confirmed || calls.Load() != 3 {
		t.Fatalf("heartbeat = (%s,%v,%v), calls=%d", leaseUntil, owned, confirmed, calls.Load())
	}
}

func TestHeartbeatClaimConfirmsOwnershipLossWithoutDatabaseError(t *testing.T) {
	t.Parallel()
	r := &durableRuntime{
		inboxHeartbeat: func(context.Context, string, string, time.Duration) (time.Time, bool, error) {
			return time.Time{}, false, nil
		},
		commandHeartbeat: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	_, owned, confirmed := r.heartbeatClaim(t.Context(), "message:1", "token")
	if owned || !confirmed {
		t.Fatalf("heartbeat = (%v,%v), want (false,true)", owned, confirmed)
	}
}

func TestRunClaimHeartbeatCancelsCommandAfterConfirmedOwnershipLoss(t *testing.T) {
	commandCtx, cancelCommand := context.WithCancel(t.Context())
	var logs bytes.Buffer
	before := testutil.ToFloat64(durableOwnershipCancellationTotal.WithLabelValues(ownershipCancellationLost))
	r := &durableRuntime{
		logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		heartbeatEvery: time.Millisecond,
		heartbeatWait:  func(context.Context, time.Duration) bool { return true },
		inboxHeartbeat: func(context.Context, string, string, time.Duration) (time.Time, bool, error) {
			return time.Time{}, false, nil
		},
		commandHeartbeat: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	r.runClaimHeartbeat(t.Context(), "sensitive-message-id", "token", time.Now().Add(time.Minute), cancelCommand)
	if commandCtx.Err() == nil {
		t.Fatal("command context was not canceled")
	}
	after := testutil.ToFloat64(durableOwnershipCancellationTotal.WithLabelValues(ownershipCancellationLost))
	if after-before != 1 {
		t.Fatalf("ownership_lost counter delta = %v", after-before)
	}
	if strings.Contains(logs.String(), "sensitive-message-id") || !strings.Contains(logs.String(), `"message_token":"anon:`) {
		t.Fatalf("ownership log did not pseudonymize the message identity: %s", logs.String())
	}
}

func TestRunClaimHeartbeatCancelsBeforeLeaseExpiryWhenRenewalStaysUnconfirmed(t *testing.T) {
	commandCtx, cancelCommand := context.WithCancel(t.Context())
	defer cancelCommand()
	base := time.Now()
	current := base
	var calls atomic.Int32
	before := testutil.ToFloat64(durableOwnershipCancellationTotal.WithLabelValues(ownershipCancellationUnconfirmed))
	r := &durableRuntime{
		heartbeatEvery:         2 * time.Millisecond,
		heartbeatRetryDelay:    time.Millisecond,
		heartbeatAttemptBudget: time.Millisecond,
		claimLease:             40 * time.Millisecond,
		ownershipSafetyMargin:  10 * time.Millisecond,
		now:                    func() time.Time { return current },
		heartbeatWait: func(ctx context.Context, delay time.Duration) bool {
			if ctx.Err() != nil {
				return false
			}
			current = current.Add(delay)
			return true
		},
		inboxHeartbeat: func(context.Context, string, string, time.Duration) (time.Time, bool, error) {
			calls.Add(1)
			return time.Time{}, false, errors.New("postgres unavailable")
		},
		commandHeartbeat: func(context.Context, string, string) (bool, error) {
			return false, errors.New("postgres unavailable")
		},
	}
	leaseUntil := base.Add(r.claimLease)
	r.runClaimHeartbeat(t.Context(), "message:1", "token", leaseUntil, cancelCommand)
	if commandCtx.Err() == nil {
		t.Fatal("command was not canceled after persistent heartbeat failures")
	}
	after := testutil.ToFloat64(durableOwnershipCancellationTotal.WithLabelValues(ownershipCancellationUnconfirmed))
	if after-before != 1 {
		t.Fatalf("ownership_unconfirmed counter delta = %v", after-before)
	}
	wantCancelAt := leaseUntil.Add(-r.ownershipSafetyMargin)
	if !current.Equal(wantCancelAt) {
		t.Fatalf("command canceled at %s, want safety deadline %s", current, wantCancelAt)
	}
	if calls.Load() < 4 {
		t.Fatalf("heartbeat calls = %d, want repeated failures before cancellation", calls.Load())
	}
}

func TestRunClaimHeartbeatAllowsTransientFailureThenRecovery(t *testing.T) {
	heartbeatCtx, stopHeartbeat := context.WithCancel(t.Context())
	commandCtx, cancelCommand := context.WithCancel(t.Context())
	defer cancelCommand()
	var calls atomic.Int32
	base := time.Now()
	current := base
	const claimLease = 100 * time.Millisecond
	r := &durableRuntime{
		heartbeatEvery:         2 * time.Millisecond,
		heartbeatRetryDelay:    time.Millisecond,
		heartbeatAttemptBudget: time.Millisecond,
		claimLease:             claimLease,
		ownershipSafetyMargin:  20 * time.Millisecond,
		now:                    func() time.Time { return current },
		heartbeatWait: func(ctx context.Context, delay time.Duration) bool {
			if ctx.Err() != nil {
				return false
			}
			current = current.Add(delay)
			return true
		},
		inboxHeartbeat: func(context.Context, string, string, time.Duration) (time.Time, bool, error) {
			if calls.Add(1) <= 3 {
				return time.Time{}, false, errors.New("temporary postgres error")
			}
			stopHeartbeat()
			return current.Add(claimLease), true, nil
		},
		commandHeartbeat: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	r.runClaimHeartbeat(heartbeatCtx, "message:1", "token", base.Add(r.claimLease), cancelCommand)
	if calls.Load() != 4 {
		t.Fatalf("heartbeat calls = %d, want three failures then one recovery", calls.Load())
	}
	select {
	case <-commandCtx.Done():
		t.Fatal("transient heartbeat recovery canceled the command")
	default:
	}
}

func TestRunClaimHeartbeatShutdownDoesNotCancelCommand(t *testing.T) {
	heartbeatCtx, stopHeartbeat := context.WithCancel(t.Context())
	commandCtx, cancelCommand := context.WithCancel(t.Context())
	defer cancelCommand()
	r := &durableRuntime{heartbeatEvery: time.Hour}
	stopHeartbeat()
	r.runClaimHeartbeat(heartbeatCtx, "message:1", "token", time.Now().Add(durableClaimLease), cancelCommand)
	select {
	case <-commandCtx.Done():
		t.Fatal("heartbeat shutdown canceled the command")
	default:
	}
}

func TestDurableCommandUsesConfiguredHandlerDeadline(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `TRUNCATE bot_webhook_heads, bot_reply_outbox, bot_command_executions, bot_webhook_inbox RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}

	msg := webhook.Message{Msg: "!help", Room: "room", JSON: &webhook.MessageJSON{
		MessageID: "deadline-message", ChatID: "room-1",
	}}
	payload, err := jsonv2.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	inbox := durability.NewInboxRepository(pool)
	_, err = inbox.Admit(ctx, durability.InboxMessage{
		MessageID: "message:deadline-message", RoomID: "room-1", OrderingKey: "room:room-1", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := inbox.Claim(ctx, "deadline-token", durableClaimLease)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil {
		t.Fatal("deadline inbox claim is nil")
	}

	const handlerTimeout = 25 * time.Millisecond
	deadlineSeen := make(chan time.Duration, 1)
	r := &durableRuntime{
		inbox:    inbox,
		commands: durability.NewCommandExecutionRepository(pool),
		bot: durableMessageProcessorFunc(func(commandCtx context.Context, _ *webhook.Message) error {
			deadline, ok := commandCtx.Deadline()
			if !ok {
				return errors.New("command context has no deadline")
			}
			deadlineSeen <- time.Until(deadline)
			<-commandCtx.Done()
			return commandCtx.Err()
		}),
		handlerTimeout:    handlerTimeout,
		heartbeatEvery:    time.Hour,
		settlementTimeout: durableSettlementTimeout,
		inboxHeartbeat:    inbox.Heartbeat,
		commandHeartbeat:  durability.NewCommandExecutionRepository(pool).Heartbeat,
	}
	if err := r.processInboxClaim(ctx, claim, "deadline-token"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("process deadline inbox claim error = %v, want deadline exceeded", err)
	}

	seen := <-deadlineSeen
	if seen <= 0 || seen > handlerTimeout {
		t.Fatalf("command deadline remaining = %s, want (0,%s]", seen, handlerTimeout)
	}
	var commandStatus, inboxStatus string
	var inboxPayload []byte
	err = pool.QueryRow(ctx, `SELECT status FROM bot_command_executions WHERE message_id = $1`, claim.MessageID).Scan(&commandStatus)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `SELECT status, payload FROM bot_webhook_inbox WHERE message_id = $1`, claim.MessageID).
		Scan(&inboxStatus, &inboxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if commandStatus != durability.CommandExecutionOutcomeUnknown {
		t.Fatalf("command status = %q, want %q", commandStatus, durability.CommandExecutionOutcomeUnknown)
	}
	if inboxStatus != "succeeded" || string(inboxPayload) != "{}" {
		t.Fatalf("inbox terminal state = (%q,%s), want (succeeded,{})", inboxStatus, inboxPayload)
	}
}

func TestCommandCompletionDistinguishesDefiniteAndUncertainFailure(t *testing.T) {
	if got := commandExecutionStatus(errors.New("validation failed")); got != durability.CommandExecutionFailed {
		t.Fatalf("definite failure status = %q", got)
	}
	if got := commandExecutionStatus(context.DeadlineExceeded); got != durability.CommandExecutionOutcomeUnknown {
		t.Fatalf("deadline failure status = %q", got)
	}
	if got := commandExecutionStatus(fmt.Errorf("stage reply: %w", orchestration.ErrCommandOutcomeUnknown)); got != durability.CommandExecutionOutcomeUnknown {
		t.Fatalf("typed uncertain failure status = %q", got)
	}
}

func TestDuplicateActiveCommandDefersInboxUntilOutcomeIsKnown(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `TRUNCATE bot_webhook_heads, bot_reply_outbox, bot_command_executions, bot_webhook_inbox RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
	inbox := durability.NewInboxRepository(pool)
	commands := durability.NewCommandExecutionRepository(pool)
	_, err = inbox.Admit(ctx, durability.InboxMessage{
		MessageID: "message:crash-window", RoomID: "room-1", OrderingKey: "room:room-1",
		Payload: []byte(`{"JSON":{"message_id":"crash-window"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := inbox.Claim(ctx, "old-token", time.Millisecond)
	if err != nil || first == nil {
		t.Fatalf("first inbox claim = %v, err = %v", first, err)
	}
	claimed, err := commands.Claim(ctx, first.MessageID, "webhook", "old-token")
	if err != nil || !claimed {
		t.Fatalf("command claim = %v, err = %v", claimed, err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err = inbox.ReclaimExpired(ctx, durableMaxAttempts, durableBatchSize); err != nil {
		t.Fatal(err)
	}
	second, err := inbox.Claim(ctx, "new-token", durableClaimLease)
	if err != nil || second == nil {
		t.Fatalf("reclaimed inbox claim = %v, err = %v", second, err)
	}

	r := &durableRuntime{
		inbox:            inbox,
		commands:         commands,
		maintenanceEvery: durableMaintenanceEvery,
		inboxMaxAttempts: durableMaxAttempts,
	}
	assertActiveCommandDefersInbox(t, ctx, pool, r, second)
	expireCommandClaim(t, ctx, pool, commands, second.MessageID)
	assertTerminalDuplicateCompletes(t, ctx, pool, r, inbox, second.MessageID)
}

func assertActiveCommandDefersInbox(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	r *durableRuntime,
	claim *durability.InboxClaim,
) {
	t.Helper()

	processed, err := r.claimCommand(ctx, claim, "new-token")
	if err != nil || processed {
		t.Fatalf("duplicate active command = %v, err = %v", processed, err)
	}
	var inboxStatus, commandStatus string
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT status, payload FROM bot_webhook_inbox WHERE message_id = $1`, claim.MessageID).
		Scan(&inboxStatus, &payload); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM bot_command_executions WHERE message_id = $1`, claim.MessageID).
		Scan(&commandStatus); err != nil {
		t.Fatal(err)
	}
	if inboxStatus != "retry" || len(payload) == 0 || commandStatus != "claimed" {
		t.Fatalf("crash recovery state = inbox(%q,%s) command(%q), want retry with payload and claimed command",
			inboxStatus, payload, commandStatus)
	}
}

func expireCommandClaim(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	commands *durability.CommandExecutionRepository,
	messageID string,
) {
	t.Helper()

	_, err := pool.Exec(ctx, `UPDATE bot_command_executions
		SET claimed_at = clock_timestamp() - interval '6 minutes'
		WHERE message_id = $1`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE bot_webhook_inbox
		SET available_at = clock_timestamp()
		WHERE message_id = $1`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := commands.ExpireStaleClaims(ctx, commandStaleAfter, durableBatchSize)
	if err != nil || expired != 1 {
		t.Fatalf("expired commands = %d, err = %v", expired, err)
	}
}

func assertTerminalDuplicateCompletes(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	r *durableRuntime,
	inbox *durability.InboxRepository,
	messageID string,
) {
	t.Helper()

	finalClaim, err := inbox.Claim(ctx, "final-token", durableClaimLease)
	if err != nil || finalClaim == nil {
		t.Fatalf("final inbox claim = %v, err = %v", finalClaim, err)
	}
	processed, err := r.claimCommand(ctx, finalClaim, "final-token")
	if err != nil || processed {
		t.Fatalf("terminal duplicate command = %v, err = %v", processed, err)
	}
	var inboxStatus string
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT status, payload FROM bot_webhook_inbox WHERE message_id = $1`, messageID).
		Scan(&inboxStatus, &payload); err != nil {
		t.Fatal(err)
	}
	if inboxStatus != "succeeded" || string(payload) != "{}" {
		t.Fatalf("terminal duplicate inbox = (%q,%s), want succeeded and scrubbed", inboxStatus, payload)
	}
}

func TestExpiredCommandObservationEmitsMetricAndActionableLog(t *testing.T) {
	var logs bytes.Buffer
	r := &durableRuntime{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	before := testutil.ToFloat64(durableCommandOutcomeUnknownTotal)
	r.observeExpiredCommands(2)
	after := testutil.ToFloat64(durableCommandOutcomeUnknownTotal)
	if after-before != 2 {
		t.Fatalf("command outcome unknown counter delta = %v, want 2", after-before)
	}
	if !strings.Contains(logs.String(), "inspect bot_command_executions status=outcome_unknown") {
		t.Fatalf("actionable expired command log missing: %s", logs.String())
	}
}

func TestDurableDefiniteFailureWritesFailed(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `TRUNCATE bot_webhook_heads, bot_reply_outbox, bot_command_executions, bot_webhook_inbox RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
	msg := webhook.Message{Msg: "!bad", Room: "room", JSON: &webhook.MessageJSON{
		MessageID: "failed-message", ChatID: "room-1",
	}}
	payload, err := jsonv2.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	inbox := durability.NewInboxRepository(pool)
	_, err = inbox.Admit(ctx, durability.InboxMessage{
		MessageID: "message:failed-message", RoomID: "room-1", OrderingKey: "room:room-1", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := inbox.Claim(ctx, "failed-token", durableClaimLease)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil {
		t.Fatal("failed inbox claim is nil")
	}
	commands := durability.NewCommandExecutionRepository(pool)
	r := &durableRuntime{
		inbox:    inbox,
		commands: commands,
		bot: durableMessageProcessorFunc(func(context.Context, *webhook.Message) error {
			return errors.New("validation failed")
		}),
		handlerTimeout:    time.Second,
		heartbeatEvery:    time.Hour,
		settlementTimeout: durableSettlementTimeout,
		inboxHeartbeat:    inbox.Heartbeat,
		commandHeartbeat:  commands.Heartbeat,
	}
	if err := r.processInboxClaim(ctx, claim, "failed-token"); err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("process failed inbox claim error = %v, want validation failure", err)
	}
	var status, summary string
	err = pool.QueryRow(ctx, `SELECT status, result_summary FROM bot_command_executions WHERE message_id = $1`, claim.MessageID).
		Scan(&status, &summary)
	if err != nil {
		t.Fatal(err)
	}
	if status != durability.CommandExecutionFailed || summary != durability.CommandExecutionFailed {
		t.Fatalf("command result = (%q,%q), want failed safe summary", status, summary)
	}
}

func TestDurableIdleBackoffIsBounded(t *testing.T) {
	delay := durablePollEvery
	for range 20 {
		delay = nextDurableIdleDelay(delay)
	}
	if delay != durablePollMax {
		t.Fatalf("idle delay = %s, want bounded max %s", delay, durablePollMax)
	}
}

func TestDurableTerminalRetentionExceedsIrisAdmissionSafetyHorizon(t *testing.T) {
	const irisAdmissionRetention = 7 * 24 * time.Hour
	if durableTerminalRetention <= irisAdmissionRetention {
		t.Fatalf("terminal retention = %s, must exceed Iris admission retention %s", durableTerminalRetention, irisAdmissionRetention)
	}
}

func TestErrorTextDropsLowTrustHTTPBody(t *testing.T) {
	err := &iris.HTTPError{StatusCode: 503, Body: "raw upstream response"}
	if got := errorText(err); got != "iris http status=503" {
		t.Fatalf("errorText() = %q", got)
	}
	var logs bytes.Buffer
	r := &durableRuntime{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	r.logError("dispatch failed", err)
	if strings.Contains(logs.String(), err.Body) || !strings.Contains(logs.String(), `"http_status":503`) {
		t.Fatalf("HTTP error log was not reduced to safe metadata: %s", logs.String())
	}
}

func TestDurableErrorLogDropsLowTrustCauseText(t *testing.T) {
	const sentinel = "raw-private-message-id"
	var logs bytes.Buffer
	r := &durableRuntime{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	r.logError("heartbeat durable command claim", errors.New("database rejected "+sentinel))
	if strings.Contains(logs.String(), sentinel) || strings.Contains(logs.String(), `"error"`) {
		t.Fatalf("durable error log exposed low-trust cause: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"reason":"operation_failed"`) {
		t.Fatalf("durable error log omitted bounded reason: %s", logs.String())
	}
}

func TestDurableErrorLogPreservesSafeMessageMetadata(t *testing.T) {
	const sentinel = "raw-private-message-id"
	var logs bytes.Buffer
	r := &durableRuntime{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	r.logError("heartbeat durable command claim", safeDurableTestError{raw: sentinel})
	if strings.Contains(logs.String(), sentinel) {
		t.Fatalf("durable error log exposed wrapped cause: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"message_token":"anon:test-token"`) ||
		!strings.Contains(logs.String(), `"reason":"database_operation_failed"`) {
		t.Fatalf("durable error log omitted safe metadata: %s", logs.String())
	}
}

func TestDurableRepositoryFailuresExposeOnlySafeMessageMetadata(t *testing.T) {
	const (
		rawID     = "message:raw-private-message-id"
		causeText = "database rejected"
	)
	t.Run("command claim", func(t *testing.T) {
		testCommandClaimRepositoryPrivacy(t, rawID, causeText)
	})
	t.Run("command heartbeat", func(t *testing.T) {
		testCommandHeartbeatRepositoryPrivacy(t, rawID, causeText)
	})
	t.Run("inbox release", func(t *testing.T) {
		testInboxReleaseRepositoryPrivacy(t, rawID, causeText)
	})
}

func assertDurableRepositoryFailureSafe(t *testing.T, err error, rawID, causeText string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected repository failure")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || !strings.Contains(pgErr.Error(), rawID) {
		t.Fatalf("database cause chain was not preserved: %v", err)
	}
	if strings.Contains(err.Error(), rawID) || strings.Contains(err.Error(), causeText) ||
		!strings.Contains(err.Error(), "message_token=anon:") ||
		!strings.Contains(err.Error(), "reason=database_operation_failed") {
		t.Fatalf("repository error crossed the safe boundary: %q", err.Error())
	}
	var logs bytes.Buffer
	(&durableRuntime{logger: slog.New(slog.NewJSONHandler(&logs, nil))}).logError("durable repository operation", err)
	if strings.Contains(logs.String(), rawID) || strings.Contains(logs.String(), causeText) ||
		!strings.Contains(logs.String(), `"message_token":"anon:`) ||
		!strings.Contains(logs.String(), `"reason":"database_operation_failed"`) {
		t.Fatalf("runtime log crossed the safe boundary: %s", logs.String())
	}
}

func testCommandClaimRepositoryPrivacy(t *testing.T, rawID, causeText string) {
	t.Helper()
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `
		CREATE FUNCTION fail_command_claim_for_privacy_test() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'database rejected %', NEW.message_id; END
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_command_claim_for_privacy_test
		BEFORE INSERT ON bot_command_executions
		FOR EACH ROW EXECUTE FUNCTION fail_command_claim_for_privacy_test()`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = durability.NewCommandExecutionRepository(pool).Claim(t.Context(), rawID, "webhook", "claim-token")
	assertDurableRepositoryFailureSafe(t, err, rawID, causeText)
}

func testCommandHeartbeatRepositoryPrivacy(t *testing.T, rawID, causeText string) {
	t.Helper()
	pool := dbtest.NewPool(t)
	repo := durability.NewCommandExecutionRepository(pool)
	claimed, err := repo.Claim(t.Context(), rawID, "webhook", "claim-token")
	if err != nil || !claimed {
		t.Fatalf("claim = %v, err = %v", claimed, err)
	}
	_, err = pool.Exec(t.Context(), `
		CREATE FUNCTION fail_command_heartbeat_for_privacy_test() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'database rejected %', NEW.message_id; END
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_command_heartbeat_for_privacy_test
		BEFORE UPDATE ON bot_command_executions
		FOR EACH ROW EXECUTE FUNCTION fail_command_heartbeat_for_privacy_test()`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Heartbeat(t.Context(), rawID, "claim-token")
	assertDurableRepositoryFailureSafe(t, err, rawID, causeText)
}

func testInboxReleaseRepositoryPrivacy(t *testing.T, rawID, causeText string) {
	t.Helper()
	pool := dbtest.NewPool(t)
	repo := durability.NewInboxRepository(pool)
	_, err := repo.Admit(t.Context(), durability.InboxMessage{
		MessageID: rawID, RoomID: "room-1", OrderingKey: "room:room-1", Payload: []byte(`{"body":"x"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := repo.Claim(t.Context(), "claim-token", durableClaimLease)
	if err != nil || claim == nil {
		t.Fatalf("claim = %v, err = %v", claim, err)
	}
	_, err = pool.Exec(t.Context(),
		"UPDATE bot_webhook_inbox SET last_error = 'processing_failed' WHERE message_id = $1", rawID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), `
		CREATE FUNCTION fail_inbox_release_for_privacy_test() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'database rejected %', NEW.message_id; END
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_inbox_release_for_privacy_test
		BEFORE UPDATE ON bot_webhook_inbox
		FOR EACH ROW EXECUTE FUNCTION fail_inbox_release_for_privacy_test()`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Release(t.Context(), rawID, "claim-token", durableMaxAttempts, durableRetryAfter, causeText+" "+rawID)
	assertDurableRepositoryFailureSafe(t, err, rawID, causeText)

	var lastError string
	if scanErr := pool.QueryRow(t.Context(),
		"SELECT last_error FROM bot_webhook_inbox WHERE message_id = $1", rawID).Scan(&lastError); scanErr != nil {
		t.Fatal(scanErr)
	}
	if lastError != durability.InboxFailureProcessingFailed || strings.Contains(lastError, rawID) || strings.Contains(lastError, causeText) {
		t.Fatalf("last_error = %q, want bounded reason", lastError)
	}
}

func TestReleaseInboxStoresBoundedReasonWithoutCauseText(t *testing.T) {
	const sentinel = "raw-private-message-id"
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `TRUNCATE bot_webhook_heads, bot_reply_outbox, bot_command_executions, bot_webhook_inbox RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
	inbox := durability.NewInboxRepository(pool)
	_, err = inbox.Admit(ctx, durability.InboxMessage{
		MessageID: "message:" + sentinel, RoomID: "room-1", OrderingKey: "room:room-1", Payload: []byte(`{"JSON":{"message_id":"safe"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := inbox.Claim(ctx, "release-token", durableClaimLease)
	if err != nil || claim == nil {
		t.Fatalf("claim = %v, err = %v", claim, err)
	}
	r := &durableRuntime{
		inbox:            inbox,
		inboxMaxAttempts: durableMaxAttempts,
		inboxRetryAfter:  durableRetryAfter,
	}
	r.releaseInbox(ctx, claim, "release-token", errors.New("claim failed for "+sentinel))

	var lastError string
	if err = pool.QueryRow(ctx, "SELECT last_error FROM bot_webhook_inbox WHERE message_id = $1", claim.MessageID).Scan(&lastError); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lastError, sentinel) || lastError != "command_claim_failed" {
		t.Fatalf("last_error = %q, want bounded reason without raw identity", lastError)
	}
}

func TestAdmitMessageLogsValidationConstraintOnFailure(t *testing.T) {
	pool := dbtest.NewPool(t)
	var logs bytes.Buffer
	admitter := durableAdmitter{
		inbox:  durability.NewInboxRepository(pool),
		logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	err := admitter.AdmitMessage(t.Context(), &webhook.Message{
		JSON: &webhook.MessageJSON{MessageID: "message:missing-room", Message: "hello"},
	})
	if err == nil {
		t.Fatal("admission must fail when the room id is missing")
	}

	logged := logs.String()
	if !strings.Contains(logged, "invalid_argument") || !strings.Contains(logged, "room id must not be empty") {
		t.Fatalf("admission failure reason missing from log: %s", logged)
	}
	if strings.Contains(logged, "hello") {
		t.Fatalf("admission log leaked message content: %s", logged)
	}
}

func TestAdmitMessageWithoutLoggerReturnsErrorWithoutPanic(t *testing.T) {
	admitter := durableAdmitter{inbox: durability.NewInboxRepository(nil)}

	err := admitter.AdmitMessage(t.Context(), &webhook.Message{
		JSON: &webhook.MessageJSON{MessageID: "message:no-logger", ChatID: "room-1"},
	})
	if err == nil {
		t.Fatal("admission must fail when the pool is not configured")
	}
}
