package settings

import (
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

const (
	irisWebhookTokenEnv = "IRIS_WEBHOOK_TOKEN" //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
	irisBotTokenEnv     = "IRIS_BOT_TOKEN"     //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
)

func loadRuntimeTokensAndCORS() (webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) {
	webhookToken = strings.TrimSpace(sharedenv.String(irisWebhookTokenEnv, ""))
	botToken = strings.TrimSpace(sharedenv.String(irisBotTokenEnv, ""))

	runtimeEnv := loadAppEnvironment()
	isProduction := strings.EqualFold(runtimeEnv, environmentProduction)

	corsAllowedOrigins, corsMissingInProduction = parseCORSAllowedOrigins(
		sharedenv.String("CORS_ALLOWED_ORIGINS", ""),
		isProduction,
	)

	return webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction
}
