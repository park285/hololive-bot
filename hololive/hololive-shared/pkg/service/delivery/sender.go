package delivery

import "context"

// MessageVariants는 같은 메시지의 방 유형별 본문입니다. 카카오 일반채팅은 RichText 서식을 표시하지 않으므로
// 일반 텍스트 경로는 Plain을, 확인된 오픈채팅의 RichText 경로는 RichText를 보냅니다.
type MessageVariants struct {
	Plain    string
	RichText string
}

// PreparedMessageSender는 저장된 최종 본문·경로를 구성 변경과 무관하게 그대로 전송합니다.
// PrepareMessageRequest는 방 유형을 한 번 판정해 그 경로의 변형 하나를 최종 본문으로 고릅니다.
// SendPreparedMessage는 Iris가 durable admission을 증명한 202 응답의 requestId를 돌려주며, 호출자는 이 값으로
// handoff 상태를 확인합니다.
type PreparedMessageSender interface {
	PrepareMessageRequest(ctx context.Context, roomID string, message MessageVariants) (body, route string, err error)
	SendPreparedMessage(ctx context.Context, roomID, body, route, clientRequestID string) (irisRequestID string, err error)
}
