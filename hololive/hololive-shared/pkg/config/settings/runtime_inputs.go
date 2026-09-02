package settings

import (
	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

const (
	irisWebhookTokenEnv = "IRIS_WEBHOOK_TOKEN" //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
	irisBotTokenEnv     = "IRIS_BOT_TOKEN"     //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
)

// LoadRuntimeTokensAndCORS: Iris egress 토큰과 CORS 허용 origin을 함께 읽는다.
func LoadRuntimeTokensAndCORS() (webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) {
	webhookToken = load.TrimmedEnv(irisWebhookTokenEnv)
	botToken = load.TrimmedEnv(irisBotTokenEnv)

	isProduction := load.IsProduction(load.AppEnvironment())

	corsAllowedOrigins, corsMissingInProduction = parseCORSAllowedOrigins(
		sharedenv.String("CORS_ALLOWED_ORIGINS", ""),
		isProduction,
	)

	return webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction
}
