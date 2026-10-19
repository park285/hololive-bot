package delivery

import "context"

// PreparedMessageSender는 저장된 최종 본문·경로를 구성 변경과 무관하게 그대로 전송합니다.
// SendPreparedMessage는 Iris가 durable admission을 증명한 202 응답의 requestId를 돌려주며, 호출자는 이 값으로
// handoff 상태를 확인합니다.
type PreparedMessageSender interface {
	PrepareMessageRequest(ctx context.Context, roomID, message string) (body, route string, err error)
	SendPreparedMessage(ctx context.Context, roomID, body, route, clientRequestID string) (irisRequestID string, err error)
}
