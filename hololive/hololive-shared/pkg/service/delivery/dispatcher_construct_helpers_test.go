package delivery

import (
	"log/slog"
	"testing"
)

func mustNewDispatcher(tb testing.TB, repository deliveryRepository, sender MessageSender, logger *slog.Logger, config *DispatcherConfig) *Dispatcher {
	tb.Helper()

	dispatcher, err := NewDispatcher(repository, sender, logger, config)
	if err != nil {
		tb.Fatalf("NewDispatcher() error = %v", err)
	}

	return dispatcher
}
