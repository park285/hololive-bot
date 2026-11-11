package egress

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

func sendPreparedTestMessage(ctx context.Context, sender *IrisMessageSender, message string, ids ...string) error {
	id := "test-request"

	if len(ids) > 0 {
		id = ids[0]
	}

	body, route, err := sender.PrepareMessageRequest(ctx, testIrisSenderRoomID, delivery.MessageVariants{Plain: message, RichText: message})
	if err != nil {
		return fmt.Errorf("prepare test message: %w", err)
	}

	if _, err := sender.SendPreparedMessage(ctx, testIrisSenderRoomID, body, route, id); err != nil {
		return fmt.Errorf("send test message: %w", err)
	}

	return nil
}
