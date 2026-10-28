package settings

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

// ValidateServerRuntime은 HTTP listener를 여는 runtime의 공통 기동 조건을 검사한다.
// Listener 포트, H3 transport 입력, API 인증 키 순서로 첫 위반을 돌려준다.
func ValidateServerRuntime(environment string, server *ServerConfig) error {
	if server == nil {
		return errors.New("server config is required")
	}

	if server.Port == 0 {
		return errors.New("SERVER_PORT is required")
	}

	if err := ValidateServerTransports(server); err != nil {
		return fmt.Errorf("validate server transports: %w", err)
	}

	if err := runtimepolicy.ValidateAPISecretKey(environment, server.APIKey); err != nil {
		return fmt.Errorf("validate API secret key: %w", err)
	}

	return nil
}

// ValidateKakaoRooms는 room ACL seed가 비어 있는 기동을 거절한다.
func ValidateKakaoRooms(rooms []string) error {
	if len(rooms) == 0 {
		return errors.New("KAKAO_ROOMS is required")
	}

	return nil
}

// ValidateIrisEgressInputs는 Iris로 직접 발송하는 runtime이 webhook·bot 토큰과 base URL 원천을 모두 받았는지 검사한다.
func ValidateIrisEgressInputs(iris *IrisConfig) error {
	if iris == nil {
		return errors.New("iris config is required")
	}

	if strings.TrimSpace(iris.WebhookToken) == "" {
		return errors.New("IRIS_WEBHOOK_TOKEN is required")
	}

	if strings.TrimSpace(iris.BotToken) == "" {
		return errors.New("IRIS_BOT_TOKEN is required")
	}

	if strings.TrimSpace(iris.BaseURL) == "" && strings.TrimSpace(iris.BaseURLFile) == "" {
		return errors.New("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	// URL 파일의 host는 bot token을 실은 요청의 목적지라 허용 집합 없이 받지 않는다(fail-closed).
	if strings.TrimSpace(iris.BaseURLFile) != "" && strings.TrimSpace(iris.H3ServerName) == "" && len(iris.BaseURLAllowedHosts) == 0 {
		return errors.New("IRIS_BASE_URL_FILE requires IRIS_H3_SERVER_NAME or IRIS_BASE_URL_ALLOWED_HOSTS")
	}

	return nil
}

// ValidateHolodexConfig는 Holodex 요청 시간·수·간격 설정을 검사한다. API 키 필수 여부는 runtime이
// runtimepolicy.ValidateHolodexAPIKey로 따로 결정한다.
func ValidateHolodexConfig(config *HolodexConfig) error {
	if config == nil {
		return errors.New("holodex config is required")
	}

	if err := runtimepolicy.ValidateHolodexTimeout(config.Timeout); err != nil {
		return fmt.Errorf("validate holodex timeout: %w", err)
	}

	if err := ValidateHolodexRequestConfig(config); err != nil {
		return fmt.Errorf("validate holodex request config: %w", err)
	}

	return nil
}

// ValidateOfficialScheduleRuntimeConfig는 공식 일정 클라이언트 설정과 응답 본문 상한을 검사한다.
func ValidateOfficialScheduleRuntimeConfig(config OfficialScheduleRuntimeConfig) error {
	if err := runtimepolicy.ValidateOfficialScheduleBaseURL(config.OfficialSchedule.BaseURL); err != nil {
		return fmt.Errorf("validate official schedule base URL: %w", err)
	}

	if err := runtimepolicy.ValidateOfficialScheduleTimeout(config.OfficialSchedule.Timeout); err != nil {
		return fmt.Errorf("validate official schedule timeout: %w", err)
	}

	if config.OfficialSchedule.PageCacheTTL < 0 {
		return errors.New("OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS must be >= 0")
	}

	if config.MaxResponseBodyBytes <= 0 {
		return errors.New("MAX_RESPONSE_BODY_BYTES must be positive")
	}

	return nil
}
