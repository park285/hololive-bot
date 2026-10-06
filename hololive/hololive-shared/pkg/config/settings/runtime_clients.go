package settings

import (
	"os"
	"strings"

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
// APP_ENV=production이면 IRIS_BASE_URL_FILE의 경로·소유·권한 stat 검사를 항상 한다. 우회 플래그
// IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS(live-compat이 주입하던 값)는 T18(2026-09-26)에서 중앙 runtime-config/iris_base_url이 root 소유 0644로
// 검사를 통과함을 확인해 지웠다(stack-audit T11 holo-iris-base-url-skip-stat-compat).
func LoadIrisRuntimeValidationConfig() IrisRuntimeValidationConfig {
	return IrisRuntimeValidationConfig{
		Transport:        os.Getenv("IRIS_TRANSPORT"),
		ServerName:       strings.TrimSpace(os.Getenv("IRIS_H3_SERVER_NAME")),
		AllowedHosts:     strings.Split(os.Getenv("IRIS_BASE_URL_ALLOWED_HOSTS"), ","),
		ValidateFileStat: strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production"),
	}
}

// HostAllowlistConfigured는 빈 항목만 있는 allowlist도 명시 설정으로 취급하는 기존 계약을 유지한다.
func (c IrisRuntimeValidationConfig) HostAllowlistConfigured() bool {
	return c.ServerName != "" || strings.TrimSpace(strings.Join(c.AllowedHosts, ",")) != ""
}

// LoadInternalH3ClientOptions는 내부 서비스 H3 client의 TLS 경로와 이름을 전용 키 두 개에서만 읽는다.
// 공통 서버 키(HOLOLIVE_H3_CERT_FILE, HOLOLIVE_H3_SERVER_NAME)로 내려가던 폴백은 지웠다(stack audit 2026-09-26).
// 빈 값의 HTTPS 거절과 파일 검증은 client 생성자가 소유하여 HTTP-only runtime 기동을 보존한다.
func LoadInternalH3ClientOptions() sharedh3.ClientOptions {
	return sharedh3.ClientOptions{
		CACertFile: strings.TrimSpace(os.Getenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE")),
		ServerName: strings.TrimSpace(os.Getenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME")),
	}
}
