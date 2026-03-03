package iris

import (
	"fmt"
	"strings"
	"time"
)

const (
	// PathWebhook: Iris -> Bot 인바운드 webhook 경로
	PathWebhook = "/webhook/iris"
	// PathReply: Bot -> Iris 아웃바운드 reply 경로
	PathReply = "/reply"
)

const (
	// HeaderIrisToken: Iris -> Bot 인증 헤더
	HeaderIrisToken = "X-Iris-Token"
	// HeaderIrisMessageID: Iris -> Bot 멱등성 키 헤더
	HeaderIrisMessageID = "X-Iris-Message-Id"
	// HeaderBotToken: Bot -> Iris 인증 헤더
	HeaderBotToken = "X-Bot-Token" // #nosec G101 -- header key literal, not secret.
)

var (
	// DefaultWebhookDedupTTL: webhook dedup 기본 TTL
	DefaultWebhookDedupTTL = 60 * time.Second
)

// ReplyRequest: Bot -> Iris /reply 요청 스키마
type ReplyRequest struct {
	Type     string  `json:"type"`
	Room     string  `json:"room"`
	Data     string  `json:"data"`
	ThreadID *string `json:"threadId,omitempty"`
}

// WebhookRequest: Iris -> Bot /webhook/iris 요청 스키마
type WebhookRequest struct {
	Text     string `json:"text"`
	Room     string `json:"room"`
	Sender   string `json:"sender"`
	UserID   string `json:"userId"`
	ThreadID string `json:"threadId"`
}

// DedupKey: webhook 메시지 ID 기반 dedup 키 생성
func DedupKey(messageID string) string {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return ""
	}
	return fmt.Sprintf("iris:msg:{%s}", id)
}

// ResolveToken: 개별 토큰이 비어 있으면 sharedToken으로 대체
func ResolveToken(token, sharedToken string) string {
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken != "" {
		return trimmedToken
	}
	return strings.TrimSpace(sharedToken)
}

// ResolveTokens: webhook/bot 토큰을 sharedToken 기준으로 보정
func ResolveTokens(webhookToken, botToken, sharedToken string) (resolvedWebhookToken, resolvedBotToken string) {
	return ResolveToken(webhookToken, sharedToken), ResolveToken(botToken, sharedToken)
}
