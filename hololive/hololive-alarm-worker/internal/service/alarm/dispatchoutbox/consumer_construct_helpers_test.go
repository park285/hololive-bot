package dispatchoutbox

import (
	"log/slog"
	"testing"
	"time"
)

const testConsumerQuarantineThreshold = 5 * time.Minute

func mustNewConsumer(tb testing.TB, repository Repository, claimReleaser ClaimKeyReleaser, logger *slog.Logger, opts ...ConsumerOption) *Consumer {
	tb.Helper()

	consumer, err := NewConsumer(repository, claimReleaser, logger, append([]ConsumerOption{WithQuarantineThreshold(testConsumerQuarantineThreshold)}, opts...)...)
	if err != nil {
		tb.Fatalf("NewConsumer() error = %v", err)
	}

	return consumer
}
