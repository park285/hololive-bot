package settings

import sharedenv "github.com/park285/shared-go/core/envutil"

const (
	irisWebhookTokenEnv = "IRIS_WEBHOOK_TOKEN" // #nosec G101 -- 값은 자격증명이 아니라 환경변수 이름이다.
	irisBotTokenEnv     = "IRIS_BOT_TOKEN"     // #nosec G101 -- 값은 자격증명이 아니라 환경변수 이름이다.
)

func LoadIrisTokens() (webhookToken, botToken string) {
	return sharedenv.String(irisWebhookTokenEnv, ""), sharedenv.String(irisBotTokenEnv, "")
}
