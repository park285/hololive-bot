package settings

import sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

const (
	irisWebhookTokenEnv = "IRIS_WEBHOOK_TOKEN" //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
	irisBotTokenEnv     = "IRIS_BOT_TOKEN"     //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
)

// LoadIrisTokens는 Iris webhook·bot 토큰을 앞뒤 공백 없이 읽는다. NonEgress runtime도 값 존재 여부 판단에 같은 규칙을 쓴다.
func LoadIrisTokens() (webhookToken, botToken string) {
	return sharedenv.String(irisWebhookTokenEnv, ""), sharedenv.String(irisBotTokenEnv, "")
}
