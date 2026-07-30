package settings

import (
	"strings"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

func loadRuntimeTokensAndCORS() (webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) {
	webhookToken = strings.TrimSpace(sharedenv.String("IRIS_WEBHOOK_TOKEN", ""))
	botToken = strings.TrimSpace(sharedenv.String("IRIS_BOT_TOKEN", ""))

	runtimeEnv := loadAppEnvironment()
	isProduction := strings.EqualFold(runtimeEnv, "production")
	corsAllowedOrigins, corsMissingInProduction = parseCORSAllowedOrigins(
		sharedenv.String("CORS_ALLOWED_ORIGINS", ""),
		isProduction,
	)

	return webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction
}
