package workerapp

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
)

func sendPreparedWorkerTestMessage(ctx context.Context, sender *egress.IrisMessageSender, room, message string, ids ...string) error {
	id := "test-request"

	if len(ids) > 0 {
		id = ids[0]
	}

	body, route, err := sender.PrepareMessageRequest(ctx, room, message)
	if err != nil {
		return fmt.Errorf("prepare test message: %w", err)
	}

	if err := sender.SendPreparedMessage(ctx, room, body, route, id); err != nil {
		return fmt.Errorf("send test message: %w", err)
	}

	return nil
}
