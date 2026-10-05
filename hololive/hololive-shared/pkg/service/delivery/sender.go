package delivery

import "context"

// MessageSender는 Iris 방 하나에 본문을 보내는 최소 전송 계약이다.
type MessageSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
}

// ClientRequestMessageSender는 호출자가 정한 client request ID를 Iris admission 멱등 키로 넘긴다.
type ClientRequestMessageSender interface {
	SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error
}

// PreparedMessageSender는 저장된 최종 본문·경로를 구성 변경과 무관하게 그대로 전송합니다.
type PreparedMessageSender interface {
	PrepareMessageRequest(context.Context, string, string) (string, string, error)
	SendPreparedMessage(context.Context, string, string, string, string) error
}
