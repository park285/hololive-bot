package botruntime

import (
	"context"
	"crypto/rand"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/iris-client-go/v3/webhook"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

const (
	durablePollMax    = 2 * time.Second
	durableBatchSize  = int32(100)
	commandStaleAfter = apiconfig.BotCommandStaleAfter
)

type durableAdmitter struct {
	inbox  *durability.InboxRepository
	wake   func()
	accept func(context.Context, *webhook.Message) bool
	logger *slog.Logger
	totals *workercontract.Counters
}

func (a durableAdmitter) AdmitMessage(ctx context.Context, msg *webhook.Message) error {
	if err := a.admit(ctx, msg); err != nil {
		logDurableError(a.logger, "admit durable webhook", err)

		return fmt.Errorf("admit durable webhook: %w", err)
	}

	return nil
}

func (a durableAdmitter) admit(ctx context.Context, msg *webhook.Message) error {
	if msg == nil || msg.JSON == nil {
		return errors.New("admit webhook: message identity is missing")
	}

	if a.accept != nil && !a.accept(ctx, msg) {
		if a.totals != nil {
			a.totals.RecordAdmission(workercontract.AdmissionRejected)
		}

		return nil
	}

	messageID := durability.MessageIdentity(msg.JSON.MessageID)
	if id := strings.TrimSpace(msg.JSON.MessageID); id != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("iris.message_id", id))
	}

	roomID := strings.TrimSpace(msg.Room)

	payload, err := jsonv2.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal webhook inbox payload: %w", err)
	}

	result, err := a.inbox.AdmitResult(ctx, durability.InboxMessage{
		MessageID: messageID, RoomID: roomID, OrderingKey: "room:" + roomID, Payload: payload,
	})
	if a.totals != nil {
		a.totals.RecordAdmission(result)
	}

	if err != nil {
		return fmt.Errorf("admit inbox message: %w", err)
	}

	if result == workercontract.AdmissionAccepted && a.wake != nil {
		a.wake()
	}

	return nil
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

		return fmt.Errorf("persist reply: %w", err)
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

type durableIrisClient interface {
	transport.ReplyClient
	GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error)
}

type durableRuntime struct {
	inbox                  *durability.InboxRepository
	commands               *durability.CommandExecutionRepository
	outbox                 *durability.ReplyOutboxRepository
	ledger                 *durability.DurableLedgerRepository
	bot                    durableMessageProcessor
	irisClient             durableIrisClient
	logger                 *slog.Logger
	inboxWorkers           int
	outboxWorkers          int
	inboxEnabled           bool
	outboxEnabled          bool
	handlerTimeout         time.Duration
	inboxWake              chan struct{}
	outboxWake             chan struct{}
	inboxPollSlot          chan struct{}
	outboxPollSlot         chan struct{}
	handoffWake            chan struct{}
	handoffPollSlot        chan struct{}
	cancel                 context.CancelFunc
	wg                     sync.WaitGroup
	lifecycleMu            sync.Mutex
	started                bool
	stopping               bool
	stopDone               chan struct{}
	trackersStopOnce       sync.Once
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

func newDurableRuntime(bot *orchestration.Bot, client durableIrisClient, pgPool *pgxpool.Pool, profile *apiconfig.APIWorkerProfile, logger *slog.Logger) (*durableRuntime, error) {
	if profile == nil {
		return nil, errors.New("build durable runtime: API worker profile is required")
	}

	workers := profile.Loaded.Profile.Workers
	inboxProfile := workers[apiconfig.WorkerBotWebhookInbox]
	outboxProfile := workers[apiconfig.WorkerBotReplyOutbox]
	handlerTimeout, _ := inboxProfile.Executor.AttemptTimeout.FixedDuration()
	dispatchBudget, _ := outboxProfile.Executor.AttemptTimeout.FixedDuration()

	outboxRepository, err := durability.NewReplyOutboxRepositoryWithPolicy(
		pgPool,
		profile.BotReplyOutbox.MaxAttempts,
		workercontract.Milliseconds(profile.BotReplyOutbox.AutomaticReplayHorizonMS),
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
		handlerTimeout: handlerTimeout,
		inboxWake:      make(chan struct{}, 1), outboxWake: make(chan struct{}, 1), handoffWake: make(chan struct{}, 1),
		inboxPollSlot: newDurablePollSlot(), outboxPollSlot: newDurablePollSlot(), handoffPollSlot: newDurablePollSlot(),
		// bot_reply_outbox attempt timeout은 admission 예산이자 handoff 확인 지평이다.
		dispatchBudget:        dispatchBudget,
		outboxClaimLease:      workercontract.Milliseconds(profile.BotReplyOutbox.ClaimLeaseMS),
		heartbeatEvery:        workercontract.Milliseconds(profile.BotWebhookInbox.HeartbeatIntervalMS),
		claimLease:            workercontract.Milliseconds(profile.BotWebhookInbox.ClaimLeaseMS),
		ownershipSafetyMargin: workercontract.Milliseconds(profile.BotWebhookInbox.OwnershipSafetyMarginMS),
		heartbeatRetryDelay:   250 * time.Millisecond, heartbeatAttemptBudget: 5 * time.Second,
		inboxPollEvery:        workercontract.Milliseconds(profile.BotWebhookInbox.PollIntervalMS),
		outboxPollEvery:       workercontract.Milliseconds(profile.BotReplyOutbox.PollIntervalMS),
		maintenanceEvery:      workercontract.Milliseconds(profile.BotWebhookInbox.MaintenanceIntervalMS),
		inboxRetryAfter:       workercontract.Milliseconds(profile.BotWebhookInbox.RetryAfterMS),
		outboxRetryAfter:      workercontract.Milliseconds(profile.BotReplyOutbox.RetryAfterMS),
		inboxMaxAttempts:      profile.BotWebhookInbox.MaxAttempts,
		outboxMaxAttempts:     profile.BotReplyOutbox.MaxAttempts,
		settlementTimeout:     workercontract.Milliseconds(profile.BotWebhookInbox.SettlementTimeoutMS),
		terminalRetention:     workercontract.Milliseconds(profile.BotWebhookInbox.TerminalRetentionMS),
		manualReviewRetention: workercontract.Milliseconds(profile.BotReplyOutbox.ManualReviewRetentionMS),
		inboxTracker:          workercontract.NewExecutorTracker(), outboxTracker: workercontract.NewExecutorTracker(),
		inboxTotals: &workercontract.Counters{}, outboxTotals: &workercontract.Counters{},
	}

	r.inboxSampler = newDurableQueueSampler("inbox", r.inbox.ReadySnapshot)
	r.outboxSampler = newDurableQueueSampler("outbox", r.outbox.ReadySnapshot)
	r.inboxHeartbeat = r.inbox.Heartbeat
	r.commandHeartbeat = r.commands.Heartbeat

	return r, nil
}

func newDurableQueueSampler(
	queue string,
	readySnapshot func(context.Context) (durability.ReadyQueueSnapshot, error),
) *workercontract.QueueSampler {
	return workercontract.NewQueueSampler(func(sampleCtx context.Context) (workercontract.QueueValues, error) {
		snapshot, snapshotErr := readySnapshot(sampleCtx)
		values := workercontract.QueueValues{Depth: snapshot.Depth, OldestQueuedAge: snapshot.OldestAge}

		if snapshotErr != nil {
			return values, fmt.Errorf("%s ready snapshot: %w", queue, snapshotErr)
		}

		return values, nil
	})
}

func (r *durableRuntime) runInboxWorker(ctx context.Context) {
	runDurableWorker(ctx, "inbox", r.processNextInbox, r.inboxWake, r.inboxPollSlot, r.inboxPollEvery)
}

func (r *durableRuntime) processNextInbox(ctx context.Context, claimed func()) bool {
	token := rand.Text()

	claim, err := r.inbox.Claim(ctx, token, r.claimLeaseDuration())
	if err != nil {
		r.logError("claim durable webhook", err)

		return false
	}

	if claim == nil {
		return false
	}

	claimed()

	attemptID := r.inboxTracker.BeginAttempt(time.Now())
	defer r.inboxTracker.EndAttempt(attemptID)

	err = r.processInboxClaim(ctx, claim, token)
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

	if err := jsonv2.Unmarshal(claim.Payload, &msg); err != nil {
		settleCtx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), r.settlementTimeout)
		defer cancel()

		if _, abandonErr := r.inbox.Abandon(settleCtx, claim.MessageID, token, "stored webhook payload is not decodable"); abandonErr != nil {
			r.logError("abandon poison webhook", abandonErr)

			return errors.Join(err, abandonErr)
		}

		return fmt.Errorf("unmarshal: %w", err)
	}

	claimed, err := r.claimCommand(ctx, claim, token)
	if err != nil {
		return fmt.Errorf("claim command: %w", err)
	}

	if !claimed {
		return nil
	}

	commandCtx, cancelCommand := context.WithTimeout(ctx, r.handlerTimeout)

	defer cancelCommand()

	heartbeatCtx, stopHeartbeat := context.WithCancel(commandCtx)
	heartbeatDone := make(chan struct{})

	go panicguard.Run(r.logger, panicguard.BackgroundTask, "durable-claim-heartbeat", func() {
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
	settleCtx, cancelSettle := context.WithTimeout(pgxdb.WithControlAcquire(context.WithoutCancel(ctx)), r.settlementTimeout)
	defer cancelSettle()

	return errors.Join(err, r.completeCommandAndInbox(settleCtx, claim.MessageID, token, err))
}

func (r *durableRuntime) claimCommand(ctx context.Context, claim *durability.InboxClaim, token string) (bool, error) {
	claimed, err := r.commands.Claim(ctx, claim.MessageID, "webhook", token)
	if err != nil {
		r.releaseInbox(ctx, claim, token, err)

		return false, fmt.Errorf("claim: %w", err)
	}

	if claimed {
		return true, nil
	}

	state, err := r.commands.State(ctx, claim.MessageID)
	if err != nil {
		r.releaseInbox(ctx, claim, token, err)

		return false, fmt.Errorf("state: %w", err)
	}

	if state == nil {
		err = errors.New("command execution state disappeared after claim conflict")
		r.releaseInbox(ctx, claim, token, err)

		return false, err
	}

	if state.Status == durability.CommandExecutionClaimed {
		if deferErr := r.deferInboxForClaimedCommand(ctx, claim, token, state.ClaimedAt); deferErr != nil {
			return false, fmt.Errorf("defer inbox for claimed command: %w", deferErr)
		}

		return false, nil
	}

	settleCtx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), r.settlementTimeout)
	defer cancel()

	if _, err = r.inbox.Complete(settleCtx, claim.MessageID, token); err != nil {
		r.logError("complete already executed webhook", err)

		return false, fmt.Errorf("complete already executed webhook: %w", err)
	}

	return false, nil
}

func (r *durableRuntime) deferInboxForClaimedCommand(
	ctx context.Context,
	claim *durability.InboxClaim,
	token string,
	claimedAt time.Time,
) error {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), r.settlementTimeout)
	defer cancel()

	retryAfter := claimedAt.Add(commandStaleAfter + r.maintenanceEvery).Sub(r.nowTime())

	retryAfter = max(retryAfter, r.maintenanceEvery)

	outcome, err := r.inbox.Release(ctx, claim.MessageID, token, claim.Attempts, r.inboxMaxAttempts, retryAfter,
		durability.InboxFailureCommandAlreadyClaimed)
	if err != nil {
		r.logError("defer webhook behind active command claim", err)

		return fmt.Errorf("release: %w", err)
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

		return fmt.Errorf("complete: %w", completeErr)
	}

	if status == durability.CommandExecutionOutcomeUnknown {
		durableCommandOutcomeUnknownTotal.Inc()

		if r.logger != nil {
			r.logger.Error("durable command outcome requires manual review",
				slog.String("message_token", sharedprivacylog.Pseudonym(messageID)))
		}
	}

	return nil
}
