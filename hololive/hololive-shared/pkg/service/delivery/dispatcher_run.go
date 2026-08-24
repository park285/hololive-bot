package delivery

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/panicguard"
)

// Run은 dispatcher loop를 caller goroutine에서 실행해 종료와 panic 결과를 lifecycle owner에 반환합니다.
func (d *Dispatcher) Run(ctx context.Context) error {
	if d == nil {
		return nil
	}

	if err := panicguard.RunE(d.logger, "delivery-dispatcher", func() error {
		d.run(ctx)

		return nil
	}); err != nil {
		return fmt.Errorf("run delivery dispatcher: %w", err)
	}

	return nil
}
