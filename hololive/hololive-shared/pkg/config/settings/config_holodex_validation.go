package settings

import (
	"errors"
	"fmt"
)

const MaxHolodexRetryAttempts = 9

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
