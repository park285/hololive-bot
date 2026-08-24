package youtubedispatch

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/panicguard"
)

func (d *Dispatcher) Run(ctx context.Context) error {
	if d == nil {
		return nil
	}

	if !d.started.CompareAndSwap(false, true) {
		d.logger.Warn("Outbox dispatcher already started")

		return nil
	}

	defer d.started.Store(false)

	if err := panicguard.RunE(d.logger, "youtube-outbox-dispatcher", func() error {
		d.runJoined(ctx)

		return nil
	}); err != nil {
		return fmt.Errorf("run youtube dispatcher: %w", err)
	}

	return nil
}
