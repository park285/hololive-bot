package egress

import (
	"context"
	"fmt"
)

// 실제 dispatcher와 같이 최종 본문·경로를 먼저 준비하고 식별자를 포함해 전송한다.
func sendPreparedTestMessage(ctx context.Context, sender *IrisMessageSender, message string, ids ...string) error {
	id := "test-request"

	if len(ids) > 0 {
		id = ids[0]
	}

	body, route, err := sender.PrepareMessageRequest(ctx, testIrisSenderRoomID, message)
	if err != nil {
		return fmt.Errorf("prepare test message: %w", err)
	}

	if err := sender.SendPreparedMessage(ctx, testIrisSenderRoomID, body, route, id); err != nil {
		return fmt.Errorf("send test message: %w", err)
	}

	return nil
}
