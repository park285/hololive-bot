// Package runtimepolicy는 runtime이 적재한 명시적 설정값을 검증한다.
// 환경변수 읽기와 기본값 해석은 envload와 각 소유 runtime 로더가 담당한다.
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
