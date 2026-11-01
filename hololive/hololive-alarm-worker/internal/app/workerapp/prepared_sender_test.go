package workerapp

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

func sendPreparedWorkerTestMessage(ctx context.Context, sender *egress.IrisMessageSender, room, message string, ids ...string) error {
	id := "test-request"

	if len(ids) > 0 {
		id = ids[0]
	}

	body, route, err := sender.PrepareMessageRequest(ctx, room, delivery.MessageVariants{Plain: message, RichText: message})
	if err != nil {
		return fmt.Errorf("prepare test message: %w", err)
	}

	if _, err := sender.SendPreparedMessage(ctx, room, body, route, id); err != nil {
		return fmt.Errorf("send test message: %w", err)
	}

	return nil
}
