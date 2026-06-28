package dispatch

import (
	"context"
	"fmt"
	"log/slog"

	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
)

type SendEngine struct {
	sender          messagedelivery.MessageSender
	formatter       *MessageFormatter
	logger          *slog.Logger
	config          Config
	karingMu        contextMutex
	claims          ClaimResolver
	auditLogger     *AuditLogger
	metricsRecorder *MetricsRecorder
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
	config *Config,
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
