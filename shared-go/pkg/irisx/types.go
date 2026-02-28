package irisx

// ReplyRequest: Bot -> Iris /reply 요청 스키마입니다.
type ReplyRequest struct {
	Type     string  `json:"type"`
	Room     string  `json:"room"`
	Data     string  `json:"data"`
	ThreadID *string `json:"threadId,omitempty"`
}

// WebhookRequest: Iris -> Bot /webhook/iris 요청 스키마입니다.
type WebhookRequest struct {
	Text     string `json:"text"`
	Room     string `json:"room"`
	Sender   string `json:"sender"`
	UserID   string `json:"userId"`
	ThreadID string `json:"threadId"`
}
