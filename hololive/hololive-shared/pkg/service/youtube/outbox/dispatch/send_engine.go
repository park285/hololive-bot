package dispatch

import (
	"context"
	"fmt"
	"log/slog"

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
	handoffMode     handoff.Mode
	handoff         YouTubeOutboxHandoff
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
) *SendEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &SendEngine{
		sender:          sender,
		formatter:       formatter,
		logger:          logger,
		config:          *config,
		karingMu:        newContextMutex(),
		claims:          claims,
		auditLogger:     auditLogger,
		metricsRecorder: metricsRecorder,
	}
}
