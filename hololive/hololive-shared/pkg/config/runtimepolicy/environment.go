package runtimepolicy

import "strings"

const (
	SchemeHTTP                = "http"
	SchemeHTTPS               = "https"
	EnvironmentProduction     = "production"
	PostgresSSLModeVerifyFull = "verify-full"
)

func IsProduction(environment string) bool {
	return strings.EqualFold(strings.TrimSpace(environment), EnvironmentProduction)
}
