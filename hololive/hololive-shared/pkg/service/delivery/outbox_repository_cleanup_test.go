package delivery

import (
	"context"
	"errors"
	"testing"
)

func TestYieldBetweenCleanupBatchesReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := yieldBetweenCleanupBatches(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("yieldBetweenCleanupBatches() error = %v, want context.Canceled", err)
	}
}
