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
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/iris-client-go/v2/webhook"
	"github.com/park285/shared-go/pkg/workercontract"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	// Stop이 wg.Wait로 in-flight 정산을 조인하고 종료 hook 체인(bot→admin→llm plane)은
	// AppTimeout.Shutdown(10s) 단일 ctx를 순차 공유하므로, 정산 예산이 그 절반을 넘으면
	// DB 장애 시 후속 plane의 graceful 종료가 통째로 굶는다.
	durableSettlementTimeout     = 3 * time.Second
	durableTerminalRetention     = 8 * 24 * time.Hour
	durableManualReviewRetention = 30 * 24 * time.Hour
)

type durableAdmitter struct {
	inbox  *durability.InboxRepository
	wake   func()
	logger *slog.Logger
	totals *workercontract.Counters
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
	if id := strings.TrimSpace(msg.JSON.MessageID); id != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("iris.message_id", id))
	}
	roomID := strings.TrimSpace(msg.JSON.ChatID)
	if roomID == "" {
		roomID = strings.TrimSpace(msg.Room)
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal webhook inbox payload: %w", err)
	}
	result, err := a.inbox.AdmitResult(ctx, durability.InboxMessage{
		MessageID: messageID, RoomID: roomID, OrderingKey: "room:" + roomID, Payload: payload,
	})
	a.totals.RecordAdmission(result)
	if err == nil && result == workercontract.AdmissionAccepted && a.wake != nil {
		a.wake()
	}
	return err
}

type durableReplyWriter struct {
	outbox *durability.ReplyOutboxRepository
	logger *slog.Logger
	wake   func()
	totals *workercontract.Counters
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
		w.totals.RecordAdmission(workercontract.AdmissionOutcomeUnknown)
		return err
	}
	if outcome == durability.ReplyOutboxInserted {
		w.totals.RecordAdmission(workercontract.AdmissionAccepted)
	} else {
		w.totals.RecordAdmission(workercontract.AdmissionDuplicate)
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
	inboxWorkers           int
	outboxWorkers          int
	inboxEnabled           bool
	outboxEnabled          bool
	handlerTimeout         time.Duration
	inboxWake              chan struct{}
	outboxWake             chan struct{}
	cancel                 context.CancelFunc
	wg                     sync.WaitGroup
	dispatchBudget         time.Duration
	outboxClaimLease       time.Duration
	heartbeatEvery         time.Duration
	claimLease             time.Duration
	ownershipSafetyMargin  time.Duration
	heartbeatRetryDelay    time.Duration
	heartbeatAttemptBudget time.Duration
	inboxPollEvery         time.Duration
	outboxPollEvery        time.Duration
	maintenanceEvery       time.Duration
	inboxRetryAfter        time.Duration
	outboxRetryAfter       time.Duration
	inboxMaxAttempts       int32
	outboxMaxAttempts      int32
	settlementTimeout      time.Duration
	terminalRetention      time.Duration
	manualReviewRetention  time.Duration
	inboxTracker           *workercontract.ExecutorTracker
	outboxTracker          *workercontract.ExecutorTracker
	inboxTotals            *workercontract.Counters
	outboxTotals           *workercontract.Counters
	inboxSampler           *workercontract.QueueSampler
	outboxSampler          *workercontract.QueueSampler
	inboxHeartbeat         func(context.Context, string, string, time.Duration) (time.Time, bool, error)
	commandHeartbeat       func(context.Context, string, string) (bool, error)
	now                    func() time.Time
	heartbeatWait          func(context.Context, time.Duration) bool
}

type durableMessageProcessor interface {
	ProcessMessage(context.Context, *webhook.Message) error
}

func newDurableRuntime(bot *orchestration.Bot, client iris.BotClient, pgPool *pgxpool.Pool, profile *settings.APIWorkerProfile, logger *slog.Logger) (*durableRuntime, error) {
	if profile == nil {
		return nil, errors.New("build durable runtime: API worker profile is required")
	}
	workers := profile.Loaded.Profile.Workers
	inboxProfile := workers["bot_webhook_inbox"]
	outboxProfile := workers["bot_reply_outbox"]
	outboxRepository, err := durability.NewReplyOutboxRepositoryWithPolicy(
		pgPool,
		profile.BotReplyOutbox.MaxAttempts,
		time.Duration(profile.BotReplyOutbox.AutomaticReplayHorizonMS)*time.Millisecond,
	)
	if err != nil {
		return nil, fmt.Errorf("build durable runtime: reply outbox policy: %w", err)
	}
	r := &durableRuntime{
		inbox: durability.NewInboxRepository(pgPool), commands: durability.NewCommandExecutionRepository(pgPool),
		outbox: outboxRepository, ledger: durability.NewDurableLedgerRepository(pgPool),
		bot: bot, irisClient: client, logger: logger,
		inboxWorkers: inboxProfile.Executor.ConfiguredWorkers, outboxWorkers: outboxProfile.Executor.ConfiguredWorkers,
		inboxEnabled: inboxProfile.Executor.Enabled, outboxEnabled: outboxProfile.Executor.Enabled,
		handlerTimeout: time.Duration(*inboxProfile.Executor.AttemptTimeout.Milliseconds) * time.Millisecond,
		inboxWake:      make(chan struct{}, 1), outboxWake: make(chan struct{}, 1),
		dispatchBudget:        time.Duration(profile.BotReplyOutbox.DispatchBudgetMS) * time.Millisecond,
		outboxClaimLease:      time.Duration(profile.BotReplyOutbox.ClaimLeaseMS) * time.Millisecond,
		heartbeatEvery:        time.Duration(profile.BotWebhookInbox.HeartbeatIntervalMS) * time.Millisecond,
		claimLease:            time.Duration(profile.BotWebhookInbox.ClaimLeaseMS) * time.Millisecond,
		ownershipSafetyMargin: time.Duration(profile.BotWebhookInbox.OwnershipSafetyMarginMS) * time.Millisecond,
		heartbeatRetryDelay:   250 * time.Millisecond, heartbeatAttemptBudget: 5 * time.Second,
		inboxPollEvery:        time.Duration(profile.BotWebhookInbox.PollIntervalMS) * time.Millisecond,
		outboxPollEvery:       time.Duration(profile.BotReplyOutbox.PollIntervalMS) * time.Millisecond,
		maintenanceEvery:      time.Duration(profile.BotWebhookInbox.MaintenanceIntervalMS) * time.Millisecond,
		inboxRetryAfter:       time.Duration(profile.BotWebhookInbox.RetryAfterMS) * time.Millisecond,
		outboxRetryAfter:      time.Duration(profile.BotReplyOutbox.RetryAfterMS) * time.Millisecond,
		inboxMaxAttempts:      profile.BotWebhookInbox.MaxAttempts,
		outboxMaxAttempts:     profile.BotReplyOutbox.MaxAttempts,
		settlementTimeout:     time.Duration(profile.BotWebhookInbox.SettlementTimeoutMS) * time.Millisecond,
		terminalRetention:     time.Duration(profile.BotWebhookInbox.TerminalRetentionMS) * time.Millisecond,
		manualReviewRetention: time.Duration(profile.BotReplyOutbox.ManualReviewRetentionMS) * time.Millisecond,
		inboxTracker:          workercontract.NewExecutorTracker(), outboxTracker: workercontract.NewExecutorTracker(),
		inboxTotals: &workercontract.Counters{}, outboxTotals: &workercontract.Counters{},
	}
	r.inboxSampler = workercontract.NewQueueSampler(func(sampleCtx context.Context) (workercontract.QueueValues, error) {
		snapshot, snapshotErr := r.inbox.ReadySnapshot(sampleCtx)
		return workercontract.QueueValues{Depth: snapshot.Depth, OldestQueuedAge: snapshot.OldestAge}, snapshotErr
	})
	r.outboxSampler = workercontract.NewQueueSampler(func(sampleCtx context.Context) (workercontract.QueueValues, error) {
		snapshot, snapshotErr := r.outbox.ReadySnapshot(sampleCtx)
		return workercontract.QueueValues{Depth: snapshot.Depth, OldestQueuedAge: snapshot.OldestAge}, snapshotErr
	})
	r.inboxHeartbeat = r.inbox.Heartbeat
	r.commandHeartbeat = r.commands.Heartbeat
	return r, nil
}

func (r *durableRuntime) runInboxWorker(ctx context.Context) {
	defer r.wg.Done()
	idleDelay := r.inboxPollEvery
	for ctx.Err() == nil {
		if r.processNextInbox(ctx) {
			idleDelay = r.inboxPollEvery
			continue
		}
		durableClaimIdleTotal.WithLabelValues("inbox").Inc()
		if !waitDurableWake(ctx, idleDelay, r.inboxWake) {
			return
		}
		idleDelay = nextDurableIdleDelayFrom(idleDelay, r.inboxPollEvery)
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
	attemptID := r.inboxTracker.BeginAttempt(time.Now())
	err = r.processInboxClaim(ctx, claim, token)
	r.inboxTracker.EndAttempt(attemptID)
	r.inboxTotals.RecordAttempt(workerAttemptOutcome(err))
	return true
}

func (r *durableRuntime) processInboxClaim(ctx context.Context, claim *durability.InboxClaim, token string) error {
	if claim == nil {
		err := errors.New("inbox claim is nil")
		r.logError("process durable webhook", err)
		return err
	}
	var msg webhook.Message
	if err := json.Unmarshal(claim.Payload, &msg); err != nil {
		if _, abandonErr := r.inbox.Abandon(ctx, claim.MessageID, token, "stored webhook payload is not decodable"); abandonErr != nil {
			r.logError("abandon poison webhook", abandonErr)
			return errors.Join(err, abandonErr)
		}
		return err
	}
	claimed, err := r.claimCommand(ctx, claim, token)
	if err != nil || !claimed {
		return err
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
	// shutdown이 runCtx를 취소한 뒤에도 정산은 성공해야 한다 — 취소된 ctx 그대로면
	// command가 claimed로 방치되어 재시작 후 같은 방이 head-of-line에서 수 분간 정지한다.
	// token fencing이 있어 소유권을 잃은 경우에도 안전한 no-op이다.
	settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), r.settlementTimeout)
	defer cancelSettle()
	return errors.Join(err, r.completeCommandAndInbox(settleCtx, claim.MessageID, token, err))
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
	retryAfter := claimedAt.Add(commandStaleAfter + r.maintenanceEvery).Sub(r.nowTime())
	retryAfter = max(retryAfter, r.maintenanceEvery)
	outcome, err := r.inbox.Release(ctx, claim.MessageID, token, r.inboxMaxAttempts, retryAfter,
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

func (r *durableRuntime) completeCommandAndInbox(ctx context.Context, messageID, token string, commandErr error) error {
	status := commandExecutionStatus(commandErr)
	applied, completeErr := r.commands.Complete(ctx, messageID, token, status)
	if completeErr != nil || !applied {
		if completeErr == nil {
			completeErr = errors.New("command completion lost its claim")
		}
		r.logError("complete command execution", completeErr)
		return completeErr
	}
	if _, completeErr = r.inbox.Complete(ctx, messageID, token); completeErr != nil {
		r.logError("complete durable webhook", completeErr)
		return completeErr
	}
	if status == durability.CommandExecutionOutcomeUnknown {
		durableCommandOutcomeUnknownTotal.Inc()
		if r.logger != nil {
			r.logger.Error("durable command outcome requires manual review",
				slog.String("message_token", privacylog.Pseudonym(messageID)))
		}
	}
	return nil
}
