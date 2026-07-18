package workerruntime

import (
	"context"
	"log/slog"
)

type deliveryOutboxDispatcher interface {
	Start(ctx context.Context)
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
	r.dispatcher.Start(ctx)
	if r.logger != nil {
		r.logger.Info("Notification delivery outbox dispatcher started by alarm-worker")
	}
	<-ctx.Done()
	return nil
}
