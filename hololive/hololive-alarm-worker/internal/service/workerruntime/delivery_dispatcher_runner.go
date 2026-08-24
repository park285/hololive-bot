package workerruntime

import (
	"context"
	"fmt"
	"log/slog"
)

type deliveryOutboxDispatcher interface {
	Run(ctx context.Context) error
}

type deliveryOutboxDispatcherRunner struct {
	dispatcher deliveryOutboxDispatcher
	logger     *slog.Logger
}

func NewDeliveryOutboxDispatcherRunner(dispatcher deliveryOutboxDispatcher, logger *slog.Logger) Scheduler {
	return deliveryOutboxDispatcherRunner{dispatcher: dispatcher, logger: logger}
}

func (r deliveryOutboxDispatcherRunner) Start(ctx context.Context) error {
	if r.dispatcher == nil {
		return nil
	}

	if r.logger != nil {
		r.logger.Info("Notification delivery outbox dispatcher started by alarm-worker")
	}

	if err := r.dispatcher.Run(ctx); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	return nil
}
