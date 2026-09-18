package settings

import (
	"os"
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
)

// IrisRuntimeValidationConfig는 갱신 가능한 Iris URL 파일의 검증에 필요한 설정이다.
// 인증 토큰이나 인증서 본문은 읽지 않는다.
type IrisRuntimeValidationConfig struct {
	Transport        string
	ServerName       string
	AllowedHosts     []string
	ValidateFileStat bool
}

// LoadIrisRuntimeValidationConfig는 호출 시점의 URL 검증 설정을 읽는다.
// 기존 파일 재조회 시점의 환경 해석과 production의 stat 검사 조건을 유지한다.
func LoadIrisRuntimeValidationConfig() IrisRuntimeValidationConfig {
	return IrisRuntimeValidationConfig{
		Transport:    os.Getenv("IRIS_TRANSPORT"),
		ServerName:   strings.TrimSpace(os.Getenv("IRIS_H3_SERVER_NAME")),
		AllowedHosts: strings.Split(os.Getenv("IRIS_BASE_URL_ALLOWED_HOSTS"), ","),
		ValidateFileStat: strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") &&
			!strings.EqualFold(strings.TrimSpace(os.Getenv("IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS")), "true"),
	}
}

// HostAllowlistConfigured는 빈 항목만 있는 allowlist도 명시 설정으로 취급하는 기존 계약을 유지한다.
func (c IrisRuntimeValidationConfig) HostAllowlistConfigured() bool {
	return c.ServerName != "" || strings.TrimSpace(strings.Join(c.AllowedHosts, ",")) != ""
}

// LoadInternalH3ClientOptions는 내부 서비스 전용 값, 공통 H3 값 순으로 TLS 경로와 이름을 고른다.
// 파일 검증과 client 생성은 호출자가 소유한다.
func LoadInternalH3ClientOptions() sharedh3.ClientOptions {
	return sharedh3.ClientOptions{
		CACertFile: sharedenv.StringAny("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", "HOLOLIVE_H3_CERT_FILE"),
		ServerName: sharedenv.StringAny("HOLOLIVE_INTERNAL_H3_SERVER_NAME", "HOLOLIVE_H3_SERVER_NAME"),
	}
}

// RateLimiterInstanceID는 명시된 limiter 식별자를 읽는다. 빈 값의 host/random 선택은 limiter가 소유한다.
func RateLimiterInstanceID() string {
	return strings.TrimSpace(os.Getenv("INSTANCE_ID"))
}
