package settings

import (
	"errors"
	"fmt"
)

// MaxHolodexRetryAttempts는 최초 요청을 제외한 재시도 상한입니다(총 10회).
const MaxHolodexRetryAttempts = 9

// ValidateHolodexRequestConfig는 요청 수·간격의 잘못된 설정을 시작 전에 거부합니다.
func ValidateHolodexRequestConfig(config *HolodexConfig) error {
	if config == nil {
		return errors.New("holodex config is required")
	}

	if config.Concurrency.MaxConcurrentRequests <= 0 {
		return errors.New("Holodex.Concurrency.MaxConcurrentRequests must be positive")
	}

	if config.MaxRetryAttempts < 0 || config.MaxRetryAttempts > MaxHolodexRetryAttempts {
		return fmt.Errorf("Holodex.MaxRetryAttempts must be between 0 and %d", MaxHolodexRetryAttempts)
	}

	if config.Concurrency.RequestDelay < 0 {
		return errors.New("Holodex.Concurrency.RequestDelay must be non-negative")
	}

	return nil
}
