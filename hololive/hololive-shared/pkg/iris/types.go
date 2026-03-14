// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	UserID    string  `json:"user_id,omitempty"`
	Message   string  `json:"message,omitempty"`
	ChatID    string  `json:"chat_id,omitempty"`
	Type      string  `json:"type,omitempty"`
	CreatedAt string  `json:"created_at,omitempty"`
	ThreadID  *string `json:"thread_id,omitempty"`
}
