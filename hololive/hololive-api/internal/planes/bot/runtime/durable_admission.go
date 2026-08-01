package botruntime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/iris-client-go/webhook"
)

const (
	durableClaimLease            = 2 * time.Minute
	durableDispatchBudget        = durableClaimLease - 15*time.Second
	durableHeartbeatEvery        = 20 * time.Second
	durableOwnershipSafetyMargin = 15 * time.Second
	durablePollEvery             = 100 * time.Millisecond
	durablePollMax               = 2 * time.Second
	durableMaintenanceEvery      = 15 * time.Second
	durableRetryAfter            = time.Second
	durableMaxAttempts           = int32(5)
	durableBatchSize             = int32(100)
	commandStaleAfter            = 5 * time.Minute
	durableTerminalRetention     = 8 * 24 * time.Hour
	durableManualReviewRetention = 30 * 24 * time.Hour
)

type durableAdmitter struct {
	inbox  *durability.InboxRepository
	wake   func()
	logger *slog.Logger
}

func (a durableAdmitter) AdmitMessage(ctx context.Context, msg *webhook.Message) error {
	err := a.admit(ctx, msg)
	if err != nil {
		logDurableError(a.logger, "admit durable webhook", err)
	}
	return err
}

func (a durableAdmitter) admit(ctx context.Context, msg *webhook.Message) error {
	if msg == nil || msg.JSON == nil {
		return errors.New("admit webhook: message identity is missing")
	}
	messageID := durability.MessageIdentity(msg.JSON.MessageID)
	roomID := strings.TrimSpace(msg.JSON.ChatID)
	if roomID == "" {
		roomID = strings.TrimSpace(msg.Room)
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal webhook inbox payload: %w", err)
	}
	admitted, err := a.inbox.Admit(ctx, durability.InboxMessage{
		MessageID: messageID, RoomID: roomID, OrderingKey: "room:" + roomID, Payload: payload,
	})
	if err == nil && admitted && a.wake != nil {
		a.wake()
	}
	return err
}

type durableReplyWriter struct {
	outbox *durability.ReplyOutboxRepository
	logger *slog.Logger
	wake   func()
}

func (w durableReplyWriter) RecordReply(ctx context.Context, entry *transport.ReplyOutboxEntry) error {
	if entry == nil {
		return errors.New("record reply: entry is nil")
	}
	outcome, err := w.outbox.Insert(ctx, &durability.ReplyOutboxEntry{
		MessageID: entry.MessageID, Phase: entry.Phase, Ordinal: entry.Ordinal,
		RoomID: entry.Room, Payload: []byte(entry.Payload), ClientRequestID: entry.ClientRequestID,
	})
	if err != nil {
		return err
	}
	if outcome == durability.ReplyOutboxPayloadDiverged {
		if w.logger != nil {
			w.logger.Warn("reply outbox replay payload diverged; dispatching the stored payload")
		}
		return nil
	}
	if outcome == durability.ReplyOutboxInserted && w.wake != nil {
		w.wake()
	}
	return nil
}

type durableRuntime struct {
	inbox                  *durability.InboxRepository
	commands               *durability.CommandExecutionRepository
	outbox                 *durability.ReplyOutboxRepository
	ledger                 *durability.DurableLedgerRepository
	bot                    durableMessageProcessor
	irisClient             iris.BotClient
	logger                 *slog.Logger
	workers                int
	handlerTimeout         time.Duration
	inboxWake              chan struct{}
	outboxWake             chan struct{}
	cancel                 context.CancelFunc
	wg                     sync.WaitGroup
	dispatchBudget         time.Duration
	heartbeatEvery         time.Duration
	claimLease             time.Duration
	ownershipSafetyMargin  time.Duration
	heartbeatRetryDelay    time.Duration
	heartbeatAttemptBudget time.Duration
	inboxHeartbeat         func(context.Context, string, string, time.Duration) (time.Time, bool, error)
	commandHeartbeat       func(context.Context, string, string) (bool, error)
	now                    func() time.Time
	heartbeatWait          func(context.Context, time.Duration) bool
}

type durableMessageProcessor interface {
	ProcessMessage(context.Context, *webhook.Message) error
}

func newDurableRuntime(bot *orchestration.Bot, client iris.BotClient, pgPool *pgxpool.Pool, workers int, handlerTimeout time.Duration, logger *slog.Logger) *durableRuntime {
	if workers <= 0 {
		workers = 1
	}
	r := &durableRuntime{
		inbox: durability.NewInboxRepository(pgPool), commands: durability.NewCommandExecutionRepository(pgPool),
		outbox: durability.NewReplyOutboxRepository(pgPool), ledger: durability.NewDurableLedgerRepository(pgPool),
		bot: bot, irisClient: client, logger: logger, workers: workers,
		handlerTimeout: handlerTimeout, inboxWake: make(chan struct{}, 1), outboxWake: make(chan struct{}, 1),
		dispatchBudget: durableDispatchBudget, heartbeatEvery: durableHeartbeatEvery,
		claimLease: durableClaimLease, ownershipSafetyMargin: durableOwnershipSafetyMargin,
		heartbeatRetryDelay: 250 * time.Millisecond, heartbeatAttemptBudget: 5 * time.Second,
	}
	r.inboxHeartbeat = r.inbox.Heartbeat
	r.commandHeartbeat = r.commands.Heartbeat
	return r
}

func (r *durableRuntime) runInboxWorker(ctx context.Context) {
	defer r.wg.Done()
	idleDelay := durablePollEvery
	for ctx.Err() == nil {
		if r.processNextInbox(ctx) {
			idleDelay = durablePollEvery
			continue
		}
		durableClaimIdleTotal.WithLabelValues("inbox").Inc()
		if !waitDurableWake(ctx, idleDelay, r.inboxWake) {
			return
		}
		idleDelay = nextDurableIdleDelay(idleDelay)
	}
}

func (r *durableRuntime) processNextInbox(ctx context.Context) bool {
	token := rand.Text()
	claim, err := r.inbox.Claim(ctx, token, r.claimLeaseDuration())
	if err != nil {
		r.logError("claim durable webhook", err)
		return false
	}
	if claim == nil {
		return false
	}
	r.processInboxClaim(ctx, claim, token)
	return true
}

func (r *durableRuntime) processInboxClaim(ctx context.Context, claim *durability.InboxClaim, token string) {
	if claim == nil {
		r.logError("process durable webhook", errors.New("inbox claim is nil"))
		return
	}
	var msg webhook.Message
	if err := json.Unmarshal(claim.Payload, &msg); err != nil {
		if _, abandonErr := r.inbox.Abandon(ctx, claim.MessageID, token, "stored webhook payload is not decodable"); abandonErr != nil {
			r.logError("abandon poison webhook", abandonErr)
		}
		return
	}
	claimed, err := r.claimCommand(ctx, claim, token)
	if err != nil || !claimed {
		return
	}
	commandCtx, cancelCommand := context.WithTimeout(ctx, r.handlerTimeout)
	defer cancelCommand()
	heartbeatCtx, stopHeartbeat := context.WithCancel(commandCtx)
	heartbeatDone := make(chan struct{})
	panicguard.Go(r.logger, "durable-claim-heartbeat", func() {
		defer close(heartbeatDone)
		r.runClaimHeartbeat(heartbeatCtx, claim.MessageID, token, claim.LeaseUntil, cancelCommand)
	})
	err = r.bot.ProcessMessage(commandCtx, &msg)
	if commandCtx.Err() != nil {
		err = errors.Join(err, commandCtx.Err())
	}
	stopHeartbeat()
	<-heartbeatDone
	r.completeCommandAndInbox(ctx, claim.MessageID, token, err)
}

func (r *durableRuntime) claimCommand(ctx context.Context, claim *durability.InboxClaim, token string) (bool, error) {
	claimed, err := r.commands.Claim(ctx, claim.MessageID, "webhook", token)
	if err != nil {
		r.releaseInbox(ctx, claim, token, err)
		return false, err
	}
	if claimed {
		return true, nil
	}
	state, err := r.commands.State(ctx, claim.MessageID)
	if err != nil {
		r.releaseInbox(ctx, claim, token, err)
		return false, err
	}
	if state == nil {
		err = errors.New("command execution state disappeared after claim conflict")
		r.releaseInbox(ctx, claim, token, err)
		return false, err
	}
	if state.Status == durability.CommandExecutionClaimed {
		return false, r.deferInboxForClaimedCommand(ctx, claim, token, state.ClaimedAt)
	}
	_, err = r.inbox.Complete(ctx, claim.MessageID, token)
	if err != nil {
		r.logError("complete already executed webhook", err)
	}
	return false, err
}

func (r *durableRuntime) deferInboxForClaimedCommand(
	ctx context.Context,
	claim *durability.InboxClaim,
	token string,
	claimedAt time.Time,
) error {
	retryAfter := claimedAt.Add(commandStaleAfter + durableMaintenanceEvery).Sub(r.nowTime())
	retryAfter = max(retryAfter, durableMaintenanceEvery)
	outcome, err := r.inbox.Release(ctx, claim.MessageID, token, durableMaxAttempts, retryAfter,
		durability.InboxFailureCommandAlreadyClaimed)
	if err != nil {
		r.logError("defer webhook behind active command claim", err)
		return err
	}
	if outcome == durability.InboxReleaseAbandoned {
		err = errors.New("webhook exhausted retries behind active command claim")
		r.logError("defer webhook behind active command claim", err)
		return err
	}
	return nil
}

func (r *durableRuntime) completeCommandAndInbox(ctx context.Context, messageID, token string, commandErr error) {
	status := commandExecutionStatus(commandErr)
	applied, completeErr := r.commands.Complete(ctx, messageID, token, status)
	if completeErr != nil || !applied {
		if completeErr == nil {
			completeErr = errors.New("command completion lost its claim")
		}
		r.logError("complete command execution", completeErr)
		return
	}
	if _, completeErr = r.inbox.Complete(ctx, messageID, token); completeErr != nil {
		r.logError("complete durable webhook", completeErr)
	}
	if status == durability.CommandExecutionOutcomeUnknown {
		durableCommandOutcomeUnknownTotal.Inc()
		if r.logger != nil {
			r.logger.Error("durable command outcome requires manual review",
				slog.String("message_token", privacylog.Pseudonym(messageID)))
		}
	}
}

func commandExecutionStatus(commandErr error) string {
	if commandErr == nil {
		return durability.CommandExecutionSucceeded
	}
	if orchestration.IsCommandOutcomeUnknown(commandErr) || errors.Is(commandErr, context.Canceled) || errors.Is(commandErr, context.DeadlineExceeded) {
		return durability.CommandExecutionOutcomeUnknown
	}
	return durability.CommandExecutionFailed
}

func (r *durableRuntime) releaseInbox(ctx context.Context, claim *durability.InboxClaim, token string, cause error) {
	outcome, err := r.inbox.Release(ctx, claim.MessageID, token, durableMaxAttempts, durableRetryAfter, inboxReleaseReason(cause))
	if err != nil {
		r.logError("release durable webhook", err)
		return
	}
	if outcome == durability.InboxReleaseAbandoned && r.logger != nil {
		r.logger.Error("durable webhook abandoned after max attempts",
			slog.Int("attempts", int(claim.Attempts)),
			slog.String("message_token", privacylog.Pseudonym(claim.MessageID)))
	}
}

func inboxReleaseReason(cause error) string {
	if cause == nil {
		return durability.InboxFailureCommandClaimFailed
	}
	switch {
	case errors.Is(cause, context.Canceled):
		return durability.InboxFailureCommandClaimContextCanceled
	case errors.Is(cause, context.DeadlineExceeded):
		return durability.InboxFailureCommandClaimContextDeadline
	default:
		return durability.InboxFailureCommandClaimFailed
	}
}

func (r *durableRuntime) runOutboxWorker(ctx context.Context) {
	defer r.wg.Done()
	idleDelay := durablePollEvery
	for ctx.Err() == nil {
		if r.processNextOutbox(ctx) {
			idleDelay = durablePollEvery
			continue
		}
		durableClaimIdleTotal.WithLabelValues("outbox").Inc()
		if !waitDurableWake(ctx, idleDelay, r.outboxWake) {
			return
		}
		idleDelay = nextDurableIdleDelay(idleDelay)
	}
}

func (r *durableRuntime) processNextOutbox(ctx context.Context) bool {
	token := rand.Text()
	claim, err := r.outbox.Claim(ctx, token, durableClaimLease)
	if err != nil {
		r.logError("claim reply outbox", err)
		return false
	}
	if claim == nil {
		return false
	}
	r.dispatchOutboxClaim(ctx, claim, token)
	return true
}

func (r *durableRuntime) dispatchOutboxClaim(ctx context.Context, claim *durability.ReplyOutboxClaim, token string) {
	accepted := false
	dispatchCtx, cancel := context.WithTimeout(ctx, r.dispatchBudget)
	err := transport.DispatchStoredReply(dispatchCtx, r.irisClient, claim.RoomID, claim.Payload, claim.ClientRequestID,
		r.acceptanceHook(claim.ID, token, &accepted))
	cancel()
	applied, settleErr := r.settleOutboxDispatch(ctx, claim, token, accepted, err)
	r.finishOutboxSettlement(claim, accepted, err, applied, settleErr)
}

func (r *durableRuntime) acceptanceHook(id int64, token string, accepted *bool) transport.ReplyAcceptedHook {
	return func(ctx context.Context, requestID string) error {
		*accepted = true
		applied, err := r.outbox.MarkAccepted(ctx, id, token, requestID)
		if err != nil {
			return err
		}
		if !applied {
			return errors.New("reply outbox acceptance lost its claim")
		}
		return nil
	}
}

func (r *durableRuntime) finishOutboxSettlement(claim *durability.ReplyOutboxClaim, accepted bool, dispatchErr error, applied bool, settleErr error) {
	switch {
	case settleErr != nil:
		r.logError("settle reply outbox", settleErr)
	case !applied && !accepted:
		r.logError("settle reply outbox", errors.New("reply outbox settlement lost its claim"))
	case applied:
		r.observeOutboxSettlement(claim, replyOutboxSettlementStatus(accepted, claim.Attempts, dispatchErr))
	}
}

func (r *durableRuntime) observeOutboxSettlement(claim *durability.ReplyOutboxClaim, status string) {
	if r.logger == nil || status == durability.ReplyOutboxHandoffCompleted {
		return
	}
	attrs := []any{slog.String("status", status), slog.Int("attempts", int(claim.Attempts))}
	if status == durability.ReplyOutboxRetryablePreDispatch || status == durability.ReplyOutboxOutcomeUnknown {
		r.logger.Warn("reply outbox dispatch deferred", attrs...)
		return
	}
	r.logger.Error("reply outbox dispatch reached non-success terminal state", attrs...)
}

func (r *durableRuntime) settleOutboxDispatch(ctx context.Context, claim *durability.ReplyOutboxClaim, token string, accepted bool, dispatchErr error) (bool, error) {
	status := replyOutboxSettlementStatus(accepted, claim.Attempts, dispatchErr)
	retryAfter := replyOutboxRetryAfter(status, claim.Attempts)
	return r.outbox.Settle(ctx, durability.ReplyOutboxSettlement{
		ID: claim.ID, ClaimToken: token, Status: status, LastError: errorText(dispatchErr), RetryAfter: retryAfter,
	})
}
