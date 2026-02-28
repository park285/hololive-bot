package iris

import sharedirisx "github.com/park285/llm-kakao-bots/shared-go/pkg/irisx"

// Config: Iris /config 응답 스키마
type Config struct {
	BotName         string `json:"bot_name"`
	BotHTTPPort     int    `json:"bot_http_port"`
	DBPollingRate   int    `json:"db_polling_rate"`
	MessageSendRate int    `json:"message_send_rate"`
	BotID           int64  `json:"bot_id"`
}

// DecryptRequest: 카카오톡 메시지 복호화 요청 구조체
type DecryptRequest struct {
	B64Ciphertext string `json:"b64_ciphertext"`
	UserID        *int64 `json:"user_id,omitempty"`
	Enc           int    `json:"enc"`
}

// DecryptResponse: 카카오톡 메시지 복호화 응답 구조체
type DecryptResponse struct {
	PlainText string `json:"plain_text"`
}

// ReplyRequest: 텍스트 답장 전송 요청 구조체
type ReplyRequest = sharedirisx.ReplyRequest

// WebhookRequest: Iris -> Bot 인바운드 웹훅 요청 스키마
type WebhookRequest = sharedirisx.WebhookRequest

// Message: 수신된 카카오톡 메시지 구조체
type Message struct {
	Msg    string       `json:"msg"`
	Room   string       `json:"room"`
	Sender *string      `json:"sender,omitempty"`
	JSON   *MessageJSON `json:"json,omitempty"`
}

// MessageJSON: 메시지 세부 정보를 담는 JSON 구조체
type MessageJSON struct {
	UserID    string `json:"user_id,omitempty"`
	Message   string `json:"message,omitempty"`
	ChatID    string `json:"chat_id,omitempty"`
	Type      string `json:"type,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}
