package irisx

import "time"

const (
	// PathWebhook: Iris -> Bot 인바운드 webhook 경로입니다.
	PathWebhook = "/webhook/iris"
	// PathReply: Bot -> Iris 아웃바운드 reply 경로입니다.
	PathReply = "/reply"
)

const (
	// HeaderIrisToken: Iris -> Bot 인증 헤더입니다.
	HeaderIrisToken = "X-Iris-Token"
	// HeaderIrisMessageID: Iris -> Bot 멱등성 키 헤더입니다.
	HeaderIrisMessageID = "X-Iris-Message-Id"
	// HeaderBotToken: Bot -> Iris 인증 헤더입니다.
	HeaderBotToken = "X-Bot-Token" // #nosec G101 -- HTTP header key name, not credential.
)

var (
	// DefaultWebhookDedupTTL: webhook dedup 기본 TTL입니다.
	DefaultWebhookDedupTTL = 60 * time.Second
)
