package config

import "time"

// IrisConfig: Iris 웹훅 서버 연결 및 메시지 전송 관련 설정
type IrisConfig struct {
	BaseURL                   string
	WebhookToken              string // env: IRIS_WEBHOOK_TOKEN
	BotToken                  string // env: IRIS_BOT_TOKEN
	HTTPTimeout               time.Duration
	HTTPDialTimeout           time.Duration
	HTTPResponseHeaderTimeout time.Duration
}
