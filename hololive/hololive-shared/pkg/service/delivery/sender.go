package delivery

import "context"

type MessageVariants struct {
	Plain    string
	RichText string
}

type PreparedMessageSender interface {
	PrepareMessageRequest(ctx context.Context, roomID string, message MessageVariants) (body, route string, err error)
	SendPreparedMessage(ctx context.Context, roomID, body, route, clientRequestID string) (irisRequestID string, err error)
}
