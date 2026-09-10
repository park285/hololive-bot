package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/admin-dashboard/internal/observations"
)

func TestRuntimeObservationsOutliveBuildContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	hub := observations.NewHub(nil)
	startStats(ctx, hub)
	t.Cleanup(hub.Stop)
	cancel()

	_, updates, unsubscribe := hub.Subscribe()

	defer unsubscribe()

	select {
	case <-updates:
	case <-time.After(3 * time.Second):
		t.Fatal("observations stopped with the build context")
	}
}
