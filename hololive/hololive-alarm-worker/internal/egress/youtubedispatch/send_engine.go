package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type SendEngine struct {
	sender          messagedelivery.MessageSender
	formatter       *MessageFormatter
	logger          *slog.Logger
	config          dispatchstate.Config
	karingMu        contextMutex
	claims          ClaimResolver
	auditLogger     *AuditLogger
	metricsRecorder *MetricsRecorder
	transition      deliveryTransition
	handoffMode     handoff.Mode
	handoff         YouTubeOutboxHandoff
}

type deliveryTransition interface {
	PrepareClaimed(context.Context, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) (store.PrepareClaimsResult, error)
	BeginSending(context.Context, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) (store.StartedOperation, store.ApplyResult, error)
	ApplyPreparedFailure(context.Context, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox, ytlifecycle.FailureKind, ytlifecycle.Reason, time.Duration) (store.ApplyResult, error)
	ApplyStartedFailure(context.Context, store.StartedOperation, ytlifecycle.FailureKind, ytlifecycle.Reason, time.Duration) (store.ApplyResult, error)
	CompleteSent(context.Context, store.StartedOperation, []dispatchstate.ClaimToken) (store.ApplyResult, error)
}

type contextMutex chan struct{}

func newContextMutex() contextMutex {
	mu := make(contextMutex, 1)
	mu <- struct{}{}

	return mu
}

func (m contextMutex) Lock() {
	<-m
}

func (m contextMutex) LockContext(ctx context.Context) error {
	select {
	case <-m:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("lock context mutex: %w", ctx.Err())
	}
}

func (m contextMutex) Unlock() {
	select {
	case m <- struct{}{}:
	default:
		panic("contextMutex: unlock of unlocked mutex")
	}
}

func newSendEngine(
	sender messagedelivery.MessageSender,
	formatter *MessageFormatter,
	logger *slog.Logger,
	config *dispatchstate.Config,
	claims ClaimResolver,
	auditLogger *AuditLogger,
	metricsRecorder *MetricsRecorder,
	transitions ...deliveryTransition,
) *SendEngine {
	if logger == nil {
		logger = slog.Default()
	}

	engine := &SendEngine{
		sender:          sender,
		formatter:       formatter,
		logger:          logger,
		config:          *config,
		karingMu:        newContextMutex(),
		claims:          claims,
		auditLogger:     auditLogger,
		metricsRecorder: metricsRecorder,
	}

	if len(transitions) > 0 {
		engine.transition = transitions[0]
	}

	return engine
}
