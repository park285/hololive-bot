package settings

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

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

func ValidateIrisEgressInputs(iris *IrisConfig) error {
	if iris == nil {
		return errors.New("iris config is required")
	}

	if strings.TrimSpace(iris.WebhookToken) == "" {
		return errors.New("IRIS_WEBHOOK_TOKEN is required")
	}

	return ValidateIrisClientInputs(iris)
}

func ValidateIrisClientInputs(iris *IrisConfig) error {
	if iris == nil {
		return errors.New("iris config is required")
	}

	if strings.TrimSpace(iris.BotToken) == "" {
		return errors.New("IRIS_BOT_TOKEN is required")
	}

	if strings.TrimSpace(iris.BaseURL) == "" && strings.TrimSpace(iris.BaseURLFile) == "" {
		return errors.New("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	if strings.TrimSpace(iris.BaseURLFile) != "" && strings.TrimSpace(iris.H3ServerName) == "" && len(iris.BaseURLAllowedHosts) == 0 {
		return errors.New("IRIS_BASE_URL_FILE requires IRIS_H3_SERVER_NAME or IRIS_BASE_URL_ALLOWED_HOSTS")
	}

	return nil
}

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
