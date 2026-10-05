package delivery

import "context"

// PreparedMessageSender는 저장된 최종 본문·경로를 구성 변경과 무관하게 그대로 전송합니다.
type PreparedMessageSender interface {
	PrepareMessageRequest(context.Context, string, string) (string, string, error)
	SendPreparedMessage(context.Context, string, string, string, string) error
}
